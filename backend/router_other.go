//go:build !windows

package backend

import "errors"

// AudioSession describes an app that currently has an audio session.
type AudioSession struct {
	PID    uint32
	Name   string
	Active bool
	Peak   float32
}

var ErrRoutingUnsupported = errors.New("per-app routing is only available on Windows")

func ListAudioSessions() ([]AudioSession, error) { return nil, ErrRoutingUnsupported }

// NewPlatformRouter returns nil where routing is handled by the sound server
// (PipeWire/Pulse): use Helvum/qpwgraph there.
func NewPlatformRouter() *Router { return nil }

func beginHighResTimer() func() { return func() {} }
