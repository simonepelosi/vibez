package styles_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/simone-vibes/vibez/internal/tui/styles"
)

func TestLoadTheme_BuiltIn(t *testing.T) {
	for _, name := range styles.BuiltinThemeNames() {
		t.Run(name, func(t *testing.T) {
			theme, err := styles.LoadTheme(name, "/tmp")
			if err != nil {
				t.Fatalf("unexpected error for built-in %q: %v", name, err)
			}
			if theme.Primary == "" {
				t.Errorf("expected non-empty Primary for theme %q", name)
			}
		})
	}
}

func TestLoadTheme_Default(t *testing.T) {
	d := styles.DefaultTheme()
	for _, name := range []string{"", "default"} {
		theme, err := styles.LoadTheme(name, "/tmp")
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", name, err)
		}
		if theme.Primary != d.Primary {
			t.Errorf("expected default primary %q, got %q", d.Primary, theme.Primary)
		}
	}
}

func TestLoadTheme_BuiltInsHaveDistinctColors(t *testing.T) {
	def := styles.DefaultTheme()
	for _, name := range []string{"dracula", "gruvbox", "nord"} {
		theme, _ := styles.LoadTheme(name, "/tmp")
		if theme.Primary == def.Primary {
			t.Errorf("theme %q has same Primary as default; expected distinct color", name)
		}
	}
}

func TestLoadTheme_Custom(t *testing.T) {
	dir := t.TempDir()
	themesDir := filepath.Join(dir, "themes")
	if err := os.MkdirAll(themesDir, 0o700); err != nil {
		t.Fatal(err)
	}

	custom := styles.DefaultTheme()
	custom.Primary = "#ff0000"
	custom.Bear = "#00ff00"

	data, _ := json.Marshal(custom)
	if err := os.WriteFile(filepath.Join(themesDir, "mycolor.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	theme, err := styles.LoadTheme("mycolor", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if theme.Primary != "#ff0000" {
		t.Errorf("expected Primary #ff0000, got %q", theme.Primary)
	}
	if theme.Bear != "#00ff00" {
		t.Errorf("expected Bear #00ff00, got %q", theme.Bear)
	}
}

func TestLoadTheme_InvalidColor_FallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	themesDir := filepath.Join(dir, "themes")
	_ = os.MkdirAll(themesDir, 0o700)

	raw := `{"primary": "notacolor", "secondary": "#50fa7b"}`
	_ = os.WriteFile(filepath.Join(themesDir, "bad.json"), []byte(raw), 0o600)

	theme, err := styles.LoadTheme("bad", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	def := styles.DefaultTheme()
	if theme.Primary != def.Primary {
		t.Errorf("expected invalid Primary to fall back to %q, got %q", def.Primary, theme.Primary)
	}
	if theme.Secondary != "#50fa7b" {
		t.Errorf("expected valid Secondary to be kept, got %q", theme.Secondary)
	}
}

func TestLoadTheme_UnknownName_FallsBackToDefault(t *testing.T) {
	theme, err := styles.LoadTheme("nonexistent-theme", "/tmp")
	if err == nil {
		t.Error("expected error for unknown theme, got nil")
	}
	def := styles.DefaultTheme()
	if theme.Primary != def.Primary {
		t.Errorf("expected default Primary %q, got %q", def.Primary, theme.Primary)
	}
}

func TestLoadTheme_CustomGlowPalette_PartialDefault(t *testing.T) {
	dir := t.TempDir()
	themesDir := filepath.Join(dir, "themes")
	_ = os.MkdirAll(themesDir, 0o700)

	// Only first glow entry valid, rest invalid/empty.
	raw := `{"glow_palette": ["#000000","","","","","","",""]}`
	_ = os.WriteFile(filepath.Join(themesDir, "glow.json"), []byte(raw), 0o600)

	theme, _ := styles.LoadTheme("glow", dir)
	if theme.GlowPalette[0] != "#000000" {
		t.Errorf("expected GlowPalette[0] to be #000000, got %q", theme.GlowPalette[0])
	}
	def := styles.DefaultTheme()
	for i := 1; i < 8; i++ {
		if theme.GlowPalette[i] != def.GlowPalette[i] {
			t.Errorf("expected GlowPalette[%d] to fall back to default %q, got %q",
				i, def.GlowPalette[i], theme.GlowPalette[i])
		}
	}
}

func TestApply_NoPanic(t *testing.T) {
	for _, name := range styles.BuiltinThemeNames() {
		t.Run(name, func(t *testing.T) {
			theme, _ := styles.LoadTheme(name, "/tmp")
			styles.Apply(theme) // must not panic
		})
	}
	// Restore default.
	styles.Apply(styles.DefaultTheme())
}

func TestApply_ChangesColors(t *testing.T) {
	orig := styles.ColorPrimary

	styles.Apply(styles.DraculaTheme())
	if styles.ColorPrimary == orig {
		t.Error("expected ColorPrimary to change after Apply(DraculaTheme)")
	}
	if string(styles.ColorPrimary) != styles.DraculaTheme().Primary {
		t.Errorf("expected ColorPrimary %q, got %q", styles.DraculaTheme().Primary, styles.ColorPrimary)
	}

	// Restore.
	styles.Apply(styles.DefaultTheme())
	if styles.ColorPrimary != orig {
		t.Errorf("expected ColorPrimary restored to %q, got %q", orig, styles.ColorPrimary)
	}
}

func TestApply_UpdatesProgressGradStops(t *testing.T) {
	styles.Apply(styles.DraculaTheme())
	d := styles.DraculaTheme()
	if string(styles.ProgressGradStops[0]) != d.Progress {
		t.Errorf("ProgressGradStops[0] = %q, want %q", styles.ProgressGradStops[0], d.Progress)
	}
	styles.Apply(styles.DefaultTheme())
}

func TestApply_UpdatesGlowPalette(t *testing.T) {
	styles.Apply(styles.NordTheme())
	n := styles.NordTheme()
	if string(styles.GlowPalette[0]) != n.GlowPalette[0] {
		t.Errorf("GlowPalette[0] = %q, want %q", styles.GlowPalette[0], n.GlowPalette[0])
	}
	styles.Apply(styles.DefaultTheme())
}
