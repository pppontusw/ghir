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
)

var (
	claudeSessionLimitPattern = regexp.MustCompile(`(?is)(out of\s+(extra\s+)?usage|hit your\s+(usage\s+)?limit|exceeded.*(usage|limit)|usage\s+limit|rate\s+limit).*resets?`)
	claudeResetTimePattern    = regexp.MustCompile(`(?i)resets?\s+(?:at\s+)?[A-Za-z]*\s*(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*\(?(UTC)?\)?`)
	codexResetTsPattern       = regexp.MustCompile(`(?i)resets_at\\?"?[:\s]+(\d+)`)
	codexResetInSecPattern    = regexp.MustCompile(`(?i)resets_in_seconds\\?"?[:\s]+(\d+)`)
	geminiSessionLimitPattern = regexp.MustCompile(`(?is)(terminalquotaerror|quota\s+exceeded|rate\s+limit)`)
	geminiResetDurationRegex  = regexp.MustCompile(`(?i)resets?\s+(?:after\s+)?(\d+h)?(\d+m)?(\d+s)?`)
	geminiDurationPartRegex   = regexp.MustCompile(`(?i)(\d+)([hms])`)
	issuePattern              = regexp.MustCompile(`^\d+$`)
)

type options struct {
	DryRun         bool
	SingleIssue    string
	Force          bool
	Status         bool
	Reset          bool
	ResetIssue     string
	IssuesCSV      string
	IssuesFile     string
	LogDir         string
	DoneFile       string
	PromptTemplate string
	Agent          string
	Model          string
	ClaudeBin      string
	CodexBin       string
	GeminiBin      string
	CursorBin      string
	GHBin          string
	NoColor        bool
	Help           bool
	WaitBufferSec  int
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

	if opts.Reset {
		if err := r.handleReset(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	issues, err := r.loadIssues()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if opts.Status {
		r.printStatus(issues)
		return
	}

	r.printBanner(issues)

	if opts.SingleIssue != "" {
		r.opts.Force = true
		result := r.processIssue(1, len(issues), issues[0])
		if result != resultSuccess {
			os.Exit(1)
		}
		return
	}

	succeeded, failed := 0, 0
	for i, issue := range issues {
		idx := i + 1
		result := r.processIssue(idx, len(issues), issue)
		for result == resultRetry {
			r.printf(r.colors.Blue, "Retrying issue #%s after session limit reset...\n", issue)
			result = r.processIssue(idx, len(issues), issue)
		}
		if result == resultSuccess {
			succeeded++
			continue
		}
		failed++
		r.printf(r.colors.Red, "Stopping due to failure on issue #%s\n", issue)
		break
	}

	fmt.Println()
	r.printf(r.colors.Blue, "============================================================\n")
	r.printf(r.colors.Green, "Succeeded: %d\n", succeeded)
	r.printf(r.colors.Red, "Failed: %d\n", failed)
	r.printf(r.colors.Blue, "============================================================\n")

	if failed > 0 {
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
		case "--no-color":
			opts.NoColor = true
		case "-h", "--help":
			opts.Help = true
		default:
			return opts, fmt.Errorf("unknown option: %s", arg)
		}
	}

	if opts.SingleIssue != "" && !issuePattern.MatchString(opts.SingleIssue) {
		return opts, fmt.Errorf("--issue must be numeric: %q", opts.SingleIssue)
	}
	if opts.ResetIssue != "" && !issuePattern.MatchString(opts.ResetIssue) {
		return opts, fmt.Errorf("--reset issue must be numeric: %q", opts.ResetIssue)
	}
	if opts.Agent != "claude" && opts.Agent != "codex" && opts.Agent != "gemini" && opts.Agent != "cursor-agent" {
		return opts, fmt.Errorf("--agent must be one of: claude, codex, gemini, cursor-agent")
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
  --force                       Re-run even if issue is marked completed
  --status                      Show completion status for configured issues
  --reset [id]                  Reset all completions, or one issue if id is provided
  --issues <id1,id2,...>        Comma-separated issue list (overrides file)
  --issues-file <path>          Issue list file (default: .ticket-runner/issues.txt)
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
  --wait-buffer-sec <seconds>   Extra wait seconds after reset time (default: 120)
  --no-color                    Disable ANSI colors
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
		doneFile: opts.DoneFile,
		doneSet:  done,
		colors:   colors,
	}, nil
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
	if r.opts.SingleIssue != "" {
		return []string{r.opts.SingleIssue}, nil
	}
	if r.opts.IssuesCSV != "" {
		return parseCSVIssues(r.opts.IssuesCSV)
	}
	return readIssuesFile(r.opts.IssuesFile)
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
			return nil, fmt.Errorf("issue file not found: %s (or pass --issues)", path)
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
		return r.rewriteDoneFile(fmt.Sprintf("Reset completion for issue #%s\n", r.opts.ResetIssue))
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
			r.printf(r.colors.Green, "  #%s done\n", issue)
		} else {
			r.printf(r.colors.Yellow, "  #%s pending\n", issue)
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
	r.printf(r.colors.Blue, "Total: %d | Completed: %d | Remaining: %d\n", len(issues), completed, remaining)
	r.printf(r.colors.Blue, "============================================================\n")
	fmt.Println()
}

func (r *runner) processIssue(idx, total int, issue string) issueResult {
	details, err := r.fetchIssueDetails(issue)
	if err != nil {
		r.printf(r.colors.Red, "FAILED: unable to fetch issue #%s: %v\n", issue, err)
		return resultFailed
	}

	r.printf(r.colors.Blue, "------------------------------------------------------------\n")
	r.printf(r.colors.Blue, "[%d/%d] Issue #%s: %s\n", idx, total, issue, details.Title)
	r.printf(r.colors.Blue, "------------------------------------------------------------\n")

	if r.opts.DryRun {
		if r.isCompleted(issue) {
			r.printf(r.colors.Green, "[DRY RUN] Already completed #%s, would skip\n", issue)
		} else {
			r.printf(r.colors.Yellow, "[DRY RUN] Would process issue #%s\n", issue)
		}
		return resultSuccess
	}

	if r.isCompleted(issue) && !r.opts.Force {
		r.printf(r.colors.Green, "Already completed #%s, skipping (use --force to reprocess)\n", issue)
		return resultSuccess
	}

	dirty, err := r.workingTreeDirty()
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot determine git status: %v\n", err)
		return resultFailed
	}
	if dirty {
		r.printf(r.colors.Red, "ERROR: uncommitted changes detected. Commit or stash before running.\n")
		return resultFailed
	}

	startHead, err := r.gitOutput("rev-parse", "HEAD")
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot determine pre-run git HEAD: %v\n", err)
		return resultFailed
	}

	prompt, err := r.buildPrompt(issue, details)
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot build prompt for #%s: %v\n", issue, err)
		return resultFailed
	}

	logPath := filepath.Join(r.opts.LogDir, issue+".log")
	r.printf(r.colors.Yellow, "Starting %s for issue #%s...\n", agentDisplayName(r.opts.Agent), issue)
	fmt.Printf("Log: %s\n", logPath)

	exitCode, logOutput, err := r.runAgent(prompt, logPath)
	if err != nil {
		r.printf(r.colors.Red, "FAILED: %s invocation failed for #%s: %v\n", r.opts.Agent, issue, err)
		return resultFailed
	}

	if detectSessionLimit(logOutput, r.opts.Agent, exitCode) {
		if dirtyNow, dirtyErr := r.workingTreeDirty(); dirtyErr == nil && dirtyNow {
			r.printf(r.colors.Yellow, "Session limit hit mid-work. Committing partial progress...\n")
			message := fmt.Sprintf(
				"wip: partial work on #%s - %s (session limit hit)\n\nCo-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>",
				issue, details.Title,
			)
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
		r.printf(r.colors.Red, "FAILED: %s exited with code %d for issue #%s\n", r.opts.Agent, exitCode, issue)
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
		hasIssueRef := rangeErr == nil && issueMentionedInSubjects(rangeSubjects, issue)

		if err := r.markCompleted(issue); err != nil {
			r.printf(r.colors.Red, "FAILED: could not mark #%s completed: %v\n", issue, err)
			return resultFailed
		}
		r.printf(r.colors.Green, "SUCCESS: Issue #%s committed by %s\n", issue, agentDisplayName(r.opts.Agent))
		if strings.TrimSpace(headMsg) != "" {
			r.printf(r.colors.Green, "Commit: %s\n", headMsg)
		}
		if !hasIssueRef {
			r.printf(r.colors.Yellow, "WARNING: new commit(s) do not mention #%s in subject lines.\n", issue)
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
		message := fmt.Sprintf(
			"feat: implement #%s - %s\n\nCloses #%s\n\nCo-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>",
			issue, details.Title, issue,
		)
		if err := r.commitAll(message); err != nil {
			r.printf(r.colors.Red, "FAILED: fallback commit failed for #%s: %v\n", issue, err)
			return resultFailed
		}
		if err := r.markCompleted(issue); err != nil {
			r.printf(r.colors.Red, "FAILED: could not mark #%s completed: %v\n", issue, err)
			return resultFailed
		}
		r.printf(r.colors.Green, "SUCCESS: Issue #%s committed by runner\n", issue)
		fmt.Println()
		return resultSuccess
	}

	r.printf(r.colors.Red, "FAILED: no changes produced for issue #%s\n", issue)
	r.printf(r.colors.Red, "%s ran but made no modifications. Check log: %s\n", agentDisplayName(r.opts.Agent), logPath)
	return resultFailed
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

func (r *runner) buildPrompt(issue string, details issueDetails) (string, error) {
	templateBody := ""
	if r.opts.PromptTemplate != "" {
		data, err := os.ReadFile(r.opts.PromptTemplate)
		if err != nil {
			return "", fmt.Errorf("read prompt template: %w", err)
		}
		templateBody = string(data)
	} else {
		templateBody = defaultPromptBody
	}

	replacer := strings.NewReplacer(
		"{{ISSUE_NUMBER}}", issue,
		"{{ISSUE_TITLE}}", details.Title,
		"{{ISSUE_BODY}}", details.Body,
	)
	return replacer.Replace(templateBody), nil
}

func (r *runner) runAgent(prompt, logPath string) (int, string, error) {
	logFile, err := os.Create(logPath)
	if err != nil {
		return 0, "", err
	}

	defer func() {
		_ = logFile.Close()
	}()

	output := io.MultiWriter(os.Stdout, logFile)
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

	if syncErr := logFile.Sync(); syncErr != nil {
		return exitCode, "", fmt.Errorf("sync log file: %w", syncErr)
	}
	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		return exitCode, "", fmt.Errorf("read log file: %w", readErr)
	}

	return exitCode, string(data), nil
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
	return nil
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

	wait := defaultFallbackWaitSec
	return wait, now.Add(time.Duration(wait) * time.Second)
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
