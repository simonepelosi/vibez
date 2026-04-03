package vibe_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/simone-vibes/vibez/internal/vibe"
)

func newAgent() vibe.Agent {
	return &vibe.KeywordAgent{}
}

// --- Parse tests ---

func TestParse_FocusKeyword(t *testing.T) {
	a := newAgent()
	v := a.Parse("coding session tonight")

	if v.Mood != "focused" {
		t.Errorf("Mood = %q, want %q", v.Mood, "focused")
	}
	if v.Energy != 0.5 {
		t.Errorf("Energy = %v, want 0.5", v.Energy)
	}
	if !containsGenre(v.Genres, "lofi") {
		t.Errorf("Genres %v should contain lofi", v.Genres)
	}
	if v.Query == "" {
		t.Error("Query should not be empty")
	}
}

func TestParse_GymKeyword(t *testing.T) {
	a := newAgent()
	v := a.Parse("gym heavy set")

	if v.Mood != "aggressive" {
		t.Errorf("Mood = %q, want %q", v.Mood, "aggressive")
	}
	if v.Energy != 0.9 {
		t.Errorf("Energy = %v, want 0.9", v.Energy)
	}
	if !containsGenre(v.Genres, "phonk") {
		t.Errorf("Genres %v should contain phonk", v.Genres)
	}
}

func TestParse_ChillKeyword(t *testing.T) {
	a := newAgent()
	v := a.Parse("chill afternoon")

	if v.Mood != "chill" {
		t.Errorf("Mood = %q, want %q", v.Mood, "chill")
	}
	if v.Energy != 0.2 {
		t.Errorf("Energy = %v, want 0.2", v.Energy)
	}
	if !containsGenre(v.Genres, "jazz") {
		t.Errorf("Genres %v should contain jazz", v.Genres)
	}
}

func TestParse_PartyKeyword(t *testing.T) {
	a := newAgent()
	v := a.Parse("party time hype!")

	if v.Mood != "happy" {
		t.Errorf("Mood = %q, want %q", v.Mood, "happy")
	}
	if v.Energy != 0.9 {
		t.Errorf("Energy = %v, want 0.9", v.Energy)
	}
	if !containsGenre(v.Genres, "dance") {
		t.Errorf("Genres %v should contain dance", v.Genres)
	}
}

func TestParse_SadKeyword(t *testing.T) {
	a := newAgent()
	v := a.Parse("rainy sad day")

	if v.Mood != "sad" {
		t.Errorf("Mood = %q, want %q", v.Mood, "sad")
	}
	if v.Energy != 0.3 {
		t.Errorf("Energy = %v, want 0.3", v.Energy)
	}
	if !containsGenre(v.Genres, "folk") {
		t.Errorf("Genres %v should contain folk", v.Genres)
	}
}

func TestParse_MorningKeyword(t *testing.T) {
	a := newAgent()
	v := a.Parse("morning coffee")

	if v.Mood != "happy" {
		t.Errorf("Mood = %q, want %q", v.Mood, "happy")
	}
	if v.Energy != 0.5 {
		t.Errorf("Energy = %v, want 0.5", v.Energy)
	}
	if !containsGenre(v.Genres, "indie") {
		t.Errorf("Genres %v should contain indie", v.Genres)
	}
}

func TestParse_CaseInsensitive(t *testing.T) {
	a := newAgent()

	upper := a.Parse("CODING")
	mixed := a.Parse("Coding")
	lower := a.Parse("coding")

	if upper.Mood != lower.Mood {
		t.Errorf("CODING mood %q != coding mood %q", upper.Mood, lower.Mood)
	}
	if mixed.Mood != lower.Mood {
		t.Errorf("Coding mood %q != coding mood %q", mixed.Mood, lower.Mood)
	}
}

func TestParse_UnknownInputFallback(t *testing.T) {
	a := newAgent()
	input := "electric boogaloo jazz fusion"
	v := a.Parse(input)

	if v.Mood != "unknown" {
		t.Errorf("Mood = %q, want %q", v.Mood, "unknown")
	}
	if v.Energy != 0.5 {
		t.Errorf("Energy = %v, want 0.5 (neutral)", v.Energy)
	}
	if v.Query != input {
		t.Errorf("Query = %q, want %q", v.Query, input)
	}
}

func TestParse_EmptyInput(t *testing.T) {
	a := newAgent()
	v := a.Parse("")

	// Should not panic and should return unknown vibe.
	if v == nil {
		t.Fatal("Parse returned nil")
	}
	if v.Mood != "unknown" {
		t.Errorf("Mood = %q, want %q", v.Mood, "unknown")
	}
}

func TestParse_MultipleKeywordsFirstWins(t *testing.T) {
	// "gym" and "chill" are both keywords; whichever rule matches first wins.
	a := newAgent()
	v := a.Parse("gym chill")

	// At least one of the known moods is returned — not "unknown".
	if v.Mood == "unknown" {
		t.Errorf("Mood = %q; expected a matched rule", v.Mood)
	}
}

