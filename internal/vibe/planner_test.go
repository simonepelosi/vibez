package vibe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simone-vibes/vibez/internal/provider"
)

type stubSearchProvider struct {
	results map[string]*provider.SearchResult
	err     error
}

func (s *stubSearchProvider) Name() string { return "stub" }
func (s *stubSearchProvider) Search(_ context.Context, query string) (*provider.SearchResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	if r, ok := s.results[query]; ok {
		return r, nil
	}
	return &provider.SearchResult{}, nil
}
func (s *stubSearchProvider) GetLibraryTracks(_ context.Context) ([]provider.Track, error) {
	return nil, nil
}
func (s *stubSearchProvider) GetLibraryAlbums(_ context.Context) ([]provider.Album, error) {
	return nil, nil
}
func (s *stubSearchProvider) GetLibraryPlaylists(_ context.Context) ([]provider.Playlist, error) {
	return nil, nil
}
func (s *stubSearchProvider) GetPlaylistTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}
func (s *stubSearchProvider) GetCatalogPlaylistTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}
func (s *stubSearchProvider) GetAlbumTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}
func (s *stubSearchProvider) GetLibraryAlbumTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}
func (s *stubSearchProvider) GetArtistAlbums(_ context.Context, _ string) ([]provider.Album, error) {
	return nil, nil
}
func (s *stubSearchProvider) CreatePlaylist(_ context.Context, _ string, _ []string) (provider.Playlist, error) {
	return provider.Playlist{}, nil
}
func (s *stubSearchProvider) SetSongRating(_ context.Context, _ string, _ provider.Rating) error {
	return nil
}
func (s *stubSearchProvider) GetSongRating(_ context.Context, _ string) (provider.Rating, error) {
	return provider.RatingNone, nil
}
func (s *stubSearchProvider) IsAuthenticated() bool                              { return true }
func (s *stubSearchProvider) AddToPlaylist(_ context.Context, _, _ string) error { return nil }
func (s *stubSearchProvider) GetRecommendations(_ context.Context) ([]provider.RecommendationGroup, error) {
	return nil, nil
}
func (s *stubSearchProvider) GetStationTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}

func TestKeywordAgent_Plan_ActionFromIdleAndPrompt(t *testing.T) {
	agent := &KeywordAgent{}

	planIdle, err := agent.Plan(context.Background(), PlanRequest{
		Prompt:     "late night coding",
		PlayerIdle: true,
	})
	if err != nil {
		t.Fatalf("Plan(idle): %v", err)
	}
	if planIdle.Action != ActionPlayNow {
		t.Errorf("Plan(idle).Action = %q, want %q", planIdle.Action, ActionPlayNow)
	}
	if planIdle.Mood != "focused" && planIdle.Mood != "introspective" {
		t.Errorf("Plan(idle).Mood = %q, want focused or introspective", planIdle.Mood)
	}

	planPlaying, err := agent.Plan(context.Background(), PlanRequest{
		Prompt:     "chill vibes",
		PlayerIdle: false,
	})
	if err != nil {
		t.Fatalf("Plan(playing): %v", err)
	}
	if planPlaying.Action != ActionQueueAppend {
		t.Errorf("Plan(playing).Action = %q, want %q", planPlaying.Action, ActionQueueAppend)
	}

	planPlayNowOverride, err := agent.Plan(context.Background(), PlanRequest{
		Prompt:     "chill vibes play now",
		PlayerIdle: false,
	})
	if err != nil {
		t.Fatalf("Plan(play now): %v", err)
	}
	if planPlayNowOverride.Action != ActionPlayNow {
		t.Errorf("Plan(play now).Action = %q, want %q", planPlayNowOverride.Action, ActionPlayNow)
	}
}

