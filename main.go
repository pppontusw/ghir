package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultIssueFilePath     = ".ticket-runner/issues.txt"
	defaultPromptTemplate    = ".ticket-runner/prompt.tmpl"
	defaultLogDirName        = ".ticket-runs"
	defaultDoneFileName      = ".completed"
	defaultFallbackWaitSec   = 1800
	defaultSessionBufferSec  = 120
	countdownIntervalSeconds = 300
	streamViewPretty         = "pretty"
	streamViewRaw            = "raw"
)

var (
	claudeSessionLimitPattern  = regexp.MustCompile(`(?is)(out of\s+(extra\s+)?usage|hit your\s+(usage\s+)?limit|exceeded.*(usage|limit)|usage\s+limit|rate\s+limit).*resets?`)
	claudeResetTimePattern     = regexp.MustCompile(`(?i)resets?\s+(?:at\s+)?[A-Za-z]*\s*(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*\(?(UTC)?\)?`)
	codexResetTsPattern        = regexp.MustCompile(`(?i)resets_at\\?"?[:\s]+(\d+)`)
	codexResetInSecPattern     = regexp.MustCompile(`(?i)resets_in_seconds\\?"?[:\s]+(\d+)`)
	geminiSessionLimitPattern  = regexp.MustCompile(`(?is)(terminalquotaerror|quota\s+exceeded|rate\s+limit|no\s+capacity\s+available|retryablequotaerror)`)
	geminiCapacity429WaitSec   = 900 // 15 minutes for "no capacity" / 429 from Gemini
	geminiResetDurationRegex   = regexp.MustCompile(`(?i)resets?\s+(?:after\s+)?(\d+h)?(\d+m)?(\d+s)?`)
	geminiDurationPartRegex    = regexp.MustCompile(`(?i)(\d+)([hms])`)
	internalServerErrorPattern = regexp.MustCompile(`(?i)(internal\s+server\s+error|500\s+internal|502\s+bad\s+gateway|503\s+service\s+unavailable|504\s+gateway\s+timeout|overloaded)`)
	issuePattern               = regexp.MustCompile(`^\d+$`)
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type options struct {
	DryRun          bool
	SingleIssue     string
	AllOpen         bool
	Label           string
	Force           bool
	Status          bool
	Reset           bool
	ResetIssue      string
	IssuesCSV       string
	IssuesFile      string
	Files           string
	AllFiles        string
	LogDir          string
	DoneFile        string
	PromptTemplate  string
	Agent           string
	Model           string
	ClaudeBin       string
	CodexBin        string
	GeminiBin       string
	CursorBin       string
	GHBin           string
	StreamView      string
	NoColor         bool
	Help            bool
	Version         bool
	WaitBufferSec   int
	ContinueOnError bool
	Loop            bool
}

type palette struct {
	Red    string
	Green  string
	Yellow string
	Blue   string
	Reset  string
}

type runner struct {
	opts     options
	repoRoot string
	fileMode bool
	doneFile string
	doneSet  map[string]struct{}
	colors   palette
}

type issueDetails struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type issueResult int

const (
	resultSuccess issueResult = iota
	resultFailed
	resultRetry
)

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
		printUsage()
		os.Exit(2)
	}
	if opts.Help {
		printUsage()
		return
	}
	if opts.Version {
		fmt.Printf("ticket-runner version %s (commit: %s, built at: %s)\n", version, commit, date)
		return
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	applyRepoDefaults(&opts, repoRoot)

	r, err := newRunner(opts, repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := r.preflightChecks(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if opts.Reset {
		if err := r.handleReset(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	loop := func() bool {
		failed, err := runOnce(r)
		if err != nil {
			if r.opts.Loop && (strings.Contains(err.Error(), "no open issues found") || strings.Contains(err.Error(), "no .md files found")) {
				r.printf(r.colors.Yellow, "No issues found.\n")
				return true
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return false
		}
		if !r.opts.Loop && failed > 0 {
			return false
		}
		return true
	}

	if opts.Loop {
		for {
			if !loop() {
				os.Exit(1)
			}
			r.printf(r.colors.Yellow, "Waiting 5 minutes before checking for new issues...\n")
			time.Sleep(5 * time.Minute)
		}
	}

	if !loop() {
		os.Exit(1)
	}
}

func parseArgs(args []string) (options, error) {
	opts := options{
		Agent:         "claude",
		ClaudeBin:     "claude",
		CodexBin:      "codex",
		GeminiBin:     "gemini",
		CursorBin:     "cursor-agent",
		GHBin:         "gh",
		StreamView:    streamViewPretty,
		WaitBufferSec: defaultSessionBufferSec,
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--dry-run":
			opts.DryRun = true
		case "--issue":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.SingleIssue = val
			i = next
		case "--all-open":
			opts.AllOpen = true
		case "--label":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.Label = val
			i = next
		case "--force":
			opts.Force = true
		case "--status":
			opts.Status = true
		case "--reset":
			opts.Reset = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				opts.ResetIssue = args[i+1]
				i++
			}
		case "--issues":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.IssuesCSV = val
			i = next
		case "--issues-file":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.IssuesFile = val
			i = next
		case "--files":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.Files = val
			i = next
		case "--all-files":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.AllFiles = val
			i = next
		case "--log-dir":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.LogDir = val
			i = next
		case "--done-file":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.DoneFile = val
			i = next
		case "--prompt-template":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.PromptTemplate = val
			i = next
		case "--agent":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.Agent = strings.ToLower(val)
			i = next
		case "--model":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.Model = val
			i = next
		case "--claude-bin":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.ClaudeBin = val
			i = next
		case "--codex-bin":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.CodexBin = val
			i = next
		case "--gemini-bin":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.GeminiBin = val
			i = next
		case "--cursor-bin":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.CursorBin = val
			i = next
		case "--gh-bin":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.GHBin = val
			i = next
		case "--wait-buffer-sec":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			waitSec, convErr := strconv.Atoi(val)
			if convErr != nil || waitSec < 0 {
				return opts, fmt.Errorf("--wait-buffer-sec must be a non-negative integer")
			}
			opts.WaitBufferSec = waitSec
			i = next
		case "--stream-view":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.StreamView = strings.ToLower(val)
			i = next
		case "--no-color":
			opts.NoColor = true
		case "--continue-on-error":
			opts.ContinueOnError = true
		case "--loop":
			opts.Loop = true
		case "--version":
			opts.Version = true
		case "-h", "--help":
			opts.Help = true
		default:
			return opts, fmt.Errorf("unknown option: %s", arg)
		}
	}

	hasFileFlags := opts.Files != "" || opts.AllFiles != ""
	hasIssueFlags := opts.SingleIssue != "" || opts.AllOpen || opts.IssuesCSV != "" || opts.IssuesFile != ""
	if hasFileFlags && hasIssueFlags {
		return opts, fmt.Errorf("--files/--all-files cannot be combined with --issue/--all-open/--issues/--issues-file")
	}
	if opts.Loop && !opts.AllOpen && opts.AllFiles == "" {
		return opts, fmt.Errorf("--loop requires either --all-open or --all-files")
	}
	if opts.SingleIssue != "" && !issuePattern.MatchString(opts.SingleIssue) {
		return opts, fmt.Errorf("--issue must be numeric: %q", opts.SingleIssue)
	}
	if opts.Agent != "claude" && opts.Agent != "codex" && opts.Agent != "gemini" && opts.Agent != "cursor-agent" {
		return opts, fmt.Errorf("--agent must be one of: claude, codex, gemini, cursor-agent")
	}
	if opts.StreamView != streamViewPretty && opts.StreamView != streamViewRaw {
		return opts, fmt.Errorf("--stream-view must be one of: %s, %s", streamViewPretty, streamViewRaw)
	}

	return opts, nil
}

func requireValue(flag string, args []string, idx int) (string, int, error) {
	if idx+1 >= len(args) {
		return "", idx, fmt.Errorf("%s requires a value", flag)
	}
	if strings.HasPrefix(args[idx+1], "--") {
		return "", idx, fmt.Errorf("%s requires a value", flag)
	}
	return args[idx+1], idx + 1, nil
}

func printUsage() {
	fmt.Print(`Ticket runner

Usage:
  ticket-runner [options]

Options:
  --dry-run                     Show what would run without invoking the agent CLI
  --issue <id>                  Process exactly one issue (forced re-run)
  --all-open                    Process all open issues in the repository
  --label <label>               Filter issues by label when using --all-open
  --force                       Re-run even if issue is marked completed
  --status                      Show completion status for configured issues
  --reset [id]                  Reset all completions, or one issue if id is provided
  --issues <id1,id2,...>        Comma-separated issue list (overrides file)
  --issues-file <path>          Issue list file (default: .ticket-runner/issues.txt)
  --files <path1,path2,...>      Comma-separated markdown file paths
  --all-files <dir>              Process all *.md files in directory
  --prompt-template <path>      Optional template with {{ISSUE_NUMBER}}, {{ISSUE_TITLE}}, {{ISSUE_BODY}}
  --agent <claude|codex|gemini|cursor-agent> Agent CLI to run (default: claude)
  --model <model-id>            Override model for selected agent
  --log-dir <path>              Log directory (default: .ticket-runs)
  --done-file <path>            Completion file (default: <log-dir>/.completed)
  --claude-bin <name/path>      Claude CLI command (default: claude)
  --codex-bin <name/path>       Codex CLI command (default: codex)
  --gemini-bin <name/path>      Gemini CLI command (default: gemini)
  --cursor-bin <name/path>      Cursor-agent CLI command (default: cursor-agent)
  --gh-bin <name/path>          GitHub CLI command (default: gh)
  --stream-view <pretty|raw>    Console streaming view (default: pretty)
  --wait-buffer-sec <seconds>   Extra wait seconds after reset time (default: 120)
  --no-color                    Disable ANSI colors
  --continue-on-error           Continue processing remaining issues after a failure
  --version                     Show version information
  -h, --help                    Show this help
`)
}

func findRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("must run inside a git repository")
	}
	return strings.TrimSpace(string(output)), nil
}

func applyRepoDefaults(opts *options, repoRoot string) {
	if opts.IssuesFile == "" {
		opts.IssuesFile = filepath.Join(repoRoot, defaultIssueFilePath)
	} else {
		opts.IssuesFile = resolvePath(repoRoot, opts.IssuesFile)
	}

	if opts.LogDir == "" {
		opts.LogDir = filepath.Join(repoRoot, defaultLogDirName)
	} else {
		opts.LogDir = resolvePath(repoRoot, opts.LogDir)
	}

	if opts.DoneFile == "" {
		opts.DoneFile = filepath.Join(opts.LogDir, defaultDoneFileName)
	} else {
		opts.DoneFile = resolvePath(repoRoot, opts.DoneFile)
	}

	if opts.PromptTemplate != "" {
		opts.PromptTemplate = resolvePath(repoRoot, opts.PromptTemplate)
		return
	}

	candidate := filepath.Join(repoRoot, defaultPromptTemplate)
	if _, err := os.Stat(candidate); err == nil {
		opts.PromptTemplate = candidate
	}
}

func resolvePath(repoRoot, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(repoRoot, value)
}

func newRunner(opts options, repoRoot string) (*runner, error) {
	if err := os.MkdirAll(opts.LogDir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	if err := ensureFile(opts.DoneFile); err != nil {
		return nil, fmt.Errorf("create done file: %w", err)
	}

	done, err := loadDoneSet(opts.DoneFile)
	if err != nil {
		return nil, err
	}

	colors := palette{
		Red:    "\033[0;31m",
		Green:  "\033[0;32m",
		Yellow: "\033[1;33m",
		Blue:   "\033[0;34m",
		Reset:  "\033[0m",
	}
	if opts.NoColor || os.Getenv("NO_COLOR") != "" {
		colors = palette{}
	}

	return &runner{
		opts:     opts,
		repoRoot: repoRoot,
		fileMode: opts.Files != "" || opts.AllFiles != "",
		doneFile: opts.DoneFile,
		doneSet:  done,
		colors:   colors,
	}, nil
}

func (r *runner) preflightChecks() error {
	if err := r.checkBinary("git", "git"); err != nil {
		return err
	}

	// In file mode, we don't strictly need gh for some operations, but it's safer to enforce consistency.
	// However, if we are in file mode, we might not use gh at all if we just read files.
	// But the issue requirement says "Verify git, gh, and selected agent binary are available in PATH".
	// Let's check gh if not in file mode OR if we want to be strict.
	// The current implementation uses gh to fetch issues if not in file mode.
	// Also fetchIssueDetails uses gh if not in file mode.
	if !r.fileMode {
		if err := r.checkBinary("gh", r.opts.GHBin); err != nil {
			return err
		}
	}

	var agentBin string
	switch r.opts.Agent {
	case "claude":
		agentBin = r.opts.ClaudeBin
	case "codex":
		agentBin = r.opts.CodexBin
	case "gemini":
		agentBin = r.opts.GeminiBin
	case "cursor-agent":
		agentBin = r.opts.CursorBin
	}

	if err := r.checkBinary(r.opts.Agent, agentBin); err != nil {
		return err
	}

	return nil
}

func (r *runner) checkBinary(name, binPath string) error {
	path, err := exec.LookPath(binPath)
	if err != nil {
		return fmt.Errorf("missing required binary '%s': %s not found in PATH", name, binPath)
	}
	if r.opts.DryRun {
		r.printf(r.colors.Green, "[DRY RUN] Found %s at %s\n", name, path)
	}
	return nil
}

func ensureFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

func loadDoneSet(path string) (map[string]struct{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read done file: %w", err)
	}
	done := make(map[string]struct{})
	for _, raw := range strings.Split(string(data), "\n") {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		done[id] = struct{}{}
	}
	return done, nil
}

func (r *runner) loadIssues() ([]string, error) {
	if r.opts.Files != "" {
		return r.loadFilePaths(r.opts.Files)
	}
	if r.opts.AllFiles != "" {
		return r.loadAllFiles(r.opts.AllFiles)
	}
	if r.opts.SingleIssue != "" {
		return []string{r.opts.SingleIssue}, nil
	}
	if r.opts.AllOpen {
		return r.fetchOpenIssues()
	}
	if r.opts.IssuesCSV != "" {
		return parseCSVIssues(r.opts.IssuesCSV)
	}
	return readIssuesFile(r.opts.IssuesFile)
}

func (r *runner) loadFilePaths(csv string) ([]string, error) {
	parts := strings.Split(csv, ",")
	var paths []string
	seen := make(map[string]struct{})
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		abs := resolvePath(r.repoRoot, p)
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("file not found: %s", p)
		}
		rel, relErr := filepath.Rel(r.repoRoot, abs)
		if relErr != nil {
			rel = p
		}
		if _, exists := seen[rel]; exists {
			continue
		}
		paths = append(paths, rel)
		seen[rel] = struct{}{}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no files found in --files")
	}
	return paths, nil
}

