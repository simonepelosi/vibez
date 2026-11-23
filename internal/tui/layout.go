package tui

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/art"
	"github.com/simone-vibes/vibez/internal/tui/styles"
	"github.com/simone-vibes/vibez/internal/tui/views"
)

type topHitKind uint8

const (
	topHitTab topHitKind = iota
	topHitSearch
	topHitVolume
)

type topHitZone struct {
	kind     topHitKind
	panelIdx int // -1 for Home when kind == topHitTab
	x0, x1   int
}

type navTabDef struct {
	key        string
	label      string
	shortLabel string
	panelIdx   int // -1 for Home, -2 for Search
}

type heroAction uint8

const (
	heroActNone heroAction = iota
	heroActRepeat
	heroActShuffle
	heroActPlayPause
	heroActFav
)

type heroButtonHit struct {
	action heroAction
	x0, x1 int
}

type heroHitMeta struct {
	innerH int
	progY  int
	progX0 int
	progX1 int
	ctrlY  int
	btnN   int
	btns   [4]heroButtonHit
}

type cardEdgeEntry struct {
	color color.Color
	left  string
	right string
}

type cardEdgeCache struct {
	hero   cardEdgeEntry
	active cardEdgeEntry
	dim    cardEdgeEntry
}

type cardTopRuleKey struct {
	cardW    int
	title    string
	suffix   string
	borderFg color.Color
	titleFg  color.Color
	mutedFg  color.Color
}

type cardTopRuleEntry struct {
	key cardTopRuleKey
	out string
}

type cardBotRuleKey struct {
	cardW    int
	borderFg color.Color
}

type cardBotRuleEntry struct {
	key cardBotRuleKey
	out string
}

type topBarStaticKey struct {
	valid     bool
	w         int
	active    int
	mode      viewMode
	debugView bool
	volPct    int
	muted     bool
	memStats  string
	themeGen  uint64
}

type topBarCacheKey struct {
	w         int
	active    int
	mode      viewMode
	debugView bool
	glowStep  int
	playing   bool
	volPct    int
	muted     bool
	memStats  string
	themeGen  uint64
}

type heroCtrlGlyphs struct {
	themeGen uint64
	repOff   string
	repAll   string
	repOne   string
	shufOff  string
	shufOn   string
	favNone  string
	favLove  string
	favHate  string
	playW    string
	pauseW   string
	playN    string
	pauseN   string
}

type heroControlsCacheKey struct {
	colW     int
	screenX0 int
	centered bool
	playing  bool
	loading  bool
	repeat   int
	shuffle  bool
	rating   provider.Rating
	glowStep int
	themeGen uint64
}

type heroTrackTitleKey struct {
	id, title, artist, rating string
	active                    bool
	themeGen                  uint64
}

type heroAlbumMetaKey struct {
	id, album, year, quality, catalog string
	master                            bool
	bitrate, contentW                 int
	themeGen                          uint64
}

type heroTotalDurKey struct {
	dur      time.Duration
	themeGen uint64
}

type heroWaveProgKey struct {
	pos, dur                 time.Duration
	colW, maxWaveW, screenX0 int
	centered                 bool
	themeGen                 uint64
}

type heroCardCacheKey struct {
	valid         bool
	contentW      int
	h             int
	screenX0      int
	artActive     bool
	artURL        string
	artCellAsp    float64
	hasTrack      bool
	trackID       string
	title         string
	artist        string
	album         string
	year          string
	contentRating string
	adm           bool
	outputBadge   string
	catalogBadge  string
	dur           time.Duration
	pos           time.Duration
	bitrate       int
	playing       bool
	loading       bool
	repeat        int
	shuffle       bool
	userRating    provider.Rating
	spinStep      int
	errMsg        string
	themeGen      uint64
}

type queueRowSig struct {
	id     string
	title  string
	artist string
	dur    time.Duration
}

type queuePanelCacheKey struct {
	valid      bool
	w, h       int
	offset     int
	total      int
	playingIdx int
	themeGen   uint64
}

type layoutCacheState struct {
	lastFrameLen int

	topRules    [8]cardTopRuleEntry
	topRuleNext int
	botRules    [8]cardBotRuleEntry
	botRuleNext int

	navThemeGen uint64
	navFull     [2][8]string // [0=inactive,1=active][tabIdx]
	navCompact  [2][8]string

	topBarStaticK    topBarStaticKey
	topBarPrefix     string
	topBarSuffix     string
	topBarBrandW     int
	topBarExactW     bool
	topBarStaticHits []topHitZone

	topBarKey  topBarCacheKey
	topBarStr  string
	topBarHits []topHitZone

	ctrlGlyphs   heroCtrlGlyphs
	ctrlCacheKey heroControlsCacheKey
	ctrlLine     string
	ctrlBtns     [4]heroButtonHit
	ctrlBtnN     int
	trackTitleK  heroTrackTitleKey
	trackTitleV  string
	albumMetaK   heroAlbumMetaKey
	albumMetaV   string
	totalDurK    heroTotalDurKey
	totalDurV    string
	waveProgK    heroWaveProgKey
	waveProgV    string
	waveProgX0   int
	waveProgX1   int

	heroKey    heroCardCacheKey
	heroArtRef []string
	heroRaw    []string
	heroPadded []string
	heroMeta   heroHitMeta

	qEmptyW      int
	qEmptyFg     color.Color
	qEmptyV      string
	qCacheKey    queuePanelCacheKey
	qRowSigs     []queueRowSig
	qRawLines    []string
	qPaddedLines []string

	vPaddedW     int
	vPaddedSrc   []string
	vPaddedLines []string
}

func (m *Model) statusWidth() int {
	return max(0, m.width-2)
}

func (m *Model) ensureHoriz(cols int) string {
	if cols <= 0 {
		return ""
	}
	if cols > m.cachedHorizW {
		m.cachedHorizW = cols
		m.cachedHoriz = strings.Repeat("─", cols)
	}
	return m.cachedHoriz[:cols*3]
}

func (m *Model) borderEdges(slot *cardEdgeEntry, st lipgloss.Style) (string, string) {
	fg := st.GetForeground()
	if slot.left != "" && slot.color == fg {
		return slot.left, slot.right
	}
	slot.color = fg
	slot.left = st.Render("│") + " "
	slot.right = " " + st.Render("│")
	return slot.left, slot.right
}

// renderModernLayout composes the responsive multi-card TUI layout:
//
//	Row 0:      ʕ•ᴥ•ʔ vibez ♪  ● Home  l Library  q Queue ...   AAC 256 · ♪ 72%
//	Hero Card:  ╭─ Now Playing · ♪ playing ────────────────────────────────────╮
//	            │ Cover (optional, left) │ Artist — Title / ╱╲ wave / Controls │
//	            ╰──────────────────────────────────────────────────────────────╯
//	Split/View: ╭─ Queue · 12 tracks ────────╮ ╭─ Vibe · ✨ gemini ────────────╮
//	            │ ...                        │ │ ...                           │
//	            ╰────────────────────────────╯ ╰───────────────────────────────╯
//	Footer:     NORMAL  : command · / search · l library ...
func clampLines(s string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	seen := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			seen++
			if seen == maxLines {
				return s[:i]
			}
		}
	}
	return s
}

func (m *Model) renderModernLayout() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	statusPadded := m.statusLinesPadded(m.statusWidth())
	npH := m.nowPlayingHeight()
	panelH := m.panelHeightWithStatus(len(statusPadded))

	var sb strings.Builder
	grow := m.layoutCache.lastFrameLen
	if grow < 1024 {
		grow = max(1024, m.width*m.height*2)
	} else {
		grow += 128
	}
	sb.Grow(grow)

	// ── Top Bar & Clickable Tabs ──
	topBar, _ := m.buildTopBar(m.width)
	sb.WriteString(topBar)
	sb.WriteByte('\n')

	// ── Hero Card: Now Playing ──
	heroPadded, _ := m.buildHeroViewDataPadded(m.width)
	heroEdgeL, heroEdgeR := m.borderEdges(&m.cardEdges.hero, styles.HeroBorder)
	heroSuffix := m.heroHeaderSuffix()
	sb.WriteString(m.renderCardTopRule("Now Playing", heroSuffix, m.width, styles.HeroBorder, styles.CardTitleActive, styles.QueueItemMuted))
	sb.WriteByte('\n')
	heroInnerW := max(0, m.width-4)
	for i := range npH {
		writePrePaddedCardRow(&sb, safeIdx(heroPadded, i), heroInnerW, heroEdgeL, heroEdgeR)
		sb.WriteByte('\n')
	}
	sb.WriteString(m.renderCardBottomRule(m.width, styles.HeroBorder))
	sb.WriteByte('\n')

	// ── Bottom Panel Area ──
	activePanel, activeTitle, activeSuffix, panelBorderSt, panelTitleSt := m.activeFullPanelSpec()
	switch {
	case activePanel != nil:
		m.renderFullCardPanel(&sb, activeTitle, activeSuffix, m.width, panelH, panelBorderSt, panelTitleSt, activePanel)
	default:
		m.renderSplitCards(&sb, m.width, panelH)
	}
	sb.WriteByte('\n')

	// ── Status Dock ──
	for i, line := range statusPadded {
		sb.WriteByte(' ')
		sb.WriteString(line)
		if m.width >= 2 {
			sb.WriteByte(' ')
		}
		if i < len(statusPadded)-1 {
			sb.WriteByte('\n')
		}
	}

	out := sb.String()
	m.layoutCache.lastFrameLen = len(out)
	// Clamp on ultra-short terminals so total rows never exceed m.height.
	return clampLines(out, m.height)
}

