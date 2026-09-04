package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"sonidex/backend"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
)

type statusMsg struct {
	text  string
	isErr bool
}

type streamStoppedMsg struct{ err error }

type devicesMsg struct {
	devices []string
	err     error
}

type connMode int

const (
	modeUSBADB connMode = iota
	modeWirelessADB
	modeWiFiDirect
)

func (c connMode) String() string {
	switch c {
	case modeWirelessADB:
		return "Wireless ADB"
	case modeWiFiDirect:
		return "WiFi Direct (No ADB)"
	default:
		return "USB / ADB"
	}
}

func (c connMode) next() connMode {
	return (c + 1) % 3
}

type wirelessConnectMsg struct{ err error }

type focusField int

const (
	focusDevices focusField = iota
	focusPort
	focusWirelessAddr
	focusDirectIP
)

type model struct {
	devices      []string
	cursor       int
	port         textinput.Model
	wirelessAddr textinput.Model
	directIP     textinput.Model
	mode         connMode
	focus        focusField
	running      bool
	cancel       context.CancelFunc
	statusCh     chan statusMsg
	logs         []string
	lastErr      string
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "8080"
	ti.SetValue("8080")
	ti.CharLimit = 5
	ti.Width = 10

	wa := textinput.New()
	wa.Placeholder = "192.168.1.42:5555"
	wa.Width = 22

	di := textinput.New()
	di.Placeholder = "192.168.1.42"
	di.Width = 16

	return model{
		port:         ti,
		wirelessAddr: wa,
		directIP:     di,
		statusCh:     make(chan statusMsg, 32),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(refreshDevicesCmd(), waitForStatus(m.statusCh))
}

func refreshDevicesCmd() tea.Cmd {
	return func() tea.Msg {
		devices, err := backend.ListADBDevices()
		return devicesMsg{devices: devices, err: err}
	}
}

func waitForStatus(ch chan statusMsg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func (m model) nextFocus() focusField {
	switch m.mode {
	case modeWirelessADB:
		switch m.focus {
		case focusDevices:
			return focusWirelessAddr
		case focusWirelessAddr:
			return focusPort
		default:
			return focusDevices
		}
	case modeWiFiDirect:
		if m.focus == focusDirectIP {
			return focusPort
		}
		return focusDirectIP
	default: // modeUSBADB
		if m.focus == focusDevices {
			return focusPort
		}
		return focusDevices
	}
}

func (m *model) applyFocus() {
	m.port.Blur()
	m.wirelessAddr.Blur()
	m.directIP.Blur()
	switch m.focus {
	case focusPort:
		m.port.Focus()
	case focusWirelessAddr:
		m.wirelessAddr.Focus()
	case focusDirectIP:
		m.directIP.Focus()
	}
}

func connectWirelessCmd(addr string) tea.Cmd {
	return func() tea.Msg {
		return wirelessConnectMsg{err: backend.ConnectWirelessADB(addr)}
	}
}

func (m *model) pushLog(s string) {
	m.logs = append(m.logs, s)
	if len(m.logs) > 8 {
		m.logs = m.logs[len(m.logs)-8:]
	}
}

func runLoop(ctx context.Context, addr string, statusCh chan statusMsg, done chan streamStoppedMsg) {
	maxRetries := 5
	backoff := 500 * time.Millisecond
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			done <- streamStoppedMsg{}
			return
		}
		err := backend.StartDesktopStream(ctx, addr)
		if err == nil || ctx.Err() != nil {
			done <- streamStoppedMsg{}
			return
		}
		statusCh <- statusMsg{text: fmt.Sprintf("disconnected, reconnecting (%d/%d): %v", attempt, maxRetries, err), isErr: true}
		select {
		case <-ctx.Done():
			done <- streamStoppedMsg{}
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 4*time.Second {
			backoff = 4 * time.Second
		}
	}
	done <- streamStoppedMsg{err: fmt.Errorf("connection failed after %d attempts", maxRetries)}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "tab":
			m.focus = m.nextFocus()
			m.applyFocus()
			return m, nil
		case "m":
			if !m.running {
				m.mode = m.mode.next()
				m.focus = focusDevices
				m.applyFocus()
			}
			return m, nil
		case "c":
			if m.mode == modeWirelessADB && m.focus == focusWirelessAddr {
				addr := strings.TrimSpace(m.wirelessAddr.Value())
				if addr == "" {
					m.lastErr = "enter the device's ip:port first"
					return m, nil
				}
				return m, connectWirelessCmd(addr)
			}
		case "r":
			if m.focus == focusDevices && !m.running {
				return m, refreshDevicesCmd()
			}
		case "up", "k":
			if m.focus == focusDevices && m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.focus == focusDevices && m.cursor < len(m.devices)-1 {
				m.cursor++
			}
		case "enter":
			return m.toggleStream()
		}

		var cmd tea.Cmd
		switch m.focus {
		case focusPort:
			m.port, cmd = m.port.Update(msg)
		case focusWirelessAddr:
			m.wirelessAddr, cmd = m.wirelessAddr.Update(msg)
		case focusDirectIP:
			m.directIP, cmd = m.directIP.Update(msg)
		}
		return m, cmd

	case wirelessConnectMsg:
		if msg.err != nil {
			m.lastErr = "adb connect failed: " + msg.err.Error()
			return m, nil
		}
		m.pushLog("connected — refreshing devices")
		return m, refreshDevicesCmd()

	case devicesMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.devices = nil
		} else {
			m.devices = msg.devices
			m.lastErr = ""
			if m.cursor >= len(m.devices) {
				m.cursor = 0
			}
		}
		return m, nil

	case statusMsg:
		if msg.isErr {
			m.lastErr = msg.text
		}
		m.pushLog(msg.text)
		return m, waitForStatus(m.statusCh)

	case streamStoppedMsg:
		m.running = false
		m.cancel = nil
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.pushLog(msg.err.Error())
		} else {
			m.pushLog("stream stopped")
		}
		return m, nil
	}

	return m, nil
}

