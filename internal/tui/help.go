package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// help.go implements the F1 help overlay: a shadowed dialog listing the key
// bindings, centred on the desktop.

var helpKeys = []struct{ key, desc string }{
	{"F1", "This help screen"},
	{"F2 / Space", "Pause / resume the autopilot"},
	{"F3", "Search the call database"},
	{"F4", "Edit live parameters"},
	{"F5", "Full ranked candidates view"},
	{"F10 / q", "Quit"},
	{"", ""},
	{"In dialogs:", ""},
	{"Esc", "Close the dialog"},
	{"Enter", "Apply / run"},
	{"↑ / ↓", "Move / scroll"},
	{"PgUp / PgDn", "Scroll a page"},
}

func helpView(screenW, screenH int) string {
	dw := 52
	if dw > screenW-4 {
		dw = screenW - 4
	}
	dh := len(helpKeys) + 10 // +5 banner rows, +2 blanks, +1 footer, +2 borders
	if dh > screenH-2 {
		dh = screenH - 2
	}
	interiorW := dw - 2

	lines := []string{banner(), ""}
	keyStyle := lipgloss.NewStyle().Foreground(colTitle).Background(colPanel).Bold(true)
	for _, k := range helpKeys {
		if k.key == "" && k.desc == "" {
			lines = append(lines, fitANSI("", interiorW))
			continue
		}
		if k.desc == "" {
			lines = append(lines, fitANSI(stAccent.Render(k.key), interiorW))
			continue
		}
		row := keyStyle.Render(fit(k.key, 12)) + bgSpaces(1) + stText.Render(k.desc)
		lines = append(lines, fitANSI(row, interiorW))
	}
	lines = append(lines, fitANSI("", interiorW))
	lines = append(lines, fitANSI(stDimText.Render("Press F1 or Esc to close"), interiorW))

	dialog := shadow(box("Help", strings.Join(lines, "\n"), dw, dh, true))
	return stDesktop.Width(screenW).Height(screenH).
		Align(lipgloss.Center, lipgloss.Center).Render(dialog)
}
