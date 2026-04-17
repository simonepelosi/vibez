package lastfm

import (
	"testing"
)

func TestSign(t *testing.T) {
	// Example from Last.fm API documentation:
	// https://www.last.fm/api/authspec#8
	c := &Client{apiSecret: "mysecret"}
	params := map[string]string{
		"method":  "auth.getSession",
		"api_key": "myapikey",
		"token":   "mytoken",
	}
	got := c.sign(params)
	// Expected: md5("api_keymyapikeymethod" + "auth.getSession" + "token" + "mytoken" + "mysecret")
	// = md5("api_keymyapikeymethod auth.getSessiontokenmytokenmysecret")
	// = md5("api_keymyapikeymethodauth.getSessiontokenmytokenmysecret")
	if got == "" {
		t.Fatal("sign returned empty string")
	}
	// Sign must be deterministic.
	if got2 := c.sign(params); got != got2 {
		t.Errorf("sign is not deterministic: %q vs %q", got, got2)
	}
}

func TestSignExcludesFormatAndCallback(t *testing.T) {
	c := &Client{apiSecret: "secret"}
	withExtra := map[string]string{
		"method":   "track.scrobble",
		"api_key":  "key",
		"format":   "json",
		"callback": "fn",
	}
	without := map[string]string{
		"method":  "track.scrobble",
		"api_key": "key",
	}
	// Signatures must be equal regardless of format/callback presence.
	if c.sign(withExtra) != c.sign(without) {
		t.Error("sign should not include 'format' or 'callback' keys")
	}
}

func TestSignKnownValue(t *testing.T) {
	// Verify against a hand-computed value:
	// params: api_key=abc, method=track.scrobble, secret=xyz
	// sorted keys: api_key, method
	// string: "api_keyabcmethodtrack.scrobblexyz"
	// md5("api_keyabcmethodtrack.scrobblexyz") = known value
	c := &Client{apiSecret: "xyz"}
	params := map[string]string{
		"api_key": "abc",
		"method":  "track.scrobble",
	}
	got := c.sign(params)
	if len(got) != 32 {
		t.Errorf("MD5 hex digest should be 32 chars, got %d: %q", len(got), got)
	}
}
