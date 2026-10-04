package art

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// SupportsKitty limits automatic graphics to terminals with known support.
func SupportsKitty() bool {
	if os.Getenv("TMUX") != "" {
		return false
	}
	return os.Getenv("TERM") == "xterm-ghostty" || os.Getenv("TERM") == "xterm-kitty" || os.Getenv("TERM_PROGRAM") == "ghostty"
}

// KittyUpload preserves the decoded image resolution; the terminal scales it.
func KittyUpload(img image.Image, id int, size Size) (string, error) {
	var buf bytes.Buffer
	err := kitty.EncodeGraphics(&buf, img, &kitty.Options{
		Action: kitty.TransmitAndPut, Format: kitty.PNG, Transmission: kitty.Direct,
		ID: id, Quite: 2, Chunk: true, VirtualPlacement: true,
		Columns: size.Width, Rows: size.Height, DoNotMoveCursor: true,
	})
	return buf.String(), err
}

func KittyPlacement(id int, size Size) string {
	return ansi.KittyGraphics(nil, (&kitty.Options{
		Action: kitty.Put, ID: id, Quite: 2, VirtualPlacement: true,
		Columns: size.Width, Rows: size.Height, DoNotMoveCursor: true,
	}).Options()...)
}

func KittyDelete(id int) string {
	return ansi.KittyGraphics(nil, (&kitty.Options{
		Action: kitty.Delete, ID: id, Delete: kitty.DeleteID, DeleteResources: true, Quite: 2,
	}).Options()...)
}

// KittyLines are normal text cells, so the TUI can center, clip and move them.
func KittyLines(id int, size Size) []string {
	lines := make([]string, size.Height)
	color := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (id>>16)&255, (id>>8)&255, id&255)
	for row := range size.Height {
		var s strings.Builder
		s.WriteString(color)
		for col := range size.Width {
			s.WriteRune(kitty.Placeholder)
			s.WriteRune(kitty.Diacritic(row))
			s.WriteRune(kitty.Diacritic(col))
		}
		s.WriteString("\x1b[0m")
		lines[row] = s.String()
	}
	return lines
}
