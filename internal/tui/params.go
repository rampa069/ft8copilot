package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/rampa069/ft8copilot/internal/control"
	"github.com/rampa069/ft8copilot/internal/selector"
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

const paramLabelW = 16

func (m paramModal) view(screenW, screenH int) string {
	dw := 52
	if dw > screenW-4 {
		dw = screenW - 4
	}
	interiorW := dw - 2

	// Width available for a field's value: the panel interior minus the label
	// column and the one-space gutter. Every field's rendered view is clamped to
	// this with an ANSI-aware truncation so a long value (the Selectors/Blacklist
	// chains) can never push the row past the interior — an overflow there makes
	// box()'s ANSI-naive padInterior mangle the whole line. The focused field
	// gets a textinput Width so its value scrolls and keeps the cursor in view
	// while editing; blurred fields keep Width 0 (they render fully painted).
	availW := interiorW - paramLabelW - 1
	if availW < 8 {
		availW = 8
	}
	for i := range m.inputs {
		if i == m.focus {
			m.inputs[i].Width = availW - 1 // textinput renders Width+1 cells
		} else {
			m.inputs[i].Width = 0
		}
	}

	lines := make([]string, 0, numFields+8)
	for i := range m.inputs {
		var label string
		if i == m.focus {
			label = lipgloss.NewStyle().Foreground(colTitle).Background(colPanel).Bold(true).Render(fit("▶ "+paramLabels[i], paramLabelW))
		} else {
			label = stStatLabel.Render(fit("  "+paramLabels[i], paramLabelW))
		}
		field := ansi.Truncate(m.inputs[i].View(), availW, "")
		lines = append(lines, fitANSI(label+bgSpaces(1)+field, interiorW))
		// Spell out the selector chain right under its row: the order (= match
		// priority), which selectors are still free to add, and any typo.
		if i == fldSelectors {
			lines = append(lines, m.selectorLegend(interiorW)...)
		}
	}
	lines = append(lines, fitANSI("", interiorW))
	lines = append(lines, fitANSI(stDimText.Render(m.status), interiorW))

	// Size the panel to the content so the legend always fits.
	dh := len(lines) + 2
	if dh > screenH-2 {
		dh = screenH - 2
	}

	inner := strings.Join(lines, "\n")
	dialog := shadow(box("Parameters", inner, dw, dh, true))
	return stDesktop.Width(screenW).Height(screenH).
		Align(lipgloss.Center, lipgloss.Center).Render(dialog)
}

// selectorLegend renders the call_selector chain under its input: the active
// selectors numbered in priority order, the ones still available to add, and a
// red warning for any name that isn't a real selector. It reads the field's
// live value so it updates as the user types.
func (m paramModal) selectorLegend(width int) []string {
	tokens := strings.Fields(m.inputs[fldSelectors].Value())
	inChain := make(map[string]bool, len(tokens))
	var unknown []string
	for _, t := range tokens {
		inChain[t] = true
		if !selector.Registered(t) {
			unknown = append(unknown, t)
		}
	}
	var free []string
	for _, n := range selector.Names() {
		if !inChain[n] {
			free = append(free, n)
		}
	}

	grn := lipgloss.NewStyle().Foreground(colRunning).Background(colPanel)
	dim := lipgloss.NewStyle().Foreground(colDim).Background(colPanel)
	red := lipgloss.NewStyle().Foreground(colPaused).Background(colPanel).Bold(true)

	var out []string
	const indent = "    "
	if len(tokens) == 0 {
		out = append(out, fitANSI(dim.Render(indent+"chain empty — autopilot won't call anyone"), width))
	} else {
		numbered := make([]string, len(tokens))
		for i, t := range tokens {
			numbered[i] = fmt.Sprintf("%d.%s", i+1, t)
		}
		out = appendWrapped(out, indent+"use: ", numbered, grn, dim, width)
	}
	if len(free) > 0 {
		out = appendWrapped(out, indent+"add: ", free, dim, dim, width)
	}
	if len(unknown) > 0 {
		out = append(out, fitANSI(red.Render(indent+"unknown: "+strings.Join(unknown, " ")), width))
	}
	out = append(out, fitANSI(dim.Render(indent+"order = match priority · space-separated"), width))
	return out
}

// appendWrapped adds one or more lines of "<prefix><tokens…>" word-wrapped to
// width, the prefix styled with pre and each token with tok. Continuation lines
// are indented to line up under the first token. Token names are ASCII, so byte
// length equals display width here.
func appendWrapped(dst []string, prefix string, tokens []string, tok, pre lipgloss.Style, width int) []string {
	hang := strings.Repeat(" ", len(prefix))
	lead := prefix    // styled with pre
	line := ""        // styled tokens for the current line
	used := len(lead) // plain display width used so far on the line
	n := 0            // tokens placed on the current line
	for _, t := range tokens {
		extra := len(t)
		if n > 0 {
			extra++ // separating space
		}
		if n > 0 && used+extra > width {
			dst = append(dst, fitANSI(pre.Render(lead)+line, width))
			lead, line, used, n = hang, "", len(hang), 0
			extra = len(t)
		}
		if n > 0 {
			line += bgSpaces(1) // panel-painted separator, never a black gap
		}
		line += tok.Render(t)
		used += extra
		n++
	}
	dst = append(dst, fitANSI(pre.Render(lead)+line, width))
	return dst
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