func (r *runner) loadAllFiles(dir string) ([]string, error) {
	abs := resolvePath(r.repoRoot, dir)
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		fullPath := filepath.Join(abs, entry.Name())
		rel, relErr := filepath.Rel(r.repoRoot, fullPath)
		if relErr != nil {
			rel = fullPath
		}
		paths = append(paths, rel)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no .md files found in %s", dir)
	}
	sortStringsNumeric(paths)
	return paths, nil
}

func (r *runner) fetchOpenIssues() ([]string, error) {
	// Fetch enough issues to cover most backlogs.
	// We only need the number.
	args := []string{"issue", "list", "--state", "open", "--limit", "4000", "--json", "number"}
	if r.opts.Label != "" {
		args = append(args, "--label", r.opts.Label)
	}
	out, err := r.commandOutput(r.opts.GHBin, args...)
	if err != nil {
		return nil, fmt.Errorf("fetch open issues: %w", err)
	}

	var items []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}

	var issues []string
	for _, item := range items {
		issues = append(issues, strconv.Itoa(item.Number))
	}

	if len(issues) == 0 {
		return nil, fmt.Errorf("no open issues found")
	}

	// Sort numerically ascending (Oldest First)
	sortStringsNumeric(issues)

	return issues, nil
}

func parseCSVIssues(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	var issues []string
	seen := make(map[string]struct{})
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		if !issuePattern.MatchString(id) {
			return nil, fmt.Errorf("invalid issue in --issues: %q", id)
		}
		if _, exists := seen[id]; exists {
			continue
		}
		issues = append(issues, id)
		seen[id] = struct{}{}
	}
	if len(issues) == 0 {
		return nil, fmt.Errorf("no issues found in --issues")
	}
	return issues, nil
}

func readIssuesFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("issue file not found: %s\nHint: create the file, pass --issues <list>, or use --all-open", path)
		}
		return nil, fmt.Errorf("read issues file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var issues []string
	seen := make(map[string]struct{})
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		id := fields[0]
		if !issuePattern.MatchString(id) {
			return nil, fmt.Errorf("invalid issue id at %s:%d: %q", path, i+1, id)
		}
		if _, exists := seen[id]; exists {
			continue
		}
		issues = append(issues, id)
		seen[id] = struct{}{}
	}

	if len(issues) == 0 {
		return nil, fmt.Errorf("no issue ids found in %s", path)
	}
	return issues, nil
}

func (r *runner) handleReset() error {
	if r.opts.ResetIssue != "" {
		delete(r.doneSet, r.opts.ResetIssue)
		return r.rewriteDoneFile(fmt.Sprintf("Reset completion for %s\n", r.issueLabel(r.opts.ResetIssue)))
	}
	r.doneSet = make(map[string]struct{})
	if err := os.WriteFile(r.doneFile, []byte{}, 0o644); err != nil {
		return fmt.Errorf("reset done file: %w", err)
	}
	r.printf(r.colors.Green, "Reset all completion tracking\n")
	return nil
}

func (r *runner) rewriteDoneFile(message string) error {
	var ids []string
	for id := range r.doneSet {
		ids = append(ids, id)
	}
	sortStringsNumeric(ids)
	content := strings.Join(ids, "\n")
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(r.doneFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("rewrite done file: %w", err)
	}
	r.printf(r.colors.Green, message)
	return nil
}

