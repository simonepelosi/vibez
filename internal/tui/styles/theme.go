package styles

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Theme holds every color used by the UI as hex strings (#RRGGBB or #RGB).
// An empty field falls back to the corresponding value in DefaultTheme.
type Theme struct {
	// Core palette
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
	Muted     string `json:"muted"`
	Error     string `json:"error"`
	Fg        string `json:"fg"`
	Subtle    string `json:"subtle"`
	Bg        string `json:"bg"`

	// Semantic accents
	Love       string `json:"love"`
	Active     string `json:"active"`
	Progress   string `json:"progress"`
	Surface    string `json:"surface"`
	Accent     string `json:"accent"`
	AccentWarm string `json:"accent_warm"` // amber — labels, search Tracks header, note animations

	// Bear mascot
	Bear string `json:"bear"` // bear body colour

	// Glow animation palette — dark (index 0) → bright (index 7)
	GlowPalette [8]string `json:"glow_palette"`

	// Mode chips (status-bar mode indicator background colours)
	ModeNormalBg  string `json:"mode_normal_bg"`
	ModeSearchBg  string `json:"mode_search_bg"`
	ModeCommandBg string `json:"mode_command_bg"`
	ModeChipFg    string `json:"mode_chip_fg"` // shared foreground for all mode chips
}

// DefaultTheme returns the built-in Tokyo Night / Catppuccin Mocha blend.
func DefaultTheme() Theme {
	return Theme{
		Primary:    "#C678DD",
		Secondary:  "#98C379",
		Muted:      "#5C6370",
		Error:      "#E06C75",
		Fg:         "#ABB2BF",
		Subtle:     "#61AFEF",
		Bg:         "#1a1b26",
		Love:       "#f38ba8",
		Active:     "#a6e3a1",
		Progress:   "#89b4fa",
		Surface:    "#2a2b3d",
		Accent:     "#7aa2f7",
		AccentWarm: "#E5C07B",
		Bear:       "#C4A265",
		GlowPalette: [8]string{
			"#1e1e2e", "#2d2b55", "#4a3f8a", "#6e57c4",
			"#9d7fea", "#bb9af7", "#cba6f7", "#e0d4ff",
		},
		ModeNormalBg:  "#98C379",
		ModeSearchBg:  "#61AFEF",
		ModeCommandBg: "#E5C07B",
		ModeChipFg:    "#1a1b26",
	}
}

// DraculaTheme returns a Dracula-inspired color scheme.
func DraculaTheme() Theme {
	return Theme{
		Primary:    "#ff79c6",
		Secondary:  "#50fa7b",
		Muted:      "#6272a4",
		Error:      "#ff5555",
		Fg:         "#f8f8f2",
		Subtle:     "#8be9fd",
		Bg:         "#282a36",
		Love:       "#ff6e6e",
		Active:     "#50fa7b",
		Progress:   "#8be9fd",
		Surface:    "#44475a",
		Accent:     "#bd93f9",
		AccentWarm: "#f1fa8c",
		Bear:       "#ffb86c",
		GlowPalette: [8]string{
			"#282a36", "#383a52", "#44475a", "#6272a4",
			"#9580ff", "#bd93f9", "#caa9fa", "#e9e0ff",
		},
		ModeNormalBg:  "#50fa7b",
		ModeSearchBg:  "#8be9fd",
		ModeCommandBg: "#f1fa8c",
		ModeChipFg:    "#282a36",
	}
}

// GruvboxTheme returns a Gruvbox Dark-inspired color scheme.
func GruvboxTheme() Theme {
	return Theme{
		Primary:    "#d3869b",
		Secondary:  "#b8bb26",
		Muted:      "#928374",
		Error:      "#fb4934",
		Fg:         "#ebdbb2",
		Subtle:     "#83a598",
		Bg:         "#282828",
		Love:       "#fe8019",
		Active:     "#b8bb26",
		Progress:   "#83a598",
		Surface:    "#3c3836",
		Accent:     "#458588",
		AccentWarm: "#d79921",
		Bear:       "#e8c88b",
		GlowPalette: [8]string{
			"#282828", "#32302f", "#3c3836", "#504945",
			"#665c54", "#928374", "#d5c4a1", "#ebdbb2",
		},
		ModeNormalBg:  "#b8bb26",
		ModeSearchBg:  "#83a598",
		ModeCommandBg: "#d79921",
		ModeChipFg:    "#282828",
	}
}

// NordTheme returns a Nord-inspired color scheme.
func NordTheme() Theme {
	return Theme{
		Primary:    "#b48ead",
		Secondary:  "#a3be8c",
		Muted:      "#4c566a",
		Error:      "#bf616a",
		Fg:         "#eceff4",
		Subtle:     "#88c0d0",
		Bg:         "#2e3440",
		Love:       "#bf616a",
		Active:     "#a3be8c",
		Progress:   "#81a1c1",
		Surface:    "#3b4252",
		Accent:     "#5e81ac",
		AccentWarm: "#ebcb8b",
		Bear:       "#d08770",
		GlowPalette: [8]string{
			"#2e3440", "#3b4252", "#434c5e", "#4c566a",
			"#81a1c1", "#88c0d0", "#8fbcbb", "#eceff4",
		},
		ModeNormalBg:  "#a3be8c",
		ModeSearchBg:  "#88c0d0",
		ModeCommandBg: "#ebcb8b",
		ModeChipFg:    "#2e3440",
	}
}

var builtinThemes = map[string]func() Theme{
	"default": DefaultTheme,
	"dracula": DraculaTheme,
	"gruvbox": GruvboxTheme,
	"nord":    NordTheme,
}

// BuiltinThemeNames returns the sorted list of built-in theme names.
func BuiltinThemeNames() []string {
	return []string{"default", "dracula", "gruvbox", "nord"}
}

// LoadTheme resolves name to a Theme. Resolution order:
//  1. Built-in theme by name.
//  2. File at <configDir>/themes/<name>.json.
//  3. DefaultTheme on any error.
//
// The returned error is non-nil when a non-built-in name was requested but
// the file could not be loaded or parsed. The returned Theme is always usable.
func LoadTheme(name, configDir string) (Theme, error) {
	if name == "" || name == "default" {
		return DefaultTheme(), nil
	}
	if fn, ok := builtinThemes[name]; ok {
		return fn(), nil
	}

	// Attempt to load a custom JSON theme file.
	path := filepath.Join(configDir, "themes", name+".json")
	data, err := os.ReadFile(path) //nolint:gosec // path is user-controlled config, not external input
	if err != nil {
		return DefaultTheme(), fmt.Errorf("theme %q: %w", name, err)
	}

	var t Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return DefaultTheme(), fmt.Errorf("theme %q: parsing JSON: %w", name, err)
	}

	t = mergeWithDefault(t)
	return t, nil
}