func TestOllamaPlanner_ProbeAndPlan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"name": "nomic-embed-text:latest"},
					{"name": "qwen3:8b"},
				},
			})
		case "/api/chat":
			plan := VibePlan{
				Action:      ActionPlayNow,
				Explanation: "90s Bristol trip-hop classics",
				Mood:        "introspective",
				Energy:      0.35,
				Ordered:     true,
				SeedTracks: []SeedTrack{
					{Artist: "Massive Attack", Title: "Teardrop"},
					{Artist: "Portishead", Title: "Roads"},
				},
				SearchQueries:  []string{"90s trip hop", "bristol sound"},
				ExcludeArtists: []string{"Some Band"},
			}
			rawPlan, _ := json.Marshal(plan)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]any{
					"content": "```json\n" + string(rawPlan) + "\n```",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	model, err := ProbeOllama(context.Background(), srv.URL, "", srv.Client())
	if err != nil {
		t.Fatalf("ProbeOllama: %v", err)
	}
	if model != "qwen3:8b" {
		t.Errorf("ProbeOllama skipped embedding model: got %q, want qwen3:8b", model)
	}

	planner := DetectPlanner(context.Background(), DetectOptions{
		Backend:       "auto",
		OllamaBaseURL: srv.URL,
		Client:        srv.Client(),
	})
	if planner.Name() != "ollama:qwen3:8b" {
		t.Fatalf("DetectPlanner.Name() = %q, want ollama:qwen3:8b", planner.Name())
	}

	plan, err := planner.Plan(context.Background(), PlanRequest{
		Prompt:     "90s bristol trip-hop, no Some Band",
		PlayerIdle: true,
		NowPlaying: "Portishead — Sour Times",
	})
	if err != nil {
		t.Fatalf("OllamaPlanner.Plan: %v", err)
	}
	if plan.Action != ActionPlayNow || !plan.Ordered || len(plan.SeedTracks) != 2 {
		t.Errorf("unexpected plan: %+v", plan)
	}
	if plan.Backend != "ollama:qwen3:8b" {
		t.Errorf("plan.Backend = %q, want ollama:qwen3:8b", plan.Backend)
	}
}

func TestOpenAICompatPlanner_ProbeAndPlan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "llama-3.2-3b-instruct"},
				},
			})
		case "/v1/chat/completions":
			plan := VibePlan{
				Action:        ActionQueueAppend,
				Explanation:   "Tokyo city pop night drive",
				Mood:          "nostalgic",
				Energy:        0.65,
				Ordered:       false,
				SeedTracks:    []SeedTrack{{Artist: "Tatsuro Yamashita", Title: "Ride on Time"}},
				SearchQueries: []string{"japanese city pop 80s"},
			}
			rawPlan, _ := json.Marshal(plan)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"content": string(rawPlan)}},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	planner := DetectPlanner(context.Background(), DetectOptions{
		Backend:     "lmstudio",
		LMStudioURL: srv.URL,
		Client:      srv.Client(),
	})
	if planner.Name() != "lmstudio:llama-3.2-3b-instruct" {
		t.Fatalf("DetectPlanner.Name() = %q, want lmstudio:llama-3.2-3b-instruct", planner.Name())
	}

	plan, err := planner.Plan(context.Background(), PlanRequest{
		Prompt:     "1982 tokyo city pop",
		PlayerIdle: false,
	})
	if err != nil {
		t.Fatalf("OpenAICompatPlanner.Plan: %v", err)
	}
	if plan.Action != ActionQueueAppend || len(plan.SeedTracks) != 1 {
		t.Errorf("unexpected plan: %+v", plan)
	}
}

func TestLlamaCppPlanner_ExplicitBackendAlias(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "Meta-Llama-3.1-8B-Instruct.Q4_K_M.gguf"},
				},
			})
		case "/v1/chat/completions":
			plan := VibePlan{
				Action:        ActionPlayNow,
				Explanation:   "Acoustic folk evening",
				Mood:          "chill",
				Energy:        0.25,
				Ordered:       false,
				SeedTracks:    []SeedTrack{{Artist: "Nick Drake", Title: "Pink Moon"}},
				SearchQueries: []string{"70s acoustic folk"},
			}
			rawPlan, _ := json.Marshal(plan)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"content": string(rawPlan)}},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	for _, alias := range []string{"llamacpp", "llama.cpp", "llama-server"} {
		planner := DetectPlanner(context.Background(), DetectOptions{
			Backend:     alias,
			LlamaCppURL: srv.URL,
			Client:      srv.Client(),
		})
		wantName := "llamacpp:Meta-Llama-3.1-8B-Instruct.Q4_K_M.gguf"
		if planner.Name() != wantName {
			t.Fatalf("DetectPlanner(%q).Name() = %q, want %q", alias, planner.Name(), wantName)
		}
		plan, err := planner.Plan(context.Background(), PlanRequest{
			Prompt:     "nick drake style folk",
			PlayerIdle: true,
		})
		if err != nil {
			t.Fatalf("Plan(%q): %v", alias, err)
		}
		if plan.Backend != wantName {
			t.Errorf("plan.Backend = %q, want %q", plan.Backend, wantName)
		}
	}
}

