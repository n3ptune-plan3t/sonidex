//go:build linux && !android

// Package linuxui is the Linux front-end: the mini software toolkit on a
// native Wayland or X11 window, chosen at run time. No GTK, no Qt, no GL.
package linuxui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"sonidex/backend"
	"sonidex/internal/core"
	"sonidex/internal/mini"
	"sonidex/internal/mini/wayland"
	"sonidex/internal/mini/x11"
)

// newPlatform prefers Wayland when WAYLAND_DISPLAY is set and falls back to
// X11. SONIDEX_BACKEND=wayland|x11 forces one.
func newPlatform() (mini.Platform, error) {
	want := strings.ToLower(os.Getenv("SONIDEX_BACKEND"))
	var errs []error
	if want != "x11" && os.Getenv("WAYLAND_DISPLAY") != "" {
		p, err := wayland.New()
		if err == nil {
			return p, nil
		}
		errs = append(errs, err)
	}
	if want != "wayland" && os.Getenv("DISPLAY") != "" {
		p, err := x11.New()
		if err == nil {
			return p, nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil, errors.New("no display server: neither WAYLAND_DISPLAY nor DISPLAY is set (try sonidex-tui)")
	}
	return nil, errors.Join(errs...)
}

type shower interface{ Show(bool) }

func showAll(ws []shower, v bool) {
	for _, w := range ws {
		w.Show(v)
	}
}

const logicalW = 420.0

// Run opens the streamer window and blocks until it is closed.
func Run() error {
	plat, err := newPlatform()
	if err != nil {
		return err
	}
	prefs := core.LoadPrefs()
	app := mini.NewApp(plat.Scale())
	f := app.Form

	title := &mini.Label{Text: "Sonidex Streamer", Bold: true, Center: true, Size: 18}
	modeSel := &mini.Select{Options: core.Modes}
	wirelessEntry := &mini.Entry{Placeholder: "192.168.1.42:5555"}
	connectBtn := &mini.Button{Text: "Connect"}
	directEntry := &mini.Entry{Placeholder: "192.168.1.42"}
	devSel := &mini.Select{Index: -1}
	refreshBtn := &mini.Button{Text: "Refresh Devices"}
	portEntry := &mini.Entry{MaxLen: 5}
	portEntry.SetText(core.DefaultPort)
	profSel := &mini.Select{Options: backend.ProfileNames}
	profSel.SetSelected(prefs.Profile)
	silence := &mini.Check{Text: "Pause sending while silent (saves power)", Checked: prefs.SilenceSuppress}
	status := &mini.Label{Text: "Ready", Wrap: true, MinLines: 2, Dim: true}
	start := &mini.Button{Text: "Start Streaming", Primary: true}

	wireless := []shower{}
	direct := []shower{}
	devices := []shower{}
	add := func(w mini.Widget, gap float64, group *[]shower) {
		f.Add(w, gap)
		if group != nil {
			*group = append(*group, w.(shower))
		}
	}
	dim := func(t string) *mini.Label { return &mini.Label{Text: t, Dim: true} }

	f.Add(title, 0)
	f.Add(dim("Connection Mode:"), 14)
	f.Add(modeSel, 4)
	add(dim("Device ip:port (Wireless debugging):"), 12, &wireless)
	add(wirelessEntry, 4, &wireless)
	add(connectBtn, 6, &wireless)
	add(dim("Phone IP address (same WiFi network):"), 12, &direct)
	add(directEntry, 4, &direct)
	add(dim("Android Target Device:"), 12, &devices)
	add(devSel, 4, &devices)
	add(refreshBtn, 6, &devices)
	f.Add(dim("Port:"), 12)
	f.Add(portEntry, 4)
	f.Add(dim("Latency / power profile:"), 12)
	f.Add(profSel, 4)
	f.Add(silence, 12)
	f.Add(status, 12)
	f.Add(start, 8)

	applyMode := func(m string) {
		showAll(wireless, m == core.ModeWirelessADB)
		showAll(direct, m == core.ModeWiFiDirect)
		showAll(devices, m != core.ModeWiFiDirect)
	}
	modeSel.OnChange = func(i int) { applyMode(core.Modes[i]) }

	// Size the window for the tallest mode so switching never clips.
	maxH := 0
	for _, m := range core.Modes {
		applyMode(m)
		maxH = max(maxH, f.Measure(logicalW))
	}
	applyMode(core.ModeUSBADB)
	modeSel.Index = 0

	sess := &core.Session{}
	sess.OnStatus = func(s string) { app.Do(func() { status.Text = s }) }
	sess.OnRunning = func(r bool) {
		app.Do(func() {
			if r {
				start.Text = "Stop Streaming"
			} else {
				start.Text = "Start Streaming"
			}
		})
	}

	refreshDevices := func() {
		go func() {
			devs, msg := core.RefreshDevices()
			app.Do(func() {
				sel := -1
				if len(devs) > 0 {
					sel = 0
				}
				devSel.SetOptions(devs, sel)
				status.Text = msg
			})
		}()
	}
	refreshBtn.OnClick = refreshDevices
	connectBtn.OnClick = func() {
		addr := wirelessEntry.Text
		go func() {
			msg, ok := core.ConnectWireless(addr)
			app.Do(func() { status.Text = msg })
			if ok {
				refreshDevices()
			}
		}()
	}
	profSel.OnChange = func(int) {
		prefs.Profile = profSel.Selected()
		prefs.Save()
	}
	silence.OnChange = func(on bool) {
		prefs.SilenceSuppress = on
		prefs.Save()
	}
	start.OnClick = func() {
		p := core.Params{
			Mode:     modeSel.Selected(),
			Serial:   devSel.Selected(),
			Port:     portEntry.Text,
			DirectIP: directEntry.Text,
			Profile:  profSel.Selected(),
			Silence:  silence.Checked,
		}
		go sess.Toggle(p) // may run adb; keep the UI loop free
	}
	refreshDevices()

	s := plat.Scale()
	err = app.Run(plat, mini.WindowOptions{
		Title: "Sonidex Streamer",
		AppID: "sonidex",
		W:     int(logicalW*s + 0.5),
		H:     maxH,
		MinW:  int(340*s + 0.5),
		MinH:  maxH,
	})
	sess.Stop() // drop the adb forward on the way out
	if err != nil {
		return fmt.Errorf("%w", err)
	}
	return nil
}
