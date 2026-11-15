package tui

import (
	"fmt"
	"image"
	"math"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/art"
	"github.com/simone-vibes/vibez/internal/tui/views"
)

func newModernTestModel(t *testing.T, plyr player.Player, w, h int) *Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())

	m := newModel(plyr)
	m.introStep = introDone
	m.width = w
	m.height = h
	m.supportsArtColor = func() bool { return true }
	m.memStats = "mem 12MB"
	m.playerState = player.State{
		Playing:  true,
		Position: 60 * time.Second,
		Volume:   0.70,
		Bitrate:  256,
		Track: &provider.Track{
			ID:         "track-1",
			Title:      "Modern Groove",
			Artist:     "The Testers",
			Album:      "Redesign Vol. 1",
			Duration:   3 * time.Minute,
			ArtworkURL: "https://example.invalid/cover.png",
		},
	}
	m.artwork = artworkCache{
		url:      "https://example.invalid/cover.png",
		img:      image.NewNRGBA(image.Rect(0, 0, 8, 8)),
		rendered: map[art.Size][]string{},
	}
	return m
}

func runCmdTree(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			_ = runCmdTree(sub)
		}
	}
	return msg
}

func sendMouse(t *testing.T, m *Model, msg tea.MouseMsg) tea.Msg {
	t.Helper()
	_, cmd := m.Update(msg)
	return runCmdTree(cmd)
}

func TestView_MouseModeCellMotion(t *testing.T) {
	m := newModernTestModel(t, nil, 0, 0)
	if v := m.View(); v.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("width=0 View().MouseMode = %v, want %v", v.MouseMode, tea.MouseModeCellMotion)
	}
	m.width = 120
	m.height = 36
	if v := m.View(); v.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("View().MouseMode = %v, want %v", v.MouseMode, tea.MouseModeCellMotion)
	}
}

func TestFrameFitsAllPanelsAndModes(t *testing.T) {
	sizes := []struct{ w, h int }{
		{140, 42},
		{100, 32},
		{80, 24},
		{48, 18},
	}

	tracks := make([]provider.Track, 8)
	for i := range tracks {
		tracks[i] = provider.Track{
			ID:       fmt.Sprintf("q-%d", i+1),
			Title:    fmt.Sprintf("Track With Long Name Number %d", i+1),
			Artist:   "Long Artist Ensemble",
			Duration: time.Duration(120+i*15) * time.Second,
		}
	}

	modes := []struct {
		name  string
		setup func(m *Model)
	}{
		{"home", func(m *Model) { m.debugView = false; m.mode = modeNormal; m.activePanel = -1 }},
		{"library", func(m *Model) { m.debugView = false; m.mode = modeNormal; m.activatePanel(0) }},
		{"queue", func(m *Model) { m.debugView = false; m.mode = modeNormal; m.activatePanel(1) }},
		{"lyrics", func(m *Model) { m.debugView = false; m.mode = modeNormal; m.activatePanel(2) }},
		{"feed", func(m *Model) { m.debugView = false; m.mode = modeNormal; m.activatePanel(3) }},
		{"eq", func(m *Model) { m.debugView = false; m.mode = modeNormal; m.activatePanel(4) }},
		{"about", func(m *Model) { m.debugView = false; m.mode = modeNormal; m.activatePanel(5) }},
		{"search", func(m *Model) { m.debugView = false; m.openSearchMode() }},
		{"command", func(m *Model) { m.debugView = false; m.mode = modeCommand }},
		{"debug", func(m *Model) { m.mode = modeNormal; m.debugView = true }},
	}

	for _, artOn := range []bool{false, true} {
		for _, sz := range sizes {
			for _, md := range modes {
				t.Run(fmt.Sprintf("art=%v_%dx%d_%s", artOn, sz.w, sz.h, md.name), func(t *testing.T) {
					m := newModernTestModel(t, nil, sz.w, sz.h)
					m.artMode = artOn
					m.queueTracks = tracks
					m.queue.SetTracks(tracks)
					md.setup(m)

					out := m.View().Content
					lines := strings.Split(out, "\n")
					if len(lines) != sz.h {
						t.Fatalf("line count = %d, want %d", len(lines), sz.h)
					}
					for i, ln := range lines {
						if gotW := lipgloss.Width(ln); gotW != sz.w {
							t.Fatalf("line %d width = %d, want %d\nline=%q", i, gotW, sz.w, ln)
						}
					}
				})
			}
		}
	}
}

