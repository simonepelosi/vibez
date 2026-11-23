package views

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// runLibraryCmd executes cmd (expanding batches) and feeds every resulting
// message back into lib, mimicking the Bubble Tea runtime for one round trip.
func runLibraryCmd(t *testing.T, lib *LibraryModel, cmd tea.Cmd) *LibraryModel {
	t.Helper()
	if cmd == nil {
		return lib
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			lib = runLibraryCmd(t, lib, c)
		}
		return lib
	}
	lib, _ = lib.Update(msg)
	return lib
}

// The browserless flow builds the TUI with a nil provider and only swaps in a
// real one on EngineReadyMsg. If the audio engine fails to start, that message
// never arrives, so opening a library section must report an error rather
// than dereference the nil provider and kill the program.
func TestLibrary_NilProviderReportsUnavailableInsteadOfPanicking(t *testing.T) {
	cases := []struct {
		name  string
		downs int
	}{
		{"songs", int(sectionSongs)},
		{"albums", int(sectionAlbums)},
		{"artists", int(sectionArtists)},
		{"playlists", int(sectionPlaylists)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lib := NewLibrary(nil)
			lib.SetSize(80, 24)
			for range tc.downs {
				lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			}
			lib, cmd := lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			lib = runLibraryCmd(t, lib, cmd)

			if !errors.Is(lib.LoadErr(), errLibraryUnavailable) {
				t.Fatalf("LoadErr() = %v, want errLibraryUnavailable", lib.LoadErr())
			}
			if view := lib.View(); !strings.Contains(view, "Could not load") {
				t.Fatalf("View() missing load error: %q", view)
			}
		})
	}
}