func sortStringsNumeric(values []string) {
	less := func(a, b string) bool {
		ai, aerr := strconv.Atoi(a)
		bi, berr := strconv.Atoi(b)
		if aerr == nil && berr == nil {
			return ai < bi
		}
		aBase := strings.TrimSuffix(filepath.Base(a), filepath.Ext(a))
		bBase := strings.TrimSuffix(filepath.Base(b), filepath.Ext(b))
		if an, err := strconv.Atoi(aBase); err == nil {
			if bn, err := strconv.Atoi(bBase); err == nil {
				return an < bn
			}
		}
		return a < b
	}
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if less(values[j], values[i]) {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}

func (r *runner) printStatus(issues []string) {
	r.printf(r.colors.Blue, "Completion status:\n")
	for _, issue := range issues {
		if r.isCompleted(issue) {
			r.printf(r.colors.Green, "  %s done\n", r.issueLabel(issue))
		} else {
			r.printf(r.colors.Yellow, "  %s pending\n", r.issueLabel(issue))
		}
	}
}

func (r *runner) printBanner(issues []string) {
	completed := 0
	for _, issue := range issues {
		if r.isCompleted(issue) {
			completed++
		}
	}
	remaining := len(issues) - completed
	r.printf(r.colors.Blue, "============================================================\n")
	r.printf(r.colors.Blue, "                     Ticket Runner\n")
	r.printf(r.colors.Blue, "============================================================\n")
	r.printf(r.colors.Blue, "Agent: %s\n", agentDisplayName(r.opts.Agent))
	if r.opts.Model != "" {
		r.printf(r.colors.Blue, "Model override: %s\n", r.opts.Model)
	}
	r.printf(r.colors.Blue, "Stream view: %s\n", r.opts.StreamView)
	r.printf(r.colors.Blue, "Total: %d | Completed: %d | Remaining: %d\n", len(issues), completed, remaining)
	r.printf(r.colors.Blue, "============================================================\n")
	fmt.Println()
}

func (r *runner) processIssue(idx, total int, issue string, isResume bool) issueResult {
	details, err := r.fetchIssueDetails(issue)
	if err != nil {
		r.printf(r.colors.Red, "FAILED: unable to fetch %s: %v\n", r.issueLabel(issue), err)
		return resultFailed
	}

	r.printf(r.colors.Blue, "------------------------------------------------------------\n")
	r.printf(r.colors.Blue, "[%d/%d] %s: %s\n", idx, total, r.issueLabel(issue), details.Title)
	r.printf(r.colors.Blue, "------------------------------------------------------------\n")

	if r.opts.DryRun {
		if r.isCompleted(issue) {
			r.printf(r.colors.Green, "[DRY RUN] Already completed %s, would skip\n", r.issueLabel(issue))
		} else {
			r.printf(r.colors.Yellow, "[DRY RUN] Would process %s\n", r.issueLabel(issue))
		}
		return resultSuccess
	}

	if r.isCompleted(issue) && !r.opts.Force {
		r.printf(r.colors.Green, "Already completed %s, skipping (use --force to reprocess)\n", r.issueLabel(issue))
		return resultSuccess
	}

	r.setIssueLabel(issue, "ghir:running")

	dirty, err := r.workingTreeDirty()
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot determine git status: %v\n", err)
		return resultFailed
	}
	if dirty {
		r.printf(r.colors.Red, "ERROR: uncommitted changes detected. Commit or stash before running.\n")
		r.printf(r.colors.Yellow, "Hint: review with `git status` and commit or stash changes.\n")
		return resultFailed
	}

	startHead, err := r.gitOutput("rev-parse", "HEAD")
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot determine pre-run git HEAD: %v\n", err)
		return resultFailed
	}

	prompt, err := r.buildPrompt(issue, details, isResume)
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot build prompt for %s: %v\n", r.issueLabel(issue), err)
		return resultFailed
	}

	logPath := filepath.Join(r.opts.LogDir, r.logFileName(issue))
	r.printf(r.colors.Yellow, "Starting %s for %s...\n", agentDisplayName(r.opts.Agent), r.issueLabel(issue))
	fmt.Printf("Log: %s\n", logPath)

	var exitCode int
	var logOutput string

	maxRetries := 3
	retryDelay := 5 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			r.printf(r.colors.Yellow, "Retrying due to internal server error (attempt %d/%d)...\n", attempt, maxRetries)
			time.Sleep(retryDelay)
		}

		exitCode, logOutput, err = r.runAgent(prompt, logPath)
		if err != nil {
			r.printf(r.colors.Red, "FAILED: %s invocation failed for %s: %v\n", r.opts.Agent, r.issueLabel(issue), err)
			return resultFailed
		}

		if exitCode != 0 && detectInternalServerError(logOutput) {
			if attempt < maxRetries {
				continue
			}
		}
		break
	}

	if detectSessionLimit(logOutput, r.opts.Agent, exitCode) {
		if dirtyNow, dirtyErr := r.workingTreeDirty(); dirtyErr == nil && dirtyNow {
			r.printf(r.colors.Yellow, "Session limit hit mid-work. Committing partial progress...\n")
			var message string
			if r.fileMode {
				message = fmt.Sprintf(
					"wip: partial work on %s - %s (session limit hit)\n\nCo-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>",
					issue, details.Title,
				)
			} else {
				message = fmt.Sprintf(
					"wip: partial work on #%s - %s (session limit hit)\n\nCo-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>",
					issue, details.Title,
				)
			}
			if commitErr := r.commitAll(message); commitErr != nil {
				r.printf(r.colors.Red, "FAILED: could not commit partial progress: %v\n", commitErr)
				return resultFailed
			}
		}
		waitSeconds, resetTime := waitDuration(logOutput, time.Now().UTC(), r.opts.WaitBufferSec, r.opts.Agent)
		r.waitForSessionReset(waitSeconds, resetTime)
		return resultRetry
	}

	if exitCode != 0 {
		r.printf(r.colors.Red, "FAILED: %s exited with code %d for %s\n", r.opts.Agent, exitCode, r.issueLabel(issue))
		r.printf(r.colors.Red, "Check log: %s\n", logPath)
		return resultFailed
	}

	endHead, err := r.gitOutput("rev-parse", "HEAD")
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot determine post-run git HEAD: %v\n", err)
		return resultFailed
	}

	if endHead != startHead {
		headMsg, _ := r.gitOutput("log", "-1", "--pretty=format:%s")
		rangeSubjects, rangeErr := r.gitOutput("log", "--pretty=format:%s", fmt.Sprintf("%s..%s", startHead, endHead))
		hasIssueRef := r.fileMode || (rangeErr == nil && issueMentionedInSubjects(rangeSubjects, issue))

		if err := r.markCompleted(issue); err != nil {
			r.printf(r.colors.Red, "FAILED: could not mark %s completed: %v\n", r.issueLabel(issue), err)
			return resultFailed
		}
		r.printf(r.colors.Green, "SUCCESS: %s committed by %s\n", r.issueLabel(issue), agentDisplayName(r.opts.Agent))
		if strings.TrimSpace(headMsg) != "" {
			r.printf(r.colors.Green, "Commit: %s\n", headMsg)
		}
		if !hasIssueRef {
			r.printf(r.colors.Yellow, "WARNING: new commit(s) do not mention %s in subject lines.\n", r.issueLabel(issue))
		}
		fmt.Println()
		return resultSuccess
	}

	dirty, err = r.workingTreeDirty()
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot determine post-run git status: %v\n", err)
		return resultFailed
	}
	if dirty {
		r.printf(r.colors.Yellow, "%s did not commit. Uncommitted changes found, committing now.\n", agentDisplayName(r.opts.Agent))
		var message string
		if r.fileMode {
			message = fmt.Sprintf(
				"feat: implement %s - %s\n\nCo-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>",
				issue, details.Title,
			)
		} else {
			message = fmt.Sprintf(
				"feat: implement #%s - %s\n\nCloses #%s\n\nCo-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>",
				issue, details.Title, issue,
			)
		}
		if err := r.commitAll(message); err != nil {
			r.printf(r.colors.Red, "FAILED: fallback commit failed for %s: %v\n", r.issueLabel(issue), err)
			return resultFailed
		}
		if err := r.markCompleted(issue); err != nil {
			r.printf(r.colors.Red, "FAILED: could not mark %s completed: %v\n", r.issueLabel(issue), err)
			return resultFailed
		}
		r.printf(r.colors.Green, "SUCCESS: %s committed by runner\n", r.issueLabel(issue))
		fmt.Println()
		return resultSuccess
	}

	r.printf(r.colors.Yellow, "No changes produced for %s (already done or no modifications needed)\n", r.issueLabel(issue))
	if err := r.markCompleted(issue); err != nil {
		r.printf(r.colors.Red, "FAILED: could not mark %s completed: %v\n", r.issueLabel(issue), err)
		return resultFailed
	}
	r.printf(r.colors.Green, "SUCCESS: %s completed (no changes needed)\n", r.issueLabel(issue))
	fmt.Println()
	return resultSuccess
}

func issueMentionedInSubjects(subjects, issue string) bool {
	if issue == "" {
		return false
	}

	needle := "#" + issue
	for _, subject := range strings.Split(subjects, "\n") {
		start := 0
		for {
			offset := strings.Index(subject[start:], needle)
			if offset == -1 {
				break
			}
			idx := start + offset
			after := idx + len(needle)
			if after >= len(subject) || subject[after] < '0' || subject[after] > '9' {
				return true
			}
			start = after
		}
	}

	return false
}

func (r *runner) fetchIssueDetails(issue string) (issueDetails, error) {
	if r.fileMode {
		return r.fetchFileDetails(issue)
	}
	out, err := r.commandOutput(r.opts.GHBin, "issue", "view", issue, "--json", "title,body")
	if err != nil {
		return issueDetails{}, err
	}
	var details issueDetails
	if unmarshalErr := json.Unmarshal([]byte(out), &details); unmarshalErr != nil {
		return issueDetails{}, fmt.Errorf("parse gh output: %w", unmarshalErr)
	}
	if details.Title == "" {
		return issueDetails{}, fmt.Errorf("empty issue title from gh")
	}
	return details, nil
}

func (r *runner) fetchFileDetails(filePath string) (issueDetails, error) {
	absPath := resolvePath(r.repoRoot, filePath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return issueDetails{}, fmt.Errorf("read file: %w", err)
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	title := ""
	bodyStart := 0
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			bodyStart = i + 1
			break
		}
	}

	if title == "" {
		base := filepath.Base(filePath)
		title = strings.TrimSuffix(base, filepath.Ext(base))
	}

	body := ""
	if bodyStart < len(lines) {
		body = strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))
	}

	return issueDetails{Title: title, Body: body}, nil
}

func (r *runner) buildPrompt(issue string, details issueDetails, isResume bool) (string, error) {
	templateBody := ""
	if r.opts.PromptTemplate != "" {
		data, err := os.ReadFile(r.opts.PromptTemplate)
		if err != nil {
			return "", fmt.Errorf("read prompt template: %w", err)
		}
		templateBody = string(data)
	} else if r.fileMode {
		templateBody = defaultFilePromptBody
	} else {
		templateBody = defaultPromptBody
	}

	replacer := strings.NewReplacer(
		"{{ISSUE_NUMBER}}", issue,
		"{{ISSUE_TITLE}}", details.Title,
		"{{ISSUE_BODY}}", details.Body,
		"{{FILE_PATH}}", issue,
	)
	prompt := replacer.Replace(templateBody)
	if isResume {
		prompt += "\n\nNote: This work is being resumed from a previous agent so you need to check the latest commit first to see what's already done."
	}
	return prompt, nil
}

