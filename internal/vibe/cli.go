package vibe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxCLIOutputBytes = 64 << 10 // 64 KiB cap on CLI stdout/stderr

// SupportedCLIAgents lists the supported CLI agent names that can be selected
// explicitly via :vibe <agent> or "vibe_backend" in config.json.
var SupportedCLIAgents = []string{
	"omp",
	"claude",
	"gemini",
	"codex",
	"copilot",
	"opencode",
	"llm",
}

// NormalizeCLIAgent maps aliases (such as "oh-my-pi") to the canonical CLI
// agent identifier, or returns "" if name is not a supported CLI agent.
func NormalizeCLIAgent(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "omp", "oh-my-pi", "ohmypi":
		return "omp"
	case "claude", "claude-code":
		return "claude"
	case "gemini", "gemini-cli":
		return "gemini"
	case "codex", "codex-cli":
		return "codex"
	case "copilot", "gh-copilot":
		return "copilot"
	case "opencode":
		return "opencode"
	case "llm":
		return "llm"
	default:
		return ""
	}
}

// LookPathCLI locates a CLI agent binary on PATH or in standard user-local
// bin directories (~/.bun/bin, ~/.local/bin, ~/.npm-global/bin, ~/go/bin,
// ~/.cargo/bin) so agents installed via bun/npm/pipx are found even when
// vibez is launched from a desktop shortcut with a minimal PATH.
func LookPathCLI(agent string) (string, error) {
	canon := NormalizeCLIAgent(agent)
	if canon == "" {
		return "", fmt.Errorf("unsupported CLI agent %q", agent)
	}
	if p, err := exec.LookPath(canon); err == nil && p != "" {
		return p, nil
	}
	for _, dir := range extraCLIBinDirs() {
		cand := filepath.Join(dir, canon)
		if runtime.GOOS == "windows" {
			for _, ext := range []string{".exe", ".cmd", ".bat"} {
				if isExecutableFile(cand + ext) {
					return cand + ext, nil
				}
			}
		} else if isExecutableFile(cand) {
			return cand, nil
		}
	}
	return "", fmt.Errorf("%s binary not found in PATH or user bin dirs", canon)
}

func extraCLIBinDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".cargo", "bin"),
	}
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

// CLIPlanner executes a local CLI agent subprocess (such as omp, claude,
// gemini, codex, copilot, opencode, or llm) in a hermetic temporary directory
// with tools and session persistence disabled.
type CLIPlanner struct {
	Agent      string        // canonical agent name ("omp", "claude", "gemini", ...)
	BinaryPath string        // resolved executable path (optional; resolved lazily if empty)
	Model      string        // optional model override
	Timeout    time.Duration // per-call timeout (defaults to DefaultPlanTimeout)
}

func (p *CLIPlanner) agentName() string {
	if c := NormalizeCLIAgent(p.Agent); c != "" {
		return c
	}
	return strings.ToLower(strings.TrimSpace(p.Agent))
}

// Name returns the planner identifier shown in the TUI Vibe badge (e.g.
// "omp" or "omp:gemini-3-flash-preview").
func (p *CLIPlanner) Name() string {
	name := p.agentName()
	if name == "" {
		name = "omp"
	}
	if m := sanitizeVibeText(p.Model); m != "" {
		return name + ":" + m
	}
	return name
}

