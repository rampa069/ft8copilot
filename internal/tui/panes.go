package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rampa069/ft8copilot/internal/selector"
	"github.com/rampa069/ft8copilot/internal/sequencer"
)

// panes.go renders the body panes: the ranked candidates table and the status
// panel. Each returns a plain interior block sized to fit inside a box().

// Candidate-table column widths (interior columns, single-space separated).
const (
	colMarker  = 2
	colCall    = 9
	colSNR     = 4
	colDist    = 6
	colZone    = 3
	candHeader = "CALL"
)

// candidate row/line styles.
var (
	stColHead = lipgloss.NewStyle().Foreground(colAccent).Background(colPanel).Bold(true)
	stRow     = lipgloss.NewStyle().Foreground(colText).Background(colPanel)
	stRowDim  = lipgloss.NewStyle().Foreground(colDim).Background(colPanel)
	stRowPick = lipgloss.NewStyle().Foreground(colPanel).Background(colRunning).Bold(true)
)

// renderCandidates draws the ranked pool into a width×height interior: a header
// row plus one row per candidate (chosen highlighted, ineligible dimmed).
func renderCandidates(ranked []selector.Ranked, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := make([]string, 0, height)
	lines = append(lines, stColHead.Render(candColumns("", candHeader, "SNR", "KM", "Z", "COUNTRY", width)))

	rows := height - 1
	for i, r := range ranked {
		if i >= rows {
			break
		}
		marker := " "
		if r.Chosen {
			marker = "▶"
		}
		snr := fmt.Sprintf("%d", r.SNR)
		dist := fmt.Sprintf("%d", int(r.Distance))
		zone := fmt.Sprintf("%d", r.CQZone)
		line := candColumns(marker, r.Call, snr, dist, zone, r.Country, width)
		switch {
		case r.Chosen:
			lines = append(lines, stRowPick.Render(line))
		case !r.Eligible:
			lines = append(lines, stRowDim.Render(line))
		default:
			lines = append(lines, stRow.Render(line))
		}
	}
	for len(lines) < height {
		lines = append(lines, stRow.Render(fit("", width)))
	}
	return strings.Join(lines, "\n")
}

// candColumns lays out one table line to exactly width columns.
func candColumns(marker, call, snr, dist, zone, country string, width int) string {
	used := colMarker + 1 + colCall + 1 + colSNR + 1 + colDist + 1 + colZone + 1
	countryW := width - used
	if countryW < 0 {
		countryW = 0
	}
	var b strings.Builder
	b.WriteString(fit(marker, colMarker))
	b.WriteByte(' ')
	b.WriteString(fit(call, colCall))
	b.WriteByte(' ')
	b.WriteString(fitRight(snr, colSNR))
	b.WriteByte(' ')
	b.WriteString(fitRight(dist, colDist))
	b.WriteByte(' ')
	b.WriteString(fitRight(zone, colZone))
	b.WriteByte(' ')
	b.WriteString(fit(country, countryW))
	return fit(b.String(), width)
}

// fitRight right-aligns a plain string to width w (clipping from the left).
func fitRight(s string, w int) string {
	if w <= 0 {
		return ""
	}
	width := lipgloss.Width(s)
	if width == w {
		return s
	}
	if width < w {
		return strings.Repeat(" ", w-width) + s
	}
	return fit(s, w) // too long: clip from the right
}

// status panel styles.
var (
	stStatLabel = lipgloss.NewStyle().Foreground(colDim).Background(colPanel)
	stStatVal   = lipgloss.NewStyle().Foreground(colText).Background(colPanel).Bold(true)
)

// renderStatus draws the status panel interior: identity, band, autopilot state,
// the station being worked and spot counts.
func renderStatus(st sequencer.Status, ranked []selector.Ranked, myCall string, width, height int) string {
	eligible := 0
	for _, r := range ranked {
		if r.Eligible {
			eligible++
		}
	}

	var autopilot string
	if st.Paused {
		autopilot = lipgloss.NewStyle().Foreground(colPaused).Background(colPanel).Bold(true).Render("■ PAUSED")
	} else {
		autopilot = lipgloss.NewStyle().Foreground(colRunning).Background(colPanel).Bold(true).Render("● RUNNING")
	}

	band := "—"
	if st.Band > 0 {
		band = fmt.Sprintf("%dm", st.Band)
	}
	freq := "—"
	if st.Frequency > 0 {
		freq = fmt.Sprintf("%.3f MHz", float64(st.Frequency)/1e6)
	}
	working := st.Current
	if working == "" {
		working = "—"
	}
	tx := stStatVal.Render("idle")
	if st.Transmitting {
		tx = lipgloss.NewStyle().Foreground(colAccent).Background(colPanel).Bold(true).Render("TRANSMIT")
	}

	rows := []struct{ label, val string }{
		{"Station", stStatVal.Render(orDash(myCall))},
		{"Auto", autopilot},
		{"Band", stStatVal.Render(band)},
		{"Freq", stStatVal.Render(freq)},
		{"TX", tx},
		{"Working", stStatVal.Render(working)},
		{"Spots", stStatVal.Render(fmt.Sprintf("%d (%d ok)", len(ranked), eligible))},
	}

	lines := make([]string, 0, height)
	for _, r := range rows {
		if len(lines) >= height {
			break
		}
		line := stStatLabel.Render(fit(r.label, 8)) + bgSpaces(1) + r.val
		lines = append(lines, fitANSI(line, width))
	}
	for len(lines) < height {
		lines = append(lines, fit("", width))
	}
	return strings.Join(lines, "\n")
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// fitANSI pads a possibly-styled line to width w on the right (does not clip; the
// status values are short and known to fit).
func fitANSI(s string, w int) string {
	width := lipgloss.Width(s)
	if width < w {
		return s + lipgloss.NewStyle().Background(colPanel).Render(strings.Repeat(" ", w-width))
	}
	return s
}