func TestParse_QueryNotEmpty(t *testing.T) {
	a := newAgent()
	keywords := []string{"coding", "gym", "chill", "party", "sad", "morning"}
	for _, kw := range keywords {
		v := a.Parse(kw)
		if v.Query == "" {
			t.Errorf("Parse(%q).Query is empty", kw)
		}
	}
}

// --- ToSearchQuery tests ---

func TestToSearchQuery_WithGenres(t *testing.T) {
	a := newAgent()
	v := &vibe.Vibe{
		Mood:   "focused",
		Genres: []string{"electronic", "ambient"},
	}
	q := a.ToSearchQuery(v)

	if !strings.Contains(q, "electronic") {
		t.Errorf("query %q should contain first genre 'electronic'", q)
	}
	if !strings.Contains(q, "focused") {
		t.Errorf("query %q should contain mood 'focused'", q)
	}
}

func TestToSearchQuery_WithoutGenres(t *testing.T) {
	a := newAgent()
	v := &vibe.Vibe{
		Mood:   "chill",
		Genres: []string{},
	}
	q := a.ToSearchQuery(v)

	if q != "chill" {
		t.Errorf("query = %q, want %q", q, "chill")
	}
}

func TestToSearchQuery_PhraseKeyword(t *testing.T) {
	// "wake up" is a multi-word phrase — verify it matches.
	a := newAgent()
	v := a.Parse("wake up playlist")

	if v.Mood != "happy" {
		t.Errorf("Mood = %q, want happy", v.Mood)
	}
}

// --- ToSearchQueries tests ---

func TestToSearchQueries_UnknownVibeUsesKeywords(t *testing.T) {
	a := newAgent()
	v := &vibe.Vibe{
		Mood:     "unknown",
		Keywords: []string{"jazzy", "funk"},
	}
	queries := a.ToSearchQueries(v)
	if len(queries) != 1 {
		t.Fatalf("ToSearchQueries(unknown) = %d results, want 1", len(queries))
	}
	if queries[0] != "jazzy funk" {
		t.Errorf("query = %q, want %q", queries[0], "jazzy funk")
	}
}

func TestToSearchQueries_NoGenresFallsBackToMood(t *testing.T) {
	a := newAgent()
	v := &vibe.Vibe{
		Mood:   "chill",
		Genres: []string{},
	}
	queries := a.ToSearchQueries(v)
	if len(queries) != 1 {
		t.Fatalf("ToSearchQueries(no genres) = %d results, want 1", len(queries))
	}
	if queries[0] != "chill" {
		t.Errorf("query = %q, want %q", queries[0], "chill")
	}
}

func TestToSearchQueries_MatchedVibeReturnsMultiple(t *testing.T) {
	a := newAgent()
	v := a.Parse("coding session tonight")
	queries := a.ToSearchQueries(v)
	if len(queries) < 2 {
		t.Errorf("ToSearchQueries(coding) = %d queries, want >= 2", len(queries))
	}
}

func TestToSearchQueries_ContainsGenres(t *testing.T) {
	a := newAgent()
	v := a.Parse("gym heavy set")
	queries := a.ToSearchQueries(v)

	// At least one of the genre terms should appear in the results.
	found := false
	for _, q := range queries {
		for _, g := range v.Genres {
			if strings.Contains(q, g) || q == g {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Errorf("ToSearchQueries(gym) %v does not contain any known genre", queries)
	}
}

func TestToSearchQueries_GymVibeIncludesTerms(t *testing.T) {
	a := newAgent()
	v := a.Parse("gym workout")
	queries := a.ToSearchQueries(v)
	if len(queries) == 0 {
		t.Fatal("ToSearchQueries returned empty slice")
	}
}

func TestToSearchQueries_ChillVibeIncludesTerms(t *testing.T) {
	a := newAgent()
	v := a.Parse("chill relax")
	queries := a.ToSearchQueries(v)
	if len(queries) < 2 {
		t.Errorf("expected >= 2 queries for chill vibe, got %d: %v", len(queries), queries)
	}
}

func TestToSearchQueries_PartyVibeIncludesTerms(t *testing.T) {
	a := newAgent()
	v := a.Parse("party hype")
	queries := a.ToSearchQueries(v)
	if len(queries) == 0 {
		t.Fatal("ToSearchQueries returned empty slice for party vibe")
	}
}

func TestToSearchQueries_NeverReturnsNil(t *testing.T) {
	a := newAgent()
	keywords := []string{"coding", "gym", "chill", "party", "sad", "morning", "unknown-input-xyz"}
	for _, kw := range keywords {
		v := a.Parse(kw)
		q := a.ToSearchQueries(v)
		if q == nil {
			t.Errorf("ToSearchQueries(%q) returned nil", kw)
		}
		if len(q) == 0 {
			t.Errorf("ToSearchQueries(%q) returned empty slice", kw)
		}
	}
}

// --- helpers ---

func containsGenre(genres []string, want string) bool {
	return slices.Contains(genres, want)
}
