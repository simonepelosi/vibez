package vibe

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
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

func TestPrepareCLIInvocation_HermeticPerAgent(t *testing.T) {
	t.Run("omp defaults to smol and isolates workspace", func(t *testing.T) {
		tmp := t.TempDir()
		args, stdin, env, err := prepareCLIInvocation("omp", tmp, "gemini-3-flash-preview", "SYS", "USER_PROMPT")
		if err != nil {
			t.Fatalf("prepareCLIInvocation(omp): %v", err)
		}
		if stdin != "USER_PROMPT" || len(env) != 0 {
			t.Fatalf("unexpected stdin=%q env=%v", stdin, env)
		}
		for _, wantFlag := range []string{
			"-p", "--cwd=" + tmp, "--no-tools", "--no-session",
			"--no-extensions", "--no-skills", "--no-rules", "--no-context-files",
			"--no-title", "--thinking=off", "--system-prompt=SYS", "--model=gemini-3-flash-preview",
		} {
			if !slices.Contains(args, wantFlag) {
				t.Errorf("omp args %v missing expected flag %q", args, wantFlag)
			}
		}

		defaultArgs, _, _, err := prepareCLIInvocation("omp", tmp, "", "SYS", "USER_PROMPT")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(defaultArgs, "--model=smol") {
			t.Errorf("omp default args %v should use --model=smol", defaultArgs)
		}
	})

	t.Run("claude keeps OAuth without API key and uses --bare with API key", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("ANTHROPIC_API_KEY", "")
		args, stdin, env, err := prepareCLIInvocation("claude", tmp, "haiku", "SYS", "USER_PROMPT")
		if err != nil {
			t.Fatalf("prepareCLIInvocation(claude oauth): %v", err)
		}
		if stdin != "USER_PROMPT" {
			t.Errorf("claude stdin = %q, want USER_PROMPT", stdin)
		}
		if slices.Contains(args, "--bare") {
			t.Errorf("claude args %v must not pass --bare without ANTHROPIC_API_KEY (breaks OAuth)", args)
		}
		for _, want := range []string{"-p", "--strict-mcp-config", "--disable-slash-commands", "--setting-sources", `{"disableAllHooks":true}`, "haiku"} {
			if !slices.Contains(args, want) {
				t.Errorf("claude args %v missing %q", args, want)
			}
		}
		if !slices.Contains(env, "CLAUDE_CODE_DISABLE_CLAUDE_MDS=1") {
			t.Errorf("claude env = %v, want CLAUDE_CODE_DISABLE_CLAUDE_MDS=1", env)
		}

		t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
		apiArgs, _, _, err := prepareCLIInvocation("claude", tmp, "", "SYS", "USER_PROMPT")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(apiArgs, "--bare") {
			t.Errorf("claude args %v should pass --bare when ANTHROPIC_API_KEY is set", apiArgs)
		}
	})

	t.Run("gemini writes deny-all policy and uses plan mode", func(t *testing.T) {
		tmp := t.TempDir()
		args, stdin, env, err := prepareCLIInvocation("gemini", tmp, "gemini-2.5-flash", "SYS", "USER")
		if err != nil {
			t.Fatalf("prepareCLIInvocation(gemini): %v", err)
		}
		if stdin != "" || len(env) != 0 {
			t.Fatalf("gemini stdin=%q env=%v, want empty", stdin, env)
		}
		policyPath := filepath.Join(tmp, "gemini-policy.toml")
		for _, want := range []string{"plan", "none", policyPath, "gemini-2.5-flash"} {
			if !slices.Contains(args, want) {
				t.Errorf("gemini args %v missing %q", args, want)
			}
		}
		data, err := os.ReadFile(policyPath) //nolint:gosec // test temp policy file
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", policyPath, err)
		}
		text := string(data)
		if !strings.Contains(text, `toolName = "*"`) || !strings.Contains(text, `mcpName = "*"`) || !strings.Contains(text, `decision = "deny"`) {
			t.Errorf("unexpected gemini policy:\n%s", text)
		}
	})

	t.Run("codex passes skip-git-repo-check and read-only sandbox", func(t *testing.T) {
		tmp := t.TempDir()
		args, stdin, _, err := prepareCLIInvocation("codex", tmp, "o4-mini", "SYS", "USER")
		if err != nil {
			t.Fatalf("prepareCLIInvocation(codex): %v", err)
		}
		if !strings.Contains(stdin, "SYS") || !strings.Contains(stdin, "USER") {
			t.Errorf("codex stdin = %q, want combined prompt", stdin)
		}
		for _, want := range []string{"exec", "--ephemeral", "--skip-git-repo-check", "--sandbox", "read-only", "o4-mini", "-"} {
			if !slices.Contains(args, want) {
				t.Errorf("codex args %v missing %q", args, want)
			}
		}
	})

	t.Run("opencode sets pure dir and denies permissions", func(t *testing.T) {
		tmp := t.TempDir()
		args, _, env, err := prepareCLIInvocation("opencode", tmp, "local/model", "SYS", "USER")
		if err != nil {
			t.Fatalf("prepareCLIInvocation(opencode): %v", err)
		}
		for _, want := range []string{"run", "--format", "default", "--pure", "--dir", tmp, "--model", "local/model"} {
			if !slices.Contains(args, want) {
				t.Errorf("opencode args %v missing %q", args, want)
			}
		}
		if !slices.Contains(env, `OPENCODE_CONFIG_CONTENT={"permission":{"*":"deny"}}`) {
			t.Errorf("opencode env = %v, want OPENCODE_CONFIG_CONTENT deny-all", env)
		}
	})
}

