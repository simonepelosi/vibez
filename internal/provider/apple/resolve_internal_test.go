package apple

import "testing"

func TestResolveURL(t *testing.T) {
	const base = "https://api.music.apple.com/v1"
	ok := map[string]string{
		"/me/library/songs":                  base + "/me/library/songs",
		"/v1/me/library/songs?offset=100":    base + "/me/library/songs?offset=100",
		base + "/me/library/songs?offset=25": base + "/me/library/songs?offset=25",
	}
	for in, want := range ok {
		got, err := resolveURL(base, in)
		if err != nil || got != want {
			t.Errorf("resolveURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"https://evil.example/v1/me/library/songs",
		"http://api.music.apple.com/v1/me/library/songs",
		"https://api.music.apple.com.evil.example/v1/x",
		"https://api.music.apple.com@evil.example/v1/x",
	} {
		if got, err := resolveURL(base, in); err == nil {
			t.Errorf("resolveURL(%q) = %q, want refused", in, got)
		}
	}
}
