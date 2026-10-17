package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// statusServer serves a fixed status code and records the request it saw.
func statusServer(t *testing.T, code int) (*httptest.Server, *http.Header) {
	t.Helper()
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// deadURL points at a port that refuses connections.
const deadURL = "http://127.0.0.1:1"

func TestCheckTokens_BothValid(t *testing.T) {
	user, _ := statusServer(t, http.StatusOK)
	dev, _ := statusServer(t, http.StatusOK)

	if got, _ := checkTokens(user.URL, dev.URL, "dev", "user"); got != TokensValid {
		t.Errorf("checkTokens = %v, want TokensValid", got)
	}
}

func TestCheckTokens_UserTokenExpired(t *testing.T) {
	// The combined request is rejected but the developer token alone works,
	// which isolates the Music User Token as the expired credential.
	user, _ := statusServer(t, http.StatusUnauthorized)
	dev, _ := statusServer(t, http.StatusOK)

	if got, _ := checkTokens(user.URL, dev.URL, "dev", "user"); got != UserTokenRejected {
		t.Errorf("checkTokens = %v, want UserTokenRejected", got)
	}
}

// TestCheckTokens_DeveloperTokenExpired is the regression test for the bug
// where an expired developer token was reported as an expired session, and the
// caller then cleared a perfectly good Music User Token.
func TestCheckTokens_DeveloperTokenExpired(t *testing.T) {
	user, _ := statusServer(t, http.StatusUnauthorized)
	dev, _ := statusServer(t, http.StatusUnauthorized)

	if got, _ := checkTokens(user.URL, dev.URL, "dev", "user"); got != DeveloperTokenRejected {
		t.Errorf("checkTokens = %v, want DeveloperTokenRejected", got)
	}
}

func TestCheckTokens_ForbiddenIsRejection(t *testing.T) {
	user, _ := statusServer(t, http.StatusForbidden)
	dev, _ := statusServer(t, http.StatusForbidden)

	if got, _ := checkTokens(user.URL, dev.URL, "dev", "user"); got != DeveloperTokenRejected {
		t.Errorf("checkTokens = %v, want DeveloperTokenRejected", got)
	}
}

func TestCheckTokens_MissingCredentials(t *testing.T) {
	if got, _ := CheckTokens("", "user"); got != DeveloperTokenRejected {
		t.Errorf("empty devToken = %v, want DeveloperTokenRejected", got)
	}
	if got, _ := CheckTokens("dev", ""); got != UserTokenRejected {
		t.Errorf("empty userToken = %v, want UserTokenRejected", got)
	}
	if got, _ := CheckTokens("", ""); got != DeveloperTokenRejected {
		t.Errorf("both empty = %v, want DeveloperTokenRejected", got)
	}
}

func TestCheckTokens_NetworkErrorKeepsSession(t *testing.T) {
	dev, _ := statusServer(t, http.StatusOK)

	if got, _ := checkTokens(deadURL, dev.URL, "dev", "user"); got != TokensValid {
		t.Errorf("checkTokens = %v, want TokensValid on network error", got)
	}
}

// A rejection the tiebreaker cannot attribute must not cost the user a session.
func TestCheckTokens_InconclusiveTiebreakerKeepsSession(t *testing.T) {
	user, _ := statusServer(t, http.StatusUnauthorized)

	if got, _ := checkTokens(user.URL, deadURL, "dev", "user"); got != TokensValid {
		t.Errorf("checkTokens = %v, want TokensValid on inconclusive tiebreaker", got)
	}
}

// A tiebreaker that fails for reasons of Apple's own must not be read as proof
// the developer token is good, because that attributes the rejection to the
// user token and clears a session that was never the problem.
func TestCheckTokens_ServerErrorTiebreakerKeepsSession(t *testing.T) {
	for _, code := range []int{
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
	} {
		user, _ := statusServer(t, http.StatusUnauthorized)
		dev, _ := statusServer(t, code)

		if got, _ := checkTokens(user.URL, dev.URL, "dev", "user"); got != TokensValid {
			t.Errorf("tiebreaker %d: checkTokens = %v, want TokensValid", code, got)
		}
	}
}

func TestCheckTokens_SendsBothCredentials(t *testing.T) {
	user, userHeaders := statusServer(t, http.StatusOK)
	dev, _ := statusServer(t, http.StatusOK)

	checkTokens(user.URL, dev.URL, "mydev", "myuser")

	if got := userHeaders.Get("Authorization"); got != "Bearer mydev" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer mydev")
	}
	if got := userHeaders.Get("Music-User-Token"); got != "myuser" {
		t.Errorf("Music-User-Token = %q, want %q", got, "myuser")
	}
}

// The tiebreaker must carry the developer token alone, or it proves nothing.
func TestCheckTokens_TiebreakerOmitsUserToken(t *testing.T) {
	user, _ := statusServer(t, http.StatusUnauthorized)
	dev, devHeaders := statusServer(t, http.StatusOK)

	checkTokens(user.URL, dev.URL, "mydev", "myuser")

	if got := devHeaders.Get("Authorization"); got != "Bearer mydev" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer mydev")
	}
	if got := devHeaders.Get("Music-User-Token"); got != "" {
		t.Errorf("Music-User-Token = %q, want it unset", got)
	}
}

// bodyServer serves a fixed JSON body with a 200.
func bodyServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestCheckTokens_ReportsStorefront is the regression test for #163: the
// startup check already asks /v1/me/storefront, and its answer is the
// account's storefront, which the player page cannot know on a saved token.
func TestCheckTokens_ReportsStorefront(t *testing.T) {
	user := bodyServer(t, `{"data":[{"id":"ar","type":"storefronts","href":"/v1/storefronts/ar"}]}`)
	dev, _ := statusServer(t, http.StatusOK)

	status, sf := checkTokens(user.URL, dev.URL, "dev", "user")
	if status != TokensValid || sf != "ar" {
		t.Errorf("checkTokens = %v, %q, want TokensValid, \"ar\"", status, sf)
	}
}

func TestCheckTokens_NoStorefrontWithoutBody(t *testing.T) {
	user, _ := statusServer(t, http.StatusOK)
	dev, _ := statusServer(t, http.StatusOK)

	if _, sf := checkTokens(user.URL, dev.URL, "dev", "user"); sf != "" {
		t.Errorf("storefront = %q, want \"\"", sf)
	}
}

// The tiebreaker lists every storefront there is; none of them is the
// account's, and an inconclusive first request has no answer either.
func TestCheckTokens_NoStorefrontFromTiebreakerOrNetworkError(t *testing.T) {
	user, _ := statusServer(t, http.StatusUnauthorized)
	dev := bodyServer(t, `{"data":[{"id":"us"},{"id":"ar"}]}`)

	if status, sf := checkTokens(user.URL, dev.URL, "dev", "user"); status != UserTokenRejected || sf != "" {
		t.Errorf("checkTokens = %v, %q, want UserTokenRejected, \"\"", status, sf)
	}
	if status, sf := checkTokens(deadURL, dev.URL, "dev", "user"); status != TokensValid || sf != "" {
		t.Errorf("checkTokens = %v, %q, want TokensValid, \"\"", status, sf)
	}
}