func (r *runner) runAgent(prompt, logPath string) (int, string, error) {
	logFile, err := os.Create(logPath)
	if err != nil {
		return 0, "", err
	}

	defer func() {
		_ = logFile.Close()
	}()

	renderer, notice := r.newStreamRenderer()
	if notice != "" {
		r.printf(r.colors.Yellow, "%s\n", notice)
	}

	var output io.Writer
	var consoleWriter *consoleStreamWriter
	if r.opts.StreamView == streamViewPretty && (r.opts.Agent == "codex" || r.opts.Agent == "cursor-agent" || r.opts.Agent == "gemini") {
		consoleWriter = newConsoleStreamWriter(os.Stdout, renderer)
		output = io.MultiWriter(logFile, consoleWriter)
	} else {
		output = io.MultiWriter(logFile, os.Stdout)
	}
	cmd, err := r.buildAgentCommand(prompt)
	if err != nil {
		return 0, "", err
	}
	cmd.Dir = r.repoRoot
	cmd.Stdout = output
	cmd.Stderr = output

	err = cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return 0, "", fmt.Errorf("start %s: %w", r.opts.Agent, err)
		}
	}
	if consoleWriter != nil {
		if flushErr := consoleWriter.Flush(); flushErr != nil {
			return exitCode, "", fmt.Errorf("flush stream output: %w", flushErr)
		}
	}

	if syncErr := logFile.Sync(); syncErr != nil {
		return exitCode, "", fmt.Errorf("sync log file: %w", syncErr)
	}
	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		return exitCode, "", fmt.Errorf("read log file: %w", readErr)
	}

	return exitCode, string(data), nil
}

type streamRenderer interface {
	ConsumeLine(line string) []string
	FinalLines() []string
}

type rawStreamRenderer struct{}

func (r *rawStreamRenderer) ConsumeLine(line string) []string {
	return []string{line}
}

func (r *rawStreamRenderer) FinalLines() []string {
	return nil
}

type codexPrettyRenderer struct{}

func (r *codexPrettyRenderer) ConsumeLine(line string) []string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return []string{line}
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return []string{line}
	}

	eventType, _ := payload["type"].(string)
	switch eventType {
	case "item.started":
		item := asAnyMap(payload["item"])
		if item == nil || getStringField(item, "type") != "command_execution" {
			return nil
		}
		cmd := truncateForConsole(normalizeWhitespace(getStringField(item, "command")), 120)
		if cmd == "" {
			return []string{"[cmd] started"}
		}
		return []string{fmt.Sprintf("[cmd] %s", cmd)}
	case "item.completed":
		item := asAnyMap(payload["item"])
		if item == nil {
			return nil
		}

		switch getStringField(item, "type") {
		case "command_execution":
			exitCode, hasExitCode := getIntField(item, "exit_code")
			status := strings.ToLower(getStringField(item, "status"))
			if (hasExitCode && exitCode == 0 && (status == "" || status == "completed")) ||
				(!hasExitCode && status == "completed") {
				return nil
			}

			cmd := truncateForConsole(normalizeWhitespace(getStringField(item, "command")), 120)
			header := "[cmd failed]"
			if hasExitCode {
				header = fmt.Sprintf("[cmd failed exit=%d]", exitCode)
			}
			if status != "" {
				header += " status=" + status
			}

			var lines []string
			if cmd != "" {
				lines = append(lines, fmt.Sprintf("%s %s", header, cmd))
			} else {
				lines = append(lines, header)
			}

			aggregatedOutput := strings.TrimSpace(getStringField(item, "aggregated_output"))
			for _, outputLine := range compactMultiline(aggregatedOutput, 4, 360) {
				lines = append(lines, "  "+outputLine)
			}
			return lines
		case "agent_message":
			text := strings.TrimSpace(getStringField(item, "text"))
			if text == "" {
				return nil
			}
			return prefixMultiline("[assistant] ", "  ", text)
		default:
			return nil
		}
	case "error":
		code := getStringField(payload, "code")
		message := strings.TrimSpace(getStringField(payload, "message"))
		switch {
		case code != "" && message != "":
			return []string{fmt.Sprintf("[error] %s: %s", code, message)}
		case message != "":
			return []string{"[error] " + message}
		case code != "":
			return []string{"[error] " + code}
		default:
			return []string{"[error] received error event"}
		}
	case "turn.completed":
		return []string{"[done] turn completed"}
	default:
		return nil
	}
}

func (r *codexPrettyRenderer) FinalLines() []string {
	return nil
}

type cursorAgentPrettyRenderer struct{}

func (r *cursorAgentPrettyRenderer) ConsumeLine(line string) []string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return []string{line}
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return []string{line}
	}

	eventType, _ := payload["type"].(string)
	if eventType != "result" {
		return nil
	}

	subtype := getStringField(payload, "subtype")
	isError := payload["is_error"] == true
	durationMs, hasDuration := getIntField(payload, "duration_ms")
	result := strings.TrimSpace(getStringField(payload, "result"))

	var lines []string

	// Header: [done] 56.9s or [error] ...
	if isError {
		errMsg := subtype
		if errMsg == "" {
			errMsg = "failed"
		}
		lines = append(lines, "[error] "+errMsg)
	} else if hasDuration && durationMs > 0 {
		sec := float64(durationMs) / 1000
		lines = append(lines, fmt.Sprintf("[done] %.1fs", sec))
	} else {
		lines = append(lines, "[done]")
	}

	if result != "" {
		for _, l := range prefixMultiline("[assistant] ", "  ", result) {
			lines = append(lines, l)
		}
	}

	return lines
}

func (r *cursorAgentPrettyRenderer) FinalLines() []string {
	return nil
}

// geminiPrettyRenderer buffers Gemini's final JSON result and prints a short summary.
type geminiPrettyRenderer struct {
	jsonBuf    []string
	braceCount int
}

func (r *geminiPrettyRenderer) ConsumeLine(line string) []string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	// Suppress noisy boilerplate.
	if strings.HasPrefix(trimmed, "YOLO mode is enabled") || trimmed == "Loaded cached credentials." {
		return nil
	}
	// Show tool errors.
	if strings.HasPrefix(trimmed, "Error executing tool ") {
		return []string{line}
	}
	// Start or continue JSON buffer.
	if r.braceCount > 0 || strings.HasPrefix(trimmed, "{") {
		r.jsonBuf = append(r.jsonBuf, line)
		for _, c := range trimmed {
			if c == '{' {
				r.braceCount++
			} else if c == '}' {
				r.braceCount--
			}
		}
		if r.braceCount != 0 {
			return nil
		}
		// Complete JSON object: parse and format.
		block := strings.Join(r.jsonBuf, "\n")
		r.jsonBuf = nil
		r.braceCount = 0
		return r.formatGeminiResult(block)
	}
	return nil
}

