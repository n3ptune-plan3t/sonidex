package backend

import (
	"context"
	"io"
	"net"
	"sync"
	"time"
)

func StartTCPReceiver(ctx context.Context, port string, ab *AudioBuffer, periodBytes int) error {
	l, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return err
	}
	defer l.Close()

	var connMu sync.Mutex
	activeConns := make(map[net.Conn]struct{})

	go func() {
		<-ctx.Done()
		_ = l.Close()
		connMu.Lock()
		for c := range activeConns {
			_ = c.Close()
		}
		connMu.Unlock()
	}()

	for {
		c, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}

		connMu.Lock()
		activeConns[c] = struct{}{}
		connMu.Unlock()

		go func(conn net.Conn) {
			defer func() {
				_ = conn.Close()
				connMu.Lock()
				delete(activeConns, conn)
				connMu.Unlock()
			}()

			buf := make([]byte, periodBytes)

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if _, err := io.ReadFull(conn, buf); err != nil {
					return
				}
				ab.Push(buf)
			}
		}(c)
	}
}

func StartLatencyEcho(ctx context.Context, port string) error {
	l, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return err
	}
	defer l.Close()

	var connMu sync.Mutex
	activeConns := make(map[net.Conn]struct{})

	go func() {
		<-ctx.Done()
		_ = l.Close()
		connMu.Lock()
		for c := range activeConns {
			_ = c.Close()
		}
		connMu.Unlock()
	}()

	for {
		c, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}

		connMu.Lock()
		activeConns[c] = struct{}{}
		connMu.Unlock()

		go func(conn net.Conn) {
			defer func() {
				_ = conn.Close()
				connMu.Lock()
				delete(activeConns, conn)
				connMu.Unlock()
			}()

			buf := make([]byte, 8)
			for {
				if _, err := io.ReadFull(conn, buf); err != nil {
					return
				}
				if _, err := conn.Write(buf); err != nil {
					return
				}
			}
		}(c)
	}
}

func StartTCPSenderFromChan(ctx context.Context, addr string, ch <-chan []byte) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case buf := <-ch:
			if _, err := conn.Write(buf); err != nil {
				return err
			}
		}
	}
}

func StartTCPSender(ctx context.Context, addr string, reader io.Reader) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		n, err := reader.Read(buf)
		if err != nil {
			return err
		}
		if _, err := conn.Write(buf[:n]); err != nil {
			return err
		}
	}
}

func MeasureLatency(addr string) (time.Duration, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	buf := make([]byte, 8)
	start := time.Now()
	if _, err := conn.Write(buf); err != nil {
		return 0, err
	}
	if _, err := io.ReadFull(conn, buf); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}
