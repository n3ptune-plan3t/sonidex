package backend

import (
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"

	"github.com/gen2brain/malgo"
)

// loopbackCaptureConfig returns a capture-side DeviceConfig configured to
// record whatever is currently playing on the system's default output.
//
// malgo.Loopback (ma_device_type_loopback) is only implemented on WASAPI,
// i.e. Windows - see miniaudio's own docs/examples. On Linux/BSD (PulseAudio,
// PipeWire's Pulse shim, ALSA) there is no native loopback device type; the
// standard workaround is to enumerate capture devices and open the "Monitor
// of ..." source that PulseAudio/PipeWire exposes for the active sink as an
// ordinary capture device. Without this, InitDevice either fails outright or
// silently captures from the mic instead of system audio on non-Windows
// hosts, which is very likely why this was failing during testing.
func loopbackCaptureConfig(mCtx *malgo.AllocatedContext) (malgo.DeviceConfig, error) {
	if runtime.GOOS == "windows" {
		return malgo.DefaultDeviceConfig(malgo.Loopback), nil
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)

	infos, err := mCtx.Devices(malgo.Capture)
	if err != nil {
		return cfg, fmt.Errorf("enumerating capture devices: %w", err)
	}

	for _, info := range infos {
		name := strings.ToLower(info.Name())
		if strings.Contains(name, "monitor of") || strings.Contains(name, ".monitor") {
			id := info.ID
			cfg.Capture.DeviceID = id.Pointer()
			return cfg, nil
		}
	}

	return cfg, fmt.Errorf("no loopback/monitor capture device found - on PulseAudio/PipeWire, make sure a sink monitor source is available (check with `pactl list sources short`)")
}

func StartTCPSender(ctx context.Context, targetAddr string, mCtx *malgo.AllocatedContext) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
	}

	deviceConfig, err := loopbackCaptureConfig(mCtx)
	if err != nil {
		return err
	}
	deviceConfig.Capture.Format = Format
	deviceConfig.Capture.Channels = Channels
	deviceConfig.SampleRate = SampleRate
	deviceConfig.PeriodSizeInFrames = 480

	onRecv := func(pOutput, pInput []byte, framecount uint32) {
		if len(pInput) > 0 {
			_, _ = conn.Write(pInput)
		}
	}

	callbacks := malgo.DeviceCallbacks{Data: onRecv}
	device, err := malgo.InitDevice(mCtx.Context, deviceConfig, callbacks)
	if err != nil {
		return err
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		return err
	}
	defer device.Stop()

	<-ctx.Done()
	return nil
}

func StartTCPReceiver(ctx context.Context, listenAddr string, mCtx *malgo.AllocatedContext) error {
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	defer l.Close()

	jitterBuf := &AudioBuffer{}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = Format
	deviceConfig.Playback.Channels = Channels
	deviceConfig.SampleRate = SampleRate
	deviceConfig.PeriodSizeInFrames = 480

	onPlayback := func(pOutput, pInput []byte, framecount uint32) {
		jitterBuf.Read(pOutput)
	}

	callbacks := malgo.DeviceCallbacks{Data: onPlayback}
	device, err := malgo.InitDevice(mCtx.Context, deviceConfig, callbacks)
	if err != nil {
		return err
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		return err
	}
	defer device.Stop()

	go func() {
		<-ctx.Done()
		l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			_ = tcpConn.SetNoDelay(true)
		}

		go func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, ChunkSize)
			for {
				_, err := io.ReadFull(c, buf)
				if err != nil {
					return
				}
				packet := make([]byte, ChunkSize)
				copy(packet, buf)
				jitterBuf.Push(packet)
			}
		}(conn)
	}
}
