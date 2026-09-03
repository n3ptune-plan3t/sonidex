package main

import (
	"context"
	"fmt"
	"image/color"
	"sync"
	"time"

	_ "embed"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"sonidex/backend"
)

type AppState struct {
	mu        sync.Mutex
	isRunning bool
	ctx       context.Context
	cancel    context.CancelFunc

	statusLabel *widget.Label
	actionBtn   *widget.Button
	portEntry   *widget.Entry
	ipEntry     *widget.Entry
}

func (s *AppState) getIsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isRunning
}

func (s *AppState) updateUIStatus(text string, btnText string) {
	s.statusLabel.SetText(text)
	if btnText != "" {
		s.actionBtn.SetText(btnText)
	}
}

func (s *AppState) runStreamLoop(sessionCtx context.Context, task func(ctx context.Context) error) {
	maxRetries := 5
	backoff := 500 * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if sessionCtx.Err() != nil {
			return
		}

		err := task(sessionCtx)
		if err == nil || sessionCtx.Err() != nil {
			return
		}

		s.updateUIStatus(fmt.Sprintf("Disconnected. Reconnecting (%d/%d)...", attempt, maxRetries), "")

		select {
		case <-sessionCtx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > 4*time.Second {
			backoff = 4 * time.Second
		}
	}

	s.mu.Lock()
	if s.ctx == sessionCtx {
		s.isRunning = false
		s.cancel = nil
		s.updateUIStatus("Connection failed.", "Start")
	}
	s.mu.Unlock()
}

func (s *AppState) toggleDesktopStream() {
	s.mu.Lock()
	if s.isRunning {
		if s.cancel != nil {
			s.cancel()
			s.cancel = nil
		}
		s.isRunning = false
		s.mu.Unlock()
		s.updateUIStatus("Stream stopped.", "Start Streaming")
		return
	}

	sessCtx, sessCancel := context.WithCancel(context.Background())
	s.ctx = sessCtx
	s.cancel = sessCancel
	s.isRunning = true
	s.mu.Unlock()

	s.updateUIStatus("Streaming active...", "Stop Streaming")

	go s.runStreamLoop(sessCtx, func(ctx context.Context) error {
		targetAddr := s.ipEntry.Text + ":" + s.portEntry.Text
		_ = targetAddr
		<-ctx.Done()
		return nil
	})
}

func (s *AppState) toggleAndroidReceiver() {
	s.mu.Lock()
	if s.isRunning {
		if s.cancel != nil {
			s.cancel()
			s.cancel = nil
		}
		s.isRunning = false
		s.mu.Unlock()
		s.updateUIStatus("Receiver stopped.", "Start Receiving")
		return
	}

	sessCtx, sessCancel := context.WithCancel(context.Background())
	s.ctx = sessCtx
	s.cancel = sessCancel
	s.isRunning = true
	s.mu.Unlock()

	s.updateUIStatus("Listening for audio...", "Stop Receiving")

	go s.runStreamLoop(sessCtx, func(ctx context.Context) error {
		port := s.portEntry.Text
		if port == "" {
			port = "8080"
		}
		audioBuf := backend.NewAudioBuffer(96000)
		return backend.StartTCPReceiver(ctx, port, audioBuf, 1920)
	})
}

var appIconBytes []byte

func main() {
	myApp := app.New()
	myWindow := myApp.NewWindow("Sonidex")

	state := &AppState{
		statusLabel: widget.NewLabel("Ready"),
		portEntry:   widget.NewEntry(),
		ipEntry:     widget.NewEntry(),
	}
	state.portEntry.SetText("8080")
	state.ipEntry.SetText("127.0.0.1")

	state.actionBtn = widget.NewButton("Start", func() {
		state.toggleAndroidReceiver()
	})

	content := container.NewVBox(
		widget.NewLabelWithStyle("Sonidex Audio Bridge", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Port:"),
		state.portEntry,
		state.statusLabel,
		state.actionBtn,
	)

	myWindow.SetContent(content)
	myWindow.Resize(fyne.NewSize(360, 240))
	myWindow.ShowAndRun()
}
