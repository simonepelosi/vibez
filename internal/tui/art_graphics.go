package tui

import (
	"math"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/tui/art"
)

type artworkGraphics struct {
	id     int
	gen    int
	size   art.Size
	failed bool
}

func (m *Model) artworkSize(contentW, h int) art.Size {
	aspect := m.artCellAsp
	if aspect <= 0 {
		aspect = 2
	}
	rows := h - 4
	cols := int(math.Round(float64(rows) * aspect))
	for rows > 2 && cols > contentW {
		rows--
		cols = int(math.Round(float64(rows) * aspect))
	}
	if rows < 2 || cols < 4 {
		return art.Size{}
	}
	return art.Size{Width: cols, Height: rows}
}

func (m *Model) artworkImageID() int { return 0x560000 + m.artworkGen%65535 }

// syncArtworkGraphics uploads once per cover and changes placements on resize.
func (m *Model) syncArtworkGraphics() tea.Cmd {
	if m.supportsArtGraphics == nil || !m.supportsArtGraphics() {
		return nil
	}
	if !m.artModeActive() || m.artwork.img == nil {
		if m.artGraphics.id == 0 {
			return nil
		}
		cmd := tea.Raw(art.KittyDelete(m.artGraphics.id))
		m.artGraphics = artworkGraphics{}
		return cmd
	}
	// Prepare the actual layout viewport before uploading or resizing images.
	m.nowPlayingLines(m.width-4, m.nowPlayingHeight())
	size := m.artworkSize(m.artworkViewport.Width, m.artworkViewport.Height)
	if size.Width == 0 {
		return nil
	}
	id := m.artworkImageID()
	if m.artGraphics.id == id && m.artGraphics.gen == m.artworkGen {
		if m.artGraphics.failed || m.artGraphics.size == size {
			return nil
		}
		m.artGraphics.size = size
		return tea.Raw(art.KittyPlacement(id, size))
	}
	data, err := art.KittyUpload(m.artwork.img, id, size)
	previous := m.artGraphics.id
	m.artGraphics = artworkGraphics{id: id, gen: m.artworkGen, size: size, failed: err != nil}
	if err != nil {
		m.artwork.rendered = map[art.Size][]string{}
		return nil
	}
	if previous != 0 {
		data = art.KittyDelete(previous) + data
	}
	return tea.Raw(data)
}