func TestMouse_TopBarTabsAndVolume(t *testing.T) {
	mp := newMockPlayer()
	m := newModernTestModel(t, mp, 140, 36)

	_, zones := m.buildTopBar(m.width)
	var tabZones []topHitZone
	var volZone topHitZone
	hasVol := false
	hasSearch := false

	for _, z := range zones {
		switch z.kind {
		case topHitTab:
			tabZones = append(tabZones, z)
		case topHitSearch:
			hasSearch = true
			midX := (z.x0 + z.x1) / 2
			sendMouse(t, m, tea.MouseClickMsg{X: midX, Y: 0, Button: tea.MouseLeft})
			if m.mode != modeSearch {
				t.Fatalf("clicking Search tab did not enter modeSearch (mode=%v)", m.mode)
			}
		case topHitVolume:
			hasVol = true
			volZone = z
		}
	}

	if !hasSearch || !hasVol {
		t.Fatalf("missing top bar hit zones: search=%v vol=%v", hasSearch, hasVol)
	}
	if len(tabZones) != 8 { // Brand logo + Home + Library, Queue, Lyrics, Feed, EQ, About
		t.Fatalf("tab zone count = %d, want 8", len(tabZones))
	}

	for _, z := range tabZones {
		midX := (z.x0 + z.x1) / 2
		sendMouse(t, m, tea.MouseClickMsg{X: midX, Y: 0, Button: tea.MouseLeft})
		if m.mode != modeNormal {
			t.Fatalf("clicking tab panelIdx=%d left mode=%v", z.panelIdx, m.mode)
		}
		if m.activePanel != z.panelIdx {
			t.Fatalf("clicking tab gave activePanel=%d, want %d", m.activePanel, z.panelIdx)
		}
	}

	// Re-click Search after visiting tabs to verify Search can be entered cleanly from normal mode too.
	for _, z := range zones {
		if z.kind == topHitSearch {
			sendMouse(t, m, tea.MouseClickMsg{X: (z.x0 + z.x1) / 2, Y: 0, Button: tea.MouseLeft})
			if m.mode != modeSearch {
				t.Fatalf("re-clicking Search tab did not enter modeSearch (mode=%v)", m.mode)
			}
			break
		}
	}

	// Click volume zone -> toggle mute, then click again -> unmute.
	volX := (volZone.x0 + volZone.x1) / 2
	sendMouse(t, m, tea.MouseClickMsg{X: volX, Y: 0, Button: tea.MouseLeft})
	if m.preMuteVol < 0 {
		t.Fatalf("expected muted state (preMuteVol >= 0), got %v", m.preMuteVol)
	}
	sendMouse(t, m, tea.MouseClickMsg{X: volX, Y: 0, Button: tea.MouseLeft})
	if m.preMuteVol >= 0 {
		t.Fatalf("expected unmuted state (preMuteVol < 0), got %v", m.preMuteVol)
	}

	// Wheel up & down on row 0 adjust volume by ±0.05.
	startVol := m.playerState.Volume
	sendMouse(t, m, tea.MouseWheelMsg{X: volX, Y: 0, Button: tea.MouseWheelUp})
	if math.Abs(m.playerState.Volume-(startVol+0.05)) > 1e-6 {
		t.Fatalf("wheel up volume = %v, want %v", m.playerState.Volume, startVol+0.05)
	}
	sendMouse(t, m, tea.MouseWheelMsg{X: volX, Y: 0, Button: tea.MouseWheelDown})
	if math.Abs(m.playerState.Volume-startVol) > 1e-6 {
		t.Fatalf("wheel down volume = %v, want %v", m.playerState.Volume, startVol)
	}
}

