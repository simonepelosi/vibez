package vibe_test

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestParse_WorkoutDoesNotFalseMatchWork(t *testing.T) {
	a := newAgent()
	for _, input := range []string{"workout", "workout!", "work out", "working out"} {
		v := a.Parse(input)
		if v.Mood != "aggressive" {
			t.Errorf("Parse(%q).Mood = %q, want %q", input, v.Mood, "aggressive")
		}
		if !containsGenre(v.Genres, "phonk") {
			t.Errorf("Parse(%q).Genres = %v, want phonk", input, v.Genres)
		}
	}

	for _, input := range []string{"deep work", "working", "studying"} {
		v := a.Parse(input)
		if v.Mood != "focused" {
			t.Errorf("Parse(%q).Mood = %q, want %q", input, v.Mood, "focused")
		}
	}
}

func TestParse_WordBoundariesPreventSubstringFalsePositives(t *testing.T) {
	a := newAgent()
	falsePositives := []string{
		"uneasy",       // contains "easy"
		"grunge",       // contains "run"
		"drunk",        // contains "run"
		"transmission", // contains "miss"
		"chocolate",    // contains "late"
		"lemon",        // contains "emo"
		"memory",       // contains "emo"
		"network",      // contains "work"
		"artwork",      // contains "work"
		"mandate",      // contains "date"
		"glove",        // contains "love"
	}
	for _, input := range falsePositives {
		v := a.Parse(input)
		if v.Mood != "unknown" {
			t.Errorf("Parse(%q).Mood = %q, want %q (false-positive substring match)", input, v.Mood, "unknown")
		}
		if v.Query != input {
			t.Errorf("Parse(%q).Query = %q, want %q", input, v.Query, input)
		}
	}
}

func TestParse_NegationAndClauseBoundaries(t *testing.T) {
	a := newAgent()

	if v := a.Parse("not sad, party!"); v.Mood != "happy" {
		t.Errorf("Parse('not sad, party!').Mood = %q, want happy", v.Mood)
	}
	if v := a.Parse("no chill, gym"); v.Mood != "aggressive" {
		t.Errorf("Parse('no chill, gym').Mood = %q, want aggressive", v.Mood)
	}
	if v := a.Parse("don't want sad songs, hype"); v.Mood != "happy" {
		t.Errorf("Parse(\"don't want sad songs, hype\").Mood = %q, want happy", v.Mood)
	}
	if v := a.Parse("not chill"); v.Mood != "unknown" {
		t.Errorf("Parse('not chill').Mood = %q, want unknown", v.Mood)
	}
	if v := a.Parse("don't want, chill"); v.Mood != "chill" {
		t.Errorf("Parse(\"don't want, chill\").Mood = %q, want chill", v.Mood)
	}
}

func TestToSearchQuery_UnknownUsesQueryInsteadOfLiteralUnknown(t *testing.T) {
	a := newAgent()
	v := a.Parse("radiohead")
	if got := a.ToSearchQuery(v); got != "radiohead" {
		t.Errorf("ToSearchQuery(Parse('radiohead')) = %q, want %q", got, "radiohead")
	}

	if got := a.ToSearchQuery(nil); got != "" {
		t.Errorf("ToSearchQuery(nil) = %q, want empty", got)
	}
	if qs := a.ToSearchQueries(nil); qs == nil || len(qs) != 0 {
		t.Errorf("ToSearchQueries(nil) = %#v, want non-nil empty slice", qs)
	}
}

func TestToSearchQueries_MorningDoesNotReturnPartyClubTerms(t *testing.T) {
	a := newAgent()
	v := a.Parse("morning coffee")
	queries := a.ToSearchQueries(v)
	for _, q := range queries {
		if q == "edm" || q == "club music" || q == "banger playlist" {
			t.Errorf("ToSearchQueries('morning coffee') returned party term %q: %v", q, queries)
		}
	}
	if !slices.Contains(queries, "morning vibes") && !slices.Contains(queries, "coffee shop") {
		t.Errorf("ToSearchQueries('morning coffee') missing morning terms: %v", queries)
	}
}

