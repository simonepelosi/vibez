package vibe

import (
	"math/rand"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

const (
	// maxVibeInputBytes bounds raw input bytes inspected before stripping escapes.
	maxVibeInputBytes = 512
	// maxVibeInputRunes bounds sanitized query length produced by Parse/ToSearchQuery.
	maxVibeInputRunes = 256
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

// KeywordAgent implements a word-boundary and phrase-aware keyword → vibe mapping.
type KeywordAgent struct{}

// LLMAgent is a placeholder for a future LLM-powered implementation.
type LLMAgent struct {
	// Endpoint string // e.g. OpenAI / Ollama URL
}

type rule struct {
	keywords []string
	aliases  []string
	mood     string
	energy   float64
	genres   []string
	// terms are extra search term variants beyond genre+mood combinations.
	terms []string
}

var rules = []rule{
	{
		keywords: []string{"coding", "focus", "concentrate", "work", "study", "programming"},
		aliases:  []string{"focused", "focusing", "concentrating", "working", "studying", "deep work"},
		mood:     "focused",
		energy:   0.5,
		genres:   []string{"lofi", "ambient", "electronic", "instrumental", "chillhop"},
		terms:    []string{"lofi hip hop", "study beats", "focus music", "brain food", "deep work playlist"},
	},
	{
		keywords: []string{"gym", "workout", "aggressive", "heavy", "run", "training", "sport"},
		aliases:  []string{"workouts", "work out", "working out", "running", "sports", "cardio", "lifting"},
		mood:     "aggressive",
		energy:   0.9,
		genres:   []string{"hip-hop", "phonk", "metal", "electronic", "rap"},
		terms:    []string{"workout hits", "gym motivation", "trap workout", "phonk drift", "pump up"},
	},
	{
		keywords: []string{"chill", "relax", "calm", "easy", "sunday", "cozy", "lazy"},
		aliases:  []string{"chilling", "relaxing", "relaxed", "calming", "easygoing"},
		mood:     "chill",
		energy:   0.2,
		genres:   []string{"jazz", "indie", "acoustic", "soul", "bossa nova"},
		terms:    []string{"chill vibes", "sunday morning", "mellow beats", "cozy indie", "rainy afternoon"},
	},
	{
		keywords: []string{"sunset", "drive", "road", "cruise", "summer"},
		aliases:  []string{"driving", "cruising", "road trip"},
		mood:     "nostalgic",
		energy:   0.6,
		genres:   []string{"pop", "indie pop", "synthwave", "r&b"},
		terms:    []string{"sunset drive", "summer hits", "feel good", "road trip playlist", "summer indie"},
	},
	{
		keywords: []string{"party", "hype", "dance", "club", "energy", "banger"},
		aliases:  []string{"partying", "hyped", "dancing", "clubbing", "energetic", "bangers"},
		mood:     "happy",
		energy:   0.9,
		genres:   []string{"pop", "dance", "electronic", "edm", "afrobeats"},
		terms:    []string{"party hits", "dance floor", "banger playlist", "club music", "edm hits"},
	},
	{
		keywords: []string{"sad", "melancholy", "heartbreak", "lonely", "miss", "cry", "emo"},
		aliases:  []string{"melancholic", "heartbroken", "crying", "missing you"},
		mood:     "sad",
		energy:   0.3,
		genres:   []string{"indie", "alternative", "folk", "bedroom pop", "slowcore"},
		terms:    []string{"sad songs", "heartbreak playlist", "indie sad", "cry it out", "melancholy pop"},
	},
	{
		keywords: []string{"night", "late", "midnight", "dark", "sleep", "insomnia"},
		aliases:  []string{"late night", "sleeping", "sleepy"},
		mood:     "introspective",
		energy:   0.3,
		genres:   []string{"ambient", "electronic", "dream pop", "shoegaze", "slowcore"},
		terms:    []string{"late night vibes", "midnight music", "dark ambient", "night drive", "insomnia playlist"},
	},
	{
		keywords: []string{"morning", "wake", "coffee", "breakfast", "fresh", "start"},
		aliases:  []string{"wake up", "waking up", "fresh start"},
		mood:     "happy",
		energy:   0.5,
		genres:   []string{"pop", "indie", "acoustic", "funk", "soul"},
		terms:    []string{"morning vibes", "good morning playlist", "coffee shop", "fresh start", "wake up music"},
	},
	{
		keywords: []string{"blues", "soul", "funk", "groove", "jazzy"},
		aliases:  []string{"funky", "groovy", "grooving"},
		mood:     "groovy",
		energy:   0.6,
		genres:   []string{"jazz", "blues", "soul", "funk", "r&b"},
		terms:    []string{"jazz essentials", "soul classics", "funk groove", "late night jazz", "smooth jazz"},
	},
	{
		keywords: []string{"romantic", "love", "date", "valentine", "dinner"},
		aliases:  []string{"date night", "valentines"},
		mood:     "romantic",
		energy:   0.4,
		genres:   []string{"r&b", "soul", "jazz", "pop", "indie"},
		terms:    []string{"romantic playlist", "love songs", "dinner music", "date night", "smooth r&b"},
	},
}

func isUnsafeRune(r rune) bool {
	if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
		return true
	}
	if !unicode.IsPrint(r) && r != ' ' {
		return true
	}
	switch {
	case r >= 0x202a && r <= 0x202e:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0x200e || r == 0x200f || r == 0x061c:
		return true
	}
	return false
}

// sanitizeVibeText strips ANSI escape sequences, invalid UTF-8 bytes, C0/C1/DEL
// control characters, and Unicode BiDi overrides, collapsing whitespace runs.
func sanitizeVibeText(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > maxVibeInputBytes {
		cut := maxVibeInputBytes
		for range utf8.UTFMax - 1 {
			if cut <= 0 || utf8.RuneStart(s[cut]) {
				break
			}
			cut--
		}
		s = s[:cut]
	}
	s = ansi.Strip(s)
	out := make([]rune, 0, min(len(s), maxVibeInputRunes))
	for i := 0; i < len(s) && len(out) < maxVibeInputRunes; {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r == utf8.RuneError && size == 1 {
			continue
		}
		if r == '\r' || r == '\n' || r == '\t' || r == '\v' || r == '\f' || r == '\u0085' || r == '\u2028' || r == '\u2029' {
			if len(out) > 0 && out[len(out)-1] != ' ' {
				out = append(out, ' ')
			}
			continue
		}
		if isUnsafeRune(r) {
			continue
		}
		if unicode.IsSpace(r) {
			if len(out) > 0 && out[len(out)-1] != ' ' {
				out = append(out, ' ')
			}
			continue
		}
		out = append(out, r)
	}
	return strings.TrimSpace(string(out))
}

func isNegationToken(tok string) bool {
	switch tok {
	case "not", "no", "never", "without", "dont", "don't", "aint", "ain't", "non":
		return true
	}
	return false
}

func isNegationBridgeToken(tok string) bool {
	switch tok {
	case "want", "wants", "wanted", "need", "like", "any", "some", "more", "too", "very", "so", "feel", "feeling":
		return true
	}
	return false
}

func isClauseBreakWord(tok string) bool {
	switch tok {
	case "but", "instead", "rather":
		return true
	}
	return false
}

// splitClauses splits lowercased text into clauses of word tokens so negations
// do not leak across punctuation (",", ";", ".", "!", "?") or contrast words ("but").
func splitClauses(lower string) [][]string {
	var clauses [][]string
	var curClause []string
	var curWord []rune

	flushWord := func() {
		if len(curWord) == 0 {
			return
		}
		w := strings.Trim(string(curWord), "'")
		curWord = curWord[:0]
		if w == "" {
			return
		}
		if isClauseBreakWord(w) {
			if len(curClause) > 0 {
				clauses = append(clauses, curClause)
				curClause = nil
			}
			return
		}
		curClause = append(curClause, w)
	}

	flushClause := func() {
		flushWord()
		if len(curClause) > 0 {
			clauses = append(clauses, curClause)
			curClause = nil
		}
	}

	for _, r := range lower {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'':
			curWord = append(curWord, r)
		case r == ',' || r == ';' || r == '.' || r == '!' || r == '?' || r == '/':
			flushClause()
		default:
			flushWord()
		}
	}
	flushClause()
	return clauses
}

