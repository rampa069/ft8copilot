package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// theme.go implements the retro DOS / Turbo Vision aesthetic (FT8CoPilot-bae):
// a deep-blue desktop, double-line panels with a title set into the top border
// and an optional drop shadow, a yellow-on-cyan function-key bar, and a small
// ASCII banner. These are the reusable building blocks the panes and modal
// dialogs (the sibling TUI tasks) compose.

// Palette — the classic 16-colour DOS feel, mapped onto the 256-colour cube so
// it reads well on a modern terminal.
var (
	colDesktop  = lipgloss.Color("18")  // desktop blue (behind the panels)
	colPanel    = lipgloss.Color("17")  // panel background, a touch darker
	colBorder   = lipgloss.Color("45")  // cyan border (unfocused)
	colBorderHi = lipgloss.Color("51")  // bright cyan border (focused)
	colTitle    = lipgloss.Color("226") // yellow titles
	colText     = lipgloss.Color("231") // near-white body text
	colDim      = lipgloss.Color("250") // grey secondary text
	colAccent   = lipgloss.Color("214") // orange accent
	colShadow   = lipgloss.Color("16")  // near-black drop shadow
	colRunning  = lipgloss.Color("46")  // autopilot RUNNING (green)
	colPaused   = lipgloss.Color("196") // autopilot PAUSED (red)

	colBarBg  = lipgloss.Color("45") // function-key bar background (cyan)
	colBarKey = lipgloss.Color("16") // F-key number (black on cyan)
	colBarLbl = lipgloss.Color("17") // F-key label (dark blue on cyan)
)

// Reusable styles.
var (
	stDesktop = lipgloss.NewStyle().Background(colDesktop).Foreground(colText)
	stHeader  = lipgloss.NewStyle().Background(colPanel).Foreground(colTitle).Bold(true)
	stText    = lipgloss.NewStyle().Background(colPanel).Foreground(colText)
	stDimText = lipgloss.NewStyle().Background(colPanel).Foreground(colDim)
	stAccent  = lipgloss.NewStyle().Background(colPanel).Foreground(colAccent).Bold(true)
)

// box-drawing runes (double line), shared by the panel renderer.
const (
	bxTL, bxTR, bxBL, bxBR = "╔", "╗", "╚", "╝"
	bxH, bxV               = "═", "║"
	bxTitleL, bxTitleR     = "╡ ", " ╞" // brackets framing a title in the top border
)

func borderColor(focused bool) lipgloss.Color {
	if focused {
		return colBorderHi
	}
	return colBorder
}

// box renders a double-line panel of exactly width×height cells, with body text
// inside and an optional title set into the top border. Body lines are clipped
// or padded to the interior; extra interior rows are blank. Border is cyan
// (bright when focused), the title yellow.
func box(title, body string, width, height int, focused bool) string {
	if width < 2 {
		width = 2
	}
	if height < 2 {
		height = 2
	}
	innerW := width - 2
	innerH := height - 2

	bs := lipgloss.NewStyle().Foreground(borderColor(focused)).Background(colPanel)
	ts := lipgloss.NewStyle().Foreground(colTitle).Background(colPanel).Bold(true)
	cs := lipgloss.NewStyle().Foreground(colText).Background(colPanel)

	var b strings.Builder
	b.WriteString(topBorder(title, innerW, bs, ts))
	b.WriteByte('\n')

	lines := strings.Split(body, "\n")
	for i := 0; i < innerH; i++ {
		var content string
		if i < len(lines) {
			content = lines[i]
		}
		b.WriteString(bs.Render(bxV))
		b.WriteString(padInterior(content, innerW, cs))
		b.WriteString(bs.Render(bxV))
		if i < innerH-1 {
			b.WriteByte('\n')
		}
	}

	b.WriteByte('\n')
	b.WriteString(bs.Render(bxBL + strings.Repeat(bxH, innerW) + bxBR))
	return b.String()
}

