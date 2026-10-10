package web

import (
	"strings"
	"testing"
)

func TestRenderHTMLAcceptsSupportedBitrates(t *testing.T) {
	for _, kbps := range []int{64, 256} {
		html, err := RenderHTML("dev", "user", "us", "test", kbps)
		if err != nil {
			t.Fatalf("RenderHTML(%d): %v", kbps, err)
		}
		if html == "" {
			t.Fatalf("RenderHTML(%d) returned empty HTML", kbps)
		}
	}
}

func TestRenderHTMLRejectsUnsupportedBitrate(t *testing.T) {
	_, err := RenderHTML("dev", "user", "us", "test", 320)
	if err == nil || !strings.Contains(err.Error(), "MusicKit JS/web playback max is 256 kbps AAC") {
		t.Fatalf("RenderHTML(320) error = %v", err)
	}
}

// MusicKit v3 has no isExplicitContentAllowed property, so the guarded
// assignment vibez used before #130 never ran. The flag that actually gates
// explicit playback is restrictedEnabled, which MusicKit otherwise guesses from
// the storefront country and applies to every queue.
func TestRenderHTMLLiftsExplicitContentRestriction(t *testing.T) {
	html, err := RenderHTML("dev", "user", "us", "test", 256)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if !strings.Contains(html, "music.restrictedEnabled = false") {
		t.Error("rendered HTML does not clear music.restrictedEnabled")
	}
	// Matched as an assignment, not a bare mention: the comment above the fix
	// names the property deliberately so it is not reintroduced.
	if strings.Contains(html, "music.isExplicitContentAllowed =") {
		t.Error("rendered HTML still assigns isExplicitContentAllowed, which MusicKit v3 does not define")
	}
}

// Applying the user token runs MusicKit's runTokenValidations, which reads
// restrictedEnabled and pins authorizationStatus to RESTRICTED when it is set.
// Clearing the flag afterwards would not undo that, so order matters here.
func TestRenderHTMLClearsRestrictionBeforeApplyingToken(t *testing.T) {
	html, err := RenderHTML("dev", "user", "us", "test", 256)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	clear := strings.Index(html, "music.restrictedEnabled = false")
	token := strings.Index(html, "music.musicUserToken = savedToken")
	if clear < 0 || token < 0 {
		t.Fatalf("markers not found: restrictedEnabled=%d musicUserToken=%d", clear, token)
	}
	if clear > token {
		t.Errorf("restrictedEnabled is cleared at %d, after the token is applied at %d", clear, token)
	}
}

// The Chrome backend serves this HTML over local HTTP, so the user token must
// come from the goGetUserToken binding and not from the rendered page.
func TestRenderHTMLWithoutUserTokenHoldsNoSecret(t *testing.T) {
	html, err := RenderHTML("dev", "", "us", "test", 256)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	get := strings.Index(html, "goGetUserToken()")
	use := strings.Index(html, "music.musicUserToken = savedToken")
	if get < 0 || use < 0 || get > use {
		t.Errorf("goGetUserToken at %d, token applied at %d: page must fetch the token before using it", get, use)
	}
	if strings.Contains(html, "'user'") {
		t.Error("user token leaked into rendered HTML")
	}
}