func TestParse_ReturnedSlicesAreIsolatedCopies(t *testing.T) {
	a := newAgent()
	v1 := a.Parse("coding")
	v1.Genres[0] = "corrupted-genre"
	v1.Keywords[0] = "corrupted-keyword"

	v2 := a.Parse("coding")
	if v2.Genres[0] == "corrupted-genre" || v2.Keywords[0] == "corrupted-keyword" {
		t.Fatalf("mutating returned Vibe corrupted global rules: %+v", v2)
	}
}

func TestParse_SecuritySanitizesEscapeBiDiInvalidUTF8AndOversizedInput(t *testing.T) {
	a := newAgent()

	// ANSI escapes + BiDi override + invalid UTF-8 around a known keyword.
	vKnown := a.Parse("\x1b[31m\u202eworkout\u202c\x1b[0m\xff\xfe\n")
	if vKnown.Mood != "aggressive" {
		t.Fatalf("Mood = %q, want aggressive", vKnown.Mood)
	}

	// Unknown input must be stripped of escapes/BiDi/control chars before becoming a query.
	vUnknown := a.Parse("\x1b]52;c;evil\x07\x1b[31mSigur\t\u202eRós\u202c\x00\x7f\u0085\xff\n")
	if !utf8.ValidString(vUnknown.Query) {
		t.Fatalf("Query is invalid UTF-8: %q", vUnknown.Query)
	}
	if vUnknown.Query != "Sigur Rós" {
		t.Fatalf("Query = %q, want %q", vUnknown.Query, "Sigur Rós")
	}

	// Oversized multi-kilobyte input with 4-byte emoji straddling byte cap is bounded cleanly.
	vHuge := a.Parse(strings.Repeat("🎸", 1000))
	if !utf8.ValidString(vHuge.Query) {
		t.Fatalf("oversized Query is invalid UTF-8")
	}
	if utf8.RuneCountInString(vHuge.Query) > 256 {
		t.Fatalf("oversized Query rune count = %d, want <= 256", utf8.RuneCountInString(vHuge.Query))
	}

	// Control-only input produces empty sanitized query and non-nil empty ToSearchQueries slice.
	vCtrl := a.Parse("\x1b[2J\x00\u202e\r\n\t")
	if vCtrl.Query != "" {
		t.Fatalf("control-only Query = %q, want empty", vCtrl.Query)
	}
	if qs := a.ToSearchQueries(vCtrl); qs == nil || len(qs) != 0 {
		t.Fatalf("ToSearchQueries(control-only) = %#v, want non-nil empty slice", qs)
	}
}

func TestParse_PluralsAndDerivationalStemsAndUnicodeSpaces(t *testing.T) {
	a := newAgent()

	matched := []struct {
		input string
		mood  string
	}{
		{"rainy nights", "introspective"},
		{"late nights", "introspective"},
		{"relaxation", "chill"},
		{"danceable", "happy"},
		{"parties", "happy"},
		{"heartbreaks", "sad"},
		{"sadness", "sad"},
		{"loneliness", "sad"},
		{"studies", "focused"},
		{"concentration", "focused"},
	}
	for _, tc := range matched {
		if v := a.Parse(tc.input); v.Mood != tc.mood {
			t.Errorf("Parse(%q).Mood = %q, want %q", tc.input, v.Mood, tc.mood)
		}
	}

	// Ensure stemmer does not over-strip short/unrelated words.
	for _, trap := range []string{"demonstration", "table", "notable"} {
		if v := a.Parse(trap); v.Mood != "unknown" {
			t.Errorf("Parse(%q).Mood = %q, want unknown", trap, v.Mood)
		}
	}

	// Non-ASCII spaces (U+3000 ideographic space, U+00A0 NBSP) and ZWNJ/ZWJ.
	if v := a.Parse("雨の日\u3000チル"); a.ToSearchQuery(v) != "雨の日 チル" {
		t.Errorf("ideographic space query = %q, want %q", a.ToSearchQuery(v), "雨の日 チル")
	}
	if v := a.Parse("Sigur\u00a0Rós"); a.ToSearchQuery(v) != "Sigur Rós" {
		t.Errorf("NBSP query = %q, want %q", a.ToSearchQuery(v), "Sigur Rós")
	}
	if v := a.Parse("می\u200cخواهم"); a.ToSearchQuery(v) != "می\u200cخواهم" {
		t.Errorf("ZWNJ query = %q, want %q", a.ToSearchQuery(v), "می\u200cخواهم")
	}
}