var hexColorRE = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

func validColor(s string) bool {
	return hexColorRE.MatchString(s)
}

// mergeWithDefault fills any missing or invalid color fields from DefaultTheme.
func mergeWithDefault(t Theme) Theme {
	d := DefaultTheme()
	merge := func(field, def string) string {
		if !validColor(field) {
			return def
		}
		return field
	}
	t.Primary = merge(t.Primary, d.Primary)
	t.Secondary = merge(t.Secondary, d.Secondary)
	t.Muted = merge(t.Muted, d.Muted)
	t.Error = merge(t.Error, d.Error)
	t.Fg = merge(t.Fg, d.Fg)
	t.Subtle = merge(t.Subtle, d.Subtle)
	t.Bg = merge(t.Bg, d.Bg)
	t.Love = merge(t.Love, d.Love)
	t.Active = merge(t.Active, d.Active)
	t.Progress = merge(t.Progress, d.Progress)
	t.Surface = merge(t.Surface, d.Surface)
	t.Accent = merge(t.Accent, d.Accent)
	t.AccentWarm = merge(t.AccentWarm, d.AccentWarm)
	t.Bear = merge(t.Bear, d.Bear)
	t.ModeNormalBg = merge(t.ModeNormalBg, d.ModeNormalBg)
	t.ModeSearchBg = merge(t.ModeSearchBg, d.ModeSearchBg)
	t.ModeCommandBg = merge(t.ModeCommandBg, d.ModeCommandBg)
	t.ModeChipFg = merge(t.ModeChipFg, d.ModeChipFg)
	for i := range t.GlowPalette {
		t.GlowPalette[i] = merge(t.GlowPalette[i], d.GlowPalette[i])
	}
	return t
}