func (m *Model) heroHeaderSuffix() string {
	var parts []string
	if m.playerState.Track == nil {
		parts = append(parts, "idle")
	}
	if h := m.hybridHeaderLabel(); h != "" {
		parts = append(parts, h)
	}
	return strings.Join(parts, " · ")
}

func (m *Model) hybridHeaderLabel() string {
	if !m.hybrid.enabled {
		return ""
	}
	switch {
	case m.hybrid.scanning:
		if m.hybrid.count > 0 {
			return fmt.Sprintf("hybrid: rescanning (%d)", m.hybrid.count)
		}
		return "hybrid: scanning…"
	case m.hybrid.err != "":
		return "hybrid: " + m.hybrid.err
	case m.hybrid.count == 1:
		return "hybrid: 1 track"
	default:
		return fmt.Sprintf("hybrid: %d tracks", m.hybrid.count)
	}
}

func (m *Model) activeFullPanelSpec() (linesFn func(w, h int) []string, title, suffix string, borderSt, titleSt lipgloss.Style) {
	innerW := max(0, m.width-4)
	switch {
	case m.debugView:
		return m.debugLogLines, "Debug Logs", "tail", styles.CardBorderActive, styles.CardTitleActive
	case m.mode == modeSearch:
		_, _, _, sbSt, stSt := styles.NavTabStyles(styles.NavTabSearchIdx)
		return m.searchLines, "Search", "", sbSt, stSt
	case m.mode == modeCommand:
		return m.commandLines, "Command Palette", "", styles.CardBorderActive, styles.NowPlayingLabel
	case m.activePanel >= 0 && m.activePanel < len(m.panels):
		p := m.panels[m.activePanel]
		tabIdx := styles.NavTabHomeIdx
		switch p {
		case m.library:
			title = "Library"
			tabIdx = styles.NavTabLibraryIdx
		case m.queue:
			title = "Queue"
			tabIdx = styles.NavTabQueueIdx
			if n := len(m.queueTracks); n > 0 {
				suffix = fmt.Sprintf("%d tracks", n)
			}
		case m.lyricsP:
			title = "Lyrics"
			tabIdx = styles.NavTabLyricsIdx
		case m.feedP:
			title = "Feed"
			tabIdx = styles.NavTabFeedIdx
		case m.eqP:
			title = "Equalizer"
			suffix = "10-band"
			tabIdx = styles.NavTabEQIdx
		case m.aboutP:
			title = "About"
			tabIdx = styles.NavTabAboutIdx
		default:
			title = p.NavLabel()
		}
		_, _, _, bSt, tSt := styles.NavTabStyles(tabIdx)
		return func(_, h int) []string {
			p.SetSize(innerW, h)
			return toLines(p.View(), h)
		}, title, suffix, bSt, tSt
	}
	return nil, "", "", styles.CardBorderActive, styles.CardTitleActive
}

func (m *Model) renderFullCardPanel(sb *strings.Builder, title, suffix string, cardW, panelH int, borderSt, titleSt lipgloss.Style, renderLines func(w, h int) []string) {
	edgeL, edgeR := m.borderEdges(&m.cardEdges.active, borderSt)
	sb.WriteString(m.renderCardTopRule(title, suffix, cardW, borderSt, titleSt, styles.QueueItemMuted))
	sb.WriteByte('\n')
	innerW := max(0, cardW-4)
	lines := renderLines(innerW, panelH)
	for i := range panelH {
		writeStyledCardRow(sb, safeIdx(lines, i), innerW, edgeL, edgeR)
		sb.WriteByte('\n')
	}
	sb.WriteString(m.renderCardBottomRule(cardW, borderSt))
}

func (m *Model) splitCardWidths(totalW int) (leftW, gap, rightW int) {
	if totalW >= 56 {
		gap = 1
		leftW = (totalW - gap) / 2
		rightW = totalW - leftW - gap
		return leftW, gap, rightW
	}
	leftW = totalW / 2
	rightW = totalW - leftW
	return leftW, 0, rightW
}

func (m *Model) renderSplitCards(sb *strings.Builder, totalW, panelH int) {
	leftW, gap, rightW := m.splitCardWidths(totalW)
	leftInnerW := max(0, leftW-4)
	rightInnerW := max(0, rightW-4)

	vibeActive := m.vibe.IsFocused() || m.vibe.PickerActive()
	_, _, _, _, leftTitleSt := styles.NavTabStyles(styles.NavTabQueueIdx)
	rightTabIdx := styles.NavTabHomeIdx
	if m.discovery.enabled {
		rightTabIdx = styles.NavTabFeedIdx
	}
	_, _, _, rightActiveBorderSt, rightTitleSt := styles.NavTabStyles(rightTabIdx)
	leftBorderSt := styles.CardBorder
	rightBorderSt := styles.CardBorder
	if vibeActive {
		rightBorderSt = rightActiveBorderSt
	}

	leftEdgeL, leftEdgeR := m.borderEdges(&m.cardEdges.dim, leftBorderSt)
	var rightEdgeL, rightEdgeR string
	if vibeActive {
		rightEdgeL, rightEdgeR = m.borderEdges(&m.cardEdges.active, rightBorderSt)
	} else {
		rightEdgeL, rightEdgeR = m.borderEdges(&m.cardEdges.dim, rightBorderSt)
	}

	qSuffix := ""
	if n := len(m.queueTracks); n > 0 {
		qSuffix = fmt.Sprintf("%d tracks", n)
	}
	vTitle := "Vibe"
	vSuffix := ""
	if m.discovery.enabled {
		vTitle = "Discovery"
		vSuffix = "● live"
	} else if badge := m.vibe.BackendBadge(); badge != "" {
		vSuffix = badge
	}

	sb.WriteString(m.renderCardTopRule("Queue", qSuffix, leftW, leftBorderSt, leftTitleSt, styles.QueueItemMuted))
	if gap > 0 {
		writeSpaces(sb, gap)
	}
	sb.WriteString(m.renderCardTopRule(vTitle, vSuffix, rightW, rightBorderSt, rightTitleSt, styles.QueueItemMuted))
	sb.WriteByte('\n')

	qLines := m.queuePanelLinesPadded(leftInnerW, panelH)
	vLines := m.vibePanelLinesPadded(rightInnerW, panelH)
	for i := range panelH {
		writePrePaddedCardRow(sb, safeIdx(qLines, i), leftInnerW, leftEdgeL, leftEdgeR)
		if gap > 0 {
			writeSpaces(sb, gap)
		}
		writePrePaddedCardRow(sb, safeIdx(vLines, i), rightInnerW, rightEdgeL, rightEdgeR)
		sb.WriteByte('\n')
	}

	sb.WriteString(m.renderCardBottomRule(leftW, leftBorderSt))
	if gap > 0 {
		writeSpaces(sb, gap)
	}
	sb.WriteString(m.renderCardBottomRule(rightW, rightBorderSt))
}

func (m *Model) renderCardTopRule(title, suffix string, cardW int, borderSt, titleSt, mutedSt lipgloss.Style) string {
	if cardW <= 0 {
		return ""
	}
	if cardW == 1 {
		return borderSt.Render("╭")
	}
	if cardW == 2 {
		return borderSt.Render("╭╮")
	}

	key := cardTopRuleKey{
		cardW:    cardW,
		title:    title,
		suffix:   suffix,
		borderFg: borderSt.GetForeground(),
		titleFg:  titleSt.GetForeground(),
		mutedFg:  mutedSt.GetForeground(),
	}
	for i := range m.layoutCache.topRules {
		e := m.layoutCache.topRules[i]
		if e.out != "" && e.key == key {
			return e.out
		}
	}

	avail := cardW - 2 // between ╭ and ╮
	var out string
	if title == "" || avail < 5 {
		out = borderSt.Render("╭" + m.ensureHoriz(avail) + "╮")
	} else {
		var head strings.Builder
		used := 1 // leading "─"
		head.WriteString(borderSt.Render("╭─"))
		maxTitleW := max(1, avail-3)
		tStr := ansi.Truncate(title, maxTitleW, "…")
		tStyled := " " + titleSt.Render(tStr)
		tw := lipgloss.Width(tStyled)
		head.WriteString(tStyled)
		used += tw

		if suffix != "" && avail-used >= 6 {
			suf := " · " + suffix
			maxSufW := avail - used - 2
			if lipgloss.Width(suf) > maxSufW {
				suf = ansi.Truncate(suf, maxSufW, "…")
			}
			if lipgloss.Width(suf) >= 3 {
				sStyled := mutedSt.Render(suf)
				used += lipgloss.Width(sStyled)
				head.WriteString(sStyled)
			}
		}

		if avail-used >= 1 {
			head.WriteByte(' ')
			used++
		}
		fill := max(0, avail-used)
		if fill > 0 {
			head.WriteString(borderSt.Render(m.ensureHoriz(fill) + "╮"))
		} else {
			head.WriteString(borderSt.Render("╮"))
		}
		out = head.String()
	}

	idx := m.layoutCache.topRuleNext
	m.layoutCache.topRules[idx] = cardTopRuleEntry{key: key, out: out}
	m.layoutCache.topRuleNext = (idx + 1) % len(m.layoutCache.topRules)
	return out
}

