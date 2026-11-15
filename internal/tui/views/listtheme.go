package views

import (
	"image/color"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

type listThemeSig struct {
	primary color.Color
	subtle  color.Color
	fg      color.Color
	muted   color.Color
}

func currentListThemeSig() listThemeSig {
	return listThemeSig{
		primary: styles.ColorPrimary,
		subtle:  styles.ColorSubtle,
		fg:      styles.ColorFg,
		muted:   styles.ColorMuted,
	}
}

// applyListTheme installs a clean, modern delegate and list chrome using the
// active palette from styles so Queue and Library track runtime theme swaps.
func applyListTheme(lists ...*list.Model) listThemeSig {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(styles.ColorFg).
		Padding(0, 0, 0, 2)
	delegate.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(styles.ColorMuted).
		Padding(0, 0, 0, 2)
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(styles.ColorPrimary).
		Foreground(styles.ColorPrimary).
		Bold(true).
		Padding(0, 0, 0, 1)
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(styles.ColorPrimary).
		Foreground(styles.ColorSubtle).
		Padding(0, 0, 0, 1)
	delegate.Styles.DimmedTitle = lipgloss.NewStyle().
		Foreground(styles.ColorMuted).
		Padding(0, 0, 0, 2)
	delegate.Styles.DimmedDesc = lipgloss.NewStyle().
		Foreground(styles.ColorMuted).
		Padding(0, 0, 0, 2)

	for _, l := range lists {
		if l == nil {
			continue
		}
		l.SetDelegate(delegate)
		l.Styles.Title = lipgloss.NewStyle().Foreground(styles.ColorPrimary).Bold(true)
		l.Styles.NoItems = lipgloss.NewStyle().Foreground(styles.ColorMuted).PaddingLeft(2)
		l.Styles.PaginationStyle = lipgloss.NewStyle().Foreground(styles.ColorMuted).PaddingLeft(2)
		l.Styles.ActivePaginationDot = lipgloss.NewStyle().Foreground(styles.ColorPrimary).SetString("● ")
		l.Styles.InactivePaginationDot = lipgloss.NewStyle().Foreground(styles.ColorMuted).SetString("○ ")
	}
	return currentListThemeSig()
}
