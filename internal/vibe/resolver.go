package vibe

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"unicode"

	"github.com/simone-vibes/vibez/internal/provider"
)

const (
	maxResolveWorkers = 4
	defaultCapTracks  = 15
)

// ResolveOutput holds the grounded catalog tracks and any non-fatal backend warnings.
type ResolveOutput struct {
	Tracks   []provider.Track
	Warnings []string
}

// ResolvePlan grounds a VibePlan against a provider.Provider using bounded
// concurrency. Specific SeedTracks ("Artist Title") are resolved first with
// normalized matching; SearchQueries fill any remaining slots up to maxTracks.
func ResolvePlan(ctx context.Context, prov provider.Provider, plan *VibePlan, rawQuery string, maxTracks int) (ResolveOutput, error) {
	if prov == nil {
		return ResolveOutput{}, fmt.Errorf("no provider")
	}
	if maxTracks <= 0 {
		maxTracks = defaultCapTracks
	}
	if plan == nil {
		plan = &VibePlan{SearchQueries: []string{rawQuery}}
	}

	excluded := make([]string, 0, len(plan.ExcludeArtists))
	for _, ex := range plan.ExcludeArtists {
		if n := normalizeMatchToken(ex); n != "" {
			excluded = append(excluded, n)
		}
	}
	isExcluded := func(artist string) bool {
		if len(excluded) == 0 {
			return false
		}
		na := normalizeMatchToken(artist)
		for _, ex := range excluded {
			if na == ex || strings.Contains(na, ex) {
				return true
			}
		}
		return false
	}

	seen := make(map[string]bool)
	var tracks []provider.Track
	var reasons []string

	addTrack := func(t provider.Track) bool {
		if isExcluded(t.Artist) {
			return false
		}
		key := strings.ToLower(strings.TrimSpace(t.Artist) + "||" + strings.TrimSpace(t.Title))
		if seen[key] {
			return false
		}
		seen[key] = true
		tracks = append(tracks, t)
		return true
	}

	// 1. Resolve SeedTracks in parallel (bounded by maxResolveWorkers) while
	// preserving seed order in the output.
	if len(plan.SeedTracks) > 0 {
		type seedResult struct {
			track    *provider.Track
			warnings []string
			err      error
		}
		results := make([]seedResult, len(plan.SeedTracks))
		sem := make(chan struct{}, maxResolveWorkers)
		done := make(chan int, len(plan.SeedTracks))

		for i, seed := range plan.SeedTracks {
			go func(idx int, st SeedTrack) {
				sem <- struct{}{}
				defer func() {
					<-sem
					done <- idx
				}()
				q := strings.TrimSpace(st.Artist + " " + st.Title)
				res, err := prov.Search(ctx, q)
				if err != nil || res == nil {
					results[idx] = seedResult{err: err}
					return
				}
				best := bestSeedMatch(st, res.Tracks)
				results[idx] = seedResult{track: best, warnings: res.Warnings}
			}(i, seed)
		}
		for range len(plan.SeedTracks) {
			<-done
		}
		for _, r := range results {
			if r.err != nil {
				reasons = append(reasons, r.err.Error())
			}
			reasons = append(reasons, r.warnings...)
			if r.track != nil && len(tracks) < maxTracks {
				addTrack(*r.track)
			}
		}
	}

	seedCount := len(tracks)

	// 2. If we still have room, run SearchQueries in parallel to fill the queue.
	if len(tracks) < maxTracks && len(plan.SearchQueries) > 0 {
		type queryOut struct {
			tracks   []provider.Track
			warnings []string
			err      error
		}
		chs := make([]chan queryOut, len(plan.SearchQueries))
		for i, q := range plan.SearchQueries {
			ch := make(chan queryOut, 1)
			chs[i] = ch
			go func(term string, out chan queryOut) {
				res, err := prov.Search(ctx, term)
				if err != nil || res == nil {
					out <- queryOut{err: err}
					return
				}
				out <- queryOut{tracks: res.Tracks, warnings: res.Warnings}
			}(q, ch)
		}
		var fallbackPool []provider.Track
		for _, ch := range chs {
			r := <-ch
			if r.err != nil {
				reasons = append(reasons, r.err.Error())
			}
			reasons = append(reasons, r.warnings...)
			fallbackPool = append(fallbackPool, r.tracks...)
		}
		if !plan.Ordered {
			rand.Shuffle(len(fallbackPool), func(i, j int) { //nolint:gosec // music shuffle
				fallbackPool[i], fallbackPool[j] = fallbackPool[j], fallbackPool[i]
			})
		}
		for _, t := range fallbackPool {
			if len(tracks) >= maxTracks {
				break
			}
			addTrack(t)
		}
	}

	// 3. Final fallback to the raw query if both seeds and search queries yielded nothing.
	if len(tracks) == 0 && strings.TrimSpace(rawQuery) != "" {
		res, err := prov.Search(ctx, rawQuery)
		if err != nil {
			reasons = append(reasons, err.Error())
		}
		if res != nil {
			reasons = append(reasons, res.Warnings...)
			for _, t := range res.Tracks {
				if len(tracks) >= maxTracks {
					break
				}
				addTrack(t)
			}
		}
	}

	if !plan.Ordered && seedCount == 0 && len(tracks) > 1 {
		rand.Shuffle(len(tracks), func(i, j int) { //nolint:gosec // music shuffle
			tracks[i], tracks[j] = tracks[j], tracks[i]
		})
	}
	if len(tracks) > maxTracks {
		tracks = tracks[:maxTracks]
	}

	warnings := dedupeStrings(reasons)
	if len(tracks) == 0 {
		return ResolveOutput{Warnings: warnings}, noResultsError(rawQuery, warnings)
	}
	return ResolveOutput{Tracks: tracks, Warnings: warnings}, nil
}

