package vibe

import (
	"math/rand"
	"strings"
)

// Vibe describes the emotional and musical characteristics of a request.
type Vibe struct {
	Mood     string  // "happy", "sad", "aggressive", "chill", "focused"
	Energy   float64 // 0.0–1.0
	Genres   []string
	Keywords []string
	Query    string // final search query
}

// Agent parses natural language input into a Vibe and produces search queries.
type Agent interface {
	Parse(input string) *Vibe
	ToSearchQuery(v *Vibe) string
	// ToSearchQueries returns multiple diverse search terms for variety across sessions.
	ToSearchQueries(v *Vibe) []string
}

// KeywordAgent implements a simple keyword → vibe mapping.
type KeywordAgent struct{}

// LLMAgent is a placeholder for a future LLM-powered implementation.
type LLMAgent struct {
	// Endpoint string // e.g. OpenAI / Ollama URL
}

type rule struct {
	keywords []string
	mood     string
	energy   float64
	genres   []string
	// terms are extra search term variants beyond genre+mood combinations.
	terms []string
}

var rules = []rule{
	{
		keywords: []string{"coding", "focus", "concentrate", "work", "study", "programming"},
		mood:     "focused",
		energy:   0.5,
		genres:   []string{"lofi", "ambient", "electronic", "instrumental", "chillhop"},
		terms:    []string{"lofi hip hop", "study beats", "focus music", "brain food", "deep work playlist"},
	},
	{
		keywords: []string{"gym", "workout", "aggressive", "heavy", "run", "training", "sport"},
		mood:     "aggressive",
		energy:   0.9,
		genres:   []string{"hip-hop", "phonk", "metal", "electronic", "rap"},
		terms:    []string{"workout hits", "gym motivation", "trap workout", "phonk drift", "pump up"},
	},
	{
		keywords: []string{"chill", "relax", "calm", "easy", "sunday", "cozy", "lazy"},
		mood:     "chill",
		energy:   0.2,
		genres:   []string{"jazz", "indie", "acoustic", "soul", "bossa nova"},
		terms:    []string{"chill vibes", "sunday morning", "mellow beats", "cozy indie", "rainy afternoon"},
	},
	{
		keywords: []string{"sunset", "drive", "road", "cruise", "summer"},
		mood:     "nostalgic",
		energy:   0.6,
		genres:   []string{"pop", "indie pop", "synthwave", "r&b"},
		terms:    []string{"sunset drive", "summer hits", "feel good", "road trip playlist", "summer indie"},
	},
	{
		keywords: []string{"party", "hype", "dance", "club", "energy", "banger"},
		mood:     "happy",
		energy:   0.9,
		genres:   []string{"pop", "dance", "electronic", "edm", "afrobeats"},
		terms:    []string{"party hits", "dance floor", "banger playlist", "club music", "edm hits"},
	},
	{
		keywords: []string{"sad", "melancholy", "heartbreak", "lonely", "miss", "cry", "emo"},
		mood:     "sad",
		energy:   0.3,
		genres:   []string{"indie", "alternative", "folk", "bedroom pop", "slowcore"},
		terms:    []string{"sad songs", "heartbreak playlist", "indie sad", "cry it out", "melancholy pop"},
	},
	{
		keywords: []string{"night", "late", "midnight", "dark", "sleep", "insomnia"},
		mood:     "introspective",
		energy:   0.3,
		genres:   []string{"ambient", "electronic", "dream pop", "shoegaze", "slowcore"},
		terms:    []string{"late night vibes", "midnight music", "dark ambient", "night drive", "insomnia playlist"},
	},
	{
		keywords: []string{"morning", "wake", "coffee", "breakfast", "fresh", "start"},
		mood:     "happy",
		energy:   0.5,
		genres:   []string{"pop", "indie", "acoustic", "funk", "soul"},
		terms:    []string{"morning vibes", "good morning playlist", "coffee shop", "fresh start", "wake up music"},
	},
	{
		keywords: []string{"blues", "soul", "funk", "groove", "jazzy"},
		mood:     "groovy",
		energy:   0.6,
		genres:   []string{"jazz", "blues", "soul", "funk", "r&b"},
		terms:    []string{"jazz essentials", "soul classics", "funk groove", "late night jazz", "smooth jazz"},
	},
	{
		keywords: []string{"romantic", "love", "date", "valentine", "dinner"},
		mood:     "romantic",
		energy:   0.4,
		genres:   []string{"r&b", "soul", "jazz", "pop", "indie"},
		terms:    []string{"romantic playlist", "love songs", "dinner music", "date night", "smooth r&b"},
	},
}

// Parse turns a free-text input string into a Vibe using keyword matching.
// If no rule matches, it returns a neutral vibe using the raw input as a query hint.
func (a *KeywordAgent) Parse(input string) *Vibe {
	lower := strings.ToLower(input)
	words := strings.Fields(lower)

	for _, r := range rules {
		for _, kw := range r.keywords {
			for _, word := range words {
				if word == kw || strings.Contains(lower, kw) {
					v := &Vibe{
						Mood:     r.mood,
						Energy:   r.energy,
						Genres:   r.genres,
						Keywords: r.keywords,
					}
					v.Query = a.ToSearchQuery(v)
					return v
				}
			}
		}
	}

	// No rule matched — pass input through as-is.
	return &Vibe{
		Mood:     "unknown",
		Energy:   0.5,
		Keywords: words,
		Query:    input,
	}
}

// ToSearchQuery returns a single representative search query for the vibe.
// For a matched vibe it always returns "<genre> <mood>" so tests can assert
// on predictable content; callers that want variety should use ToSearchQueries.
func (a *KeywordAgent) ToSearchQuery(v *Vibe) string {
	if v.Mood == "unknown" || len(v.Genres) == 0 {
		return v.Mood
	}
	return v.Genres[0] + " " + v.Mood
}

// ToSearchQueries returns several diverse search terms for the vibe so callers
// can query multiple terms and combine results for variety.
func (a *KeywordAgent) ToSearchQueries(v *Vibe) []string {
	if v.Mood == "unknown" {
		// Raw pass-through: use the input keywords directly.
		return []string{strings.Join(v.Keywords, " ")}
	}
	if len(v.Genres) == 0 {
		return []string{v.Mood}
	}

	// Find the matching rule to get its extra terms.
	var extraTerms []string
	for _, r := range rules {
		if r.mood == v.Mood && len(r.genres) > 0 && r.genres[0] == v.Genres[0] {
			extraTerms = r.terms
			break
		}
	}

	queries := make([]string, 0, len(v.Genres)+len(extraTerms))
	queries = append(queries, v.Genres...)
	queries = append(queries, extraTerms...)

	// Shuffle so different queries are tried each call. //nolint:gosec
	rand.Shuffle(len(queries), func(i, j int) { queries[i], queries[j] = queries[j], queries[i] })
	return queries
}
