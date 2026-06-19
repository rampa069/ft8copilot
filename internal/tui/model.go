package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	applog "github.com/rampa069/ft8copilot/internal/log"
	"github.com/rampa069/ft8copilot/internal/selector"
	"github.com/rampa069/ft8copilot/internal/sequencer"
)

// refreshInterval is how often the candidate table and status panel re-query.
const refreshInterval = time.Second

// fkeys is the function-key menu bar shown along the bottom. Quit is wired here;
// Pause (F2) is wired by FT8CoPilot-6jf and the modal views by their own tasks.
var fkeys = []functionKey{
	{"F1", "Help"},
	{"F2", "Pause"},
	{"F3", "Search"},
	{"F4", "Params"},
	{"F5", "Cands"},
	{"F6", "CQ"},
	{"F10", "Quit"},
}

// tickMsg drives the periodic refresh of live state.
type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// model is the root Bubble Tea model: it composes the candidates table, the
// status panel and the log window, refreshed on a timer.
type model struct {
	d      Deps
	width  int
	height int

	log    logView
	ranked []selector.Ranked
	status sequencer.Status

	searching bool
	search    searchModal

	editing bool
	params  paramModal

	candidates bool
	candOffset int

	help bool
}

func newModel(d Deps) model {
	return model{
		d:      d,
		log:    newLogView(d.LogSink, defaultLogBufferLines),
		search: newSearchModal(d.Store),
		params: newParamModal(d.Control),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.log.listen(), tick())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.refresh()
		return m, tick()
	case logMsg:
		m.log.add(applog.Entry(msg))
		return m, m.log.listen()
	case tea.KeyMsg:
		// While a modal is open, route keys to it (Ctrl-C still quits globally).
		if m.help {
			switch msg.String() {
			case "ctrl+c":
				m.log.close()
				return m, tea.Quit
			case "f1", "esc", "q":
				m.help = false
			}
			return m, nil
		}
		if m.searching {
			switch msg.String() {
			case "ctrl+c":
				m.log.close()
				return m, tea.Quit
			case "esc":
				m.searching = false
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.update(msg)
			return m, cmd
		}
		if m.editing {
			switch msg.String() {
			case "ctrl+c":
				m.log.close()
				return m, tea.Quit
			case "esc":
				m.editing = false
				return m, nil
			}
			var cmd tea.Cmd
			m.params, cmd = m.params.update(msg)
			return m, cmd
		}
		if m.candidates {
			switch msg.String() {
			case "ctrl+c":
				m.log.close()
				return m, tea.Quit
			case "esc", "f5", "q":
				m.candidates = false
			case "up":
				if m.candOffset > 0 {
					m.candOffset--
				}
			case "down":
				m.candOffset++
			case "pgup":
				m.candOffset -= 8
				if m.candOffset < 0 {
					m.candOffset = 0
				}
			case "pgdown":
				m.candOffset += 8
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c", "f10", "esc":
			m.log.close()
			return m, tea.Quit
		case "f2", " ":
			if m.d.Seq != nil {
				m.d.Seq.TogglePause()
				m.refresh() // reflect the new state immediately
			}
			return m, nil
		case "f3":
			m.searching = true
			return m, m.search.focus()
		case "f4":
			m.editing = true
			return m, m.params.open()
		case "f5":
			m.candidates = true
			m.candOffset = 0
			return m, nil
		case "f6":
			// Manual one-shot CQ (FT8CoPilot-2zh). The CQ-mode toggle
			// (FT8CoPilot-3ef) will replace this with a sustained mode.
			if m.d.Seq != nil {
				m.d.Seq.RequestCQ()
			}
			return m, nil
		case "f1":
			m.help = true
			return m, nil
		}
	}
	return m, nil
}

// refresh re-queries the live status and candidate ranking for the current band.
func (m *model) refresh() {
	if m.d.Seq != nil {
		m.status = m.d.Seq.Status()
	}
	if m.d.Ranker != nil {
		m.ranked = m.d.Ranker.Rank(m.status.Band)
	}
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "" // wait for the first WindowSizeMsg
	}
	header := m.renderHeader()
	footer := functionBar(m.width, fkeys)
	bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyHeight < 6 {
		bodyHeight = 6
	}
	body := m.renderBody(bodyHeight)
	switch {
	case m.help:
		body = helpView(m.width, bodyHeight)
	case m.searching:
		body = m.search.view(m.width, bodyHeight)
	case m.editing:
		body = m.params.view(m.width, bodyHeight)
	case m.candidates:
		body = m.candidatesView(m.width, bodyHeight)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m model) renderHeader() string {
	call := m.d.MyCall
	if call == "" {
		call = "unknown"
	}
	title := "FT8 CoPilot " + m.d.Version + " — " + call
	return stHeader.Width(m.width).Padding(0, 1).Render(title)
}

// renderBody tiles the body: a top row of candidates | status, and the log
// window beneath, together filling width×height.
func (m model) renderBody(height int) string {
	// Log window takes the bottom third (min 6 rows); the rest is the top row.
	logH := height / 3
	if logH < 6 {
		logH = 6
	}
	if logH > height-5 {
		logH = height - 5
	}
	topH := height - logH

	statusW := 28
	if statusW > m.width-20 {
		statusW = m.width / 2
	}
	candW := m.width - statusW

	bandTitle := "Candidates"
	if m.status.Band > 0 {
		bandTitle = fmt.Sprintf("Candidates · %dm", m.status.Band)
	}

	candBox := box(bandTitle, renderCandidates(m.ranked, candW-2, topH-2), candW, topH, true)
	statusBox := box("Status", renderStatus(m.status, m.ranked, m.d.MyCall, statusW-2, topH-2), statusW, topH, false)
	top := lipgloss.JoinHorizontal(lipgloss.Top, candBox, statusBox)

	logBox := box("Log", m.log.render(m.width-2, logH-2), m.width, logH, false)

	return lipgloss.JoinVertical(lipgloss.Left, top, logBox)
}