func TestDetectPlanner_AutoSkipsCLIAndExplicitOptInSelectsCLI(t *testing.T) {
	// "auto" stays strictly on local HTTP runtimes and falls back to keyword
	// even when CLI binaries exist.
	autoPlanner := DetectPlanner(context.Background(), DetectOptions{
		Backend:       "auto",
		OllamaBaseURL: "http://127.0.0.1:1",
		LMStudioURL:   "http://127.0.0.1:1",
		CLILookup: func(agent string) (string, error) {
			return "/usr/local/bin/" + agent, nil
		},
	})
	if autoPlanner.Name() != "keyword" {
		t.Fatalf("DetectPlanner(auto).Name() = %q, want keyword when local HTTP unavailable", autoPlanner.Name())
	}

	// Explicit opt-in selects the requested CLI agent and propagates PlanTimeout.
	wantTimeout := 42 * time.Second
	planner := DetectPlanner(context.Background(), DetectOptions{
		Backend:     "oh-my-pi",
		Model:       "claude-sonnet-4.5",
		PlanTimeout: wantTimeout,
		CLILookup: func(agent string) (string, error) {
			if agent == "omp" {
				return "/usr/local/bin/omp", nil
			}
			return "", os.ErrNotExist
		},
	})
	cp, ok := planner.(*CLIPlanner)
	if !ok || cp.Name() != "omp:claude-sonnet-4.5" || cp.Timeout != wantTimeout {
		t.Fatalf("explicit DetectPlanner(oh-my-pi) = %#v, want CLIPlanner omp:claude-sonnet-4.5 with Timeout=%v", planner, wantTimeout)
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

func TestCLIPlanner_EndToEndEnvAndPolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell wrapper test runs on unix")
	}
	dir := t.TempDir()
	fakeOpenCode := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
if [ "$OPENCODE_CONFIG_CONTENT" != '{"permission":{"*":"deny"}}' ]; then
  echo "missing permission deny env: $OPENCODE_CONFIG_CONTENT" >&2
  exit 1
fi
cat <<'EOF'
{"action":"play_now","explanation":"Hermetic test","mood":"focus","energy":0.5,"ordered":false,"seed_tracks":[{"artist":"Tycho","title":"Awake"}],"search_queries":["ambient electronic"]}
EOF
`
	if err := os.WriteFile(fakeOpenCode, []byte(script), 0o700); err != nil { //nolint:gosec // test script executable
		t.Fatalf("WriteFile: %v", err)
	}
	plan, err := (&CLIPlanner{Agent: "opencode", BinaryPath: fakeOpenCode}).Plan(context.Background(), PlanRequest{
		Prompt:     "coding focus",
		PlayerIdle: true,
	})
	if err != nil {
		t.Fatalf("opencode wrapper failed: %v", err)
	}
	if plan.Backend != "opencode" || len(plan.SeedTracks) != 1 {
		t.Errorf("unexpected plan: %+v", plan)
	}
}

func TestScrubbedCLIEnv_StripsSecretsAndAppliesOverrides(t *testing.T) {
	t.Setenv("APPLE_DEVELOPER_TOKEN", "secret-dev-token")
	t.Setenv("APPLE_USER_TOKEN", "secret-user-token")
	t.Setenv("VIBEZ_CLIENT_KEY", "secret-client-key")
	t.Setenv("LASTFM_API_SECRET", "secret-lastfm")
	t.Setenv("CLAUDE_CODE_DISABLE_CLAUDE_MDS", "0")

	env := scrubbedCLIEnv("CLAUDE_CODE_DISABLE_CLAUDE_MDS=1")
	var claudeFlagCount int
	for _, kv := range env {
		upper := strings.ToUpper(kv)
		if strings.HasPrefix(upper, "APPLE_") ||
			strings.HasPrefix(upper, "VIBEZ_") ||
			strings.HasPrefix(upper, "LASTFM_") {
			t.Errorf("scrubbedCLIEnv leaked secret variable: %q", kv)
		}
		if strings.HasPrefix(upper, "CLAUDE_CODE_DISABLE_CLAUDE_MDS=") {
			claudeFlagCount++
			if kv != "CLAUDE_CODE_DISABLE_CLAUDE_MDS=1" {
				t.Errorf("override value = %q, want 1", kv)
			}
		}
	}
	if claudeFlagCount != 1 {
		t.Errorf("CLAUDE_CODE_DISABLE_CLAUDE_MDS count = %d, want 1 (no duplicate)", claudeFlagCount)
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
