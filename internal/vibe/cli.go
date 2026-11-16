package vibe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxCLIOutputBytes = 64 << 10 // 64 KiB cap on CLI stdout/stderr

// limitedBuffer buffers up to max bytes and silently discards excess writes
// so concurrent child stdout/stderr draining cannot deadlock or exhaust memory.
type limitedBuffer struct {
	max int
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.max > 0 {
		if rem := b.max - b.buf.Len(); rem > 0 {
			if len(p) > rem {
				b.buf.Write(p[:rem])
			} else {
				b.buf.Write(p)
			}
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return b.buf.String()
}

// ValidateCLIModelName rejects leading '-' flags, control characters, and
// characters outside [a-zA-Z0-9._:/@-] so caller-supplied model names cannot
// inject CLI flags or shell metacharacters across vibe planners or TUI :vibe.
func ValidateCLIModelName(model string) (string, error) {
	for i := 0; i < len(model); i++ {
		if model[i] < 0x20 || model[i] == 0x7f {
			return "", fmt.Errorf("invalid model name: contains control character")
		}
	}
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > 128 {
		return "", fmt.Errorf("invalid model name: exceeds 128 characters")
	}
	if strings.HasPrefix(trimmed, "-") {
		return "", fmt.Errorf("invalid model name %q: must not start with '-'", trimmed)
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		isValid := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '.' || c == '_' || c == ':' || c == '/' || c == '@' || c == '-'
		if !isValid {
			return "", fmt.Errorf("invalid model name %q: unsupported character %q", trimmed, c)
		}
	}
	return trimmed, nil
}

func isWindowsBatchShim(bin string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(bin)))
	return ext == ".cmd" || ext == ".bat"
}

func validateBatchArgs(args []string) error {
	for _, a := range args {
		for i := 0; i < len(a); i++ {
			c := a[i]
			if c < 0x20 || c == 0x7f || strings.ContainsRune("&|<>^%!\"()", rune(c)) {
				return fmt.Errorf("unsafe character in Windows batch wrapper argument")
			}
		}
	}
	return nil
}

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
	if isWindowsBatchShim(bin) {
		if err := validateBatchArgs(args); err != nil {
			return nil, fmt.Errorf("%s: %w", agent, err)
		}
	}

	cmd := exec.CommandContext(runCtx, bin, args...) //nolint:gosec // G204: bin is resolved via LookPathCLI from an allowlisted agent name
	cmd.Dir = tmpDir
	cmd.Env = scrubbedCLIEnv(extraEnv...)
	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData)
	}
	var stdoutBuf, stderrBuf limitedBuffer
	stdoutBuf.max = maxCLIOutputBytes
	stderrBuf.max = maxCLIOutputBytes
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	cmd.WaitDelay = 500 * time.Millisecond

	waitErr := cmd.Run()

	errBytes := stderrBuf.buf.Bytes()
	rawOut := strings.TrimSpace(stdoutBuf.String())
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
	if usage, ok := parseCLITokenUsage(rawOut, string(errBytes)); ok {
		plan.Usage = usage
	} else {
		plan.Usage = EstimateTokenUsage(systemPrompt+"\n\n"+userMsg, extracted)
	}
	sanitized := SanitizePlan(&plan, req.PlayerIdle)
	if len(sanitized.SeedTracks) == 0 && len(sanitized.SearchQueries) == 0 {
		return nil, fmt.Errorf("%s returned empty plan", agent)
	}
	return sanitized, nil
}

