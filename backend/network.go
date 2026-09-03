package backend

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
)

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

func StartTCPSender(ctx context.Context, targetAddr string, mCtx *malgo.AllocatedContext, periodFrames uint32) error {
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
	deviceConfig.PeriodSizeInFrames = periodFrames
	
	writeErr := make(chan error, 1)
	onRecv := func(pOutput, pInput []byte, framecount uint32) {
		if len(pInput) == 0 {
			return
		}
		if _, err := conn.Write(pInput); err != nil {
			select {
			case writeErr <- err:
			default:
			}
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

	select {
	case <-ctx.Done():
		return nil
	case err := <-writeErr:
		return fmt.Errorf("connection lost: %w", err)
	}
}

func StartTCPReceiver(ctx context.Context, listenAddr string, mCtx *malgo.AllocatedContext, periodFrames uint32) error {
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	defer l.Close()

	periodBytes := BytesForPeriod(periodFrames)
	jitterBuf := NewAudioBuffer(periodBytes)

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = Format
	deviceConfig.Playback.Channels = Channels
	deviceConfig.SampleRate = SampleRate
	deviceConfig.PeriodSizeInFrames = periodFrames

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

	var connMu sync.Mutex
	var activeConn net.Conn

	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			_ = tcpConn.SetNoDelay(true)
		}

		connMu.Lock()
		if activeConn != nil {
			activeConn.Close()
		}
		activeConn = conn
		connMu.Unlock()

		go func(c net.Conn) {
			defer func() {
				connMu.Lock()
				if activeConn == c {
					activeConn = nil
				}
				connMu.Unlock()
				c.Close()
			}()
			buf := make([]byte, periodBytes)
			for {
				if _, err := io.ReadFull(c, buf); err != nil {
					return
				}
				packet := make([]byte, periodBytes)
				copy(packet, buf)
				jitterBuf.Push(packet)
			}
		}(conn)
	}
}

func StartLatencyEcho(ctx context.Context, listenAddr string) error {
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	defer l.Close()

	go func() {
		<-ctx.Done()
		l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go func(c net.Conn) {
			defer c.Close()
			_, _ = io.Copy(c, c)
		}(conn)
	}
}

func MeasureLatency(ctx context.Context, targetAddr string, samples int) (time.Duration, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
	}

	if samples < 1 {
		samples = 1
	}

	buf := make([]byte, 8)
	var total time.Duration
	for i := 0; i < samples; i++ {
		binary.BigEndian.PutUint64(buf, uint64(i))
		start := time.Now()
		if _, err := conn.Write(buf); err != nil {
			return 0, fmt.Errorf("ping %d: %w", i, err)
		}
		if _, err := io.ReadFull(conn, buf); err != nil {
			return 0, fmt.Errorf("ping %d: %w", i, err)
		}
		total += time.Since(start)
	}
	return total / time.Duration(samples), nil
}package backend

import (
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"

	"github.com/gen2brain/malgo"
)

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
