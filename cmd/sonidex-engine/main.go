// sonidex-engine is the headless streaming engine behind the OpenTUI
// front end (tui/). It speaks newline-delimited JSON on stdio:
//
//	stdin  (commands): {"op":"list_devices"}
//	                   {"op":"connect_wireless","addr":"192.168.1.42:5555"}
//	                   {"op":"start","mode":"usb|wireless|direct","serial":"...",
//	                                  "ip":"...","port":"8080","profile":0,"silence":true}
//	                   {"op":"stop"}   {"op":"quit"}
//	stdout (events):   {"ev":"devices","devices":[...],"error":"..."}
//	                   {"ev":"wireless","ok":true,"error":"..."}
//	                   {"ev":"status","text":"...","err":false}
//	                   {"ev":"started","label":"...","port":"8080"}
//	                   {"ev":"stopped","error":"..."}
//
// stdout carries protocol messages only; nothing else may write to it.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"sonidex/backend"
)

type command struct {
	Op      string `json:"op"`
	Mode    string `json:"mode"`
	Serial  string `json:"serial"`
	IP      string `json:"ip"`
	Addr    string `json:"addr"`
	Port    string `json:"port"`
	Profile int    `json:"profile"`
	Silence bool   `json:"silence"`
}

type event map[string]any

type engine struct {
	out *json.Encoder
	wmu sync.Mutex // serialises writes to stdout

	mu     sync.Mutex // guards the session fields below
	cancel context.CancelFunc
	serial string // adb serial to clean up on stop ("" for direct mode)
	port   string
}

func (e *engine) emit(ev event) {
	e.wmu.Lock()
	defer e.wmu.Unlock()
	_ = e.out.Encode(ev)
}

func (e *engine) status(text string, isErr bool) {
	e.emit(event{"ev": "status", "text": text, "err": isErr})
}

func validPort(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "8080", true
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return "", false
	}
	return p, true
}

func (e *engine) listDevices() {
	devices, err := backend.ListADBDevices()
	if devices == nil {
		devices = []string{}
	}
	ev := event{"ev": "devices", "devices": devices}
	if err != nil {
		ev["error"] = err.Error()
	}
	e.emit(ev)
}

func (e *engine) connectWireless(addr string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		e.emit(event{"ev": "wireless", "ok": false, "error": "enter the device's ip:port first"})
		return
	}
	if err := backend.ConnectWirelessADB(addr); err != nil {
		e.emit(event{"ev": "wireless", "ok": false, "error": err.Error()})
		return
	}
	e.emit(event{"ev": "wireless", "ok": true})
}

// runLoop mirrors the reconnect/backoff behaviour of the old Bubble Tea TUI.
func (e *engine) runLoop(ctx context.Context, addr string, opts backend.StreamOptions) error {
	const maxRetries = 5
	backoff := 500 * time.Millisecond
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil
		}
		err := backend.StartDesktopStreamOpts(ctx, addr, opts)
		if err == nil || ctx.Err() != nil {
			return nil
		}
		e.status(fmt.Sprintf("disconnected, reconnecting (%d/%d): %v", attempt, maxRetries, err), true)
		select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 4*time.Second {
			backoff = 4 * time.Second
		}
	}
	return fmt.Errorf("connection failed after %d attempts", maxRetries)
}

func (e *engine) start(c command) {
	e.mu.Lock()
	if e.cancel != nil {
		e.mu.Unlock()
		e.status("already streaming", true)
		return
	}
	e.mu.Unlock()

	port, ok := validPort(c.Port)
	if !ok {
		e.emit(event{"ev": "stopped", "error": "invalid port"})
		return
	}

	var addr, label, serial string
	switch c.Mode {
		case "direct":
			ip := strings.TrimSpace(c.IP)
			if ip == "" {
				e.emit(event{"ev": "stopped", "error": "enter the phone's IP address"})
				return
			}
			addr, label = ip+":"+port, ip
		default: // usb, wireless: both tunnel through adb reverse
			serial = strings.TrimSpace(c.Serial)
			if serial == "" {
				e.emit(event{"ev": "stopped", "error": "no ADB device selected — refresh devices"})
				return
			}
			if err := backend.SetupADBReverse(serial, port); err != nil {
				e.emit(event{"ev": "stopped", "error": "adb forward failed: " + err.Error()})
				return
			}
			addr, label = "127.0.0.1:"+port, serial
	}

	prof := backend.Profile(c.Profile)
	if c.Profile < 0 || c.Profile >= len(backend.ProfileNames) {
		prof = backend.ProfileLowLatency
	}
	opts := backend.StreamOptions{Profile: prof, SuppressSilence: c.Silence}

	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.cancel, e.serial, e.port = cancel, serial, port
	e.mu.Unlock()

	e.emit(event{"ev": "started", "label": label, "port": port})

	go func() {
		err := e.runLoop(ctx, addr, opts)
		e.finish(cancel)
		ev := event{"ev": "stopped"}
		if err != nil {
			ev["error"] = err.Error()
		}
		e.emit(ev)
	}()
}

// finish clears the session (if it is still the current one) and removes
// the adb reverse tunnel.
func (e *engine) finish(cancel context.CancelFunc) {
	e.mu.Lock()
	serial, port := e.serial, e.port
	if e.cancel != nil {
		e.cancel = nil
		e.serial, e.port = "", ""
	} else {
		serial = ""
	}
	e.mu.Unlock()
	cancel()
	if serial != "" {
		_ = backend.RemoveADBReverse(serial, port)
	}
}

func (e *engine) stop() {
	e.mu.Lock()
	cancel := e.cancel
	e.mu.Unlock()
	if cancel != nil {
		e.status("stopping...", false)
		cancel() // runLoop returns, goroutine in start() emits "stopped"
	}
}

func main() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
	e := &engine{out: json.NewEncoder(os.Stdout)}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var c command
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			e.status("bad command: "+err.Error(), true)
			continue
		}
		switch c.Op {
			case "list_devices":
				go e.listDevices()
			case "connect_wireless":
				go e.connectWireless(c.Addr)
			case "start":
				e.start(c)
			case "stop":
				e.stop()
			case "quit":
				goto shutdown
			default:
				e.status("unknown op: "+c.Op, true)
		}
	}
	shutdown:
	// stdin closed (frontend exited or crashed) or quit: tear down cleanly.
	e.mu.Lock()
	cancel, serial, port := e.cancel, e.serial, e.port
	e.cancel = nil
	e.mu.Unlock()
	if cancel != nil {
		cancel()
		if serial != "" {
			_ = backend.RemoveADBReverse(serial, port)
		}
		time.Sleep(150 * time.Millisecond) // let the capture device close
	}
}
