package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rampamac/ft8copilot/internal/control"
)

// Editable fields, in display order.
const (
	fldTXPower = iota
	fldTXRetries
	fldFollowFreq
	fldRetryTime
	fldSelectors
	fldBlackList
	numFields
)

var paramLabels = [numFields]string{
	fldTXPower:    "TX power (W)",
	fldTXRetries:  "TX retries",
	fldFollowFreq: "Follow freq",
	fldRetryTime:  "Retry time (m)",
	fldSelectors:  "Selectors",
	fldBlackList:  "Blacklist",
}

// paramModal is the F4 parameter editor: a small form bound to the control
// Controller. Enter applies live; Esc cancels.
type paramModal struct {
	ctrl   *control.Controller
	inputs [numFields]textinput.Model
	focus  int
	status string
}

func newParamModal(ctrl *control.Controller) paramModal {
	m := paramModal{ctrl: ctrl}
	for i := range m.inputs {
		ti := textinput.New()
		ti.Prompt = ""
		ti.CharLimit = 80
		// Width 0 disables the textinput's own (unstyled, black) padding; the row
		// gets the panel background from fitANSI/box instead.
		ti.Width = 0
		ti.TextStyle = lipgloss.NewStyle().Foreground(colText).Background(colPanel)
		ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(colDim).Background(colPanel)
		// The (hidden) cursor cell at the end of each field defaults to a blank
		// with no background; give it the panel background so it isn't black.
		ti.Cursor.TextStyle = lipgloss.NewStyle().Foreground(colText).Background(colPanel)
		m.inputs[i] = ti
	}
	return m
}

// open loads the current parameters into the form and focuses the first field.
func (m *paramModal) open() tea.Cmd {
	if m.ctrl == nil {
		m.status = "no control surface"
		return nil
	}
	p := m.ctrl.Params()
	m.inputs[fldTXPower].SetValue(strconv.Itoa(p.TXPower))
	m.inputs[fldTXRetries].SetValue(strconv.Itoa(p.TXRetries))
	m.inputs[fldFollowFreq].SetValue(yesNo(p.FollowFrequency))
	m.inputs[fldRetryTime].SetValue(strconv.Itoa(p.RetryTime))
	m.inputs[fldSelectors].SetValue(strings.Join(p.CallSelector, " "))
	m.inputs[fldBlackList].SetValue(strings.Join(p.BlackList, ", "))
	m.status = "↑/↓ move · Enter apply · Esc cancel"
	m.focus = 0
	return m.setFocus(0)
}

// setFocus blurs all fields and focuses field i.
func (m *paramModal) setFocus(i int) tea.Cmd {
	for j := range m.inputs {
		m.inputs[j].Blur()
	}
	m.focus = (i + numFields) % numFields
	return m.inputs[m.focus].Focus()
}

func (m paramModal) update(msg tea.Msg) (paramModal, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "up", "shift+tab":
			return m, m.setFocus(m.focus - 1)
		case "down", "tab":
			return m, m.setFocus(m.focus + 1)
		case "enter":
			m.apply()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return m, cmd
}

// apply parses the form and pushes it through the Controller.
func (m *paramModal) apply() {
	if m.ctrl == nil {
		m.status = "no control surface"
		return
	}
	power, err := strconv.Atoi(strings.TrimSpace(m.inputs[fldTXPower].Value()))
	if err != nil {
		m.status = "TX power must be a number"
		return
	}
	retries, err := strconv.Atoi(strings.TrimSpace(m.inputs[fldTXRetries].Value()))
	if err != nil {
		m.status = "TX retries must be a number"
		return
	}
	follow, err := parseBool(m.inputs[fldFollowFreq].Value())
	if err != nil {
		m.status = "Follow freq must be yes or no"
		return
	}
	retry, err := strconv.Atoi(strings.TrimSpace(m.inputs[fldRetryTime].Value()))
	if err != nil {
		m.status = "Retry time must be a number"
		return
	}

	p := control.Params{
		TXPower:         power,
		TXRetries:       retries,
		FollowFrequency: follow,
		RetryTime:       retry,
		CallSelector:    strings.Fields(m.inputs[fldSelectors].Value()),
		BlackList:       splitList(m.inputs[fldBlackList].Value()),
	}
	if err := m.ctrl.Apply(p); err != nil {
		m.status = "error: " + err.Error()
		return
	}
	m.status = "applied ✓"
}

func (m paramModal) view(screenW, screenH int) string {
	dw := 52
	if dw > screenW-4 {
		dw = screenW - 4
	}
	dh := numFields + 6
	if dh > screenH-2 {
		dh = screenH - 2
	}
	interiorW := dw - 2

	lines := make([]string, 0, numFields+3)
	for i := range m.inputs {
		var label string
		if i == m.focus {
			label = lipgloss.NewStyle().Foreground(colTitle).Background(colPanel).Bold(true).Render(fit("▶ "+paramLabels[i], 16))
		} else {
			label = stStatLabel.Render(fit("  "+paramLabels[i], 16))
		}
		lines = append(lines, fitANSI(label+bgSpaces(1)+m.inputs[i].View(), interiorW))
	}
	lines = append(lines, fitANSI("", interiorW))
	lines = append(lines, fitANSI(stDimText.Render(m.status), interiorW))

	inner := strings.Join(lines, "\n")
	dialog := shadow(box("Parameters", inner, dw, dh, true))
	return stDesktop.Width(screenW).Height(screenH).
		Align(lipgloss.Center, lipgloss.Center).Render(dialog)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes", "y", "true", "on", "1":
		return true, nil
	case "no", "n", "false", "off", "0":
		return false, nil
	}
	return false, fmt.Errorf("not a yes/no value: %q", s)
}

// splitList splits a comma-separated list, trimming blanks.
func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
