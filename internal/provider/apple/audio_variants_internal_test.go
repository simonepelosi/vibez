package apple

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"
)

func TestNormalizeAudioVariants(t *testing.T) {
	for _, tc := range []struct {
		name     string
		variants []string
		traits   []string
		want     []string
	}{
		{"none", nil, nil, nil},
		{"variants kept in order", []string{"lossy-stereo", "lossless", "hi-res-lossless", "dolby-atmos"}, nil,
			[]string{"lossy-stereo", "lossless", "hi-res-lossless", "dolby-atmos"}},
		{"case and space", []string{" Lossless ", "Dolby-Atmos"}, nil, []string{"lossless", "dolby-atmos"}},
		{"amp-api traits map onto variants", nil, []string{"atmos", "spatial", "lossless"},
			[]string{"dolby-atmos", "spatial", "lossless"}},
		{"both lists de-duplicate", []string{"lossless", "dolby-atmos"}, []string{"atmos", "lossless", ""},
			[]string{"lossless", "dolby-atmos"}},
	} {
		if got := normalizeAudioVariants(tc.variants, tc.traits); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAudioVariantsEndpointHelpers(t *testing.T) {
	ep := withAudioVariants("/catalog/us/songs?ids=1%2C2")
	if !hasAudioVariants(ep) {
		t.Fatalf("withAudioVariants = %q, want extend=audioVariants", ep)
	}
	if got := withAudioVariants(ep); got != ep {
		t.Errorf("withAudioVariants is not idempotent: %q then %q", ep, got)
	}
	plain := withoutAudioVariants(ep)
	if hasAudioVariants(plain) || plain != "/catalog/us/songs?ids=1%2C2" {
		t.Errorf("withoutAudioVariants = %q", plain)
	}
}

func TestIsBadRequestUsesStatusNotMessage(t *testing.T) {
	bad := &apiError{status: http.StatusBadRequest, msg: "anything"}
	if !isBadRequest(bad) || !isBadRequest(fmt.Errorf("wrapped: %w", bad)) {
		t.Error("isBadRequest missed a 400")
	}
	if isBadRequest(&apiError{status: http.StatusUnauthorized, msg: "apple music api 400 lookalike"}) {
		t.Error("isBadRequest matched a 401 by its message")
	}
	if isBadRequest(errors.New("apple music api 400 Bad Request")) {
		t.Error("isBadRequest matched a plain error by its message")
	}
}