func (m *Model) renderCardBottomRule(cardW int, borderSt lipgloss.Style) string {
	if cardW <= 0 {
		return ""
	}
	if cardW == 1 {
		return borderSt.Render("╰")
	}
	if cardW == 2 {
		return borderSt.Render("╰╯")
	}
	key := cardBotRuleKey{cardW: cardW, borderFg: borderSt.GetForeground()}
	for i := range m.layoutCache.botRules {
		e := m.layoutCache.botRules[i]
		if e.out != "" && e.key == key {
			return e.out
		}
	}
	out := borderSt.Render("╰" + m.ensureHoriz(cardW-2) + "╯")
	idx := m.layoutCache.botRuleNext
	m.layoutCache.botRules[idx] = cardBotRuleEntry{key: key, out: out}
	m.layoutCache.botRuleNext = (idx + 1) % len(m.layoutCache.botRules)
	return out
}

func writeStyledCardRow(sb *strings.Builder, line string, innerW int, edgeL, edgeR string) {
	if innerW <= 0 {
		sb.WriteString(strings.TrimRight(edgeL, " "))
		return
	}
	sb.WriteString(edgeL)
	writePadded(sb, line, innerW)
	sb.WriteString(edgeR)
}

func writePrePaddedCardRow(sb *strings.Builder, paddedLine string, innerW int, edgeL, edgeR string) {
	if innerW <= 0 {
		sb.WriteString(strings.TrimRight(edgeL, " "))
		return
	}
	sb.WriteString(edgeL)
	if paddedLine == "" {
		writeSpaces(sb, innerW)
	} else {
		sb.WriteString(paddedLine)
	}
	sb.WriteString(edgeR)
}

func (m *Model) vibePanelLinesPadded(w, h int) []string {
	vLines := m.vibe.Lines(w, h, m.glowStep)
	if w <= 0 || h <= 0 {
		return nil
	}
	c := &m.layoutCache
	if c.vPaddedW == w &&
		len(c.vPaddedSrc) == len(vLines) &&
		len(c.vPaddedLines) == h &&
		(len(vLines) == 0 || &vLines[0] == &c.vPaddedSrc[0]) {
		return c.vPaddedLines
	}
	padded := make([]string, h)
	for i := range h {
		if i < len(vLines) && vLines[i] != "" {
			padded[i] = padRight(vLines[i], w)
		}
	}
	c.vPaddedW = w
	c.vPaddedSrc = vLines
	c.vPaddedLines = padded
	return padded
}

func (m *Model) navTabSpecs() [8]navTabDef {
	panelIdxFor := func(target ContentView) int {
		for i, p := range m.panels {
			if p == target {
				return i
			}
		}
		return -1
	}
	return [8]navTabDef{
		{key: "", label: "Home", shortLabel: "Home", panelIdx: -1},
		{key: "l", label: "Library", shortLabel: "Lib", panelIdx: panelIdxFor(m.library)},
		{key: "q", label: "Queue", shortLabel: "Queue", panelIdx: panelIdxFor(m.queue)},
		{key: "y", label: "Lyrics", shortLabel: "Lyr", panelIdx: panelIdxFor(m.lyricsP)},
		{key: "F", label: "Feed", shortLabel: "Feed", panelIdx: panelIdxFor(m.feedP)},
		{key: "e", label: "EQ", shortLabel: "EQ", panelIdx: panelIdxFor(m.eqP)},
		{key: "/", label: "Search", shortLabel: "Find", panelIdx: -2},
		{key: "?", label: "About", shortLabel: "?", panelIdx: panelIdxFor(m.aboutP)},
	}
}

func (m *Model) isNavTabActive(tab navTabDef) bool {
	if tab.panelIdx == -2 {
		return m.mode == modeSearch
	}
	if m.debugView || m.mode == modeSearch || m.mode == modeCommand {
		return false
	}
	if tab.panelIdx == -1 {
		return m.activePanel == -1
	}
	return m.activePanel == tab.panelIdx
}

func (m *Model) renderNavTab(idx int, tab navTabDef, active, compact bool) string {
	lbl := tab.label
	if compact {
		lbl = tab.shortLabel
	}
	activeSt, keySt, labelSt, _, _ := styles.NavTabStyles(idx)
	if active {
		return activeSt.Render("● " + lbl)
	}
	if tab.key == "" {
		return " " + keySt.Render("◆") + " " + labelSt.Render(lbl) + " "
	}
	if tab.key == "?" && compact {
		return " " + keySt.Render("?") + " "
	}
	return " " + keySt.Render(tab.key) + " " + labelSt.Render(lbl) + " "
}

func (m *Model) renderNavTabCached(idx int, tab navTabDef, active, compact bool, themeGen uint64) string {
	if m.layoutCache.navThemeGen != themeGen {
		m.layoutCache.navThemeGen = themeGen
		m.layoutCache.navFull = [2][8]string{}
		m.layoutCache.navCompact = [2][8]string{}
	}
	actIdx := 0
	if active {
		actIdx = 1
	}
	table := &m.layoutCache.navFull
	if compact {
		table = &m.layoutCache.navCompact
	}
	if cached := table[actIdx][idx]; cached != "" {
		return cached
	}
	s := m.renderNavTab(idx, tab, active, compact)
	table[actIdx][idx] = s
	return s
}