func TestMouse_HeroWaveSeekAndTransportButtons(t *testing.T) {
	mp := newMockPlayer()
	m := newModernTestModel(t, mp, 140, 40)
	m.artMode = true

	_, meta := m.buildHeroViewData(m.width)
	if meta.progY < 0 || meta.progX1 <= meta.progX0 {
		t.Fatalf("invalid hero wave bounds: %+v", meta)
	}

	// Click midpoint of progress wave -> seek ~50% of 3m duration (90s).
	midWaveX := (meta.progX0 + meta.progX1) / 2
	sendMouse(t, m, tea.MouseClickMsg{X: midWaveX, Y: meta.progY, Button: tea.MouseLeft})
	if !mp.seekCalled {
		t.Fatal("clicking hero wave did not call Seek")
	}
	if mp.seekPos < 75*time.Second || mp.seekPos > 105*time.Second {
		t.Fatalf("seekPos = %v, want ~90s", mp.seekPos)
	}

	if meta.btnN != 4 {
		t.Fatalf("hero button count = %d, want 4", meta.btnN)
	}
	for i := range meta.btnN {
		b := meta.btns[i]
		midBtnX := (b.x0 + b.x1) / 2
		prevRepeat := m.playerState.RepeatMode
		prevShuffle := m.playerState.ShuffleMode
		sendMouse(t, m, tea.MouseClickMsg{X: midBtnX, Y: meta.ctrlY, Button: tea.MouseLeft})
		switch b.action {
		case heroActRepeat:
			if m.playerState.RepeatMode == prevRepeat {
				t.Fatal("repeat button click did not cycle RepeatMode")
			}
		case heroActShuffle:
			if m.playerState.ShuffleMode == prevShuffle {
				t.Fatal("shuffle button click did not toggle ShuffleMode")
			}
		case heroActPlayPause:
			if !mp.pauseCalled {
				t.Fatal("play/pause button click did not call Pause()")
			}
		case heroActFav:
			// Triggered toggleCurrentTrackRating(provider.RatingLoved) cleanly without error.
		}
	}
}

func TestMouse_SplitQueueClickPlayAndWheelScroll(t *testing.T) {
	mp := newMockPlayer()
	m := newModernTestModel(t, mp, 120, 30)

	tracks := make([]provider.Track, 18)
	for i := range tracks {
		tracks[i] = provider.Track{
			ID:       fmt.Sprintf("track-%02d", i+1),
			Title:    fmt.Sprintf("Song %d", i+1),
			Artist:   "Tester",
			Duration: 2 * time.Minute,
		}
	}
	m.queueTracks = tracks
	m.queue.SetTracks(tracks)

	panelBodyY0 := 1 + (2 + m.nowPlayingHeight()) + 1
	leftW, _, _ := m.splitCardWidths(m.width)
	queueX := leftW / 2

	// Wheel down scrolls mini queue list; wheel up returns toward top.
	sendMouse(t, m, tea.MouseWheelMsg{X: queueX, Y: panelBodyY0 + 1, Button: tea.MouseWheelDown})
	if m.queueMiniOffset != 1 {
		t.Fatalf("after wheel down queueMiniOffset = %d, want 1", m.queueMiniOffset)
	}
	sendMouse(t, m, tea.MouseWheelMsg{X: queueX, Y: panelBodyY0 + 1, Button: tea.MouseWheelUp})
	if m.queueMiniOffset != 0 {
		t.Fatalf("after wheel up queueMiniOffset = %d, want 0", m.queueMiniOffset)
	}

	// Clicking inside the Queue card must NOT truncate the queue or change playback.
	origTrackID := m.playerState.Track.ID
	sendMouse(t, m, tea.MouseClickMsg{X: queueX, Y: panelBodyY0 + 2, Button: tea.MouseLeft})
	if m.playerState.Track == nil || m.playerState.Track.ID != origTrackID {
		t.Fatalf("clicking Queue card unexpectedly changed track to %+v, want %q", m.playerState.Track, origTrackID)
	}
	if len(m.queueTracks) != 18 || len(mp.setQueueIDs) != 0 {
		t.Fatalf("clicking Queue card mutated queue (len=%d, setQueueIDs=%v)", len(m.queueTracks), mp.setQueueIDs)
	}
}

func TestMouse_VibeFocusAndPickerClick(t *testing.T) {
	m := newModernTestModel(t, nil, 120, 30)
	panelBodyY0 := 1 + (2 + m.nowPlayingHeight()) + 1
	leftW, gap, rightW := m.splitCardWidths(m.width)
	vibeX := leftW + gap + rightW/2

	// Clicking the right card focuses Vibe input.
	sendMouse(t, m, tea.MouseClickMsg{X: vibeX, Y: panelBodyY0 + 1, Button: tea.MouseLeft})
	if !m.vibe.IsFocused() {
		t.Fatal("clicking Vibe card did not focus Vibe input")
	}

	// Clicking row 2 while discovery picker is active selects option 0 and emits DiscoveryMetricSelectedMsg.
	m.vibe.ShowPicker(0.5)
	msg := sendMouse(t, m, tea.MouseClickMsg{X: vibeX, Y: panelBodyY0 + 2, Button: tea.MouseLeft})
	if m.vibe.PickerActive() {
		t.Fatal("clicking picker row did not close picker")
	}
	if _, ok := msg.(views.DiscoveryMetricSelectedMsg); !ok {
		t.Fatalf("picker click msg = %T (%v), want views.DiscoveryMetricSelectedMsg", msg, msg)
	}
}