func (r *geminiPrettyRenderer) formatGeminiResult(block string) []string {
	var payload struct {
		Response string `json:"response"`
		Stats    struct {
			Models map[string]struct {
				API struct {
					TotalRequests  int `json:"totalRequests"`
					TotalErrors    int `json:"totalErrors"`
					TotalLatencyMs int `json:"totalLatencyMs"`
				} `json:"api"`
				Tokens struct {
					Input      int `json:"input"`
					Prompt     int `json:"prompt"`
					Candidates int `json:"candidates"`
					Total      int `json:"total"`
					Cached     int `json:"cached"`
					Thoughts   int `json:"thoughts"`
					Tool       int `json:"tool"`
				} `json:"tokens"`
			} `json:"models"`
			Tools struct {
				TotalCalls      int `json:"totalCalls"`
				TotalSuccess    int `json:"totalSuccess"`
				TotalFail       int `json:"totalFail"`
				TotalDurationMs int `json:"totalDurationMs"`
			} `json:"tools"`
			Files struct {
				TotalLinesAdded   int `json:"totalLinesAdded"`
				TotalLinesRemoved int `json:"totalLinesRemoved"`
			} `json:"files"`
		} `json:"stats"`
	}
	if err := json.Unmarshal([]byte(block), &payload); err != nil {
		return []string{block}
	}
	var lines []string
	// Assistant response (first few lines).
	if payload.Response != "" {
		for i, l := range compactMultiline(payload.Response, 0, 0) {
			if i == 0 {
				lines = append(lines, "[assistant] "+l)
			} else {
				lines = append(lines, "  "+l)
			}
		}
	}
	// Stats: tokens and API.
	for _, m := range payload.Stats.Models {
		lines = append(lines, fmt.Sprintf("  tokens: %d (cached %d) · requests: %d · latency: %.1fs",
			m.Tokens.Total, m.Tokens.Cached, m.API.TotalRequests, float64(m.API.TotalLatencyMs)/1000))
		break
	}
	// Tools summary.
	t := payload.Stats.Tools
	lines = append(lines, fmt.Sprintf("  tools: %d calls, %d ok, %d fail · %.1fs",
		t.TotalCalls, t.TotalSuccess, t.TotalFail, float64(t.TotalDurationMs)/1000))
	// Files.
	f := payload.Stats.Files
	if f.TotalLinesAdded != 0 || f.TotalLinesRemoved != 0 {
		lines = append(lines, fmt.Sprintf("  files: +%d −%d", f.TotalLinesAdded, f.TotalLinesRemoved))
	}
	return lines
}

func (r *geminiPrettyRenderer) FinalLines() []string {
	if len(r.jsonBuf) == 0 {
		return nil
	}
	block := strings.Join(r.jsonBuf, "\n")
	r.jsonBuf = nil
	return r.formatGeminiResult(block)
}

type consoleStreamWriter struct {
	out      io.Writer
	renderer streamRenderer
	pending  []byte
	mu       sync.Mutex
}

func newConsoleStreamWriter(out io.Writer, renderer streamRenderer) *consoleStreamWriter {
	return &consoleStreamWriter{
		out:      out,
		renderer: renderer,
	}
}

func (w *consoleStreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pending = append(w.pending, p...)
	for {
		newlineIndex := bytes.IndexByte(w.pending, '\n')
		if newlineIndex < 0 {
			break
		}

		lineBytes := w.pending[:newlineIndex]
		if len(lineBytes) > 0 && lineBytes[len(lineBytes)-1] == '\r' {
			lineBytes = lineBytes[:len(lineBytes)-1]
		}
		if err := w.emitLineLocked(string(lineBytes)); err != nil {
			return 0, err
		}

		w.pending = w.pending[newlineIndex+1:]
	}

	return len(p), nil
}

func (w *consoleStreamWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.pending) > 0 {
		remaining := w.pending
		if len(remaining) > 0 && remaining[len(remaining)-1] == '\r' {
			remaining = remaining[:len(remaining)-1]
		}
		if err := w.emitLineLocked(string(remaining)); err != nil {
			return err
		}
		w.pending = nil
	}

	for _, line := range w.renderer.FinalLines() {
		if _, err := fmt.Fprintln(w.out, line); err != nil {
			return err
		}
	}
	return nil
}