func (m *Model) buildTopBar(w int) (string, []topHitZone) {
	if w <= 0 {
		return "", nil
	}

	themeGen := styles.ThemeGen()
	volPct := int(m.playerState.Volume * 100)
	muted := m.preMuteVol >= 0
	key := topBarCacheKey{
		w:         w,
		active:    m.activePanel,
		mode:      m.mode,
		debugView: m.debugView,
		glowStep:  m.glowStep,
		playing:   m.playerState.Playing,
		volPct:    volPct,
		muted:     muted,
		memStats:  m.memStats,
		themeGen:  themeGen,
	}
	if m.layoutCache.topBarStr != "" && m.layoutCache.topBarKey == key {
		return m.layoutCache.topBarStr, m.layoutCache.topBarHits
	}

	bear := padRight(views.BearExpr(m.glowStep, m.playerState.Playing), 5)
	brandText := "vibez ♪"
	if w < 52 {
		brandText = "vibez"
	}
	brand := bear + " " + views.RenderGlowTitle(brandText, m.glowStep)

	staticKey := topBarStaticKey{
		valid:     true,
		w:         w,
		active:    m.activePanel,
		mode:      m.mode,
		debugView: m.debugView,
		volPct:    volPct,
		muted:     muted,
		memStats:  m.memStats,
		themeGen:  themeGen,
	}
	if m.layoutCache.topBarStaticK == staticKey && m.layoutCache.topBarExactW {
		out := m.layoutCache.topBarPrefix + brand + m.layoutCache.topBarSuffix
		m.layoutCache.topBarKey = key
		m.layoutCache.topBarStr = out
		m.layoutCache.topBarHits = m.layoutCache.topBarStaticHits
		return out, m.layoutCache.topBarStaticHits
	}

	var volStr string
	if muted {
		volStr = styles.Playing.Render("🔇mute")
	} else {
		volStr = styles.NowPlayingTime.Render("♪") + styles.QueueItem.Render(fmt.Sprintf(" %3d%%", volPct))
	}
	volW := lipgloss.Width(volStr)

	metaPrefix := ""
	if m.memStats != "" && w >= 76 {
		metaPrefix = styles.QueueItemMuted.Render(m.memStats) + "  "
	}
	rightStr := metaPrefix + volStr

	brandW := lipgloss.Width(brand)
	rightW := lipgloss.Width(rightStr)
	availTabsW := w - 2 - brandW - rightW - 2 // 1 col outer pad each side + min gaps

	tabs := m.navTabSpecs()
	type renderedTab struct {
		str  string
		w    int
		spec navTabDef
	}
	var tabBuf [8]renderedTab
	tabCount := 0
	const sepW = 1

	measureTabs := func(compact bool) ([8]renderedTab, int, int) {
		var items [8]renderedTab
		tot := 0
		for i, tab := range tabs {
			s := m.renderNavTabCached(i, tab, m.isNavTabActive(tab), compact, themeGen)
			sw := lipgloss.Width(s)
			if i > 0 {
				tot += sepW
			}
			tot += sw
			items[i] = renderedTab{str: s, w: sw, spec: tab}
		}
		return items, len(tabs), tot
	}

	fullItems, fullCount, fullW := measureTabs(false)
	switch {
	case availTabsW >= fullW:
		tabBuf = fullItems
		tabCount = fullCount
	case availTabsW >= 10:
		compactItems, compactCount, compactW := measureTabs(true)
		if availTabsW >= compactW {
			tabBuf = compactItems
			tabCount = compactCount
		} else {
			// Micro mode: keep active pill (if not overflowing) plus distinct colored key pills.
			used := 0
			for i, tab := range tabs {
				act := m.isNavTabActive(tab)
				actSt, keySt, lblSt, _, _ := styles.NavTabStyles(i)
				var s string
				switch {
				case act:
					s = actSt.Render(tab.shortLabel)
				case tab.key != "":
					s = keySt.Render(tab.key)
				default:
					s = lblSt.Render("Home")
				}
				sw := lipgloss.Width(s)
				need := sw
				if tabCount > 0 {
					need += sepW
				}
				if used+need > availTabsW {
					break
				}
				used += need
				tabBuf[tabCount] = renderedTab{str: s, w: sw, spec: tab}
				tabCount++
			}
		}
	}
	tabItems := tabBuf[:tabCount]

	hits := make([]topHitZone, 0, tabCount+2)
	prefix := ""
	curX := 0
	if w >= 40 {
		prefix = " "
		curX++
	}

	hits = append(hits, topHitZone{kind: topHitTab, panelIdx: -1, x0: curX, x1: curX + brandW})
	curX += brandW

	tabsW := 0
	for i, it := range tabItems {
		if i > 0 {
			tabsW += sepW
		}
		tabsW += it.w
	}

	rightPadEdge := 0
	if w >= 40 {
		rightPadEdge = 1
	}
	gapAfterBrand := 0
	gapBeforeRight := max(1, w-curX-tabsW-rightW-rightPadEdge)
	if len(tabItems) > 0 {
		// Center the navigation tabs across the full header width w while clamping
		// safely between the left brand and right volume indicator. Because tabsW
		// is constant across tab switches and animation ticks, (w-tabsW)/2 keeps
		// every tab at an immutable X coordinate.
		spare := max(0, w-curX-tabsW-rightW-rightPadEdge)
		minLeftGap := min(1, spare)
		maxLeftGap := max(0, spare-min(1, spare))
		idealLeftGap := (w-tabsW)/2 - curX
		gapAfterBrand = max(minLeftGap, min(maxLeftGap, idealLeftGap))
		gapBeforeRight = max(1, spare-gapAfterBrand)
	}

	var suffixBuf strings.Builder
	suffixBuf.Grow(w * 2)

	writeSpaces(&suffixBuf, gapAfterBrand)
	curX += gapAfterBrand

	for i, it := range tabItems {
		if i > 0 {
			suffixBuf.WriteByte(' ')
			curX++
		}
		kind := topHitTab
		if it.spec.panelIdx == -2 {
			kind = topHitSearch
		}
		hits = append(hits, topHitZone{kind: kind, panelIdx: it.spec.panelIdx, x0: curX, x1: curX + it.w})
		suffixBuf.WriteString(it.str)
		curX += it.w
	}

	if curX+gapBeforeRight+rightW+rightPadEdge <= w {
		writeSpaces(&suffixBuf, gapBeforeRight)
		curX += gapBeforeRight
		metaW := lipgloss.Width(metaPrefix)
		suffixBuf.WriteString(rightStr)
		volStart := curX + metaW
		hits = append(hits, topHitZone{kind: topHitVolume, x0: volStart, x1: volStart + volW})
		curX += rightW
	} else {
		// Ultra-narrow fallback: ensure volume always remains visible.
		spaceLeft := max(0, w-curX-volW)
		writeSpaces(&suffixBuf, spaceLeft)
		curX += spaceLeft
		suffixBuf.WriteString(volStr)
		hits = append(hits, topHitZone{kind: topHitVolume, x0: curX, x1: curX + volW})
		curX += volW
	}

	if rem := w - curX; rem > 0 {
		writeSpaces(&suffixBuf, rem)
	}

	suffix := suffixBuf.String()
	out := prefix + brand + suffix
	exactW := lipgloss.Width(out) == w
	if !exactW {
		out = padRight(out, w)
	}
	m.layoutCache.topBarStaticK = staticKey
	m.layoutCache.topBarPrefix = prefix
	m.layoutCache.topBarSuffix = suffix
	m.layoutCache.topBarBrandW = brandW
	m.layoutCache.topBarExactW = exactW
	m.layoutCache.topBarStaticHits = hits
	m.layoutCache.topBarKey = key
	m.layoutCache.topBarStr = out
	m.layoutCache.topBarHits = hits
	return out, hits
}

// artModeActive reports whether the now-playing hero is currently showing
// (or loading) the album-art view rather than text-only layout.
func (m *Model) artModeActive() bool {
	if !m.artMode || m.supportsArtColor == nil {
		return false
	}
	t := m.playerState.Track
	if t == nil || t.ArtworkURL == "" || m.artwork.url != t.ArtworkURL || m.artwork.failed {
		return false
	}
	return m.supportsArtColor()
}

func (m *Model) canFitSideBySideArt(contentW, availH int) (ok bool, artRows, artCols int) {
	if !m.artModeActive() || availH < 5 {
		return false, 0, 0
	}
	aspect := m.artCellAsp
	if aspect <= 0 {
		aspect = 2.0
	}
	artRows = availH
	artCols = int(math.Round(float64(artRows) * aspect))
	const gutter = 3
	const minRightW = 28
	for artRows > 5 && contentW < artCols+gutter+minRightW {
		artRows--
		artCols = int(math.Round(float64(artRows) * aspect))
	}
	if artRows < 5 || artCols < 4 || contentW < artCols+gutter+minRightW {
		return false, 0, 0
	}
	return true, artRows, artCols
}

func (m *Model) nowPlayingHeight() int {
	if !m.artModeActive() {
		return 7
	}
	target := min(14, max(7, (m.height-14)/2))
	if m.width > 0 {
		contentW := max(0, m.width-4)
		if ok, _, _ := m.canFitSideBySideArt(contentW, target); !ok {
			return 7
		}
	}
	return target
}

func (m *Model) nowPlayingLines(contentW, h int) []string {
	lines, _ := m.buildHeroContent(contentW, h, 0)
	return lines
}

func (m *Model) nowPlayingArtLines(contentW, h int) []string {
	lines, _ := m.buildHeroArtContent(contentW, h, 0)
	return lines
}

func (m *Model) nowPlayingTextLines(contentW, h int) []string {
	lines, _ := m.buildHeroTextContent(contentW, h, 0)
	return lines
}

func (m *Model) buildHeroViewData(cardW int) ([]string, heroHitMeta) {
	npH := m.nowPlayingHeight()
	contentW := max(0, cardW-4)
	raw, _, meta := m.buildHeroContentCached(contentW, npH, 2) // "│ " is 2 cols wide
	meta.innerH = npH
	return raw, meta
}

func (m *Model) buildHeroViewDataPadded(cardW int) ([]string, heroHitMeta) {
	npH := m.nowPlayingHeight()
	contentW := max(0, cardW-4)
	_, padded, meta := m.buildHeroContentCached(contentW, npH, 2) // "│ " is 2 cols wide
	meta.innerH = npH
	return padded, meta
}

func (m *Model) buildHeroContent(contentW, h, screenX0 int) ([]string, heroHitMeta) {
	raw, _, meta := m.buildHeroContentCached(contentW, h, screenX0)
	return raw, meta
}