func isTokenNegated(clause []string, idx int) bool {
	if idx > 0 && isNegationToken(clause[idx-1]) {
		return true
	}
	if idx > 1 && isNegationToken(clause[idx-2]) && isNegationBridgeToken(clause[idx-1]) {
		return true
	}
	return false
}

func scoreRule(r *rule, clauses [][]string) int {
	score := 0
	for _, clause := range clauses {
		if len(clause) == 0 {
			continue
		}
		used := make([]bool, len(clause))

		matchPhrase := func(phrase string) {
			parts := strings.Fields(phrase)
			if len(parts) < 2 || len(parts) > len(clause) {
				return
			}
			for start := 0; start <= len(clause)-len(parts); start++ {
				if isTokenNegated(clause, start) {
					continue
				}
				ok := true
				for j, p := range parts {
					if used[start+j] || clause[start+j] != p {
						ok = false
						break
					}
				}
				if ok {
					for j := range parts {
						used[start+j] = true
					}
					score += 2 * len(parts)
				}
			}
		}

		matchWord := func(word string) {
			if strings.ContainsRune(word, ' ') {
				return
			}
			for i, tok := range clause {
				if used[i] || tok != word || isTokenNegated(clause, i) {
					continue
				}
				used[i] = true
				score += 2
			}
		}

		for _, kw := range r.keywords {
			matchPhrase(kw)
		}
		for _, al := range r.aliases {
			matchPhrase(al)
		}
		for _, kw := range r.keywords {
			matchWord(kw)
		}
		for _, al := range r.aliases {
			matchWord(al)
		}
	}
	return score
}

