package main

import (
	"context"
	"fmt"
	"log"
	"runtime"

	"sonidex/backend"

	fyne.io/fyne/v2"
	fyne.io/fyne/v2/app"
	fyne.io/fyne/v2/container"
	fyne.io/fyne/v2/layout"
	fyne.io/fyne/v2/widget"
	"github.com/gen2brain/malgo"
)

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
	s.latencySelect.SetSelected("Low (10ms)")

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

func (s *AppState) toggleDesktopStream() {
	if s.isRunning {
		if s.cancel != nil {
			s.cancel()
		}
		s.isRunning = false
		s.actionBtn.SetText("Connect & Stream Audio")
		s.statusLbl.SetText("Status: Idle")
		return
	}

	selectedDevice := s.deviceSelect.Selected
	if selectedDevice == "" {
		s.statusLbl.SetText("Error: Select a USB device first")
		return
	}

	port := s.portEntry.Text
	if err := backend.SetupADBReverse(selectedDevice, port); err != nil {
		s.statusLbl.SetText("Error: ADB reverse forward failed")
		return
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.isRunning = true
	s.actionBtn.SetText("Stop Streaming")
	s.statusLbl.SetText("Streaming Lossless PCM via USB...")

	go func() {
		err := backend.StartTCPSender(s.ctx, "127.0.0.1:"+port, s.mCtx)
		if err != nil && s.isRunning {
			s.statusLbl.SetText("Stream Error: " + err.Error())
			s.isRunning = false
			s.actionBtn.SetText("Connect & Stream Audio")
		}
	}()
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

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.isRunning = true
	s.actionBtn.SetText("Stop Receiving")
	s.statusLbl.SetText("Listening for USB stream...")

	go func() {
		err := backend.StartTCPReceiver(s.ctx, ":"+s.portEntry.Text, s.mCtx)
		if err != nil && s.isRunning {
			s.statusLbl.SetText("Receiver Error: " + err.Error())
			s.isRunning = false
			s.actionBtn.SetText("Start Receiving Audio")
		}
	}()
}