func (m *Model) buildHeroContentCached(contentW, h, screenX0 int) (raw, padded []string, meta heroHitMeta) {
	if h <= 0 {
		return nil, nil, heroHitMeta{innerH: h, progY: -1, ctrlY: -1}
	}

	useArt := false
	var artSize art.Size
	if m.artModeActive() && h >= 5 {
		if ok, artRows, artCols := m.canFitSideBySideArt(contentW, h); ok {
			useArt = true
			artSize = art.Size{Width: artCols, Height: artRows}
		}
	}

	t := m.playerState.Track
	spinStep := 0
	if m.playerState.Loading {
		spinStep = m.glowStep % 10
	}
	key := heroCardCacheKey{
		valid:      true,
		contentW:   contentW,
		h:          h,
		screenX0:   screenX0,
		artActive:  useArt,
		artURL:     m.artwork.url,
		artCellAsp: m.artCellAsp,
		hasTrack:   t != nil,
		pos:        m.playerState.Position,
		bitrate:    m.playerState.Bitrate,
		playing:    m.playerState.Playing,
		loading:    m.playerState.Loading,
		repeat:     m.playerState.RepeatMode,
		shuffle:    m.playerState.ShuffleMode,
		spinStep:   spinStep,
		errMsg:     m.errMsg,
		themeGen:   styles.ThemeGen(),
	}
	if t != nil {
		outBadge, catBadge := formatBadge(t, m.playerState.Bitrate)
		key.trackID = t.ID
		key.title = t.Title
		key.artist = t.Artist
		key.album = t.Album
		key.year = t.ReleaseYear()
		key.contentRating = t.ContentRating
		key.adm = t.IsAppleDigitalMaster
		key.outputBadge = outBadge
		key.catalogBadge = catBadge
		key.dur = t.Duration
		key.userRating = m.trackRating(t)
	}

	c := &m.layoutCache
	if c.heroKey == key && len(c.heroRaw) == h && len(c.heroPadded) == h {
		artMatch := true
		if useArt {
			artLines := m.artwork.rendered[artSize]
			if len(artLines) != len(c.heroArtRef) {
				artMatch = false
			} else if len(artLines) > 0 {
				artMatch = &artLines[0] == &c.heroArtRef[0]
			} else {
				artMatch = m.artwork.img == nil
			}
		}
		if artMatch {
			return c.heroRaw, c.heroPadded, c.heroMeta
		}
	}

	if useArt {
		raw, meta = m.buildHeroArtContent(contentW, h, screenX0)
	} else {
		raw, meta = m.buildHeroTextContent(contentW, h, screenX0)
	}
	padded = make([]string, len(raw))
	for i, line := range raw {
		if line != "" {
			padded[i] = padRight(line, contentW)
		}
	}
	c.heroKey = key
	if useArt {
		c.heroArtRef = m.artwork.rendered[artSize]
	} else {
		c.heroArtRef = nil
	}
	c.heroRaw = raw
	c.heroPadded = padded
	c.heroMeta = meta
	return raw, padded, meta
}

func (m *Model) renderTrackTitleText(t *provider.Track) string {
	themeGen := styles.ThemeGen()
	active := m.playerState.Playing || m.playerState.Loading
	key := heroTrackTitleKey{
		id:       t.ID,
		title:    t.Title,
		artist:   t.Artist,
		rating:   t.ContentRating,
		active:   active,
		themeGen: themeGen,
	}
	if m.layoutCache.trackTitleV != "" && m.layoutCache.trackTitleK == key {
		return m.layoutCache.trackTitleV
	}

	muted := styles.QueueItemMuted
	var titleStr string
	if active {
		titleStr = styles.NowPlayingTitlePlaying.Render(t.Title)
	} else {
		titleStr = styles.NowPlayingTitle.Render(t.Title)
	}
	if strings.EqualFold(t.ContentRating, "explicit") {
		titleStr += " " + muted.Render("🅴")
	}
	out := styles.NowPlayingTrackArtist.Render(t.Artist) + muted.Render(" — ") + titleStr
	m.layoutCache.trackTitleK = key
	m.layoutCache.trackTitleV = out
	return out
}

func (m *Model) renderTrackTitleLine(t *provider.Track, contentW int, truncate bool) string {
	line := m.renderTrackTitleText(t)
	if truncate {
		line = ansi.Truncate(line, contentW, "…")
	}
	return centerStr(line, contentW)
}

func (m *Model) renderAlbumMetaText(t *provider.Track, contentW int, truncate, includeTime bool) string {
	year := t.ReleaseYear()
	output, catalog := formatBadge(t, m.playerState.Bitrate)
	if output == "" && catalog == "" && m.playerState.Bitrate > 0 {
		output = qualityLabel(m.playerState.Bitrate)
	}
	var albumKey heroAlbumMetaKey
	canCache := !includeTime && truncate
	if canCache {
		albumKey = heroAlbumMetaKey{
			id:       t.ID,
			album:    t.Album,
			year:     year,
			quality:  output,
			catalog:  catalog,
			master:   t.IsAppleDigitalMaster,
			bitrate:  m.playerState.Bitrate,
			contentW: contentW,
			themeGen: styles.ThemeGen(),
		}
		if m.layoutCache.albumMetaV != "" && m.layoutCache.albumMetaK == albumKey {
			return m.layoutCache.albumMetaV
		}
	}

	albumText := t.Album
	if year != "" && albumText != "" {
		albumText += " (" + year + ")"
	}
	elapsed := views.FormatDuration(m.playerState.Position)
	total := views.FormatDuration(t.Duration)

	build := func(withFormat bool) string {
		var sb strings.Builder
		if albumText != "" {
			sb.WriteString(styles.NowPlayingAlbum.Render(albumText))
		}
		if t.IsAppleDigitalMaster {
			if sb.Len() > 0 {
				sb.WriteString(styles.NowPlayingAlbum.Render(" · "))
			}
			sb.WriteString(styles.NowPlayingLabel.Render("✦ Master"))
		}
		if withFormat && (output != "" || catalog != "") {
			if sb.Len() > 0 {
				sb.WriteString(styles.NowPlayingAlbum.Render(" · "))
			}
			if output != "" {
				sb.WriteString(styles.NowPlayingLabel.Render(output))
			}
			if catalog != "" {
				if output != "" {
					sb.WriteByte(' ')
				}
				sb.WriteString(styles.NowPlayingAlbum.Render("(catalog: " + catalog + ")"))
			}
		}
		if includeTime {
			if sb.Len() > 0 {
				sb.WriteString(styles.NowPlayingAlbum.Render(" · "))
			}
			sb.WriteString(styles.NowPlayingTime.Render(elapsed + " / " + total))
		}
		return sb.String()
	}

	line := build(true)
	if truncate && lipgloss.Width(line) > contentW {
		line = build(false)
		line = ansi.Truncate(line, contentW, "…")
	}
	if canCache {
		m.layoutCache.albumMetaK = albumKey
		m.layoutCache.albumMetaV = line
	}
	return line
}

func (m *Model) renderAlbumMetaLine(t *provider.Track, contentW int, truncate bool) string {
	return centerStr(m.renderAlbumMetaText(t, contentW, truncate, true), contentW)
}

func (m *Model) cachedTotalDuration(dur time.Duration) string {
	key := heroTotalDurKey{dur: dur, themeGen: styles.ThemeGen()}
	if m.layoutCache.totalDurV != "" && m.layoutCache.totalDurK == key {
		return m.layoutCache.totalDurV
	}
	s := styles.QueueItemMuted.Render(fmt.Sprintf("%-5s", views.FormatDuration(dur)))
	m.layoutCache.totalDurK = key
	m.layoutCache.totalDurV = s
	return s
}

func (m *Model) buildWaveProgressRow(t *provider.Track, colW, maxWaveW, screenX0 int, centered bool) (line string, progX0, progX1 int) {
	key := heroWaveProgKey{
		pos:      m.playerState.Position,
		dur:      t.Duration,
		colW:     colW,
		maxWaveW: maxWaveW,
		screenX0: screenX0,
		centered: centered,
		themeGen: styles.ThemeGen(),
	}
	if m.layoutCache.waveProgV != "" && m.layoutCache.waveProgK == key {
		return m.layoutCache.waveProgV, m.layoutCache.waveProgX0, m.layoutCache.waveProgX1
	}

	elapsed := fmt.Sprintf("%5s", views.FormatDuration(m.playerState.Position))
	left := styles.NowPlayingTime.Render(elapsed)
	right := m.cachedTotalDuration(t.Duration)
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)

	const gap = 2
	waveW := max(8, colW-leftW-rightW-gap*2)
	if maxWaveW > 0 && waveW > maxWaveW {
		waveW = maxWaveW
	}
	wave := views.RenderProgressBar(m.playerState.Position, t.Duration, waveW)
	full := left + strings.Repeat(" ", gap) + wave + strings.Repeat(" ", gap) + right
	fullW := lipgloss.Width(full)

	pad := 0
	if centered && colW > fullW {
		pad = (colW - fullW) / 2
		full = strings.Repeat(" ", pad) + full
	}
	progX0 = screenX0 + pad + leftW + gap
	progX1 = progX0 + waveW
	out := ansi.Truncate(full, colW, "")
	m.layoutCache.waveProgK = key
	m.layoutCache.waveProgV = out
	m.layoutCache.waveProgX0 = progX0
	m.layoutCache.waveProgX1 = progX1
	return out, progX0, progX1
}

func (m *Model) ensureCtrlGlyphs(themeGen uint64) *heroCtrlGlyphs {
	g := &m.layoutCache.ctrlGlyphs
	if g.repOff != "" && g.themeGen == themeGen {
		return g
	}
	muted := styles.QueueItemMuted
	g.themeGen = themeGen
	g.repOff = muted.Render("↺")
	g.repAll = styles.ControlActive.Render("↺")
	g.repOne = styles.ControlActive.Render("↻")
	g.shufOff = muted.Render("⇄")
	g.shufOn = styles.ControlActive.Render("⇄")
	g.favNone = muted.Render("♡")
	g.favLove = styles.FavoriteActive.Render("♥")
	g.favHate = styles.DislikeActive.Render("⊘")
	// Keep all three play-state labels the exact same width (10 inner chars)
	// with two spaces after the icon so ⏸ never crowds Playing and toggling
	// play/pause/loading never shifts neighbouring buttons.
	g.playW = styles.ControlPillPlaying.Render("⏸  Playing")
	g.pauseW = styles.ControlPillPaused.Render("▶  Paused ")
	g.playN = styles.ControlPillPlaying.Render("⏸")
	g.pauseN = styles.ControlPillPaused.Render("▶")
	return g
}