func (w *consoleStreamWriter) emitLineLocked(line string) error {
	for _, formattedLine := range w.renderer.ConsumeLine(line) {
		if _, err := fmt.Fprintln(w.out, formattedLine); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) newStreamRenderer() (streamRenderer, string) {
	if r.opts.StreamView == streamViewRaw {
		return &rawStreamRenderer{}, ""
	}
	if r.opts.Agent == "codex" {
		return &codexPrettyRenderer{}, ""
	}
	if r.opts.Agent == "cursor-agent" {
		return &cursorAgentPrettyRenderer{}, ""
	}
	if r.opts.Agent == "gemini" {
		return &geminiPrettyRenderer{}, ""
	}
	return &rawStreamRenderer{}, fmt.Sprintf(
		"Stream view %q is not implemented for %s yet; showing raw output.",
		r.opts.StreamView,
		agentDisplayName(r.opts.Agent),
	)
}

func asAnyMap(value any) map[string]any {
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

func getStringField(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	value, ok := fields[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func getIntField(fields map[string]any, key string) (int, bool) {
	if fields == nil {
		return 0, false
	}

	value, ok := fields[key]
	if !ok || value == nil {
		return 0, false
	}

	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		n, err := strconv.Atoi(v.String())
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncateForConsole(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	if maxLen <= 3 {
		return value[:maxLen]
	}
	return value[:maxLen-3] + "..."
}

func compactMultiline(value string, maxLines int, maxChars int) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	if maxChars > 0 && len(trimmed) > maxChars {
		trimmed = truncateForConsole(trimmed, maxChars)
	}

	lines := strings.Split(trimmed, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = append(lines[:maxLines], "...")
	}

	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return lines
}

func prefixMultiline(firstPrefix, nextPrefix, value string) []string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	if len(lines) == 0 {
		return nil
	}
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}

	var formatted []string
	for idx, line := range lines {
		if idx == 0 {
			formatted = append(formatted, firstPrefix+line)
			continue
		}
		formatted = append(formatted, nextPrefix+line)
	}
	return formatted
}

func (r *runner) buildAgentCommand(prompt string) (*exec.Cmd, error) {
	switch r.opts.Agent {
	case "claude":
		args := []string{
			"--print",
			"--verbose",
			"--output-format", "text",
			"--dangerously-skip-permissions",
		}
		if r.opts.Model != "" {
			args = append(args, "--model", r.opts.Model)
		}
		cmd := exec.Command(r.opts.ClaudeBin, args...)
		cmd.Stdin = strings.NewReader(prompt)
		return cmd, nil
	case "codex":
		args := []string{
			"exec",
			"--json",
			"--dangerously-bypass-approvals-and-sandbox",
		}
		if r.opts.Model != "" {
			args = append(args, "--model", r.opts.Model)
		}
		args = append(args, prompt)
		cmd := exec.Command(r.opts.CodexBin, args...)
		return cmd, nil
	case "gemini":
		args := []string{
			"--output-format",
			"json",
			"--yolo",
		}
		if r.opts.Model != "" {
			args = append(args, "-m", r.opts.Model)
		}
		args = append(args, "-p", prompt)
		cmd := exec.Command(r.opts.GeminiBin, args...)
		return cmd, nil
	case "cursor-agent":
		args := []string{
			"--print",
			"--output-format",
			"json",
			"--force",
		}
		if r.opts.Model != "" {
			args = append(args, "--model", r.opts.Model)
		}
		args = append(args, prompt)
		cmd := exec.Command(r.opts.CursorBin, args...)
		return cmd, nil
	default:
		return nil, fmt.Errorf("unsupported agent: %s", r.opts.Agent)
	}
}

func (r *runner) workingTreeDirty() (bool, error) {
	out, err := r.gitOutput("status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (r *runner) commitAll(message string) error {
	if _, err := r.gitOutput("add", "-A"); err != nil {
		return err
	}
	if _, err := r.gitOutput("commit", "--no-verify", "-m", message); err != nil {
		return err
	}
	return nil
}

func (r *runner) markCompleted(issue string) error {
	if r.isCompleted(issue) {
		return nil
	}
	f, err := os.OpenFile(r.doneFile, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open done file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()
	if _, err := f.WriteString(issue + "\n"); err != nil {
		return fmt.Errorf("write done file: %w", err)
	}
	r.doneSet[issue] = struct{}{}
	r.setIssueLabel(issue, "ghir:done")
	return nil
}

func (r *runner) setIssueLabel(issue, newLabel string) {
	if r.fileMode || r.opts.DryRun {
		return
	}
	allLabels := []string{"ghir:queued", "ghir:running", "ghir:done"}
	var toRemove []string
	for _, l := range allLabels {
		if l != newLabel {
			toRemove = append(toRemove, l)
		}
	}
	_, _ = r.commandOutput(r.opts.GHBin, "issue", "edit", issue, "--add-label", newLabel, "--remove-label", strings.Join(toRemove, ","))
}

func (r *runner) isCompleted(issue string) bool {
	_, ok := r.doneSet[issue]
	return ok
}

func (r *runner) waitForSessionReset(waitSeconds int, resetTime time.Time) {
	r.printf(r.colors.Yellow, "============================================================\n")
	r.printf(r.colors.Yellow, "SESSION LIMIT HIT - waiting until %s (%ds)\n", resetTime.Format("2006-01-02 15:04 UTC"), waitSeconds)
	r.printf(r.colors.Yellow, "============================================================\n")

	remaining := waitSeconds
	for remaining > 0 {
		minutes := remaining / 60
		r.printf(r.colors.Yellow, "  waiting... %d minutes remaining\n", minutes)
		sleepFor := countdownIntervalSeconds
		if remaining < sleepFor {
			sleepFor = remaining
		}
		time.Sleep(time.Duration(sleepFor) * time.Second)
		remaining -= sleepFor
	}

	r.printf(r.colors.Green, "Session limit should be reset. Resuming...\n")
}

func waitDuration(logOutput string, now time.Time, bufferSec int, agent string) (int, time.Time) {
	if agent == "codex" {
		return waitDurationCodex(logOutput, now, bufferSec)
	}
	if agent == "gemini" {
		return waitDurationGemini(logOutput, now, bufferSec)
	}
	return waitDurationClaude(logOutput, now, bufferSec)
}

func waitDurationClaude(logOutput string, now time.Time, bufferSec int) (int, time.Time) {
	match := claudeResetTimePattern.FindStringSubmatch(logOutput)
	if len(match) == 0 {
		wait := defaultFallbackWaitSec
		return wait, now.Add(time.Duration(wait) * time.Second)
	}

	hour, err := strconv.Atoi(match[1])
	if err != nil {
		wait := defaultFallbackWaitSec
		return wait, now.Add(time.Duration(wait) * time.Second)
	}

	minute := 0
	if match[2] != "" {
		minute, err = strconv.Atoi(match[2])
		if err != nil || minute < 0 || minute > 59 {
			wait := defaultFallbackWaitSec
			return wait, now.Add(time.Duration(wait) * time.Second)
		}
	}

	ampm := strings.ToLower(strings.TrimSpace(match[3]))
	switch ampm {
	case "am":
		if hour == 12 {
			hour = 0
		}
	case "pm":
		if hour != 12 {
			hour += 12
		}
	case "":
		if hour < 0 || hour > 23 {
			wait := defaultFallbackWaitSec
			return wait, now.Add(time.Duration(wait) * time.Second)
		}
	default:
		wait := defaultFallbackWaitSec
		return wait, now.Add(time.Duration(wait) * time.Second)
	}

	if hour < 0 || hour > 23 {
		wait := defaultFallbackWaitSec
		return wait, now.Add(time.Duration(wait) * time.Second)
	}

	reset := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if !reset.After(now) {
		reset = reset.Add(24 * time.Hour)
	}

	withBuffer := reset.Add(time.Duration(bufferSec) * time.Second)
	wait := int(withBuffer.Sub(now).Seconds())
	if wait <= 0 {
		wait = defaultFallbackWaitSec
		withBuffer = now.Add(time.Duration(wait) * time.Second)
	}
	return wait, withBuffer
}

func waitDurationCodex(logOutput string, now time.Time, bufferSec int) (int, time.Time) {
	match := codexResetTsPattern.FindStringSubmatch(logOutput)
	if len(match) >= 2 {
		seconds, err := strconv.ParseInt(match[1], 10, 64)
		if err == nil && seconds > 0 {
			reset := time.Unix(seconds, 0).UTC()
			withBuffer := reset.Add(time.Duration(bufferSec) * time.Second)
			wait := int(withBuffer.Sub(now).Seconds())
			if wait > 0 {
				return wait, withBuffer
			}
		}
	}

	secondsMatch := codexResetInSecPattern.FindStringSubmatch(logOutput)
	if len(secondsMatch) >= 2 {
		waitSeconds, err := strconv.Atoi(secondsMatch[1])
		if err == nil && waitSeconds > 0 {
			wait := waitSeconds + bufferSec
			return wait, now.Add(time.Duration(wait) * time.Second)
		}
	}

	wait := defaultFallbackWaitSec
	return wait, now.Add(time.Duration(wait) * time.Second)
}

func waitDurationGemini(logOutput string, now time.Time, bufferSec int) (int, time.Time) {
	match := geminiResetDurationRegex.FindStringSubmatch(logOutput)
	if len(match) >= 4 {
		durationText := strings.Join([]string{match[1], match[2], match[3]}, "")
		if durationText != "" {
			durationSeconds := parseGeminiDurationSeconds(durationText)
			if durationSeconds > 0 {
				wait := durationSeconds + bufferSec
				return wait, now.Add(time.Duration(wait) * time.Second)
			}
		}
	}

	// "No capacity available" / 429 from Gemini: retry after 15 minutes
	lower := strings.ToLower(logOutput)
	if strings.Contains(lower, "no capacity") || strings.Contains(lower, "retryablequotaerror") || strings.Contains(lower, "code: 429") {
		wait := geminiCapacity429WaitSec + bufferSec
		return wait, now.Add(time.Duration(wait) * time.Second)
	}

	wait := defaultFallbackWaitSec
	return wait, now.Add(time.Duration(wait) * time.Second)
}

// isGemini429CapacityLog returns true if the log is from Gemini failing with 429 / no capacity.
// The CLI prints this in multiple forms (JSON error body, stack trace, RetryableQuotaError).
func isGemini429CapacityLog(logOutput string) bool {
	lower := strings.ToLower(logOutput)
	// Phrases that appear in the actual Gemini CLI output when capacity is exhausted
	return strings.Contains(lower, "no capacity available") ||
		strings.Contains(lower, "retryablequotaerror") ||
		strings.Contains(lower, "model_capacity_exhausted") ||
		(strings.Contains(lower, "resource_exhausted") && strings.Contains(lower, "429")) ||
		strings.Contains(lower, "ratelimitexceeded")
}

func detectSessionLimit(logOutput, agent string, exitCode int) bool {
	if agent == "codex" {
		if detectCodexErrorEventLimit(logOutput) {
			return true
		}
		if exitCode == 0 {
			return false
		}
		lower := strings.ToLower(logOutput)
		if strings.Contains(lower, "usage_limit_reached") {
			return true
		}
		if strings.Contains(lower, "usage limit") {
			return strings.Contains(lower, "resets_at") ||
				strings.Contains(lower, "resets_in_seconds") ||
				strings.Contains(lower, "http 429") ||
				strings.Contains(lower, "too many requests") ||
				strings.Contains(lower, "hit your usage limit")
		}
		return false
	}
	if agent == "gemini" {
		if exitCode != 0 && isGemini429CapacityLog(logOutput) {
			return true
		}
		if detectGeminiErrorPayloadLimit(logOutput) {
			return true
		}
		if exitCode == 0 {
			return false
		}
		return geminiSessionLimitPattern.MatchString(logOutput)
	}
	if agent == "cursor-agent" {
		return false
	}
	return claudeSessionLimitPattern.MatchString(logOutput)
}

func detectInternalServerError(logOutput string) bool {
	return internalServerErrorPattern.MatchString(logOutput)
}

func detectCodexErrorEventLimit(logOutput string) bool {
	for _, raw := range strings.Split(logOutput, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}

		eventType, _ := payload["type"].(string)
		if eventType != "error" {
			continue
		}

		if code, ok := payload["code"].(string); ok {
			lowerCode := strings.ToLower(code)
			if strings.Contains(lowerCode, "usage_limit_reached") || strings.Contains(lowerCode, "usage limit") {
				return true
			}
		}

		if message, ok := payload["message"].(string); ok {
			lowerMessage := strings.ToLower(message)
			if strings.Contains(lowerMessage, "usage_limit_reached") || strings.Contains(lowerMessage, "usage limit") {
				return true
			}
		}

		if _, hasReset := payload["resets_at"]; hasReset {
			return true
		}
	}
	return false
}

func detectGeminiErrorPayloadLimit(logOutput string) bool {
	for _, raw := range strings.Split(logOutput, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}

		isError, ok := payload["is_error"].(bool)
		if !ok || !isError {
			continue
		}

		var messageParts []string
		if result, ok := payload["result"].(string); ok {
			messageParts = append(messageParts, result)
		}
		if message, ok := payload["message"].(string); ok {
			messageParts = append(messageParts, message)
		}

		combined := strings.Join(messageParts, " ")
		if geminiSessionLimitPattern.MatchString(combined) {
			return true
		}
	}
	return false
}

func parseGeminiDurationSeconds(durationText string) int {
	matches := geminiDurationPartRegex.FindAllStringSubmatch(strings.ToLower(durationText), -1)
	if len(matches) == 0 {
		return 0
	}

	total := 0
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		value, err := strconv.Atoi(m[1])
		if err != nil || value < 0 {
			return 0
		}
		switch m[2] {
		case "h":
			total += value * 3600
		case "m":
			total += value * 60
		case "s":
			total += value
		}
	}

	return total
}

func (r *runner) commandOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = r.repoRoot

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(buf.String())

		if name == r.opts.GHBin {
			lowerOut := strings.ToLower(out)
			lowerErr := strings.ToLower(err.Error())
			if strings.Contains(lowerOut, "auth") || strings.Contains(lowerOut, "credentials") || strings.Contains(lowerErr, "auth") || strings.Contains(lowerErr, "credentials") {
				hint := "Hint: run `gh auth login` to authenticate or check repository permissions."
				if out == "" {
					out = hint
				} else {
					out = out + "\n" + hint
				}
			}
		}

		if out == "" {
			return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, out)
	}

	return strings.TrimSpace(buf.String()), nil
}