func (m model) toggleStream() (tea.Model, tea.Cmd) {
	if m.running {
		if m.cancel != nil {
			m.cancel()
		}
		if m.mode != modeWiFiDirect && len(m.devices) > 0 && m.cursor < len(m.devices) {
			_ = backend.RemoveADBReverse(m.devices[m.cursor], m.port.Value())
		}
		m.running = false
		m.pushLog("stopping...")
		return m, nil
	}

	port := strings.TrimSpace(m.port.Value())
	if port == "" {
		port = "8080"
	}

	var addr, label string
	switch m.mode {
	case modeWiFiDirect:
		ip := strings.TrimSpace(m.directIP.Value())
		if ip == "" {
			m.lastErr = "enter the phone's IP address"
			return m, nil
		}
		addr = ip + ":" + port
		label = ip
	default:
		if len(m.devices) == 0 {
			m.lastErr = "no ADB devices — press r to refresh"
			return m, nil
		}
		serial := m.devices[m.cursor]
		if err := backend.SetupADBReverse(serial, port); err != nil {
			m.lastErr = "adb forward failed: " + err.Error()
			return m, nil
		}
		addr = "127.0.0.1:" + port
		label = serial
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.running = true
	m.lastErr = ""
	m.pushLog("streaming to " + label + " on port " + port)

	done := make(chan streamStoppedMsg, 1)
	go runLoop(ctx, addr, m.statusCh, done)

	return m, func() tea.Msg { return <-done }
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Sonidex Streamer (TUI)") + "\n\n")

	b.WriteString(dimStyle.Render("Mode (m to cycle): ") + selectedStyle.Render(m.mode.String()) + "\n\n")

	switch m.mode {
	case modeWirelessADB:
		b.WriteString(dimStyle.Render("Device ip:port: ") + m.wirelessAddr.View() + "  " + helpStyle.Render("(c to connect)") + "\n\n")
	case modeWiFiDirect:
		b.WriteString(dimStyle.Render("Phone IP: ") + m.directIP.View() + "\n\n")
	}

	if m.mode != modeWiFiDirect {
		b.WriteString(dimStyle.Render("Android Target Device:") + "\n")
		if len(m.devices) == 0 {
			b.WriteString(dimStyle.Render("  (none found — press r to refresh)") + "\n")
		}
		for i, d := range m.devices {
			cursor := "  "
			style := lipgloss.NewStyle()
			if i == m.cursor {
				cursor = "> "
				style = selectedStyle
			}
			b.WriteString(style.Render(cursor+d) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(dimStyle.Render("Port: ") + m.port.View() + "\n\n")

	status := "Ready"
	statusStyle := dimStyle
	if m.running {
		status = "Streaming active..."
		statusStyle = okStyle
	}
	if m.lastErr != "" {
		status = m.lastErr
		statusStyle = errStyle
	}
	b.WriteString(statusStyle.Render(status) + "\n\n")

	if len(m.logs) > 0 {
		b.WriteString(boxStyle.Render(strings.Join(m.logs, "\n")) + "\n\n")
	}

	action := "enter: start streaming"
	if m.running {
		action = "enter: stop streaming"
	}
	b.WriteString(helpStyle.Render(fmt.Sprintf("↑/↓: select · r: refresh · tab: next field · m: mode · %s · q: quit", action)))

	return b.String()
}

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
	}
}
