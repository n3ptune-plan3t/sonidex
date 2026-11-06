// Package core holds the GUI-independent streamer logic shared by every
// front-end (Win32, X11/Wayland, ...): connection modes, ADB helpers, the
// start/stop/reconnect session and the saved preferences. It has no UI
// dependency; front-ends get status text and run-state changes through
// callbacks, which may fire on any goroutine.
package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"sonidex/backend"
)

const (
	ModeUSBADB      = "USB / ADB"
	ModeWirelessADB = "Wireless ADB"
	ModeWiFiDirect  = "WiFi Direct (No ADB)"

	DefaultPort = "8080"
)

// Modes lists the connection modes in menu order.
var Modes = []string{ModeUSBADB, ModeWirelessADB, ModeWiFiDirect}

// RefreshDevices lists ADB devices and returns a status line for the UI.
func RefreshDevices() (devices []string, msg string) {
	devices, err := backend.ListADBDevices()
	if err != nil {
		return nil, "adb not found in PATH."
	}
	if len(devices) == 0 {
		return nil, "No ADB devices found."
	}
	return devices, "Select target device."
}

// ConnectWireless runs `adb connect` for a Wireless-debugging address.
func ConnectWireless(addr string) (msg string, ok bool) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "Enter the device's ip:port first.", false
	}
	if err := backend.ConnectWirelessADB(addr); err != nil {
		return "Wireless adb connect failed.", false
	}
	return "Connected. Select the device below.", true
}

// Params is everything the UI chooses before pressing Start.
type Params struct {
	Mode     string
	Serial   string // selected ADB device (ADB modes)
	Port     string
	DirectIP string // phone address (WiFi Direct)
	Profile  string
	Silence  bool // pause sending while silent
}

// Session owns one running stream at a time and its reconnect loop.
type Session struct {
	// Router is non-nil on Windows (per-app patchbay).
	Router *backend.Router
	// OnStatus receives user-facing status lines; OnRunning the run state.
	OnStatus  func(string)
	OnRunning func(bool)

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	serial  string // what Start forwarded, so Stop can undo exactly that
	port    string
	adb     bool
}

func (s *Session) status(m string) {
	if s.OnStatus != nil {
		s.OnStatus(m)
	}
}

func (s *Session) setRunning(v bool) {
	if s.OnRunning != nil {
		s.OnRunning(v)
	}
}

func (s *Session) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Toggle starts the stream, or stops it when one is running. It may block
// briefly on `adb`, so front-ends call it off their UI thread.
func (s *Session) Toggle(p Params) {
	if s.Running() {
		s.Stop()
		s.status("Stream stopped.")
		return
	}
	s.Start(p)
}

// Start validates p, prepares the transport and launches the stream loop.
func (s *Session) Start(p Params) {
	port := strings.TrimSpace(p.Port)
	if port == "" {
		port = DefaultPort
	}

	var addr string
	adb := false
	switch p.Mode {
	case ModeWiFiDirect:
		ip := strings.TrimSpace(p.DirectIP)
		if ip == "" {
			s.status("Enter the phone's IP address.")
			return
		}
		addr = ip + ":" + port
	default:
		if p.Serial != "" {
			if err := backend.SetupADBReverse(p.Serial, port); err != nil {
				s.status("ADB forward failed. Check USB debugging.")
				return
			}
			adb = true
		}
		addr = "127.0.0.1:" + port
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.running = cancel, true
	s.serial, s.port, s.adb = p.Serial, port, adb
	s.mu.Unlock()

	s.status("Streaming active...")
	s.setRunning(true)

	opts := backend.StreamOptions{
		Profile:         backend.ProfileFromName(p.Profile),
		SuppressSilence: p.Silence,
		Router:          s.Router,
	}
	go s.loop(ctx, func(c context.Context) error {
		return backend.StartDesktopStreamOpts(c, addr, opts)
	})
}

// Stop ends the stream and removes the ADB forward it created. Safe to call
// when nothing is running.
func (s *Session) Stop() {
	s.mu.Lock()
	cancel, serial, port, adb := s.cancel, s.serial, s.port, s.adb
	was := s.running
	s.cancel, s.running, s.adb = nil, false, false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if adb {
		_ = backend.RemoveADBReverse(serial, port)
	}
	if was {
		s.setRunning(false)
	}
}

// loop re-runs task with exponential backoff until it succeeds, the session
// is cancelled, or retries run out.
func (s *Session) loop(ctx context.Context, task func(context.Context) error) {
	const maxRetries = 5
	backoff := 500 * time.Millisecond
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			return
		}
		err := task(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		s.status(fmt.Sprintf("Disconnected. Reconnecting (%d/%d)...", attempt, maxRetries))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 4*time.Second {
			backoff = 4 * time.Second
		}
	}
	s.mu.Lock()
	if ctx.Err() != nil { // stopped while we were giving up
		s.mu.Unlock()
		return
	}
	s.running, s.cancel = false, nil
	s.mu.Unlock()
	s.status("Connection failed.")
	s.setRunning(false)
}
