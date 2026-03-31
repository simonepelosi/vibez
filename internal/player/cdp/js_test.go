package cdp

import (
	"encoding/json"
	"strings"
	"testing"
)

// roundtripIDs parses the JSON string literal embedded in a vibez JS call
// and returns the decoded []string so we can assert on IDs without caring
// about whitespace or encoding details.
func parseIDsFromJS(t *testing.T, expr string, fnName string) []string {
	t.Helper()
	prefix := "window." + fnName + " && window." + fnName + "("
	if !strings.HasPrefix(expr, prefix) {
		t.Fatalf("JS expression does not start with %q:\n%s", prefix, expr)
	}
	// Strip prefix and trailing ")"
	inner := strings.TrimSuffix(strings.TrimPrefix(expr, prefix), ")")
	// inner is a JSON string literal, e.g. `"[\"123\"]"` — decode it twice.
	var jsonStr string
	if err := json.Unmarshal([]byte(inner), &jsonStr); err != nil {
		t.Fatalf("could not unmarshal JSON string from %q: %v", inner, err)
	}
	var ids []string
	if err := json.Unmarshal([]byte(jsonStr), &ids); err != nil {
		t.Fatalf("could not unmarshal IDs from %q: %v", jsonStr, err)
	}
	return ids
}

// ─── buildSetQueueJS ────────────────────────────────────────────────────────

