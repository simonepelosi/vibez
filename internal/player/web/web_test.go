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

// On a saved token MusicKit never fetches /v1/me/storefront, so storefrontId
// is its localStorage value or its default "us" until the first /v1/me/account
// call sets storefrontCountryCode and raises storefrontCountryCodeDidChange.
// storefrontIdentifierDidChange belongs to a different property that never
// fires on this page, and a page listening to it saved "us" over every non-US
// account's storefront on each relaunch (#163).
func TestRenderHTMLListensForStorefrontCountryCode(t *testing.T) {
	html, err := RenderHTML("dev", "user", "us", "test", 256)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if !strings.Contains(html, "addEventListener('storefrontCountryCodeDidChange'") {
		t.Error("rendered HTML does not listen for storefrontCountryCodeDidChange")
	}
	if strings.Contains(html, "addEventListener('storefrontIdentifierDidChange'") {
		t.Error("rendered HTML still listens for storefrontIdentifierDidChange, which never fires with a saved token")
	}
}

// Go passes a storefront only alongside a saved session, once the startup
// check has confirmed it. The page then starts MusicKit on it, so its own
// catalog lookups use the account's storefront from the first request.
func TestRenderHTMLConfiguresStorefrontOnlyWhenGiven(t *testing.T) {
	html, err := RenderHTML("dev", "", "ar", "test", 256)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if !strings.Contains(html, "configOpts.storefrontId = 'ar'") {
		t.Error("rendered HTML does not configure MusicKit with the given storefront")
	}
	html, err = RenderHTML("dev", "", "", "test", 256)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if strings.Contains(html, "configOpts.storefrontId =") {
		t.Error("rendered HTML configures a storefront when none was given")
	}
}

// A storefront from a hand-edited config.json is interpolated into JS string
// literals, so anything that isn't a storefront ID must not reach the page.
func TestRenderHTMLDropsMalformedStorefront(t *testing.T) {
	for _, sf := range []string{"us'; alert(1); '", "us`", "us\\", "u s"} {
		html, err := RenderHTML("dev", "", sf, "test", 256)
		if err != nil {
			t.Fatalf("RenderHTML(%q): %v", sf, err)
		}
		if strings.Contains(html, "configOpts.storefrontId =") {
			t.Errorf("RenderHTML(%q) configured MusicKit with a malformed storefront", sf)
		}
		if strings.Contains(html, sf) {
			t.Errorf("RenderHTML(%q) rendered the malformed storefront into the page", sf)
		}
	}
}

// MusicKit's setQueue() calls assertUserStorefront(), which throws
// CONTENT_EQUIVALENT whenever storekit.storefrontCountryCode !== storefrontId.
// On a saved token authorize() is skipped, leaving storefrontCountryCode at
// its default "us" until music.me() fetches /v1/me/account.
func TestRenderHTMLSyncsAccountStorefrontBeforeReady(t *testing.T) {
	html, err := RenderHTML("dev", "user", "it", "test", 256)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	meIdx := strings.Index(html, "await music.me()")
	syncIdx := strings.Index(html, "await syncAccountStorefront()")
	readyIdx := strings.Index(html, "await goAuthComplete('done')")
	if meIdx < 0 || syncIdx < 0 || readyIdx < 0 || meIdx > readyIdx || syncIdx > readyIdx {
		t.Errorf("music.me() at %d, syncAccountStorefront() at %d, goAuthComplete at %d: page must sync account storefront before signalling ready", meIdx, syncIdx, readyIdx)
	}
	if !strings.Contains(html, "window.vibezSetUserToken = async function(token)") {
		t.Error("rendered HTML does not expose window.vibezSetUserToken for mid-session re-auth")
	}
	if !strings.Contains(html, "aligning storefrontId=") {
		t.Error("rendered HTML does not log storefrontId fallback via goLog when music.me() fails")
	}
}