func TestMouse_EQBandClickAndWheelGain(t *testing.T) {
	mp := newMockPlayer()
	m := newModernTestModel(t, mp, 120, 34)

	// Open EQ panel (index 4).
	m.activatePanel(4)
	_ = m.View() // sync EQ inner size (`w - 4`) before click

	panelBodyY0 := 1 + (2 + m.nowPlayingHeight()) + 1
	colW := max(7, ((m.width-4)-2)/len(m.eqP.m.Bands()))
	targetX := 2 + colW*2 + 1 // band index 2
	sendMouse(t, m, tea.MouseClickMsg{X: targetX, Y: panelBodyY0 + 1, Button: tea.MouseLeft})
	if got := m.eqP.m.Cursor(); got != 2 {
		t.Fatalf("EQ cursor after click = %d, want 2", got)
	}

	beforeGain := m.eqP.m.Bands()[2].Gain
	sendMouse(t, m, tea.MouseWheelMsg{X: targetX, Y: panelBodyY0 + 1, Button: tea.MouseWheelUp})
	afterGain := m.eqP.m.Bands()[2].Gain
	if afterGain <= beforeGain {
		t.Fatalf("EQ band 2 gain after wheel up = %v, want > %v", afterGain, beforeGain)
	}
}

func TestTopBar_NoHorizontalJitterAcrossAnimationVolumeAndActiveTab(t *testing.T) {
	m := newModernTestModel(t, nil, 140, 36)
	_, baseZones := m.buildTopBar(m.width)
	if len(baseZones) < 10 {
		t.Fatalf("unexpected zone count %d", len(baseZones))
	}

	// Brand (0) + every nav tab (1..8) must never shift horizontally as bear
	// animation steps advance, playback toggles, volume/bitrate/mem change,
	// or different tabs become active.
	tabEnd := len(baseZones) - 1 // last zone is topHitVolume
	for _, playing := range []bool{false, true} {
		for _, vol := range []float64{0.05, 0.70, 1.00} {
			for _, bitrate := range []int{256, 2304} {
				for _, panel := range []int{-1, 0, 1, 2, 3, 4, 5} {
					m.playerState.Playing = playing
					m.playerState.Volume = vol
					m.playerState.Bitrate = bitrate
					m.activePanel = panel
					for step := range 24 {
						m.glowStep = step
						_, zones := m.buildTopBar(m.width)
						if len(zones) != len(baseZones) {
							t.Fatalf("zone count changed to %d, want %d", len(zones), len(baseZones))
						}
						for i := range tabEnd {
							if zones[i].x0 != baseZones[i].x0 || zones[i].x1 != baseZones[i].x1 {
								t.Fatalf("top bar jitter at zone %d (step=%d play=%v vol=%.2f br=%d panel=%d): got [%d,%d), want [%d,%d)",
									i, step, playing, vol, bitrate, panel,
									zones[i].x0, zones[i].x1, baseZones[i].x0, baseZones[i].x1)
							}
						}
					}
				}
			}
		}
	}
	tabsW := baseZones[tabEnd-1].x1 - baseZones[1].x0
	wantStart := (m.width - tabsW) / 2
	if got := baseZones[1].x0; got != wantStart {
		t.Fatalf("navigation tabs start at x=%d, want centered x=%d (w=%d, tabsW=%d)", got, wantStart, m.width, tabsW)
	}
}