func (m *Model) buildHeroControlsRow(t *provider.Track, colW, screenX0 int, centered bool) (string, [4]heroButtonHit, int) {
	themeGen := styles.ThemeGen()
	rating := m.trackRating(t)
	spinStep := 0
	if m.playerState.Loading {
		spinStep = m.glowStep % 10
	}
	key := heroControlsCacheKey{
		colW:     colW,
		screenX0: screenX0,
		centered: centered,
		playing:  m.playerState.Playing,
		loading:  m.playerState.Loading,
		repeat:   m.playerState.RepeatMode,
		shuffle:  m.playerState.ShuffleMode,
		rating:   rating,
		glowStep: spinStep,
		themeGen: themeGen,
	}
	if m.layoutCache.ctrlLine != "" && m.layoutCache.ctrlCacheKey == key {
		return m.layoutCache.ctrlLine, m.layoutCache.ctrlBtns, m.layoutCache.ctrlBtnN
	}

	g := m.ensureCtrlGlyphs(themeGen)
	widePill := colW >= 44
	var playStr string
	switch {
	case m.playerState.Loading:
		spinnerFrames := [10]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		frame := spinnerFrames[spinStep]
		if widePill {
			playStr = styles.ControlPillLoading.Render(frame + "  Loading")
		} else {
			playStr = styles.ControlPillLoading.Render(frame)
		}
	case m.playerState.Playing:
		if widePill {
			playStr = g.playW
		} else {
			playStr = g.playN
		}
	default:
		if widePill {
			playStr = g.pauseW
		} else {
			playStr = g.pauseN
		}
	}

	repeatStr := g.repOff
	switch m.playerState.RepeatMode {
	case player.RepeatModeAll:
		repeatStr = g.repAll
	case player.RepeatModeOne:
		repeatStr = g.repOne
	}

	shuffleStr := g.shufOff
	if m.playerState.ShuffleMode {
		shuffleStr = g.shufOn
	}

	heartStr := g.favNone
	switch rating {
	case provider.RatingLoved:
		heartStr = g.favLove
	case provider.RatingDisliked:
		heartStr = g.favHate
	}

	repW := lipgloss.Width(repeatStr)
	shufW := lipgloss.Width(shuffleStr)
	pillW := lipgloss.Width(playStr)
	favW := lipgloss.Width(heartStr)

	const iconGap = "  "
	const iconGapW = 2
	const pillGap = "   "
	const pillGapW = 3

	leftWingW := repW + iconGapW + shufW + pillGapW
	rightWingW := pillGapW + favW
	totalW := leftWingW + pillW + rightWingW

	var btns [4]heroButtonHit
	btns[0] = heroButtonHit{action: heroActRepeat, x0: 0, x1: repW}
	btns[1] = heroButtonHit{action: heroActShuffle, x0: repW + iconGapW, x1: repW + iconGapW + shufW}
	btns[2] = heroButtonHit{action: heroActPlayPause, x0: leftWingW, x1: leftWingW + pillW}
	btns[3] = heroButtonHit{action: heroActFav, x0: leftWingW + pillW + pillGapW, x1: totalW}

	var sb strings.Builder
	sb.WriteString(repeatStr)
	sb.WriteString(iconGap)
	sb.WriteString(shuffleStr)
	sb.WriteString(pillGap)
	sb.WriteString(playStr)
	sb.WriteString(pillGap)
	sb.WriteString(heartStr)

	pad := 0
	if centered && colW > totalW {
		// Anchor the Play/Pause pill at the exact geometric center (colW - pillW) / 2
		// while clamping safely so neither wing overflows colW.
		idealPillX := (colW - pillW) / 2
		pad = max(0, min(colW-totalW, idealPillX-leftWingW))
	}
	for i := range btns {
		btns[i].x0 += screenX0 + pad
		btns[i].x1 += screenX0 + pad
	}

	var out string
	if pad > 0 {
		out = strings.Repeat(" ", pad) + sb.String()
	} else {
		out = ansi.Truncate(sb.String(), colW, "")
	}
	m.layoutCache.ctrlCacheKey = key
	m.layoutCache.ctrlLine = out
	m.layoutCache.ctrlBtns = btns
	m.layoutCache.ctrlBtnN = len(btns)
	return out, btns, len(btns)
}

func (m *Model) statusLineInline(w int, centered bool) string {
	if m.errMsg == "" {
		return ""
	}
	cleaned := provider.Clean(m.errMsg)
	var rendered string
	if strings.HasPrefix(cleaned, "✓") {
		text := truncateStr(cleaned, max(10, w))
		rendered = styles.ControlActive.Render(text)
	} else {
		const prefix = "⚠  "
		errText := truncateStr(cleaned, max(10, w-len([]rune(prefix))))
		rendered = styles.ErrorStyle.Render(prefix + errText)
	}
	if centered {
		return centerStr(rendered, w)
	}
	return ansi.Truncate(rendered, w, "…")
}

func (m *Model) buildHeroArtContent(contentW, h, screenX0 int) ([]string, heroHitMeta) {
	meta := heroHitMeta{innerH: h, progY: -1, ctrlY: -1}
	t := m.playerState.Track
	if t == nil {
		return m.buildHeroTextContent(contentW, h, screenX0)
	}

	ok, artRows, artCols := m.canFitSideBySideArt(contentW, h)
	if !ok {
		return m.buildHeroTextContent(contentW, h, screenX0)
	}

	size := art.Size{Width: artCols, Height: artRows}
	if m.artwork.rendered == nil {
		m.artwork.rendered = map[art.Size][]string{}
	}
	artLines := m.artwork.rendered[size]
	if artLines != nil {
		m.artwork.touchSize(size)
	} else if m.artwork.img != nil {
		const maxRenderedArtworkSizes = 16
		if len(m.artwork.rendered) >= maxRenderedArtworkSizes {
			if len(m.artwork.renderedOrder) > 0 {
				oldest := m.artwork.renderedOrder[0]
				copy(m.artwork.renderedOrder, m.artwork.renderedOrder[1:])
				m.artwork.renderedOrder = m.artwork.renderedOrder[:len(m.artwork.renderedOrder)-1]
				delete(m.artwork.rendered, oldest)
			} else {
				m.artwork.rendered = map[art.Size][]string{}
			}
		}
		artLines = m.renderArt(m.artwork.img, size)
		for i, line := range artLines {
			artLines[i] = padRight(line, artCols)
		}
		m.artwork.rendered[size] = artLines
		m.artwork.renderedOrder = append(m.artwork.renderedOrder, size)
	}

	const gutter = 3
	availRightW := max(1, contentW-artCols-gutter)
	rightW := min(availRightW, max(70, availRightW*3/4))
	outerPad := max(0, (contentW-(artCols+gutter+rightW))/2)
	rightX0 := screenX0 + outerPad + artCols + gutter

	trackStr := ansi.Truncate(m.renderTrackTitleText(t), rightW, "…")
	albumStr := m.renderAlbumMetaText(t, rightW, true, false)
	progStr, px0, px1 := m.buildWaveProgressRow(t, rightW, 56, rightX0, false)
	progVisW := max(1, lipgloss.Width(progStr))
	ctrlStr, btns, btnN := m.buildHeroControlsRow(t, progVisW, rightX0, true)
	statusStr := m.statusLineInline(rightW, false)

	var rightRows []string
	var progRelY, ctrlRelY int
	switch {
	case h >= 7:
		rightRows = []string{trackStr, albumStr, "", progStr, "", ctrlStr, statusStr}
		progRelY = 3
		ctrlRelY = 5
	default:
		rightRows = []string{trackStr, albumStr, progStr, "", ctrlStr}
		progRelY = 2
		ctrlRelY = 4
	}

	artTop := max(0, (h-artRows)/2)
	metaTop := max(0, (h-len(rightRows))/2)

	// Screen coordinate Y: row 0 = top bar, row 1 = hero top border, row 2 = first hero content row.
	const heroContentY0 = 2
	meta.progY = heroContentY0 + metaTop + progRelY
	meta.progX0 = px0
	meta.progX1 = px1
	meta.ctrlY = heroContentY0 + metaTop + ctrlRelY
	meta.btns = btns
	meta.btnN = btnN

	lines := make([]string, h)
	var rowBuf strings.Builder
	for y := range h {
		var leftCol string
		if ai := y - artTop; ai >= 0 && ai < len(artLines) {
			leftCol = artLines[ai]
		}
		var rightCol string
		if ri := y - metaTop; ri >= 0 && ri < len(rightRows) {
			rightCol = rightRows[ri]
		}
		rowBuf.Reset()
		writeSpaces(&rowBuf, outerPad)
		if leftCol == "" {
			writeSpaces(&rowBuf, artCols)
		} else {
			rowBuf.WriteString(leftCol)
		}
		writeSpaces(&rowBuf, gutter)
		rowBuf.WriteString(rightCol)
		if rightCol == "" && outerPad+artCols+gutter <= contentW {
			lines[y] = rowBuf.String()
		} else {
			lines[y] = ansi.Truncate(rowBuf.String(), contentW, "")
		}
	}
	return lines, meta
}