// bestSeedMatch picks the candidate track from candidates that best matches
// the requested (Artist, Title) seed, filtering out karaoke/tribute covers when
// not requested.
func bestSeedMatch(seed SeedTrack, candidates []provider.Track) *provider.Track {
	if len(candidates) == 0 {
		return nil
	}
	wantArtist := normalizeMatchToken(seed.Artist)
	wantTitle := normalizeSongTitle(seed.Title)
	wantKaraoke := strings.Contains(wantArtist, "karaoke") || strings.Contains(wantTitle, "karaoke")

	bestIdx := -1
	bestScore := 0

	for i, c := range candidates {
		gotArtist := normalizeMatchToken(c.Artist)
		gotTitle := normalizeSongTitle(c.Title)
		if !wantKaraoke && isUnwantedTributeOrKaraoke(gotArtist, gotTitle) {
			continue
		}

		score := 0
		switch {
		case gotTitle == wantTitle:
			score += 60
		case wantTitle != "" && (strings.Contains(gotTitle, wantTitle) || strings.Contains(wantTitle, gotTitle)):
			score += 35
		}

		switch {
		case gotArtist == wantArtist:
			score += 40
		case wantArtist != "" && (strings.Contains(gotArtist, wantArtist) || strings.Contains(wantArtist, gotArtist)):
			score += 25
		}

		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	if bestIdx >= 0 && bestScore >= 50 {
		t := candidates[bestIdx]
		return &t
	}
	return nil
}

func isUnwantedTributeOrKaraoke(normArtist, normTitle string) bool {
	for _, bad := range []string{"karaoke", "tribute", "8 bit", "made famous by", "originally performed by"} {
		if strings.Contains(normArtist, bad) || strings.Contains(normTitle, bad) {
			return true
		}
	}
	return false
}

// normalizeSongTitle strips parenthetical/bracketed remaster/feat suffixes and
// normalizes alphanumeric tokens for fuzzy title comparison.
func normalizeSongTitle(s string) string {
	lower := strings.ToLower(s)
	// Strip parenthetical or bracketed suffixes like "(Remastered 2019)" or "[feat. X]".
	for _, pair := range [][2]byte{{'(', ')'}, {'[', ']'}} {
		for {
			open := strings.IndexByte(lower, pair[0])
			if open < 0 {
				break
			}
			closeIdx := strings.IndexByte(lower[open:], pair[1])
			if closeIdx < 0 {
				lower = lower[:open]
				break
			}
			lower = lower[:open] + " " + lower[open+closeIdx+1:]
		}
	}
	if idx := strings.Index(lower, " - "); idx > 0 {
		lower = lower[:idx]
	}
	if idx := strings.Index(lower, " feat."); idx > 0 {
		lower = lower[:idx]
	}
	if idx := strings.Index(lower, " ft."); idx > 0 {
		lower = lower[:idx]
	}
	return normalizeMatchToken(lower)
}

func normalizeMatchToken(s string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
		} else if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// BuildLocalLibraryIndex formats a compact "Artist — Album (Genre)" summary
// (capped at maxLocalSummaryBytes) from a list of local tracks so an on-device
// planner can curate tracks that actually exist in the user's --local folder.
func BuildLocalLibraryIndex(tracks []provider.Track) string {
	if len(tracks) == 0 {
		return ""
	}
	seen := make(map[string]bool)
	var b strings.Builder
	for _, t := range tracks {
		artist := sanitizeVibeText(t.Artist)
		title := sanitizeVibeText(t.Title)
		if artist == "" || title == "" {
			continue
		}
		entry := fmt.Sprintf("- %s — %s", artist, title)
		if album := sanitizeVibeText(t.Album); album != "" {
			entry += fmt.Sprintf(" [%s]", album)
		}
		key := strings.ToLower(entry)
		if seen[key] {
			continue
		}
		seen[key] = true
		if b.Len()+len(entry)+1 > maxLocalSummaryBytes {
			break
		}
		b.WriteString(entry)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func noResultsError(query string, reasons []string) error {
	reasons = dedupeStrings(reasons)
	if len(reasons) == 0 {
		return fmt.Errorf("no results for %q", query)
	}
	return fmt.Errorf("no results for %q (%s)", query, strings.Join(reasons, "; "))
}

func dedupeStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
