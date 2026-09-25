package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
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

// formatArtworkURL ensures {w}x{h} placeholders are resolved and validates HTTP prefix.
func formatArtworkURL(raw string) string {
	if raw == "" {
		return ""
	}
	url := strings.ReplaceAll(raw, "{w}x{h}", "512x512")
	url = strings.ReplaceAll(url, "{w}", "512")
	url = strings.ReplaceAll(url, "{h}", "512")
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return url
	}
	return ""
}

// sanitizeStr trims whitespace, truncates to 128 characters, and ensures minimum length of 2.
func sanitizeStr(s string) string {
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

// activityEqual reports whether two activities represent the same display state.
func activityEqual(a, b *Activity) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Details != b.Details || a.State != b.State || a.Type != b.Type {
		return false
	}
	if (a.Assets == nil) != (b.Assets == nil) {
		return false
	}
	if a.Assets != nil && b.Assets != nil {
		if a.Assets.LargeImage != b.Assets.LargeImage ||
			a.Assets.LargeText != b.Assets.LargeText ||
			a.Assets.SmallImage != b.Assets.SmallImage ||
			a.Assets.SmallText != b.Assets.SmallText {
			return false
		}
	}
	if (a.Timestamps == nil) != (b.Timestamps == nil) {
		return false
	}
	if a.Timestamps != nil && b.Timestamps != nil {
		// Allow a 1-second drift so normal position ticks don't re-dispatch identical songs
		diff := a.Timestamps.Start - b.Timestamps.Start
		if diff < -1 || diff > 1 {
			return false
		}
	}
	return true
}

func trackIdent(t *provider.Track) string {
	if t == nil {
		return ""
	}
	return fmt.Sprintf("%s:%s:%s", t.ID, t.Title, t.Artist)
}