func TestHeroControlsAndWave_NoHorizontalJitterDuringPlay(t *testing.T) {
	m := newModernTestModel(t, nil, 140, 40)
	for _, artOn := range []bool{false, true} {
		m.artMode = artOn
		_, baseMeta := m.buildHeroViewData(m.width)
		for _, playing := range []bool{true, false} {
			for _, pos := range []time.Duration{5 * time.Second, 65 * time.Second, 605 * time.Second} {
				for step := range 24 {
					m.playerState.Playing = playing
					m.playerState.Position = pos
					m.glowStep = step
					_, meta := m.buildHeroViewData(m.width)
					if meta.progX0 != baseMeta.progX0 || meta.progX1 != baseMeta.progX1 {
						t.Fatalf("wave jitter (art=%v play=%v pos=%v step=%d): got [%d,%d), want [%d,%d)",
							artOn, playing, pos, step, meta.progX0, meta.progX1, baseMeta.progX0, baseMeta.progX1)
					}
					for i := range baseMeta.btnN {
						if meta.btns[i] != baseMeta.btns[i] {
							t.Fatalf("button %d jitter (art=%v play=%v pos=%v step=%d): got %+v, want %+v",
								i, artOn, playing, pos, step, meta.btns[i], baseMeta.btns[i])
						}
					}
				}
			}
		}
	}
}

func TestTopBar_AllTabsHaveDistinctRenderedColors(t *testing.T) {
	m := newModernTestModel(t, nil, 140, 36)
	tabs := m.navTabSpecs()
	inactiveSeen := make(map[string]bool, len(tabs))
	activeSeen := make(map[string]bool, len(tabs))
	for i, tab := range tabs {
		inact := m.renderNavTab(i, tab, false, false)
		act := m.renderNavTab(i, tab, true, false)
		if inactiveSeen[inact] {
			t.Fatalf("duplicate inactive rendering at tab %d: %q", i, inact)
		}
		if activeSeen[act] {
			t.Fatalf("duplicate active rendering at tab %d: %q", i, act)
		}
		inactiveSeen[inact] = true
		activeSeen[act] = true
	}
}

func TestView_StrippedFrameNoHorizontalJitterAcrossGlowSteps(t *testing.T) {
	glowSteps := []int{0, 6, 12, 18, 24, 30}
	modes := []struct {
		name    string
		playing bool
		loading bool
	}{
		{name: "paused", playing: false, loading: false},
		{name: "playing", playing: true, loading: false},
		{name: "loading", playing: false, loading: true},
	}

	for _, artOn := range []bool{false, true} {
		for _, mode := range modes {
			m := newModernTestModel(t, nil, 140, 36)
			m.artMode = artOn
			m.playerState.Playing = mode.playing
			m.playerState.Loading = mode.loading

			var baseLines []string
			var bearStart, vibezIdx int
			for idx, step := range glowSteps {
				m.glowStep = step
				lines := strings.Split(ansi.Strip(m.View().Content), "\n")
				if len(lines) != 36 {
					t.Fatalf("art=%v mode=%s step=%d line count = %d, want 36", artOn, mode.name, step, len(lines))
				}
				if idx == 0 {
					baseLines = lines
					bearStart = strings.Index(lines[0], "ʕ")
					vibezIdx = strings.Index(lines[0], "vibez")
					if bearStart < 0 || vibezIdx <= bearStart {
						t.Fatalf("unexpected header layout in %q", lines[0])
					}
					continue
				}

				// Line 0: only the 5-col bear inside [bearStart:vibezIdx] may change.
				if w := ansi.StringWidth(lines[0]); w != 140 {
					t.Fatalf("art=%v mode=%s step=%d line[0] width = %d, want 140", artOn, mode.name, step, w)
				}
				curBearStart := strings.Index(lines[0], "ʕ")
				curVibezIdx := strings.Index(lines[0], "vibez")
				if curBearStart != bearStart || curVibezIdx < 0 {
					t.Fatalf("art=%v mode=%s step=%d header bear/vibez shifted: bear %d->%d, vibez %d",
						artOn, mode.name, step, bearStart, curBearStart, curVibezIdx)
				}
				if got, want := ansi.StringWidth(lines[0][curBearStart:curVibezIdx]), 6; got != want {
					t.Fatalf("art=%v mode=%s step=%d bear+gap width = %d, want %d (%q)",
						artOn, mode.name, step, got, want, lines[0][curBearStart:curVibezIdx])
				}
				if lines[0][:curBearStart] != baseLines[0][:bearStart] {
					t.Fatalf("art=%v mode=%s step=%d header prefix drifted: %q vs %q",
						artOn, mode.name, step, lines[0][:curBearStart], baseLines[0][:bearStart])
				}
				if lines[0][curVibezIdx:] != baseLines[0][vibezIdx:] {
					t.Fatalf("art=%v mode=%s step=%d header after vibez shifted:\ngot:  %q\nwant: %q",
						artOn, mode.name, step, lines[0][curVibezIdx:], baseLines[0][vibezIdx:])
				}

				// Lines 1..height-1 must remain byte-for-byte identical across ticks,
				// except for:
				// - the 5-cell Vibe bear eyes on the "press v to start" row
				// - the 1-cell Braille spinner rune before " Loading" in loading mode
				// Neither may shift any surrounding text horizontally.
				for row := 1; row < len(lines); row++ {
					if w := ansi.StringWidth(lines[row]); w != 140 {
						t.Fatalf("art=%v mode=%s step=%d row=%d width = %d, want 140", artOn, mode.name, step, row, w)
					}
					if lines[row] == baseLines[row] {
						continue
					}
					const vibeAnchor = "press v to start"
					if curAnchor := strings.Index(lines[row], vibeAnchor); curAnchor >= 0 {
						baseAnchor := strings.Index(baseLines[row], vibeAnchor)
						if baseAnchor >= 0 &&
							ansi.StringWidth(lines[row][:curAnchor]) == ansi.StringWidth(baseLines[row][:baseAnchor]) &&
							lines[row][curAnchor:] == baseLines[row][baseAnchor:] {
							continue
						}
					}
					const loadAnchor = " Loading"
					if mode.loading {
						curLoad := strings.Index(lines[row], loadAnchor)
						baseLoad := strings.Index(baseLines[row], loadAnchor)
						if curLoad >= 0 && baseLoad >= 0 &&
							ansi.StringWidth(lines[row][:curLoad]) == ansi.StringWidth(baseLines[row][:baseLoad]) &&
							lines[row][curLoad:] == baseLines[row][baseLoad:] {
							continue
						}
					}
					t.Fatalf("art=%v mode=%s step=%d row=%d shifted unexpectedly:\ngot:  %q\nwant: %q",
						artOn, mode.name, step, row, lines[row], baseLines[row])
				}
			}
		}
	}
}