func TestPlanWithFallback_FallsBackToKeywordOnError(t *testing.T) {
	badPlanner := &OllamaPlanner{BaseURL: "http://127.0.0.1:1"}
	plan, err := PlanWithFallback(context.Background(), badPlanner, PlanRequest{
		Prompt:     "rainy chill evening",
		PlayerIdle: true,
	})
	if err != nil {
		t.Fatalf("PlanWithFallback: %v", err)
	}
	if plan.Backend != "keyword" {
		t.Errorf("plan.Backend = %q, want keyword fallback", plan.Backend)
	}
	if plan.Action != ActionPlayNow {
		t.Errorf("plan.Action = %q, want %q", plan.Action, ActionPlayNow)
	}
}

func TestResolvePlan_SeedsAvoidKaraokeAndExcludedArtistsAndFillFromQueries(t *testing.T) {
	prov := &stubSearchProvider{
		results: map[string]*provider.SearchResult{
			"Massive Attack Teardrop": {
				Tracks: []provider.Track{
					{ID: "k1", Title: "Teardrop (Originally Performed by Massive Attack) [Karaoke Version]", Artist: "Karaoke All-Stars"},
					{ID: "m1", Title: "Teardrop (Remastered 2019)", Artist: "Massive Attack"},
				},
			},
			"Portishead Roads": {
				Tracks: []provider.Track{
					{ID: "p1", Title: "Roads", Artist: "Portishead"},
				},
			},
			"bristol trip hop": {
				Tracks: []provider.Track{
					{ID: "b1", Title: "Blocked Song", Artist: "Excluded Artist"},
					{ID: "t1", Title: "Hell Is Round the Corner", Artist: "Tricky"},
				},
			},
		},
	}

	plan := &VibePlan{
		Action:  ActionPlayNow,
		Ordered: true,
		SeedTracks: []SeedTrack{
			{Artist: "Massive Attack", Title: "Teardrop"},
			{Artist: "Portishead", Title: "Roads"},
		},
		SearchQueries:  []string{"bristol trip hop"},
		ExcludeArtists: []string{"Excluded Artist"},
	}

	out, err := ResolvePlan(context.Background(), prov, plan, "trip hop", 10)
	if err != nil {
		t.Fatalf("ResolvePlan: %v", err)
	}
	if len(out.Tracks) != 3 {
		t.Fatalf("len(out.Tracks) = %d, want 3: %+v", len(out.Tracks), out.Tracks)
	}
	if out.Tracks[0].ID != "m1" || out.Tracks[1].ID != "p1" || out.Tracks[2].ID != "t1" {
		t.Errorf("resolved track IDs = [%s, %s, %s], want [m1, p1, t1]",
			out.Tracks[0].ID, out.Tracks[1].ID, out.Tracks[2].ID)
	}
}

func TestBuildLocalLibraryIndex_SanitizesAndDeduplicates(t *testing.T) {
	idx := BuildLocalLibraryIndex([]provider.Track{
		{Artist: "Daft Punk", Title: "Something About Us", Album: "Discovery"},
		{Artist: "Daft Punk", Title: "Something About Us", Album: "Discovery"},
		{Artist: "Air\x1b[31m", Title: "La Femme d'Argent", Album: "Moon Safari"},
	})
	if strings.Count(idx, "Something About Us") != 1 {
		t.Errorf("expected deduplicated entry, got:\n%s", idx)
	}
	if strings.Contains(idx, "\x1b") {
		t.Errorf("ANSI escape not stripped from local index: %q", idx)
	}
}

func TestResolvePlan_ReturnsErrorWhenAllEmpty(t *testing.T) {
	prov := &stubSearchProvider{err: errors.New("offline")}
	plan := &VibePlan{SearchQueries: []string{"ambient"}}
	_, err := ResolvePlan(context.Background(), prov, plan, "ambient", 10)
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Errorf("ResolvePlan error = %v, want error containing 'offline'", err)
	}
}