func (r *runner) gitOutput(args ...string) (string, error) {
	return r.commandOutput("git", args...)
}

func (r *runner) printf(color, format string, values ...any) {
	if color == "" {
		fmt.Printf(format, values...)
		return
	}
	fmt.Print(color)
	fmt.Printf(format, values...)
	fmt.Print(r.colors.Reset)
}

func agentDisplayName(agent string) string {
	switch agent {
	case "codex":
		return "Codex"
	case "gemini":
		return "Gemini"
	case "cursor-agent":
		return "Cursor Agent"
	default:
		return "Claude"
	}
}

func (r *runner) issueLabel(issue string) string {
	if r.fileMode {
		return issue
	}
	return "#" + issue
}

func (r *runner) logFileName(issue string) string {
	if r.fileMode {
		return strings.ReplaceAll(issue, "/", "__") + ".log"
	}
	return issue + ".log"
}

const defaultPromptBody = `You are implementing a fix or feature for GitHub issue #{{ISSUE_NUMBER}}.

## Issue: {{ISSUE_TITLE}}

{{ISSUE_BODY}}

## Instructions

1. Read and understand the issue above thoroughly.
2. Study existing code and related files before making changes.
3. Implement the fix or feature completely. No TODO placeholders.
4. Run the appropriate quality checks and tests for files you modified.
5. Fix any failing tests or lint issues.
6. Create a git commit with either:
   - "fix: <description> (closes #{{ISSUE_NUMBER}})" for bug fixes
   - "feat: <description> (closes #{{ISSUE_NUMBER}})" for features
7. Do not push to remote. Commit locally only.
`

const defaultFilePromptBody = `You are implementing a task described in {{FILE_PATH}}.

## Task: {{ISSUE_TITLE}}

{{ISSUE_BODY}}

## Instructions

1. Read and understand the task above thoroughly.
2. Study existing code and related files before making changes.
3. Implement the fix or feature completely. No TODO placeholders.
4. Run the appropriate quality checks and tests for files you modified.
5. Fix any failing tests or lint issues.
6. Create a git commit with:
   - "fix: <description>" for bug fixes
   - "feat: <description>" for features
7. Do not push to remote. Commit locally only.
`

func runOnce(r *runner) (int, error) {
	issues, err := r.loadIssues()
	if err != nil {
		return 0, err
	}

	if r.opts.Status {
		r.printStatus(issues)
		return 0, nil
	}

	r.printBanner(issues)

	var wg sync.WaitGroup
	for _, issue := range issues {
		if !r.isCompleted(issue) {
			wg.Add(1)
			go func(i string) {
				defer wg.Done()
				r.setIssueLabel(i, "ghir:queued")
			}(issue)
		}
	}
	wg.Wait()

	if r.opts.SingleIssue != "" {
		r.opts.Force = true
		result := r.processIssue(1, len(issues), issues[0], false)
		if result != resultSuccess {
			return 1, nil
		}
		return 0, nil
	}

	succeeded, failed := 0, 0
	var failedIssues []string
	for i, issue := range issues {
		idx := i + 1
		result := r.processIssue(idx, len(issues), issue, false)
		for result == resultRetry {
			r.printf(r.colors.Blue, "Retrying %s after session limit reset...\n", r.issueLabel(issue))
			result = r.processIssue(idx, len(issues), issue, true)
		}
		if result == resultSuccess {
			succeeded++
			continue
		}
		failed++
		failedIssues = append(failedIssues, r.issueLabel(issue))
		if r.opts.ContinueOnError {
			r.printf(r.colors.Red, "Failed on %s, continuing due to --continue-on-error\n", r.issueLabel(issue))
			continue
		}
		r.printf(r.colors.Red, "Stopping due to failure on %s\n", r.issueLabel(issue))
		break
	}

	fmt.Println()
	r.printf(r.colors.Blue, "============================================================\n")
	r.printf(r.colors.Green, "Succeeded: %d\n", succeeded)
	r.printf(r.colors.Red, "Failed: %d\n", failed)
	if len(failedIssues) > 0 {
		r.printf(r.colors.Red, "Failed issues: %s\n", strings.Join(failedIssues, ", "))
	}
	r.printf(r.colors.Blue, "============================================================\n")

	return failed, nil
}