// Parse turns a free-text input string into a Vibe using word-boundary and
// phrase-aware keyword matching. If no rule matches, it returns a neutral vibe
// using the sanitized input as a query hint.
func (a *KeywordAgent) Parse(input string) *Vibe {
	clean := sanitizeVibeText(input)
	lower := strings.ToLower(clean)
	clauses := splitClauses(lower)

	bestIdx := -1
	bestScore := 0
	for i := range rules {
		if s := scoreRule(&rules[i], clauses); s > bestScore {
			bestScore = s
			bestIdx = i
		}
	}

	if bestIdx >= 0 {
		r := &rules[bestIdx]
		v := &Vibe{
			Mood:     r.mood,
			Energy:   r.energy,
			Genres:   slices.Clone(r.genres),
			Keywords: slices.Clone(r.keywords),
		}
		v.Query = a.ToSearchQuery(v)
		return v
	}

	// No rule matched — pass sanitized input through as-is.
	return &Vibe{
		Mood:     "unknown",
		Energy:   0.5,
		Keywords: strings.Fields(lower),
		Query:    clean,
	}
}

// ToSearchQuery returns a single representative search query for the vibe.
// For a matched vibe it always returns "<genre> <mood>" so tests can assert
// on predictable content; callers that want variety should use ToSearchQueries.
func (a *KeywordAgent) ToSearchQuery(v *Vibe) string {
	if v == nil {
		return ""
	}
	if v.Mood == "unknown" {
		if q := sanitizeVibeText(v.Query); q != "" {
			return q
		}
		if kw := sanitizeVibeText(strings.Join(v.Keywords, " ")); kw != "" {
			return kw
		}
		return ""
	}
	mood := sanitizeVibeText(v.Mood)
	if len(v.Genres) == 0 {
		return mood
	}
	genre := sanitizeVibeText(v.Genres[0])
	if genre == "" {
		return mood
	}
	if mood == "" {
		return genre
	}
	return genre + " " + mood
}

// ToSearchQueries returns several diverse search terms for the vibe so callers
// can query multiple terms and combine results for variety.
func (a *KeywordAgent) ToSearchQueries(v *Vibe) []string {
	if v == nil {
		return []string{}
	}
	if v.Mood == "unknown" {
		// Raw pass-through: prefer explicit keywords, then Query.
		if kw := sanitizeVibeText(strings.Join(v.Keywords, " ")); kw != "" {
			return []string{kw}
		}
		if q := sanitizeVibeText(v.Query); q != "" {
			return []string{q}
		}
		return []string{}
	}
	if len(v.Genres) == 0 {
		if mood := sanitizeVibeText(v.Mood); mood != "" {
			return []string{mood}
		}
		return []string{}
	}

	// Find the matching rule to get its extra terms. Check full genre slice
	// equality first to disambiguate rules sharing mood + first genre (e.g.
	// "party" and "morning" both use mood="happy" and genres[0]="pop").
	var extraTerms []string
	matchedExact := false
	for _, r := range rules {
		if r.mood == v.Mood && slices.Equal(r.genres, v.Genres) {
			extraTerms = r.terms
			matchedExact = true
			break
		}
	}
	if !matchedExact {
		for _, r := range rules {
			if r.mood == v.Mood && len(r.genres) > 0 && r.genres[0] == v.Genres[0] {
				extraTerms = r.terms
				break
			}
		}
	}

	queries := make([]string, 0, len(v.Genres)+len(extraTerms))
	for _, g := range v.Genres {
		if cg := sanitizeVibeText(g); cg != "" {
			queries = append(queries, cg)
		}
	}
	queries = append(queries, extraTerms...)

	// Shuffle so different queries are tried each call. //nolint:gosec
	rand.Shuffle(len(queries), func(i, j int) { queries[i], queries[j] = queries[j], queries[i] })
	return queries
}