func (m *Model) buildHeroTextContent(contentW, h, screenX0 int) ([]string, heroHitMeta) {
	meta := heroHitMeta{innerH: h, progY: -1, ctrlY: -1}
	muted := styles.QueueItemMuted
	t := m.playerState.Track

	if h <= 0 {
		return nil, meta
	}

	if t == nil {
		lines := make([]string, h)
		mid := h / 2
		if h >= 3 {
			lines[mid] = centerStr(muted.Render("silence is not a vibe"), contentW)
		}
		if h >= 5 {
			lines[min(h-1, mid+2)] = centerStr(muted.Render("made with ❤️ by simonepelosi · press ? for about"), contentW)
		}
		if m.errMsg != "" {
			lines[max(0, mid-2)] = m.statusLine(contentW)
		}
		return lines, meta
	}

	trackLine := m.renderTrackTitleLine(t, contentW, true)
	albumLine := centerStr(m.renderAlbumMetaText(t, contentW, true, false), contentW)
	progLine, px0, px1 := m.buildWaveProgressRow(t, contentW, 64, screenX0, true)
	ctrlLine, btns, btnN := m.buildHeroControlsRow(t, contentW, screenX0, true)
	errLine := m.statusLine(contentW)

	var block []string
	var progIdx, ctrlIdx int
	switch {
	case h >= 10:
		block = []string{
			trackLine,
			albumLine,
			"",
			progLine,
			"",
			ctrlLine,
			"",
			errLine,
		}
		progIdx = 3
		ctrlIdx = 5
	case h >= 7:
		block = []string{trackLine, albumLine, "", progLine, "", ctrlLine, errLine}
		progIdx = 3
		ctrlIdx = 5
	case h >= 5:
		block = []string{trackLine, albumLine, progLine, "", ctrlLine}
		progIdx = 2
		ctrlIdx = 4
	default:
		block = []string{trackLine, albumLine, progLine, ctrlLine}
		progIdx = 2
		ctrlIdx = 3
	}

	topPad := max(0, (h-len(block))/2)
	const heroContentY0 = 2
	meta.progY = heroContentY0 + topPad + progIdx
	meta.progX0 = px0
	meta.progX1 = px1
	meta.ctrlY = heroContentY0 + topPad + ctrlIdx
	meta.btns = btns
	meta.btnN = btnN

	lines := make([]string, h)
	for i := range h {
		bi := i - topPad
		if bi >= 0 && bi < len(block) {
			lines[i] = ansi.Truncate(block[bi], contentW, "…")
		}
	}
	return lines, meta
}

// queuePanelLines returns the Queue card body rows for the left split.
func (m *Model) queuePanelLines(w, h int) []string {
	raw, _ := m.queuePanelLinesCached(w, h)
	return raw
}

func (m *Model) queuePanelLinesPadded(w, h int) []string {
	_, padded := m.queuePanelLinesCached(w, h)
	return padded
}

func (m *Model) queuePanelLinesCached(w, h int) (raw, padded []string) {
	tracks := m.queue.m.Tracks()
	total := len(tracks)
	if h <= 0 {
		return nil, nil
	}

	themeGen := styles.ThemeGen()
	c := &m.layoutCache

	if total == 0 {
		m.queueMiniOffset = 0
		key := queuePanelCacheKey{valid: true, w: w, h: h, offset: 0, total: 0, playingIdx: -1, themeGen: themeGen}
		if c.qCacheKey == key && len(c.qRawLines) == h && len(c.qPaddedLines) == h {
			return c.qRawLines, c.qPaddedLines
		}
		res := make([]string, h)
		pad := make([]string, h)
		fg := styles.QueueItemMuted.GetForeground()
		if c.qEmptyV == "" || c.qEmptyW != w || c.qEmptyFg != fg {
			c.qEmptyW = w
			c.qEmptyFg = fg
			c.qEmptyV = styles.QueueItemMuted.Render(truncateStr("Queue is empty · / search · l library", max(1, w)))
		}
		res[0] = c.qEmptyV
		if w > 0 {
			pad[0] = padRight(c.qEmptyV, w)
		}
		c.qCacheKey = key
		c.qRowSigs = c.qRowSigs[:0]
		c.qRawLines = res
		c.qPaddedLines = pad
		return res, pad
	}

	playingIdx := playingQueueIdx(tracks, m.playerState.Track)

	maxOffset := max(0, total-h)
	if m.queueMiniOffset > maxOffset {
		m.queueMiniOffset = maxOffset
	}
	end := min(total, m.queueMiniOffset+h)
	visibleN := end - m.queueMiniOffset

	key := queuePanelCacheKey{
		valid:      true,
		w:          w,
		h:          h,
		offset:     m.queueMiniOffset,
		total:      total,
		playingIdx: playingIdx,
		themeGen:   themeGen,
	}
	if c.qCacheKey == key && len(c.qRowSigs) == visibleN && len(c.qRawLines) == h && len(c.qPaddedLines) == h {
		match := true
		for j := range visibleN {
			t := tracks[m.queueMiniOffset+j]
			sig := c.qRowSigs[j]
			if sig.id != t.ID || sig.title != t.Title || sig.artist != t.Artist || sig.dur != t.Duration {
				match = false
				break
			}
		}
		if match {
			return c.qRawLines, c.qPaddedLines
		}
	}

	indexW := len(fmt.Sprintf("%d", total))
	showDur := w >= 36

	if cap(c.qRowSigs) < visibleN {
		c.qRowSigs = make([]queueRowSig, visibleN)
	} else {
		c.qRowSigs = c.qRowSigs[:visibleN]
	}

	result := make([]string, h)
	padded = make([]string, h)
	for j := range visibleN {
		i := m.queueMiniOffset + j
		t := tracks[i]
		c.qRowSigs[j] = queueRowSig{id: t.ID, title: t.Title, artist: t.Artist, dur: t.Duration}

		idx := fmt.Sprintf("%*d. ", indexW, i+1)
		durStr := ""
		durSpace := 0
		if showDur && t.Duration > 0 {
			durStr = views.FormatDuration(t.Duration)
			durSpace = len(durStr) + 2
		}
		label := t.Artist + " — " + t.Title
		markerW := 0
		if i == playingIdx {
			markerW = 2 // "▶ "
		}
		maxLabelW := max(4, w-indexW-2-markerW-durSpace)
		trimmed := truncateStr(label, maxLabelW)

		var row string
		if i == playingIdx {
			row = styles.ControlActive.Render(idx) + styles.ControlActive.Render("▶ "+trimmed)
		} else {
			if artistPart, titlePart, ok := strings.Cut(trimmed, " — "); ok {
				row = styles.QueueItemMuted.Render(idx) +
					styles.NowPlayingArtist.Render(artistPart) +
					styles.QueueItemMuted.Render(" — ") +
					styles.QueueItem.Render(titlePart)
			} else {
				row = styles.QueueItemMuted.Render(idx) + styles.QueueItem.Render(trimmed)
			}
		}
		if durStr != "" {
			pad := max(1, w-lipgloss.Width(row)-len(durStr))
			row += strings.Repeat(" ", pad) + styles.QueueItemMuted.Render(durStr)
		}
		result[j] = row
		if w > 0 {
			padded[j] = padRight(row, w)
		}
	}

	c.qCacheKey = key
	c.qRawLines = result
	c.qPaddedLines = padded
	return result, padded
}

// panelHeight returns the number of content rows available for the bottom card(s).
// Overhead = topbar(1) + hero borders(2) + hero content(npH) + panel borders(2) + status(N).
func (m *Model) panelHeight() int {
	return m.panelHeightWithStatus(len(m.statusLines(m.statusWidth())))
}

func (m *Model) panelHeightWithStatus(statusCount int) int {
	fixedOverhead := 5 + m.nowPlayingHeight() + statusCount
	return max(3, m.height-fixedOverhead)
}

func (m *Model) activePanelInnerWidth() int {
	return max(0, m.width-4)
}

// ── Shared navigation & action helpers ────────────────────────────────────────

func (m *Model) activatePanel(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.panels) {
		return nil
	}
	m.debugView = false
	m.mode = modeNormal
	m.vibe.Blur()
	m.activePanel = idx
	m.lastKey = ""

	p := m.panels[idx]
	if p == m.feedP && m.feedP.m.NeedsLoad() {
		m.feedP.m.SetLoading()
		return m.fetchFeedCmd()
	}
	if p == m.lyricsP && m.lastLyricsTrackID == "" && m.playerState.Track != nil {
		m.lastLyricsTrackID = views.PlaybackID(*m.playerState.Track)
		m.lyricsP.m.SetLoading()
		return m.fetchLyricsCmd(m.playerState.Track)
	}
	return nil
}