func TestBuildSetQueueJS_SingleID(t *testing.T) {
	expr, err := buildSetQueueJS([]string{"123456789"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := parseIDsFromJS(t, expr, "vibezSetQueue")
	if len(ids) != 1 || ids[0] != "123456789" {
		t.Errorf("decoded IDs = %v, want [123456789]", ids)
	}
}

func TestBuildSetQueueJS_MultipleIDs(t *testing.T) {
	input := []string{"111", "222", "333"}
	expr, err := buildSetQueueJS(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := parseIDsFromJS(t, expr, "vibezSetQueue")
	if len(ids) != 3 {
		t.Fatalf("decoded %d IDs, want 3", len(ids))
	}
	for i, want := range input {
		if ids[i] != want {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
}

func TestBuildSetQueueJS_EmptySlice(t *testing.T) {
	expr, err := buildSetQueueJS([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := parseIDsFromJS(t, expr, "vibezSetQueue")
	if len(ids) != 0 {
		t.Errorf("decoded %d IDs, want 0", len(ids))
	}
}

func TestBuildSetQueueJS_LibraryIDs(t *testing.T) {
	// Library IDs (i.* prefix) must survive the round-trip unchanged.
	input := []string{"i.AbCdEf123", "i.XyZ456"}
	expr, err := buildSetQueueJS(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := parseIDsFromJS(t, expr, "vibezSetQueue")
	for i, want := range input {
		if ids[i] != want {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
}

func TestBuildSetQueueJS_SpecialChars(t *testing.T) {
	// Ensure IDs with quotes/backslashes are properly escaped.
	input := []string{`a"b`, `c\d`}
	expr, err := buildSetQueueJS(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := parseIDsFromJS(t, expr, "vibezSetQueue")
	for i, want := range input {
		if ids[i] != want {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
}

// ─── buildSetPlaylistJS ─────────────────────────────────────────────────────

func TestBuildSetPlaylistJS_CatalogPlaylist(t *testing.T) {
	expr, err := buildSetPlaylistJS("pl.abc123", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(expr, "vibezSetPlaylist") {
		t.Errorf("expression does not mention vibezSetPlaylist: %s", expr)
	}
	if !strings.Contains(expr, `"pl.abc123"`) {
		t.Errorf("expression does not contain playlist ID: %s", expr)
	}
	if !strings.HasSuffix(strings.TrimSpace(expr), ",0)") {
		t.Errorf("expression does not end with startIdx=0: %s", expr)
	}
}

func TestBuildSetPlaylistJS_LibraryPlaylist(t *testing.T) {
	expr, err := buildSetPlaylistJS("p.AbCdEf", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(expr, `"p.AbCdEf"`) {
		t.Errorf("library playlist ID not in expression: %s", expr)
	}
	if !strings.HasSuffix(strings.TrimSpace(expr), ",3)") {
		t.Errorf("startIdx not in expression: %s", expr)
	}
}

func TestBuildSetPlaylistJS_SpecialChars(t *testing.T) {
	expr, err := buildSetPlaylistJS(`id"with"quotes`, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The playlist ID must be properly JSON-escaped inside the expression.
	var decoded string
	// Extract the first JSON string token from the call args.
	start := strings.Index(expr, `(`) + 1
	end := strings.LastIndex(expr, `,`)
	if err := json.Unmarshal([]byte(expr[start:end]), &decoded); err != nil {
		t.Fatalf("could not decode playlist ID from expression %q: %v", expr, err)
	}
	if decoded != `id"with"quotes` {
		t.Errorf("decoded playlist ID = %q, want original", decoded)
	}
}

// ─── buildAppendQueueJS ─────────────────────────────────────────────────────

func TestBuildAppendQueueJS_SingleID(t *testing.T) {
	expr, err := buildAppendQueueJS([]string{"987654321"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := parseIDsFromJS(t, expr, "vibezAppendQueue")
	if len(ids) != 1 || ids[0] != "987654321" {
		t.Errorf("decoded IDs = %v, want [987654321]", ids)
	}
}

func TestBuildAppendQueueJS_MultipleIDs(t *testing.T) {
	input := []string{"aaa", "bbb", "ccc"}
	expr, err := buildAppendQueueJS(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := parseIDsFromJS(t, expr, "vibezAppendQueue")
	if len(ids) != len(input) {
		t.Fatalf("decoded %d IDs, want %d", len(ids), len(input))
	}
	for i, want := range input {
		if ids[i] != want {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
}

func TestBuildAppendQueueJS_LibraryIDs(t *testing.T) {
	input := []string{"i.LibraryTrack1", "i.LibraryTrack2"}
	expr, err := buildAppendQueueJS(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := parseIDsFromJS(t, expr, "vibezAppendQueue")
	for i, want := range input {
		if ids[i] != want {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
}

func TestBuildAppendQueueJS_MixedIDs(t *testing.T) {
	input := []string{"123456", "i.AbcDef", "789012"}
	expr, err := buildAppendQueueJS(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := parseIDsFromJS(t, expr, "vibezAppendQueue")
	if len(ids) != 3 {
		t.Fatalf("decoded %d IDs, want 3", len(ids))
	}
}

func TestBuildAppendQueueJS_EmptySlice(t *testing.T) {
	expr, err := buildAppendQueueJS([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := parseIDsFromJS(t, expr, "vibezAppendQueue")
	if len(ids) != 0 {
		t.Errorf("decoded %d IDs, want 0", len(ids))
	}
}

// ─── function name prefix guard ─────────────────────────────────────────────

func TestBuildSetQueueJS_GuardPrefix(t *testing.T) {
	expr, _ := buildSetQueueJS([]string{"x"})
	if !strings.HasPrefix(expr, "window.vibezSetQueue && window.vibezSetQueue(") {
		t.Errorf("guard prefix missing in: %s", expr)
	}
}

func TestBuildAppendQueueJS_GuardPrefix(t *testing.T) {
	expr, _ := buildAppendQueueJS([]string{"x"})
	if !strings.HasPrefix(expr, "window.vibezAppendQueue && window.vibezAppendQueue(") {
		t.Errorf("guard prefix missing in: %s", expr)
	}
}

func TestBuildSetPlaylistJS_GuardPrefix(t *testing.T) {
	expr, _ := buildSetPlaylistJS("pl.x", 0)
	if !strings.HasPrefix(expr, "window.vibezSetPlaylist && window.vibezSetPlaylist(") {
		t.Errorf("guard prefix missing in: %s", expr)
	}
}
