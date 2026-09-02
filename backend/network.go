package backend

import (
	"context"
	"io"
	"net"

	"github.com/gen2brain/malgo"
)

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

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Loopback)
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