// Plan runs the CLI agent in a fresh 0700 temporary directory with tools and
// workspace context discovery disabled, parses the JSON VibePlan from stdout,
// and sanitizes all fields.
func (p *CLIPlanner) Plan(ctx context.Context, req PlanRequest) (*VibePlan, error) {
	agent := p.agentName()
	if agent == "" {
		return nil, errors.New("empty CLI agent name")
	}
	bin := p.BinaryPath
	if bin == "" {
		resolved, err := LookPathCLI(agent)
		if err != nil {
			return nil, err
		}
		bin = resolved
	}

	tmpDir, err := os.MkdirTemp("", "vibez-vibe-*")
	if err != nil {
		return nil, fmt.Errorf("create hermetic temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	_ = os.Chmod(tmpDir, 0o700) //nolint:gosec // G302: 0700 is owner-only directory permission

	timeout := resolvePlanTimeout(p.Timeout)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	userMsg := buildUserMessage(req)
	args, stdinData, extraEnv, err := prepareCLIInvocation(agent, tmpDir, p.Model, systemPrompt, userMsg)
	if err != nil {
		return nil, fmt.Errorf("%s prepare invocation: %w", agent, err)
	}

	cmd := exec.CommandContext(runCtx, bin, args...) //nolint:gosec // G204: bin is resolved via LookPathCLI from an allowlisted agent name
	cmd.Dir = tmpDir
	cmd.Env = scrubbedCLIEnv(extraEnv...)
	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s start: %w", agent, err)
	}

	outBytes, _ := io.ReadAll(io.LimitReader(stdoutPipe, maxCLIOutputBytes))
	errBytes, _ := io.ReadAll(io.LimitReader(stderrPipe, maxCLIOutputBytes))
	waitErr := cmd.Wait()

	rawOut := strings.TrimSpace(string(outBytes))
	if rawOut == "" && waitErr != nil {
		return nil, fmt.Errorf("%s failed: %w (%s)", agent, waitErr, sanitizeVibeText(string(errBytes)))
	}

	extracted := extractJSONObject(rawOut)
	if extracted == "" {
		// Some CLIs emit output on stderr in non-TTY mode; check errBytes only if stdout had no JSON.
		extracted = extractJSONObject(string(errBytes))
	}

	var plan VibePlan
	if err := json.Unmarshal([]byte(extracted), &plan); err != nil {
		if waitErr != nil {
			return nil, fmt.Errorf("%s error: %w", agent, waitErr)
		}
		return nil, fmt.Errorf("%s decode plan: %w", agent, err)
	}
	plan.Backend = p.Name()
	sanitized := SanitizePlan(&plan, req.PlayerIdle)
	if len(sanitized.SeedTracks) == 0 && len(sanitized.SearchQueries) == 0 {
		return nil, fmt.Errorf("%s returned empty plan", agent)
	}
	return sanitized, nil
}

const geminiDenyPolicyTOML = `[[rule]]
toolName = "*"
decision = "deny"
priority = 999

[[rule]]
mcpName = "*"
toolName = "*"
decision = "deny"
priority = 999
`

// prepareCLIInvocation constructs the hermetic command-line arguments, stdin
// payload, and extra environment variables for the given CLI agent inside
// tmpDir, disabling tools, MCP servers, hooks, user rules, and session history.
func prepareCLIInvocation(agent, tmpDir, model, sysPrompt, userMsg string) (args []string, stdin string, extraEnv []string, err error) {
	combined := sysPrompt + "\n\n" + userMsg
	model = sanitizeVibeText(model)

	switch agent {
	case "omp":
		ompModel := model
		if ompModel == "" {
			ompModel = "smol"
		}
		args = []string{
			"-p",
			"--cwd=" + tmpDir,
			"--no-tools",
			"--no-session",
			"--no-extensions",
			"--no-skills",
			"--no-rules",
			"--no-context-files",
			"--no-title",
			"--thinking=off",
			"--model=" + ompModel,
			"--system-prompt=" + sysPrompt,
		}
		return args, userMsg, nil, nil

	case "claude":
		// Keep OAuth/keychain auth intact when ANTHROPIC_API_KEY is unset while
		// isolating hooks, plugins, MCP, slash commands, and CLAUDE.md files;
		// add --bare when an explicit API key is present.
		args = []string{
			"-p",
			"--output-format", "text",
			"--tools", "",
			"--no-session-persistence",
			"--strict-mcp-config",
			"--disable-slash-commands",
			"--setting-sources", "",
			"--settings", `{"disableAllHooks":true}`,
			"--system-prompt", sysPrompt,
		}
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			args = append(args, "--bare")
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		return args, userMsg, []string{"CLAUDE_CODE_DISABLE_CLAUDE_MDS=1"}, nil

	case "gemini":
		policyPath := filepath.Join(tmpDir, "gemini-policy.toml")
		if err := os.WriteFile(policyPath, []byte(geminiDenyPolicyTOML), 0o600); err != nil {
			return nil, "", nil, fmt.Errorf("write gemini policy: %w", err)
		}
		args = []string{
			"-p", combined,
			"--output-format", "text",
			"--approval-mode", "plan",
			"--extensions", "none",
			"--policy", policyPath,
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		return args, "", nil, nil

	case "codex":
		args = []string{
			"exec",
			"--ephemeral",
			"--skip-git-repo-check",
			"--sandbox", "read-only",
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, "-")
		return args, combined, nil, nil

	case "copilot":
		args = []string{
			"-p", combined,
			"-s",
			"--no-custom-instructions",
			"--available-tools=",
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		return args, "", nil, nil

	case "opencode":
		args = []string{
			"run",
			"--format", "default",
			"--pure",
			"--dir", tmpDir,
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, combined)
		return args, "", []string{`OPENCODE_CONFIG_CONTENT={"permission":{"*":"deny"}}`}, nil

	case "llm":
		args = []string{
			"--no-log",
			"-s", sysPrompt,
		}
		if model != "" {
			args = append(args, "-m", model)
		}
		return args, userMsg, nil, nil

	default:
		return []string{"-p"}, combined, nil, nil
	}
}

// scrubbedCLIEnv returns a copy of os.Environ() with vibez/Apple Music/Last.fm
// secrets removed, user-local bin directories appended to PATH so bun/node
// wrappers locate their runtime, and any explicit overrides applied cleanly.
func scrubbedCLIEnv(overrides ...string) []string {
	overrideKeys := make(map[string]struct{}, len(overrides))
	for _, kv := range overrides {
		if k, _, ok := strings.Cut(kv, "="); ok {
			overrideKeys[strings.ToUpper(k)] = struct{}{}
		}
	}

	raw := os.Environ()
	out := make([]string, 0, len(raw)+len(overrides)+2)
	var pathVal string
	for _, kv := range raw {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(k)
		if strings.HasPrefix(upper, "APPLE_") ||
			strings.HasPrefix(upper, "VIBEZ_") ||
			strings.HasPrefix(upper, "LASTFM_") ||
			strings.HasPrefix(upper, "DISCORD_") {
			continue
		}
		if _, overridden := overrideKeys[upper]; overridden {
			continue
		}
		if upper == "PATH" {
			pathVal = v
			continue
		}
		out = append(out, kv)
	}

	extra := extraCLIBinDirs()
	if len(extra) > 0 {
		var buf bytes.Buffer
		buf.WriteString(pathVal)
		for _, d := range extra {
			if !strings.Contains(pathVal, d) {
				if buf.Len() > 0 {
					buf.WriteByte(os.PathListSeparator)
				}
				buf.WriteString(d)
			}
		}
		pathVal = buf.String()
	}
	if pathVal != "" {
		out = append(out, "PATH="+pathVal)
	}
	out = append(out, "NO_COLOR=1")
	out = append(out, overrides...)
	return out
}
