package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	minW    = 40  // below this the layout cannot work
	minH    = 15  // the compact layout fits in this
	maxLogs = 200 // log lines kept; the log box shows as many as fit
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	frameStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	logStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1)
)

// fit truncates a (possibly styled) line to w cells.
func fit(s string, w int) string { return ansi.Truncate(s, w, "…") }

// View draws one full-window frame: header, settings, status, a log box that
// takes whatever height is left, and the key help pinned to the last row.
func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "" // wait for the first WindowSizeMsg
	}
	if m.width < minW || m.height < minH {
		msg := dimStyle.Width(m.width).Align(lipgloss.Center).
			Render(fmt.Sprintf("Too small: %d×%d (need %d×%d)", m.width, m.height, minW, minH))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}

	innerH := m.height - 2 // inside the frame's border
	inner, logs := m.layout(false)
	if lipgloss.Height(inner) > innerH || !logs {
		// Not enough room for the airy layout (or for the log box): drop the
		// blank separator lines and use it if that is what makes things fit.
		if alt, altLogs := m.layout(true); lipgloss.Height(inner) > innerH || altLogs {
			inner = alt
		}
	}

	frame := frameStyle.Width(m.width - 2).Height(innerH).Render(inner)
	// Never emit more than the window holds: extra lines would scroll the screen.
	return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(frame)
}

// layout builds the frame's contents. compact removes the blank separator
// lines and shortens the help. logs reports whether the log box fit.
func (m model) layout(compact bool) (inner string, logs bool) {
	cw := m.width - 4      // inside the frame's border and padding
	innerH := m.height - 2 // inside the frame's border
	gap := "\n\n"
	if compact {
		gap = "\n"
	}

	var top strings.Builder
	top.WriteString(m.header(cw) + "\n")
	top.WriteString(dimStyle.Render(strings.Repeat("─", cw)) + gap)
	top.WriteString(m.settings(cw, innerH, gap))
	top.WriteString(m.statusLine(cw))
	body := top.String()
	footer := m.footer(cw, compact)

	// Blank lines reserved above the log box and above the footer.
	reserve := 2
	if compact {
		reserve = 0
	}
	if free := innerH - lipgloss.Height(body) - lipgloss.Height(footer) - reserve; free >= 3 {
		body += gap + m.logBox(cw, free)
		logs = true
	}
	pad := innerH - lipgloss.Height(body) - lipgloss.Height(footer) + 1
	if pad < 1 {
		pad = 1
	}
	return body + strings.Repeat("\n", pad) + footer, logs
}

func (m model) header(cw int) string {
	left := titleStyle.Render("Sonidex Streamer")
	right := dimStyle.Render("○ idle")
	if m.running {
		right = okStyle.Render("● streaming")
	}
	gap := cw - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m model) settings(cw, innerH int, gap string) string {
	var b strings.Builder
	b.WriteString(fit(dimStyle.Render("Mode (m to cycle): ")+selectedStyle.Render(m.mode.String()), cw) + gap)

	switch m.mode {
	case modeWirelessADB:
		b.WriteString(fit(dimStyle.Render("Device ip:port: ")+m.wirelessAddr.View()+"  "+helpStyle.Render("(c to connect)"), cw) + gap)
	case modeWiFiDirect:
		b.WriteString(fit(dimStyle.Render("Phone IP: ")+m.directIP.View(), cw) + gap)
	}

	if m.mode != modeWiFiDirect {
		b.WriteString(dimStyle.Render("Android Target Device:") + "\n")
		b.WriteString(m.deviceList(cw, clamp(innerH-19, 1, 8)))
		if gap != "\n" {
			b.WriteString("\n")
		}
	}

	b.WriteString(fit(dimStyle.Render("Port: ")+m.port.View(), cw) + gap)
	silence := "off"
	if m.silenceSkip {
		silence = "on"
	}
	prof := dimStyle.Render("Profile (l): ") + selectedStyle.Render(m.profile.String())
	pause := dimStyle.Render("Pause on silence (s): ") + selectedStyle.Render(silence)
	if lipgloss.Width(prof)+3+lipgloss.Width(pause) <= cw {
		b.WriteString(prof + "   " + pause + gap)
	} else {
		b.WriteString(fit(prof, cw) + "\n" + fit(pause, cw) + gap)
	}
	return b.String()
}

// deviceList shows at most rows devices, scrolled to keep the cursor visible.
func (m model) deviceList(cw, rows int) string {
	if len(m.devices) == 0 {
		return dimStyle.Render("  (none found — press r to refresh)") + "\n"
	}
	start := 0
	if len(m.devices) > rows {
		start = clamp(m.cursor-rows/2, 0, len(m.devices)-rows)
	}
	end := min(start+rows, len(m.devices))

	var b strings.Builder
	for i := start; i < end; i++ {
		line := "  " + m.devices[i]
		style := lipgloss.NewStyle()
		if i == m.cursor {
			line = "> " + m.devices[i]
			style = selectedStyle
		}
		b.WriteString(fit(style.Render(line), cw) + "\n")
	}
	if hidden := len(m.devices) - (end - start); hidden > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  … %d more (↑/↓)", hidden)) + "\n")
	}
	return b.String()
}

func (m model) statusLine(cw int) string {
	status, style := "Ready", dimStyle
	if m.running {
		status, style = "Streaming active...", okStyle
	}
	if m.lastErr != "" {
		status, style = m.lastErr, errStyle
	}
	return fit(style.Render(status), cw)
}

// logBox is the activity log, h rows tall including its border.
func (m model) logBox(cw, h int) string {
	lines := h - 2
	logs := m.logs
	if len(logs) > lines {
		logs = logs[len(logs)-lines:]
	}
	rows := make([]string, 0, len(logs))
	for _, l := range logs {
		rows = append(rows, fit(l, cw-4))
	}
	if len(rows) == 0 {
		rows = append(rows, dimStyle.Render("no activity yet"))
	}
	return logStyle.Width(cw - 2).Height(lines).Render(strings.Join(rows, "\n"))
}

func (m model) footer(cw int, compact bool) string {
	action := "enter: start streaming"
	if m.running {
		action = "enter: stop streaming"
	}
	text := fmt.Sprintf("↑/↓: select · r: refresh · tab: next field · m: mode · l: profile · s: silence · %s · q: quit", action)
	if compact {
		text = "↑↓ select · r refresh · tab field · m mode · l/s options · enter start/stop · q quit"
	}
	return helpStyle.Width(cw).Render(text)
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }
