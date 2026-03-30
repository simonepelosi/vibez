package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Quit       key.Binding
	PlayPause  key.Binding
	Next       key.Binding
	Previous   key.Binding
	VolumeUp   key.Binding
	VolumeDown key.Binding
	ViewNow    key.Binding
	ViewQueue  key.Binding
	ViewLib    key.Binding
	ViewSearch key.Binding
	Search     key.Binding
	Select     key.Binding
	Up         key.Binding
	Down       key.Binding
	Help       key.Binding
}

var keys = keyMap{
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	PlayPause: key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("space", "play/pause"),
	),
	Next: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "next"),
	),
	Previous: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "previous"),
	),
	VolumeUp: key.NewBinding(
		key.WithKeys("+", "="),
		key.WithHelp("+", "vol up"),
	),
	VolumeDown: key.NewBinding(
		key.WithKeys("-"),
		key.WithHelp("-", "vol down"),
	),
	ViewNow: key.NewBinding(
		key.WithKeys("1"),
		key.WithHelp("1", "now playing"),
	),
	ViewQueue: key.NewBinding(
		key.WithKeys("2"),
		key.WithHelp("2", "queue"),
	),
	ViewLib: key.NewBinding(
		key.WithKeys("3"),
		key.WithHelp("3", "library"),
	),
	ViewSearch: key.NewBinding(
		key.WithKeys("4"),
		key.WithHelp("4", "search"),
	),
	Search: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "search"),
	),
	Select: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
}