func (m *Model) openSearchMode() {
	m.debugView = false
	m.activePanel = -1
	m.vibe.Blur()
	m.mode = modeSearch
	m.searchCursor = len([]rune(m.searchQuery))
	m.lastKey = ""
}

func (m *Model) toggleMute() tea.Cmd {
	if m.preMuteVol >= 0 {
		vol := m.preMuteVol
		m.preMuteVol = -1
		m.appendLog(fmt.Sprintf("[vol] unmuted → %.0f%%", vol*100))
		return m.playerCmd(func(p player.Player) error { return p.SetVolume(vol) })
	}
	m.preMuteVol = m.playerState.Volume
	m.appendLog("[vol] muted")
	return m.playerCmd(func(p player.Player) error { return p.SetVolume(0) })
}

func (m *Model) cycleRepeatMode() tea.Cmd {
	next := player.RepeatModeOff
	switch m.playerState.RepeatMode {
	case player.RepeatModeOff:
		next = player.RepeatModeAll
	case player.RepeatModeAll:
		next = player.RepeatModeOne
	case player.RepeatModeOne:
		next = player.RepeatModeOff
	}
	m.playerState.RepeatMode = next
	m.appendLog(fmt.Sprintf("[player] repeat → %d", next))
	return m.playerCmd(func(p player.Player) error { return p.SetRepeat(next) })
}

func (m *Model) toggleShuffleMode() tea.Cmd {
	on := !m.playerState.ShuffleMode
	m.playerState.ShuffleMode = on
	m.appendLog(fmt.Sprintf("[player] shuffle → %v", on))
	return m.playerCmd(func(p player.Player) error { return p.SetShuffle(on) })
}

func (m *Model) playQueueFrom(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.queueTracks) {
		return nil
	}
	t := m.queueTracks[idx]
	m.queueTracks = m.queueTracks[idx:]
	if idx < len(m.queueIDs) {
		m.queueIDs = m.queueIDs[idx:]
	} else {
		ids := make([]string, len(m.queueTracks))
		for i, qt := range m.queueTracks {
			ids[i] = views.PlaybackID(qt)
		}
		m.queueIDs = ids
	}
	m.queue.SetTracks(m.queueTracks)
	m.appendLog(fmt.Sprintf("[queue] playing from position %d: %s — %s", idx+1, t.Artist, t.Title))
	m.playerState.Track = &t
	m.applyTrackPalette(&t)
	m.playerState.Loading = true
	m.playerState.Playing = false
	m.playerState.Position = 0
	ids := m.queueIDs
	return m.playerCmd(func(p player.Player) error { return p.SetQueue(ids) })
}

// ── Mouse handling ────────────────────────────────────────────────────────────

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.width <= 0 || m.height <= 0 || m.introStep != introDone {
		return nil
	}

	switch ev := msg.(type) {
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(ev.Mouse())
	case tea.MouseClickMsg:
		if ev.Button != tea.MouseLeft {
			return nil
		}
		return m.handleMouseClick(ev.Mouse())
	}
	return nil
}

func (m *Model) handleMouseWheel(ev tea.Mouse) tea.Cmd {
	up := ev.Button == tea.MouseWheelUp
	down := ev.Button == tea.MouseWheelDown
	if !up && !down {
		return nil
	}

	if m.mode == modePlaylistPicker {
		if up && m.playlistPickerCursor > 0 {
			m.playlistPickerCursor--
		} else if down && m.playlistPickerCursor < len(m.playlistPickerItems)-1 {
			m.playlistPickerCursor++
		}
		return nil
	}

	// Wheel on top bar adjusts volume.
	if ev.Y == 0 {
		if up {
			return m.adjustVolume(0.05)
		}
		return m.adjustVolume(-0.05)
	}

	panelTopY := 1 + (2 + m.nowPlayingHeight())
	if ev.Y < panelTopY {
		return nil
	}

	keyDir := "down"
	keyCode := tea.KeyDown
	if up {
		keyDir = "up"
		keyCode = tea.KeyUp
	}
	synKey := tea.KeyPressMsg{Code: keyCode, Text: keyDir}

	switch {
	case m.debugView:
		if up {
			m.debugScroll++
		} else if m.debugScroll > 0 {
			m.debugScroll--
		}
		return nil
	case m.mode == modeSearch:
		_, cmd := m.search.Update(synKey)
		return cmd
	case m.mode == modeCommand:
		if up {
			m.moveCmdSuggestion(-1)
		} else {
			m.moveCmdSuggestion(1)
		}
		return nil
	case m.activePanel >= 0 && m.activePanel < len(m.panels):
		p := m.panels[m.activePanel]
		if p == m.eqP {
			return m.eqP.Update(synKey)
		}
		return m.forwardToActivePanel(synKey)
	default:
		// Split Home view: scroll Queue card when cursor is over left half.
		leftW, gap, _ := m.splitCardWidths(m.width)
		if ev.X < leftW+gap && len(m.queueTracks) > 0 {
			panelH := m.panelHeight()
			maxOff := max(0, len(m.queueTracks)-panelH)
			if up && m.queueMiniOffset > 0 {
				m.queueMiniOffset--
			} else if down && m.queueMiniOffset < maxOff {
				m.queueMiniOffset++
			}
		}
	}
	return nil
}

func (m *Model) handleMouseClick(ev tea.Mouse) tea.Cmd {
	if m.mode == modePlaylistPicker {
		return nil
	}

	// Row 0: Top header tabs & mute toggle.
	if ev.Y == 0 {
		_, zones := m.buildTopBar(m.width)
		for _, z := range zones {
			if ev.X >= z.x0 && ev.X < z.x1 {
				switch z.kind {
				case topHitVolume:
					return m.toggleMute()
				case topHitSearch:
					m.openSearchMode()
					return nil
				case topHitTab:
					if z.panelIdx < 0 {
						m.debugView = false
						m.mode = modeNormal
						m.vibe.Blur()
						m.activePanel = -1
						m.lastKey = ""
						return nil
					}
					return m.activatePanel(z.panelIdx)
				}
			}
		}
		return nil
	}

	// Hero card: wave progress seek & transport buttons.
	npH := m.nowPlayingHeight()
	heroEndY := 1 + (2 + npH)
	if ev.Y < heroEndY {
		if m.playerState.Track == nil {
			return nil
		}
		_, meta := m.buildHeroViewData(m.width)
		if ev.Y == meta.progY && ev.X >= meta.progX0 && ev.X < meta.progX1 {
			span := max(1, meta.progX1-meta.progX0-1)
			frac := clamp(float64(ev.X-meta.progX0)/float64(span), 0, 1)
			newPos := time.Duration(frac * float64(m.playerState.Track.Duration))
			m.playerState.Position = newPos
			m.appendLog(fmt.Sprintf("[player] seek → %s", views.FormatDuration(newPos)))
			pos := newPos
			return m.playerCmd(func(p player.Player) error { return p.Seek(pos) })
		}
		if ev.Y == meta.ctrlY {
			for i := range meta.btnN {
				b := meta.btns[i]
				if ev.X >= b.x0 && ev.X < b.x1 {
					return m.triggerHeroAction(b.action)
				}
			}
		}
		return nil
	}

	// Bottom panel area.
	panelH := m.panelHeight()
	panelBodyY0 := heroEndY + 1
	panelBodyY1 := panelBodyY0 + panelH
	if ev.Y < heroEndY || ev.Y >= panelBodyY1+1 {
		return nil
	}

	// Full-width active panel clicks.
	if m.activePanel >= 0 && m.activePanel < len(m.panels) && !m.debugView && m.mode == modeNormal {
		if m.panels[m.activePanel] == m.eqP {
			relX := ev.X - 2 // left card border + 1 space
			m.eqP.m.SelectBandAtX(relX)
		}
		return nil
	}

	// Split cards click in Home view.
	if m.activePanel == -1 && m.mode == modeNormal && !m.debugView {
		leftW, gap, _ := m.splitCardWidths(m.width)
		if ev.X < leftW {
			m.vibe.Blur()
			return nil
		}
		if ev.X >= leftW+gap {
			if m.vibe.PickerActive() {
				row := ev.Y - panelBodyY0
				if cmd := m.vibe.SelectPickerAtRow(row); cmd != nil {
					return cmd
				}
			}
			m.vibe.Focus()
			return nil
		}
	}
	return nil
}

func (m *Model) triggerHeroAction(act heroAction) tea.Cmd {
	switch act {
	case heroActRepeat:
		return m.cycleRepeatMode()
	case heroActShuffle:
		return m.toggleShuffleMode()
	case heroActPlayPause:
		return m.togglePlayPause()
	case heroActFav:
		return m.toggleCurrentTrackRating(provider.RatingLoved)
	default:
		return nil
	}
}
