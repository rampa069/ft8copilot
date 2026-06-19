package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rampamac/ft8copilot/internal/db"
)

// searchModal is the F3 database-search dialog: a text box plus a scrollable
// results table, rendered as a shadowed panel centred on the desktop.
type searchModal struct {
	store   *db.Store
	input   textinput.Model
	results []db.Record
	offset  int
	status  string
}

func newSearchModal(store *db.Store) searchModal {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.Placeholder = "call / country / grid…"
	ti.CharLimit = 40
	// Width 0 disables the textinput's own padding, which is rendered with the
	// terminal-default background (black). The row is padded with the panel
	// background by fitANSI/box instead.
	ti.Width = 0
	ti.PromptStyle = lipgloss.NewStyle().Foreground(colTitle).Background(colPanel).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(colText).Background(colPanel)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(colDim).Background(colPanel)
	ti.Cursor.TextStyle = lipgloss.NewStyle().Foreground(colText).Background(colPanel)
	return searchModal{store: store, input: ti, status: "type a query, press Enter"}
}

// focus puts the cursor in the input and returns the blink command.
func (s *searchModal) focus() tea.Cmd { return s.input.Focus() }

// update handles input editing, running the query (Enter) and scrolling.
func (s searchModal) update(msg tea.Msg) (searchModal, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "enter":
			s.run()
			return s, nil
		case "up":
			if s.offset > 0 {
				s.offset--
			}
			return s, nil
		case "down":
			s.offset++
			return s, nil
		case "pgup":
			s.offset -= 8
			if s.offset < 0 {
				s.offset = 0
			}
			return s, nil
		case "pgdown":
			s.offset += 8
			return s, nil
		}
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

// run executes the search and refreshes the results/status.
func (s *searchModal) run() {
	if s.store == nil {
		s.status = "no database"
		return
	}
	recs, err := s.store.Search(db.Query{Text: strings.TrimSpace(s.input.Value()), Limit: 500})
	if err != nil {
		s.results = nil
		s.status = "error: " + err.Error()
		return
	}
	s.results = recs
	s.offset = 0
	s.status = fmt.Sprintf("%d result(s)", len(recs))
}

// view renders the dialog centred on a desktop-blue field of screenW×screenH.
func (s searchModal) view(screenW, screenH int) string {
	dw := 66
	if dw > screenW-4 {
		dw = screenW - 4
	}
	dh := 18
	if dh > screenH-2 {
		dh = screenH - 2
	}
	interiorW, interiorH := dw-2, dh-2
	resultsH := interiorH - 3 // input, status, hint
	if resultsH < 1 {
		resultsH = 1
	}

	inner := strings.Join([]string{
		fitANSI(s.input.View(), interiorW),
		fitANSI(stDimText.Render(s.status), interiorW),
		renderRecords(s.results, interiorW, resultsH, s.offset),
		fitANSI(stDimText.Render("Enter search · ↑/↓ scroll · Esc close"), interiorW),
	}, "\n")

	dialog := shadow(box("Search Database", inner, dw, dh, true))
	return stDesktop.Width(screenW).Height(screenH).
		Align(lipgloss.Center, lipgloss.Center).Render(dialog)
}

// record-table column widths.
const (
	recCall = 9
	recSNR  = 4
	recBand = 5
	recStat = 4
	recGrid = 5
)

var (
	stRecHead = lipgloss.NewStyle().Foreground(colAccent).Background(colPanel).Bold(true)
	stRecRow  = lipgloss.NewStyle().Foreground(colText).Background(colPanel)
	stRecWkd  = lipgloss.NewStyle().Foreground(colRunning).Background(colPanel)
)

// renderRecords draws a scrollable record table into width×height: a header plus
// rows from offset. Worked stations are tinted green.
func renderRecords(recs []db.Record, width, height, offset int) string {
	lines := make([]string, 0, height)
	lines = append(lines, stRecHead.Render(recColumns("CALL", "SNR", "BAND", "ST", "GRID", "COUNTRY", width)))

	rows := height - 1
	if offset > len(recs)-rows {
		offset = len(recs) - rows
	}
	if offset < 0 {
		offset = 0
	}
	for i := offset; i < len(recs) && len(lines) < height; i++ {
		r := recs[i]
		line := recColumns(
			r.Call,
			fmt.Sprintf("%d", r.SNR),
			fmt.Sprintf("%dm", r.Band),
			statusLabel(r.Status),
			r.Grid,
			r.Country,
			width,
		)
		if r.Status >= 2 {
			lines = append(lines, stRecWkd.Render(line))
		} else {
			lines = append(lines, stRecRow.Render(line))
		}
	}
	for len(lines) < height {
		lines = append(lines, stRecRow.Render(fit("", width)))
	}
	return strings.Join(lines, "\n")
}

func recColumns(call, snr, band, st, grid, country string, width int) string {
	used := recCall + 1 + recSNR + 1 + recBand + 1 + recStat + 1 + recGrid + 1
	countryW := width - used
	if countryW < 0 {
		countryW = 0
	}
	var b strings.Builder
	b.WriteString(fit(call, recCall))
	b.WriteByte(' ')
	b.WriteString(fitRight(snr, recSNR))
	b.WriteByte(' ')
	b.WriteString(fitRight(band, recBand))
	b.WriteByte(' ')
	b.WriteString(fit(st, recStat))
	b.WriteByte(' ')
	b.WriteString(fit(grid, recGrid))
	b.WriteByte(' ')
	b.WriteString(fit(country, countryW))
	return fit(b.String(), width)
}

func statusLabel(st int) string {
	switch st {
	case 0:
		return "new"
	case 1:
		return "wip"
	case 2:
		return "wkd"
	default:
		return "?"
	}
}
