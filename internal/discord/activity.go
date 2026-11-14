package discord

import (
	"net/url"
	"strings"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
)

// ActivityTypeListening indicates "Listening to ..." in Discord.
const ActivityTypeListening = 2

// Activity represents the Rich Presence payload sent to Discord.
type Activity struct {
	Type       int         `json:"type"`
	Details    string      `json:"details,omitempty"`
	State      string      `json:"state,omitempty"`
	Timestamps *Timestamps `json:"timestamps,omitempty"`
	Assets     *Assets     `json:"assets,omitempty"`
}

// Timestamps defines start/end epoch milliseconds or seconds for Discord.
type Timestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

// Assets defines icons and hover text.
type Assets struct {
	LargeImage string `json:"large_image,omitempty"`
	LargeText  string `json:"large_text,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
}

// BuildActivity constructs a Discord Activity from player.State.
func BuildActivity(st player.State) *Activity {
	if st.Track == nil {
		return nil
	}

	title := sanitizeStr(st.Track.Title)
	artist := sanitizeStr(st.Track.Artist)
	album := sanitizeStr(st.Track.Album)

	if title == "" {
		title = "Unknown Track"
	}

	act := &Activity{
		Type:    ActivityTypeListening,
		Details: title,
		State:   artist,
		Assets:  &Assets{},
	}

	// Artwork & assets
	artURL := formatArtworkURL(st.Track.ArtworkURL)
	if artURL != "" {
		act.Assets.LargeImage = artURL
	} else {
		act.Assets.LargeImage = "vibez_logo"
	}

	if album != "" {
		act.Assets.LargeText = album
	} else {
		act.Assets.LargeText = "vibez"
	}

	if st.Playing {
		act.Assets.SmallImage = "play"
		act.Assets.SmallText = "Playing"

		// Calculate progress timestamps if duration is known
		if st.Track.Duration > 0 {
			now := time.Now()
			start := now.Add(-st.Position)
			end := start.Add(st.Track.Duration)
			act.Timestamps = &Timestamps{
				Start: start.Unix(),
				End:   end.Unix(),
			}
		}
	} else {
		act.Assets.SmallImage = "pause"
		act.Assets.SmallText = "Paused"
	}

	return act
}

// formatArtworkURL ensures {w}x{h} placeholders are resolved and validates URL structure.
func formatArtworkURL(raw string) string {
	if raw == "" {
		return ""
	}
	expanded := strings.ReplaceAll(raw, "{w}x{h}", "512x512")
	expanded = strings.ReplaceAll(expanded, "{w}", "512")
	expanded = strings.ReplaceAll(expanded, "{h}", "512")
	for i := 0; i < len(expanded); i++ {
		if expanded[i] < 0x20 || expanded[i] == 0x7f {
			return ""
		}
	}
	u, err := url.Parse(expanded)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return ""
	}
	return u.String()
}

// sanitizeStr strips control characters, trims whitespace, truncates to 128 characters, and ensures minimum length of 2.
func sanitizeStr(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) == 1 {
		return string(runes) + " "
	}
	if len(runes) > 128 {
		return string(runes[:128])
	}
	return string(runes)
}
