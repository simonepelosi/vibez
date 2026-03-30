package vibe

import "strings"

// Vibe describes the emotional and musical characteristics of a request.
type Vibe struct {
	Mood     string  // "happy", "sad", "aggressive", "chill", "focused"
	Energy   float64 // 0.0–1.0
	Genres   []string
	Keywords []string
	Query    string // final search query
}

// Agent parses natural language input into a Vibe and produces a search query.
type Agent interface {
	Parse(input string) *Vibe
	ToSearchQuery(v *Vibe) string
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
}

var rules = []rule{
	{
		keywords: []string{"coding", "focus", "concentrate"},
		mood:     "focused",
		energy:   0.5,
		genres:   []string{"electronic", "ambient", "lofi"},
	},
	{
		keywords: []string{"gym", "workout", "aggressive", "heavy"},
		mood:     "aggressive",
		energy:   0.9,
		genres:   []string{"hip-hop", "metal", "phonk"},
	},
	{
		keywords: []string{"chill", "relax", "calm", "sunset"},
		mood:     "chill",
		energy:   0.2,
		genres:   []string{"jazz", "indie", "ambient"},
	},
	{
		keywords: []string{"party", "hype", "dance"},
		mood:     "happy",
		energy:   0.9,
		genres:   []string{"pop", "dance", "electronic"},
	},
	{
		keywords: []string{"sad", "melancholy", "rainy", "night"},
		mood:     "sad",
		energy:   0.3,
		genres:   []string{"indie", "alternative", "folk"},
	},
	{
		keywords: []string{"morning", "wake up", "coffee"},
		mood:     "happy",
		energy:   0.5,
		genres:   []string{"pop", "indie"},
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

// ToSearchQuery builds an Apple Music / generic search string from a Vibe.
func (a *KeywordAgent) ToSearchQuery(v *Vibe) string {
	if len(v.Genres) == 0 {
		return v.Mood
	}
	// Use the first genre + mood as the query for best results.
	return v.Genres[0] + " " + v.Mood
}
