package views

import (
	"fmt"
	"math"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

const (
	eqMinGain = -12.0
	eqMaxGain = 12.0
	eqStep    = 0.5

	eqOverheadRows = 5 // title + blank + gain row + freq label + help line
	eqMinBarH      = 4
	eqMaxBarH      = 24
)

type EQChangeMsg struct {
	Bands []player.EQBand
}

type EQModel struct {
	bands  []player.EQBand
	cursor int
	width  int
	height int
}

func NewEqualizer(bands []player.EQBand) *EQModel {
	if len(bands) == 0 {
		bands = player.DefaultEQBands()
	}
	return &EQModel{bands: bands}
}

func (m *EQModel) SetSize(w, h int) { m.width = w; m.height = h }

func (m *EQModel) Bands() []player.EQBand { return m.bands }

func (m *EQModel) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "left", "h":
		if m.cursor > 0 {
			m.cursor--
		}
	case "right", "l":
		if m.cursor < len(m.bands)-1 {
			m.cursor++
		}
	case "up", "k":
		b := &m.bands[m.cursor]
		b.Gain = math.Min(eqMaxGain, b.Gain+eqStep)
		return func() tea.Msg { return EQChangeMsg{Bands: m.bands} }
	case "down", "j":
		b := &m.bands[m.cursor]
		b.Gain = math.Max(eqMinGain, b.Gain-eqStep)
		return func() tea.Msg { return EQChangeMsg{Bands: m.bands} }
	case "0":
		m.bands[m.cursor].Gain = 0
		return func() tea.Msg { return EQChangeMsg{Bands: m.bands} }
	case "r":
		for i := range m.bands {
			m.bands[i].Gain = 0
		}
		return func() tea.Msg { return EQChangeMsg{Bands: m.bands} }
	}
	return nil
}

func (m *EQModel) barHeight() int {
	available := m.height - eqOverheadRows
	return max(eqMinBarH, min(eqMaxBarH, available))
}

func (m *EQModel) View() string {
	if len(m.bands) == 0 {
		return ""
	}

	title := lipgloss.NewStyle().
		Foreground(styles.ColorPrimary).
		Bold(true).
		Render("Equalizer")

	help := lipgloss.NewStyle().
		Foreground(styles.ColorMuted).
		Render("←/→ band  ↑/↓ gain  0 reset band  r reset all  e close")

	colW := max(7, (m.width-2)/len(m.bands))
	barH := m.barHeight()

	var cols []string
	for i, b := range m.bands {
		cols = append(cols, m.renderBand(i, b, colW, barH))
	}

	bands := lipgloss.JoinHorizontal(lipgloss.Top, cols...)

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		bands,
		help,
	)
}

func (m *EQModel) renderBand(idx int, b player.EQBand, colW, barH int) string {
	filled := int(math.Round((b.Gain - eqMinGain) / (eqMaxGain - eqMinGain) * float64(barH)))
	zeroLine := barH / 2

	selected := idx == m.cursor

	barColor := styles.ColorSubtle
	if b.Gain > 0 {
		barColor = styles.ColorActive
	} else if b.Gain < 0 {
		barColor = styles.ColorError
	}
	if selected {
		barColor = styles.ColorPrimary
	}

	var rows []string
	for row := barH; row >= 0; row-- {
		var ch string
		switch {
		case row == zeroLine:
			ch = "─"
		case row == filled:
			ch = "█"
		case (b.Gain > 0 && row <= filled && row > zeroLine) ||
			(b.Gain < 0 && row >= filled && row < zeroLine):
			ch = "│"
		default:
			ch = " "
		}
		rows = append(rows, lipgloss.NewStyle().Foreground(barColor).Render(ch))
	}

	bar := strings.Join(rows, "\n")

	freq := formatFreq(b.Frequency)
	gain := fmt.Sprintf("%+.1f", b.Gain)

	freqStyle := lipgloss.NewStyle().Width(colW).Align(lipgloss.Center).Foreground(styles.ColorMuted)
	gainStyle := lipgloss.NewStyle().Width(colW).Align(lipgloss.Center).Foreground(styles.ColorFg)
	if selected {
		freqStyle = freqStyle.Foreground(styles.ColorPrimary).Bold(true)
		gainStyle = gainStyle.Foreground(styles.ColorPrimary).Bold(true)
	}

	col := lipgloss.JoinVertical(lipgloss.Center,
		gainStyle.Render(gain),
		bar,
		freqStyle.Render(freq),
	)

	return lipgloss.NewStyle().Width(colW).Align(lipgloss.Center).Render(col)
}

func formatFreq(f float64) string {
	if f >= 1000 {
		return fmt.Sprintf("%.0fk", f/1000)
	}
	return fmt.Sprintf("%.0f", f)
}
