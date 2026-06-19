package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rampa069/ft8copilot/internal/selector"
)

// candidates.go implements the F5 full-screen ranked candidates view: the whole
// band pool, best-to-worst, with more detail than the inline panel and
// scrolling. It reuses m.ranked (refreshed every tick).

// detailed-table column widths.
const (
	dCall = 9
	dSNR  = 4
	dKM   = 6
	dCQ   = 3
	dITU  = 4
	dGrid = 6
	dElig = 9
)

var (
	stDetHead = lipgloss.NewStyle().Foreground(colAccent).Background(colPanel).Bold(true)
	stDetRow  = lipgloss.NewStyle().Foreground(colText).Background(colPanel)
	stDetDim  = lipgloss.NewStyle().Foreground(colDim).Background(colPanel)
	stDetPick = lipgloss.NewStyle().Foreground(colPanel).Background(colRunning).Bold(true)
)

// renderCandidatesFull draws the detailed, scrollable candidate table into
// width×height: a header plus rows from offset.
func renderCandidatesFull(ranked []selector.Ranked, width, height, offset int) string {
	lines := make([]string, 0, height)
	lines = append(lines, stDetHead.Render(detColumns(" ", "CALL", "SNR", "KM", "CQ", "ITU", "GRID", "STATUS", "COUNTRY", width)))

	rows := height - 1
	if offset > len(ranked)-rows {
		offset = len(ranked) - rows
	}
	if offset < 0 {
		offset = 0
	}
	for i := offset; i < len(ranked) && len(lines) < height; i++ {
		r := ranked[i]
		marker := " "
		if r.Chosen {
			marker = "▶"
		}
		status := "ok"
		if !r.Eligible {
			status = r.Reason
		}
		line := detColumns(
			marker, r.Call,
			fmt.Sprintf("%d", r.SNR),
			fmt.Sprintf("%d", int(r.Distance)),
			fmt.Sprintf("%d", r.CQZone),
			fmt.Sprintf("%d", r.ITUZone),
			r.Grid, status, r.Country, width,
		)
		switch {
		case r.Chosen:
			lines = append(lines, stDetPick.Render(line))
		case r.Eligible:
			lines = append(lines, stDetRow.Render(line))
		default:
			lines = append(lines, stDetDim.Render(line))
		}
	}
	for len(lines) < height {
		lines = append(lines, stDetRow.Render(fit("", width)))
	}
	return strings.Join(lines, "\n")
}

func detColumns(marker, call, snr, km, cq, itu, grid, status, country string, width int) string {
	used := colMarker + 1 + dCall + 1 + dSNR + 1 + dKM + 1 + dCQ + 1 + dITU + 1 + dGrid + 1 + dElig + 1
	countryW := width - used
	if countryW < 0 {
		countryW = 0
	}
	var b strings.Builder
	b.WriteString(fit(marker, colMarker))
	b.WriteByte(' ')
	b.WriteString(fit(call, dCall))
	b.WriteByte(' ')
	b.WriteString(fitRight(snr, dSNR))
	b.WriteByte(' ')
	b.WriteString(fitRight(km, dKM))
	b.WriteByte(' ')
	b.WriteString(fitRight(cq, dCQ))
	b.WriteByte(' ')
	b.WriteString(fitRight(itu, dITU))
	b.WriteByte(' ')
	b.WriteString(fit(grid, dGrid))
	b.WriteByte(' ')
	b.WriteString(fit(status, dElig))
	b.WriteByte(' ')
	b.WriteString(fit(country, countryW))
	return fit(b.String(), width)
}

// candidatesView renders the full-screen candidates table inside a single panel
// filling width×height (no centring; it replaces the body).
func (m model) candidatesView(width, height int) string {
	title := "Candidates"
	if m.status.Band > 0 {
		title = fmt.Sprintf("Candidates · %dm · %d spots", m.status.Band, len(m.ranked))
	}
	body := renderCandidatesFull(m.ranked, width-2, height-2, m.candOffset)
	return box(title, body, width, height, true)
}