// topBorder builds the top edge, optionally embedding a title like ╔═╡ Title ╞══╗.
func topBorder(title string, innerW int, bs, ts lipgloss.Style) string {
	if title == "" {
		return bs.Render(bxTL + strings.Repeat(bxH, innerW) + bxTR)
	}
	// Space the label takes: "═" + "╡ " + title + " ╞", with at least one
	// trailing "═" before the corner.
	label := bxTitleL + title + bxTitleR
	fixed := 1 + lipgloss.Width(label) + 1 // leading ═ + label + at least one ═
	if fixed > innerW {
		// Title too wide for the panel: fall back to a plain border.
		return bs.Render(bxTL + strings.Repeat(bxH, innerW) + bxTR)
	}
	fill := innerW - 1 - lipgloss.Width(label)
	return bs.Render(bxTL+bxH) +
		ts.Render(label) +
		bs.Render(strings.Repeat(bxH, fill)+bxTR)
}

// padInterior fits content to exactly innerW columns for a panel interior,
// padding the tail with background-styled spaces so no cell is left unpainted.
// A plain `fit` would pad with default-background spaces, which render black
// when the content carries embedded SGR resets (e.g. multi-segment lines or the
// ASCII banner) — that is the cause of the "black gaps" inside dialogs.
func padInterior(content string, innerW int, cs lipgloss.Style) string {
	vis := lipgloss.Width(content)
	if vis > innerW {
		return cs.Render(fit(content, innerW)) // clip; content is normally pre-sized
	}
	out := cs.Render(content)
	if vis < innerW {
		out += cs.Render(strings.Repeat(" ", innerW-vis))
	}
	return out
}

// bgSpaces returns n spaces painted with the panel background, for use as a
// separator between independently-styled segments (a literal " " would render
// with the terminal default background — black).
func bgSpaces(n int) string {
	if n <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Background(colPanel).Render(strings.Repeat(" ", n))
}

// fit clips or right-pads a plain string to exactly w display columns.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	width := lipgloss.Width(s)
	if width == w {
		return s
	}
	if width < w {
		return s + strings.Repeat(" ", w-width)
	}
	// Truncate rune-by-rune until it fits.
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > w {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	for used < w {
		b.WriteByte(' ')
		used++
	}
	return b.String()
}

// shadow adds a one-cell drop shadow to the right and bottom of a rendered block,
// for modal dialogs floating over the desktop. The shadow cells use a near-black
// background so the panel appears to lift off the screen.
func shadow(block string) string {
	sh := lipgloss.NewStyle().Background(colShadow)
	lines := strings.Split(block, "\n")
	w := lipgloss.Width(block)
	out := make([]string, 0, len(lines)+1)
	for i, ln := range lines {
		if i == 0 {
			out = append(out, ln) // top row casts no shadow above itself
			continue
		}
		out = append(out, ln+sh.Render(" "))
	}
	// Bottom shadow row, offset one cell to the right.
	out = append(out, " "+sh.Render(strings.Repeat(" ", w)))
	return strings.Join(out, "\n")
}

// functionKey is one entry in the bottom menu bar.
type functionKey struct{ key, label string }

// functionBar renders the bottom menu bar across the full width: black F-key
// numbers and dark-blue labels on a cyan field, padded to width.
func functionBar(width int, keys []functionKey) string {
	keyStyle := lipgloss.NewStyle().Background(colBarBg).Foreground(colBarKey).Bold(true)
	lblStyle := lipgloss.NewStyle().Background(colBarBg).Foreground(colBarLbl)
	pad := lipgloss.NewStyle().Background(colBarBg)

	var b strings.Builder
	b.WriteString(pad.Render(" "))
	for i, fk := range keys {
		if i > 0 {
			b.WriteString(pad.Render("  "))
		}
		b.WriteString(keyStyle.Render(fk.key))
		b.WriteString(pad.Render(" "))
		b.WriteString(lblStyle.Render(fk.label))
	}
	used := lipgloss.Width(b.String())
	if used < width {
		b.WriteString(pad.Render(strings.Repeat(" ", width-used)))
	}
	return b.String()
}

// banner is a small ASCII wordmark for splash / help screens. Kept narrow enough
// (44 cols) to fit a default terminal.
func banner() string {
	art := []string{
		`╔═╗╔╦╗╔═╗  ╔═╗┌─┐╔═╗┬┬  ┌─┐┌┬┐`,
		`╠╣  ║ ╚═╗  ║  │ │╠═╝││  │ │ │ `,
		`╚   ╩ ╚═╝  ╚═╝└─┘╩  ┴┴─┘└─┘ ┴ `,
	}
	style := lipgloss.NewStyle().Foreground(colBorderHi).Background(colPanel).Bold(true)
	for i, l := range art {
		art[i] = style.Render(l)
	}
	return strings.Join(art, "\n")
}
