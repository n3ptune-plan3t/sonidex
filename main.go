package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"sonidex/backend"
)

func softwareRenderingRequested() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SONIDEX_NO_GPU")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func applySoftwareRendering() {
	_ = os.Setenv("SONIDEX_NO_GPU", "1")
	_ = os.Setenv("LIBGL_ALWAYS_SOFTWARE", "1")
	_ = os.Setenv("GALLIUM_DRIVER", "llvmpipe")
}

type sessionController struct {
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	status  *widget.Label
	button  *widget.Button
}

func (c *sessionController) loop(sessCtx context.Context, task func(context.Context) error, reconnectFmt string, failedText string, startText string) {
	maxRetries := 5
	backoff := 500 * time.Millisecond
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if sessCtx.Err() != nil {
			return
		}
		err := task(sessCtx)
		if err == nil || sessCtx.Err() != nil {
			return
		}
		c.status.SetText(fmt.Sprintf(reconnectFmt, attempt, maxRetries))
		select {
		case <-sessCtx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 4*time.Second {
			backoff = 4 * time.Second
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = false
	c.cancel = nil
	c.status.SetText(failedText)
	c.button.SetText(startText)
}

func newGPUCheck(a fyne.App, status *widget.Label) *widget.Check {
	check := widget.NewCheck("Disable GPU (software rendering)", func(enabled bool) {
		a.Preferences().SetBool("no_gpu", enabled)
		if enabled {
			applySoftwareRendering()
			status.SetText("Software rendering on. Restart the app to apply fully.")
		} else {
			status.SetText("Hardware rendering on. Restart the app to apply fully.")
		}
	})
	check.Checked = a.Preferences().BoolWithFallback("no_gpu", softwareRenderingRequested())
	check.Refresh()
	return check
}

func showStreamer(a fyne.App) {
	myWindow := a.NewWindow("Sonidex Streamer")
	statusLabel := widget.NewLabel("Ready")
	portEntry := widget.NewEntry()
	portEntry.SetText("8080")
	deviceSelect := widget.NewSelect([]string{}, func(string) {})
	ctrl := &sessionController{status: statusLabel}
	refreshDevices := func() {
		devices, err := backend.ListADBDevices()
		if err != nil {
			deviceSelect.Options = []string{}
			deviceSelect.Selected = ""
			deviceSelect.Refresh()
			statusLabel.SetText("adb not found in PATH.")
			return
		}
		if len(devices) == 0 {
			deviceSelect.Options = []string{}
			deviceSelect.Selected = ""
			deviceSelect.Refresh()
			statusLabel.SetText("No ADB devices found.")
			return
		}
		deviceSelect.Options = devices
		deviceSelect.SetSelected(devices[0])
		statusLabel.SetText("Select target device.")
	}
	ctrl.button = widget.NewButton("Start Streaming", func() {
		ctrl.mu.Lock()
		if ctrl.running {
			cancel := ctrl.cancel
			ctrl.cancel = nil
			ctrl.running = false
			serial := deviceSelect.Selected
			port := strings.TrimSpace(portEntry.Text)
			if port == "" {
				port = "8080"
			}
			ctrl.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			if serial != "" {
				_ = backend.RemoveADBReverse(serial, port)
			}
			statusLabel.SetText("Stream stopped.")
			ctrl.button.SetText("Start Streaming")
			return
		}
		port := strings.TrimSpace(portEntry.Text)
		if port == "" {
			port = "8080"
		}
		serial := deviceSelect.Selected
		if serial != "" {
			if err := backend.SetupADBReverse(serial, port); err != nil {
				ctrl.mu.Unlock()
				statusLabel.SetText("ADB forward failed. Check USB debugging.")
				return
			}
		}
		sessCtx, sessCancel := context.WithCancel(context.Background())
		ctrl.cancel = sessCancel
		ctrl.running = true
		ctrl.mu.Unlock()
		statusLabel.SetText("Streaming active...")
		ctrl.button.SetText("Stop Streaming")
		addr := "127.0.0.1:" + port
		go ctrl.loop(sessCtx, func(ctx context.Context) error {
			return backend.StartDesktopStream(ctx, addr)
		}, "Disconnected. Reconnecting (%d/%d)...", "Connection failed.", "Start Streaming")
	})
	refreshBtn := widget.NewButton("Refresh Devices", func() {
		refreshDevices()
	})
	gpuCheck := newGPUCheck(a, statusLabel)
	content := container.NewVBox(
		widget.NewLabelWithStyle("Sonidex Streamer", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Android Target Device:"),
		deviceSelect,
		refreshBtn,
		widget.NewLabel("Port:"),
		portEntry,
		gpuCheck,
		statusLabel,
		ctrl.button,
	)
	myWindow.SetContent(content)
	myWindow.Resize(fyne.NewSize(420, 400))
	refreshDevices()
	myWindow.ShowAndRun()
}

func showReceiver(a fyne.App) {
	myWindow := a.NewWindow("Sonidex Receiver")
	statusLabel := widget.NewLabel("Ready")
	portEntry := widget.NewEntry()
	portEntry.SetText("8080")
	ctrl := &sessionController{status: statusLabel}
	ctrl.button = widget.NewButton("Start Receiving", func() {
		ctrl.mu.Lock()
		if ctrl.running {
			cancel := ctrl.cancel
			ctrl.cancel = nil
			ctrl.running = false
			ctrl.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			statusLabel.SetText("Receiver stopped.")
			ctrl.button.SetText("Start Receiving")
			return
		}
		port := strings.TrimSpace(portEntry.Text)
		if port == "" {
			port = "8080"
		}
		sessCtx, sessCancel := context.WithCancel(context.Background())
		ctrl.cancel = sessCancel
		ctrl.running = true
		ctrl.mu.Unlock()
		statusLabel.SetText("Listening for audio...")
		ctrl.button.SetText("Stop Receiving")
		go ctrl.loop(sessCtx, func(ctx context.Context) error {
			return backend.StartReceiverWithPlayback(ctx, port)
		}, "Disconnected. Reconnecting (%d/%d)...", "Connection failed.", "Start Receiving")
	})
	gpuCheck := newGPUCheck(a, statusLabel)
	content := container.NewVBox(
		widget.NewLabelWithStyle("Sonidex Receiver", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Port:"),
		portEntry,
		gpuCheck,
		statusLabel,
		ctrl.button,
	)
	myWindow.SetContent(content)
	myWindow.Resize(fyne.NewSize(360, 280))
	myWindow.ShowAndRun()
}

func main() {
	if softwareRenderingRequested() {
		applySoftwareRendering()
	}
	myApp := app.New()
	if myApp.Preferences().BoolWithFallback("no_gpu", false) {
		applySoftwareRendering()
	}
	if runtime.GOOS == "android" {
		showReceiver(myApp)
		return
	}
	showStreamer(myApp)
}