// parseCLITokenUsage scans CLI stdout/stderr for a JSON object carrying a
// "usage" block (e.g. input_tokens/output_tokens, prompt_tokens/completion_tokens,
// or input/output/totalTokens).
func parseCLITokenUsage(stdout, stderr string) (TokenUsage, bool) {
	type usageBlock struct {
		InputTokens      int `json:"input_tokens"`
		OutputTokens     int `json:"output_tokens"`
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		Input            int `json:"input"`
		Output           int `json:"output"`
		TotalTokens      int `json:"total_tokens"`
		TotalTokensCamel int `json:"totalTokens"`
	}
	type envelope struct {
		Usage usageBlock `json:"usage"`
	}
	for _, stream := range []string{stdout, stderr} {
		for line := range strings.SplitSeq(stream, "\n") {
			line = strings.TrimSpace(line)
			if !strings.Contains(line, `"usage"`) {
				continue
			}
			var env envelope
			if err := json.Unmarshal([]byte(extractJSONObject(line)), &env); err != nil {
				continue
			}
			u := env.Usage
			in := max(0, max(u.InputTokens, max(u.PromptTokens, u.Input)))
			out := max(0, max(u.OutputTokens, max(u.CompletionTokens, u.Output)))
			tot := max(0, max(u.TotalTokens, u.TotalTokensCamel))
			if tot <= 0 {
				tot = in + out
			}
			if tot > 0 {
				return TokenUsage{
					InputTokens:  in,
					OutputTokens: out,
					TotalTokens:  tot,
				}, true
			}
		}
	}
	return TokenUsage{}, false
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
	model, err = ValidateCLIModelName(model)
	if err != nil {
		return nil, "", nil, err
	}

	switch agent {
	case "omp":
		args = []string{
			"-p",
			"--cwd=" + tmpDir,
			"--no-tools",
			"--no-lsp",
			"--no-pty",
			"--offline",
			"--no-session",
			"--no-extensions",
			"--no-skills",
			"--no-rules",
			"--no-context-files",
			"--no-title",
			"--thinking=off",
			"--system-prompt=" + sysPrompt,
		}
		// Omit --model when unset so omp uses its configured default; --model is
		// a fuzzy model-name match rather than the "smol" role.
		if model != "" {
			args = append(args, "--model="+model)
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
			"-p", "Follow the instructions provided via stdin.",
			"--output-format", "text",
			"--approval-mode", "plan",
			"--extensions", "none",
			"--admin-policy", policyPath,
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		return args, combined, nil, nil

	case "codex":
		codexHome, herr := prepareIsolatedCodexHome(tmpDir)
		if herr != nil {
			return nil, "", nil, herr
		}
		args = []string{
			"exec",
			"--ephemeral",
			"--skip-git-repo-check",
			"--sandbox", "read-only",
			"-c", "mcp_servers={}",
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, "-")
		return args, combined, []string{"CODEX_HOME=" + codexHome}, nil

	case "copilot":
		args = []string{
			"-s",
			"--no-custom-instructions",
			"--disable-builtin-mcps",
			"--disallow-temp-dir",
			"--no-remote",
			"--no-remote-export",
			"--no-ask-user",
			"--no-auto-update",
			"--available-tools=__vibez_none__",
			"--deny-tool=shell",
			"--deny-tool=write",
			"--deny-tool=edit",
			"--deny-tool=web_fetch",
			"--deny-tool=url",
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		return args, combined, []string{
			"COPILOT_ALLOW_ALL=false",
			"COPILOT_ASSISTED_APPROVAL=false",
		}, nil

	case "opencode":
		// cmd.Dir is already set to tmpDir; avoid --pure/--dir which opencode 2.2+ rejects on `run`.
		promptPath := filepath.Join(tmpDir, "prompt.txt")
		if err := os.WriteFile(promptPath, []byte(combined), 0o600); err != nil {
			return nil, "", nil, fmt.Errorf("write opencode prompt: %w", err)
		}
		args = []string{
			"run",
			"--format", "default",
			"-f", promptPath,
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, "--", "Follow the instructions in the attached file.")
		return args, "", []string{
			`OPENCODE_CONFIG_CONTENT={"plugin":[],"mcp":{},"tools":{"*":false},"permission":{"*":"deny"}}`,
			`OPENCODE_PERMISSION={"*":"deny"}`,
		}, nil

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
		if isScrubbedCLIEnvVar(upper) {
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

func isScrubbedCLIEnvVar(upper string) bool {
	if strings.HasPrefix(upper, "APPLE_") ||
		strings.HasPrefix(upper, "VIBEZ_") ||
		strings.HasPrefix(upper, "LASTFM_") ||
		strings.HasPrefix(upper, "DISCORD_") {
		return true
	}
	switch upper {
	case "COPILOT_ALLOW_ALL",
		"COPILOT_ASSISTED_APPROVAL",
		"COPILOT_CUSTOM_INSTRUCTIONS_DIRS",
		"OPENCODE_CONFIG",
		"OPENCODE_CONFIG_DIR",
		"OPENCODE_PERMISSION",
		"OMP_PROFILE",
		"PI_CODING_AGENT_DIR",
		"CODEX_HOME":
		return true
	}
	return false
}

// prepareIsolatedCodexHome creates an isolated CODEX_HOME inside tmpDir with
// an empty config.toml so user MCP servers and plugins are never loaded,
// copying only auth.json (if a non-symlink regular file <= 1 MiB exists) so
// OAuth credentials keep working.
func prepareIsolatedCodexHome(tmpDir string) (string, error) {
	isolated := filepath.Join(tmpDir, "codex-home")
	if err := os.MkdirAll(isolated, 0o700); err != nil {
		return "", fmt.Errorf("create isolated codex home: %w", err)
	}
	if err := os.WriteFile(filepath.Join(isolated, "config.toml"), nil, 0o600); err != nil {
		return "", fmt.Errorf("write empty codex config: %w", err)
	}

	var candidates []string
	if envHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); envHome != "" {
		candidates = append(candidates, filepath.Join(envHome, "auth.json"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".codex", "auth.json"))
	}
	const maxCodexAuthBytes = 1 << 20
	for _, src := range candidates {
		fi, err := os.Lstat(src) //nolint:gosec // G703: candidate comes from user CODEX_HOME or ~/.codex
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() || fi.Size() > maxCodexAuthBytes {
			continue
		}
		raw, err := os.ReadFile(src) //nolint:gosec // G304,G703: validated regular file from user codex home
		if err != nil {
			continue
		}
		if werr := os.WriteFile(filepath.Join(isolated, "auth.json"), raw, 0o600); werr != nil { //nolint:gosec // G703: isolated is inside Go-created tmpDir
			return "", fmt.Errorf("copy codex auth.json: %w", werr)
		}
		break
	}
	return isolated, nil
}
