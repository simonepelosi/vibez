package openurl

import "testing"

func TestValidateURL(t *testing.T) {
	valid := []string{
		"http://localhost:7777/login?state=abc",
		"https://www.last.fm/api/auth/?api_key=k&token=t",
		"https://music.apple.com/us/album/test/123",
	}
	for _, u := range valid {
		if err := validateURL(u); err != nil {
			t.Errorf("validateURL(%q) = %v, want nil", u, err)
		}
	}

	invalid := []string{
		"",
		"-flag",
		"https://example.com/\nfoo",
		"https://example.com/\rfoo",
		"https://example.com/\x00",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"smb://server/share",
		"ms-msdt:/id",
		"https:///missing-host",
		"//evil.com/path",
		"not-a-url",
	}
	for _, u := range invalid {
		if err := validateURL(u); err == nil {
			t.Errorf("validateURL(%q) = nil, want error", u)
		}
	}
}
