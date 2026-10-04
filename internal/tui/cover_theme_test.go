package tui

import (
	"image"
	"image/color"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lucasb-eyer/go-colorful"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

func solidCover(c color.Color) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}
func TestCoverThemeUsesImageHueWithReadableText(t *testing.T) {
	for _, c := range []color.Color{color.NRGBA{180, 70, 30, 255}, color.NRGBA{25, 80, 200, 255}, color.NRGBA{40, 150, 70, 255}} {
		theme, ok := themeFromCover(solidCover(c), styles.DefaultTheme())
		if !ok {
			t.Fatal("missing image theme")
		}
		want, _ := colorful.MakeColor(c)
		h, _, _ := want.Hsl()
		got, _ := colorful.Hex(theme.Primary)
		gh, _, gl := got.Hsl()
		if absHue(gh, h) > 5 || gl < 0.6 {
			t.Fatalf("cover hue lost or accent unreadable: %s", theme.Primary)
		}
	}
}
func absHue(a, b float64) float64 {
	d := math.Abs(a - b)
	if d > 180 {
		d = 360 - d
	}
	return d
}
func TestCoverThemeOverridesGenreAndRejectsStaleCover(t *testing.T) {
	defer styles.Apply(styles.DefaultTheme())
	cfg := testCfg()
	cfg.CoverTheme = true
	cfg.GenreTheme = true
	m := New(cfg, &mockProvider{}, nil, Options{})
	m.supportsArtColor = func() bool { return true }
	m.artwork.url = "new-cover"
	m.artworkGen = 2
	theme, ok := themeFromCover(solidCover(color.NRGBA{180, 70, 30, 255}), m.baseTheme)
	if !ok {
		t.Fatal("missing cover theme")
	}
	m.Update(artworkLoadedMsg{url: "old-cover", gen: 1, img: solidCover(color.Black), theme: &theme})
	if styles.ColorHex(styles.ColorPrimary) == theme.Primary {
		t.Fatal("stale image changed theme")
	}
	m.Update(artworkLoadedMsg{url: "new-cover", gen: 2, img: solidCover(color.Black), theme: &theme})
	m.syncMusicTheme(&provider.Track{ArtworkURL: "new-cover", Genres: []string{"Pop"}})
	if styles.ColorHex(styles.ColorPrimary) != theme.Primary {
		t.Fatal("playback tick reset cover colors to purple")
	}
	m.syncMusicTheme(&provider.Track{ArtworkURL: "next-cover"})
	if !strings.EqualFold(styles.ColorHex(styles.ColorPrimary), m.baseTheme.Primary) {
		t.Fatal("next cover inherited previous colors")
	}
}

func TestCoverThemeDownloadsAndExtractsWithoutArtView(t *testing.T) {
	defer styles.Apply(styles.DefaultTheme())
	data := testArtworkPNG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	defer srv.Close()
	m := newModel(nil)
	m.cfg.CoverTheme = true
	m.artMode = false
	m.supportsArtColor = func() bool { return true }
	_, cmd := m.Update(playerStateMsg{Track: &provider.Track{ID: "cover-song", ArtworkURL: srv.URL}})
	if cmd == nil {
		t.Fatal("cover theme did not request artwork outside art view")
	}
	loaded, ok := runArtworkCommand(t, cmd).(artworkLoadedMsg)
	if !ok || loaded.err != nil || loaded.theme == nil {
		t.Fatalf("image was not downloaded and themed: %#v", loaded)
	}
	m.Update(loaded)
	if m.musicTheme != "cover" {
		t.Fatal("downloaded palette was not applied")
	}
	if _, ok := themeFromCover(image.NewNRGBA(image.Rect(0, 0, 4, 4)), m.baseTheme); ok {
		t.Fatal("transparent cover must keep the base theme")
	}
}