func TestView_NoDuplicateHeaderFooterInfoAndSpacedPlaySymbol(t *testing.T) {
	m := newModernTestModel(t, nil, 140, 36)
	m.playerState.Bitrate = 256
	m.playerState.Playing = true
	m.playerState.Track.AudioVariants = []string{"lossy-stereo", "lossless"}
	m.vibe.SetBackend("gemini:flash")

	stripped := ansi.Strip(m.View().Content)
	lines := strings.Split(stripped, "\n")

	// 1. Pause symbol next to "Playing" has breathing room (two spaces, never tight).
	if !strings.Contains(stripped, "⏸  Playing") || strings.Contains(stripped, "⏸ Playing") {
		t.Fatalf("expected spaced \"⏸  Playing\" button without tight \"⏸ Playing\", got:\n%s", stripped)
	}
	m.playerState.Playing = false
	pausedStripped := ansi.Strip(m.View().Content)
	if !strings.Contains(pausedStripped, "▶  Paused") {
		t.Fatalf("expected spaced \"▶  Paused\" button, got:\n%s", pausedStripped)
	}
	m.playerState.Playing = true

	// 2. Top bar must not duplicate audio quality/bitrate shown inside Hero.
	if strings.Contains(lines[0], "AAC") || strings.Contains(lines[0], "Lossless") {
		t.Fatalf("top bar duplicated quality badge: %q", lines[0])
	}

	// 3. Hero top border must not duplicate Playing/Paused pill or inner "Now Playing" title.
	heroTop := lines[1]
	if strings.Contains(heroTop, "playing") || strings.Contains(heroTop, "paused") {
		t.Fatalf("hero border duplicated playback state: %q", heroTop)
	}
	if count := strings.Count(stripped, "Now Playing"); count != 1 {
		t.Fatalf("\"Now Playing\" count = %d, want 1 across frame:\n%s", count, stripped)
	}

	// 4. Vibe backend badge appears once in the split card header, not duplicated in body.
	if count := strings.Count(stripped, "gemini:flash"); count != 1 {
		t.Fatalf("\"gemini:flash\" count = %d, want 1:\n%s", count, stripped)
	}
	if strings.Contains(stripped, "Vibe [") {
		t.Fatalf("unexpected duplicate Vibe inline badge in:\n%s", stripped)
	}

	// 5. Footer is one concise line on desktop widths, without legacy mode badges or top-bar tab copies.
	status := m.statusLines(m.statusWidth())
	if len(status) != 1 {
		t.Fatalf("expected 1 footer line at width=%d, got %d: %q", m.width, len(status), status)
	}
	footerLine := ansi.Strip(status[0])
	for _, legacyMode := range []string{"NORMAL", "QUEUE", "LIBRARY", "LYRICS", "FEED", "EQUALIZER", "ABOUT", "SEARCH", "CMD"} {
		if strings.Contains(footerLine, legacyMode) {
			t.Fatalf("footer %q unexpectedly contained legacy mode badge %q", footerLine, legacyMode)
		}
	}
	for _, dupTab := range []string{"library", "queue", "lyrics", "feed", "equalizer", "about"} {
		if strings.Contains(footerLine, dupTab) {
			t.Fatalf("footer %q duplicated top bar tab %q", footerLine, dupTab)
		}
	}

	// 6. Full panels keep their title once in the card border, without repeating inside body.
	panelChecks := []struct {
		panel any
		title string
	}{
		{m.eqP, "Equalizer"},
		{m.lyricsP, "Lyrics"},
		{m.feedP, "Feed"},
		{m.aboutP, "About"},
		{m.library, "Library"},
	}
	for _, pc := range panelChecks {
		for idx, p := range m.panels {
			if p == pc.panel {
				_ = m.activatePanel(idx)
				break
			}
		}
		panelStart := 1 + 2 + m.nowPlayingHeight()
		panelLines := strings.Split(ansi.Strip(m.View().Content), "\n")
		panelOut := strings.Join(panelLines[panelStart:panelStart+2+m.panelHeight()], "\n")
		if count := strings.Count(panelOut, pc.title); count != 1 {
			t.Fatalf("panel %q appeared %d times inside panel card, want 1 (card header only):\n%s", pc.title, count, panelOut)
		}
		if st := m.statusLines(m.statusWidth()); len(st) != 1 {
			t.Fatalf("panel %q footer wrapped to %d lines at w=%d, want 1: %q", pc.title, len(st), m.width, st)
		}
	}

	// 7. Search and command overlays keep a single unified footer line without mode badges.
	m.activePanel = -1
	m.openSearchMode()
	m.searchQuery = "daft"
	m.searchCursor = 4
	searchOut := ansi.Strip(m.View().Content)
	if count := strings.Count(searchOut, "daft"); count != 1 {
		t.Fatalf("search query count = %d, want 1 inside search card:\n%s", count, searchOut)
	}
	if st := m.statusLines(m.statusWidth()); len(st) != 1 || strings.Contains(ansi.Strip(st[0]), "SEARCH") {
		t.Fatalf("unexpected search footer: %q", st)
	}

	m.mode = modeCommand
	m.cmdBuf = "vol"
	cmdOut := ansi.Strip(m.View().Content)
	if strings.Contains(cmdOut, "Commands\n") {
		t.Fatalf("unexpected duplicate inline Commands header:\n%s", cmdOut)
	}
	if st := m.statusLines(m.statusWidth()); len(st) != 1 || strings.Contains(ansi.Strip(st[0]), "CMD") {
		t.Fatalf("unexpected command footer: %q", st)
	}

	// 8. Hero transport controls always have at least 1 blank row separating them from the progress bar,
	// have no directional prev/next arrows (◁/▷), and center the Play/Pause pill exactly at (colW - pillW) / 2.
	m.mode = modeNormal
	if strings.Contains(stripped, "◁") || strings.Contains(stripped, "▷") {
		t.Fatalf("unexpected directional prev/next arrows in Hero:\n%s", stripped)
	}
	colW := m.width - 4
	_, btns, _ := m.buildHeroControlsRow(m.playerState.Track, colW, 0, true)
	pillBtn := btns[2]
	pillW := pillBtn.x1 - pillBtn.x0
	if wantPillX := (colW - pillW) / 2; pillBtn.x0 != wantPillX {
		t.Fatalf("Play/Pause pill starts at x=%d, want centered x=%d (colW=%d, pillW=%d)", pillBtn.x0, wantPillX, colW, pillW)
	}
	for _, artOn := range []bool{false, true} {
		m.artMode = artOn
		_, meta := m.buildHeroArtContent(m.width-4, m.nowPlayingHeight(), 2)
		if gap := meta.ctrlY - meta.progY; gap < 2 {
			t.Fatalf("artMode=%v: controls row (y=%d) is too close to progress bar (y=%d); want gap >= 2", artOn, meta.ctrlY, meta.progY)
		}
	}
}
