package vibe

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestNormalizeCLIAgent(t *testing.T) {
	cases := map[string]string{
		"omp":         "omp",
		"OMP":         "omp",
		"oh-my-pi":    "omp",
		"ohmypi":      "omp",
		"claude":      "claude",
		"claude-code": "claude",
		"gemini":      "gemini",
		"codex":       "codex",
		"copilot":     "copilot",
		"opencode":    "opencode",
		"llm":         "llm",
		"unknown":     "",
	}
	for in, want := range cases {
		if got := NormalizeCLIAgent(in); got != want {
			t.Errorf("NormalizeCLIAgent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildCLIInvocation_OMPUsesHermeticFlagsAndStdin(t *testing.T) {
	args, stdin := buildCLIInvocation("omp", "/tmp/hermetic", "gemini-3-flash-preview", "SYS", "USER_PROMPT")
	if stdin != "USER_PROMPT" {
		t.Fatalf("stdin = %q, want USER_PROMPT", stdin)
	}
	for _, wantFlag := range []string{
		"-p",
		"--cwd=/tmp/hermetic",
		"--no-tools",
		"--no-session",
		"--no-extensions",
		"--no-skills",
		"--no-rules",
		"--no-context-files",
		"--no-title",
		"--thinking=off",
		"--system-prompt=SYS",
		"--model=gemini-3-flash-preview",
	} {
		if !slices.Contains(args, wantFlag) {
			t.Errorf("omp args %v missing expected flag %q", args, wantFlag)
		}
	}

	defaultArgs, _ := buildCLIInvocation("omp", "/tmp/hermetic", "", "SYS", "USER_PROMPT")
	if !slices.Contains(defaultArgs, "--model=smol") {
		t.Errorf("omp default args %v should use --model=smol", defaultArgs)
	}
}

func TestDetectPlanner_SelectsOMPWhenLocalHTTPUnavailable(t *testing.T) {
	planner := DetectPlanner(context.Background(), DetectOptions{
		Backend:       "auto",
		Model:         "claude-sonnet-4.5",
		OllamaBaseURL: "http://127.0.0.1:1",
		LMStudioURL:   "http://127.0.0.1:1",
		CLILookup: func(agent string) (string, error) {
			if agent == "omp" {
				return "/usr/local/bin/omp", nil
			}
			return "", os.ErrNotExist
		},
	})
	if planner.Name() != "omp:claude-sonnet-4.5" {
		t.Fatalf("DetectPlanner.Name() = %q, want omp:claude-sonnet-4.5", planner.Name())
	}

	// With Privacy: "local-only", CLI agents are skipped and KeywordAgent is used.
	localOnly := DetectPlanner(context.Background(), DetectOptions{
		Backend:       "auto",
		Privacy:       "local-only",
		OllamaBaseURL: "http://127.0.0.1:1",
		LMStudioURL:   "http://127.0.0.1:1",
		CLILookup: func(agent string) (string, error) {
			return "/usr/local/bin/" + agent, nil
		},
	})
	if localOnly.Name() != "keyword" {
		t.Fatalf("DetectPlanner(local-only).Name() = %q, want keyword", localOnly.Name())
	}
}

func TestCLIPlanner_EndToEndOMPWrapper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell wrapper test runs on unix")
	}
	dir := t.TempDir()
	fakeOMP := filepath.Join(dir, "omp")
	script := `#!/bin/sh
echo "Working..." >&2
cat <<'EOF'
{
  "action": "play_now",
  "explanation": "OMP curated synthwave",
  "mood": "focus",
  "energy": 0.65,
  "ordered": true,
  "seed_tracks": [{"artist": "Kavinsky", "title": "Nightcall"}],
  "search_queries": ["retrowave instrumental"],
  "exclude_artists": []
}
EOF
`
	if err := os.WriteFile(fakeOMP, []byte(script), 0o755); err != nil { //nolint:gosec // test script executable
		t.Fatalf("WriteFile: %v", err)
	}

	planner := &CLIPlanner{
		Agent:      "omp",
		BinaryPath: fakeOMP,
		Model:      "fast",
	}
	plan, err := planner.Plan(context.Background(), PlanRequest{
		Prompt:     "midnight drive",
		PlayerIdle: true,
	})
	if err != nil {
		t.Fatalf("CLIPlanner.Plan: %v", err)
	}
	if plan.Backend != "omp:fast" {
		t.Errorf("plan.Backend = %q, want omp:fast", plan.Backend)
	}
	if len(plan.SeedTracks) != 1 || plan.SeedTracks[0].Artist != "Kavinsky" {
		t.Errorf("plan.SeedTracks = %+v, want Kavinsky - Nightcall", plan.SeedTracks)
	}
}

func TestScrubbedCLIEnv_StripsSecrets(t *testing.T) {
	t.Setenv("APPLE_DEVELOPER_TOKEN", "secret-dev-token")
	t.Setenv("APPLE_USER_TOKEN", "secret-user-token")
	t.Setenv("VIBEZ_CLIENT_KEY", "secret-client-key")
	t.Setenv("LASTFM_API_SECRET", "secret-lastfm")

	env := scrubbedCLIEnv()
	for _, kv := range env {
		upper := strings.ToUpper(kv)
		if strings.HasPrefix(upper, "APPLE_") ||
			strings.HasPrefix(upper, "VIBEZ_") ||
			strings.HasPrefix(upper, "LASTFM_") {
			t.Errorf("scrubbedCLIEnv leaked secret variable: %q", kv)
		}
	}
}

func TestCLIPlanner_LiveOMP(t *testing.T) {
	if os.Getenv("VIBEZ_LIVE_OMP_TEST") != "1" {
		t.Skip("set VIBEZ_LIVE_OMP_TEST=1 to run live omp smoke test")
	}
	planner := DetectPlanner(context.Background(), DetectOptions{Backend: "omp"})
	if !strings.HasPrefix(planner.Name(), "omp") {
		t.Fatalf("DetectPlanner(omp) = %q, want omp", planner.Name())
	}
	plan, err := planner.Plan(context.Background(), PlanRequest{
		Prompt:     "late night coding synthwave",
		PlayerIdle: true,
	})
	if err != nil {
		t.Fatalf("live omp Plan: %v", err)
	}
	t.Logf("Live omp plan: backend=%s mood=%s explanation=%q seeds=%d queries=%v",
		plan.Backend, plan.Mood, plan.Explanation, len(plan.SeedTracks), plan.SearchQueries)
}
