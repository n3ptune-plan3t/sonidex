package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"time"

	"sonidex/backend"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/gen2brain/malgo"
)
const maxStreamRetries = 5

type AppState struct {
	ctx           context.Context
	cancel        context.CancelFunc
	isRunning     bool
	mCtx          *malgo.AllocatedContext
	statusLbl     *widget.Label
	actionBtn     *widget.Button
	deviceSelect  *widget.Select
	latencySelect *widget.Select
	portEntry     *widget.Entry
	adbDevice     string
	adbPort       string
}

func main() {
	mCtx, err := backend.InitMalgo()
	if err != nil {
		log.Fatalf("Audio device init failed: %v", err)
	}
	defer mCtx.Free()

	a := app.New()
	w := a.NewWindow("Sonidex")
	w.Resize(fyne.NewSize(360, 320))

	state := &AppState{
		mCtx:      mCtx,
		statusLbl: widget.NewLabel("Status: Ready"),
		portEntry: widget.NewEntry(),
	}
	state.portEntry.SetText("5005")

	if runtime.GOOS == "android" {
		buildAndroidUI(w, state)
	} else {
		buildDesktopUI(w, state)
	}

	w.ShowAndRun()
}

func buildDesktopUI(w fyne.Window, s *AppState) {
	s.deviceSelect = widget.NewSelect([]string{}, nil)
	s.deviceSelect.PlaceHolder = "Select Connected Device"

	refreshBtn := widget.NewButton("Scan USB Devices", func() {
		devices, err := backend.ListADBDevices()
		if err != nil || len(devices) == 0 {
			s.deviceSelect.Options = []string{}
			s.deviceSelect.SetSelected("")
			s.statusLbl.SetText("Status: No ADB devices found")
			return
		}
		s.deviceSelect.Options = devices
		s.deviceSelect.SetSelected(devices[0])
		s.statusLbl.SetText(fmt.Sprintf("Status: Found %d device(s)", len(devices)))
	})

	s.latencySelect = widget.NewSelect([]string{"Ultra Low (5ms)", "Low (10ms)", "Safe (20ms)"}, nil)
	s.latencySelect.SetSelected(backend.DefaultLatencyPreset)

	s.actionBtn = widget.NewButton("Connect & Stream Audio", func() {
		s.toggleDesktopStream()
	})

	content := container.NewVBox(
		widget.NewLabelWithStyle("Sonidex USB Host", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("1. Android Target Device:"),
		container.NewBorder(nil, nil, nil, refreshBtn, s.deviceSelect),
		widget.NewLabel("2. Buffer Latency Target:"),
		s.latencySelect,
		widget.NewLabel("3. Communication Port:"),
		s.portEntry,
		layout.NewSpacer(),
		s.actionBtn,
		s.statusLbl,
	)

	w.SetContent(container.NewPadded(content))
}

func buildAndroidUI(w fyne.Window, s *AppState) {
	s.actionBtn = widget.NewButton("Start Receiving Audio", func() {
		s.toggleAndroidReceiver()
	})

	content := container.NewVBox(
		widget.NewLabelWithStyle("Sonidex Sink", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Port:"),
		s.portEntry,
		layout.NewSpacer(),
		s.actionBtn,
		s.statusLbl,
	)

	w.SetContent(container.NewPadded(content))
}

func (s *AppState) selectedPeriodFrames() uint32 {
	label := backend.DefaultLatencyPreset
	if s.latencySelect != nil && s.latencySelect.Selected != "" {
		label = s.latencySelect.Selected
	}
	if frames, ok := backend.LatencyPresets[label]; ok {
		return frames
	}
	return backend.LatencyPresets[backend.DefaultLatencyPreset]
}
func (s *AppState) runStreamLoop(label string, run func(ctx context.Context) error, onGiveUp func()) {
	backoff := time.Second
	for attempt := 0; ; attempt++ {
		err := run(s.ctx)
		if s.ctx.Err() != nil {
			return
		}
		if err == nil {
			return
		}
		if attempt >= maxStreamRetries {
			s.statusLbl.SetText(fmt.Sprintf("%s failed: %v", label, err))
			s.isRunning = false
			onGiveUp()
			return
		}
		s.statusLbl.SetText(fmt.Sprintf("%s dropped (%v) - retrying in %s...", label, err, backoff))
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 8*time.Second {
			backoff *= 2
		}
	}
}

func (s *AppState) toggleDesktopStream() {
	if s.isRunning {
		if s.cancel != nil {
			s.cancel()
		}
		s.isRunning = false
		s.actionBtn.SetText("Connect & Stream Audio")
		s.statusLbl.SetText("Status: Idle")
		if s.adbDevice != "" && s.adbPort != "" {
			_ = backend.RemoveADBReverse(s.adbDevice, s.adbPort)
			s.adbDevice, s.adbPort = "", ""
		}
		return
	}

	selectedDevice := s.deviceSelect.Selected
	if selectedDevice == "" {
		s.statusLbl.SetText("Error: Select a USB device first")
		return
	}

	port := s.portEntry.Text
	if err := backend.SetupADBReverse(selectedDevice, port); err != nil {
		s.statusLbl.SetText("Error: ADB port forward failed")
		return
	}
	s.adbDevice, s.adbPort = selectedDevice, port

	periodFrames := s.selectedPeriodFrames()

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.isRunning = true
	s.actionBtn.SetText("Stop Streaming")
	s.statusLbl.SetText("Streaming Lossless PCM via USB...")

	go s.runStreamLoop("Stream", func(ctx context.Context) error {
		return backend.StartTCPSender(ctx, "127.0.0.1:"+port, s.mCtx, periodFrames)
	}, func() {
		s.actionBtn.SetText("Connect & Stream Audio")
	})
}

func (s *AppState) toggleAndroidReceiver() {
	if s.isRunning {
		if s.cancel != nil {
			s.cancel()
		}
		s.isRunning = false
		s.actionBtn.SetText("Start Receiving Audio")
		s.statusLbl.SetText("Status: Idle")
		return
	}

	periodFrames := backend.LatencyPresets[backend.DefaultLatencyPreset]

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.isRunning = true
	s.actionBtn.SetText("Stop Receiving")
	s.statusLbl.SetText("Listening for USB stream...")

	go s.runStreamLoop("Receiver", func(ctx context.Context) error {
		return backend.StartTCPReceiver(ctx, ":"+s.portEntry.Text, s.mCtx, periodFrames)
	}, func() {
		s.actionBtn.SetText("Start Receiving Audio")
	})
}
