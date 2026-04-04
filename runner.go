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
	"strconv"
	"strings"
	"time"

	"ghir/tui"
)

// separatorLine is the repeated banner line used in status and session-wait output.
const separatorLine = "============================================================\n"

// palette holds ANSI color codes for runner output; empty strings when NoColor is set.
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

type issueExecution struct {
	result  issueResult
	changed bool
	details issueDetails
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

	if r.requiresGH() {
		if err := r.checkBinary("gh", r.opts.GHBin); err != nil {
			return err
		}
	}

	if err := r.checkBinary(r.opts.Agent, getAgentBin(r.opts.agentConfig)); err != nil {
		return err
	}

	return nil
}

func (r *runner) requiresGH() bool {
	strategy := strings.TrimSpace(r.opts.Strategy)
	if strategy == "" {
		strategy = tui.DefaultStrategy
	}
	return !r.fileMode || strategy != tui.DefaultStrategy
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
	var paths []string
	seen := make(map[string]struct{})
	for _, path := range splitCSVTrimmed(csv) {
		abs := resolvePath(r.repoRoot, path)
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		rel := repoRelativePath(r.repoRoot, abs)
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
		paths = append(paths, repoRelativePath(r.repoRoot, fullPath))
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no .md files found in %s", dir)
	}
	sortStringsNumeric(paths)
	return paths, nil
}

func (r *runner) fetchOpenIssues() ([]string, error) {
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

	sortStringsNumeric(issues)

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
	r.printf(r.colors.Green, "%s", message)
	return nil
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
	r.printf(r.colors.Blue, separatorLine)
	r.printf(r.colors.Blue, "                     Ticket Runner\n")
	r.printf(r.colors.Blue, separatorLine)
	r.printf(r.colors.Blue, "Agent: %s\n", agentDisplayName(r.opts.Agent))
	if r.opts.Model != "" {
		r.printf(r.colors.Blue, "Model override: %s\n", r.opts.Model)
	}
	r.printf(r.colors.Blue, "Stream view: %s\n", r.opts.StreamView)
	r.printf(r.colors.Blue, "Total: %d | Completed: %d | Remaining: %d\n", len(issues), completed, remaining)
	r.printf(r.colors.Blue, separatorLine)
	fmt.Println()
}

func (r *runner) processIssue(idx, total int, issue string, isResume bool) issueResult {
	return r.processIssueWithStrategy(idx, total, issue, isResume, false).result
}

func (r *runner) processIssueWithStrategy(idx, total int, issue string, isResume bool, deferChangedCompletion bool) issueExecution {
	details, err := r.fetchIssueDetails(issue)
	if err != nil {
		r.printf(r.colors.Red, "FAILED: unable to fetch %s: %v\n", r.issueLabel(issue), err)
		return issueExecution{result: resultFailed}
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
		return issueExecution{result: resultSuccess, details: details}
	}

	if r.isCompleted(issue) && !r.opts.Force {
		r.printf(r.colors.Green, "Already completed %s, skipping (use --force to reprocess)\n", r.issueLabel(issue))
		return issueExecution{result: resultSuccess, details: details}
	}

	r.setIssueLabel(issue, "ghir:running")

	dirty, err := r.workingTreeDirty()
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot determine git status: %v\n", err)
		return issueExecution{result: resultFailed, details: details}
	}
	if dirty {
		r.printf(r.colors.Red, "ERROR: uncommitted changes detected. Commit or stash before running.\n")
		r.printf(r.colors.Yellow, "Hint: review with `git status` and commit or stash changes.\n")
		return issueExecution{result: resultFailed, details: details}
	}

	startHead, err := r.gitOutput("rev-parse", "HEAD")
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot determine pre-run git HEAD: %v\n", err)
		return issueExecution{result: resultFailed, details: details}
	}

	prompt, err := r.buildPrompt(issue, details, isResume)
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot build prompt for %s: %v\n", r.issueLabel(issue), err)
		return issueExecution{result: resultFailed, details: details}
	}

	logPath := filepath.Join(r.opts.LogDir, r.logFileName(issue))
	r.printf(r.colors.Yellow, "Starting %s for %s...\n", agentDisplayName(r.opts.Agent), r.issueLabel(issue))
	fmt.Printf("Log: %s\n", logPath)

	exitCode, logOutput, err := r.runAgentWithRetry(prompt, logPath)
	if err != nil {
		r.printf(r.colors.Red, "FAILED: %s invocation failed for %s: %v\n", r.opts.Agent, r.issueLabel(issue), err)
		return issueExecution{result: resultFailed, details: details}
	}

	if retry, commitErr := r.handleSessionLimitRetry(logOutput, exitCode, "mid-work", r.buildIssueCommitMessage(issue, details.Title, true)); commitErr != nil {
		r.printf(r.colors.Red, "FAILED: %v\n", commitErr)
		return issueExecution{result: resultFailed, details: details}
	} else if retry {
		return issueExecution{result: resultRetry, details: details}
	}

	if exitCode != 0 {
		r.printf(r.colors.Red, "FAILED: %s exited with code %d for %s\n", r.opts.Agent, exitCode, r.issueLabel(issue))
		r.printf(r.colors.Red, "Check log: %s\n", logPath)
		return issueExecution{result: resultFailed, details: details}
	}

	endHead, err := r.gitOutput("rev-parse", "HEAD")
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot determine post-run git HEAD: %v\n", err)
		return issueExecution{result: resultFailed, details: details}
	}

	if endHead != startHead {
		headMsg, _ := r.gitOutput("log", "-1", "--pretty=format:%s")
		rangeSubjects, rangeErr := r.gitOutput("log", "--pretty=format:%s", fmt.Sprintf("%s..%s", startHead, endHead))
		hasIssueRef := r.fileMode || (rangeErr == nil && issueMentionedInSubjects(rangeSubjects, issue))

		if !deferChangedCompletion && !r.tryMarkCompleted(issue) {
			return issueExecution{result: resultFailed, details: details}
		}
		r.printf(r.colors.Green, "SUCCESS: %s committed by %s\n", r.issueLabel(issue), agentDisplayName(r.opts.Agent))
		if strings.TrimSpace(headMsg) != "" {
			r.printf(r.colors.Green, "Commit: %s\n", headMsg)
		}
		if !hasIssueRef {
			r.printf(r.colors.Yellow, "WARNING: new commit(s) do not mention %s in subject lines.\n", r.issueLabel(issue))
		}
		fmt.Println()
		return issueExecution{result: resultSuccess, changed: true, details: details}
	}

	dirty, err = r.workingTreeDirty()
	if err != nil {
		r.printf(r.colors.Red, "FAILED: cannot determine post-run git status: %v\n", err)
		return issueExecution{result: resultFailed, details: details}
	}
	if dirty {
		r.printf(r.colors.Yellow, "%s did not commit. Uncommitted changes found, committing now.\n", agentDisplayName(r.opts.Agent))
		if err := r.commitAll(r.buildIssueCommitMessage(issue, details.Title, false)); err != nil {
			r.printf(r.colors.Red, "FAILED: fallback commit failed for %s: %v\n", r.issueLabel(issue), err)
			return issueExecution{result: resultFailed, details: details}
		}
		if !deferChangedCompletion && !r.tryMarkCompleted(issue) {
			return issueExecution{result: resultFailed, details: details}
		}
		r.printf(r.colors.Green, "SUCCESS: %s committed by runner\n", r.issueLabel(issue))
		fmt.Println()
		return issueExecution{result: resultSuccess, changed: true, details: details}
	}

	r.printf(r.colors.Yellow, "No changes produced for %s (already done or no modifications needed)\n", r.issueLabel(issue))
	if !r.tryMarkCompleted(issue) {
		return issueExecution{result: resultFailed, details: details}
	}
	r.printf(r.colors.Green, "SUCCESS: %s completed (no changes needed)\n", r.issueLabel(issue))
	fmt.Println()
	return issueExecution{result: resultSuccess, details: details}
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
	if r.opts.StreamView == streamViewPretty && (r.opts.Agent == "codex" || r.opts.Agent == "cursor-agent" || r.opts.Agent == "gemini" || r.opts.Agent == "pi") {
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

const (
	agentRetryMaxAttempts = 3
	agentRetryDelay       = 5 * time.Second
)

func (r *runner) runAgentWithRetry(prompt, logPath string) (exitCode int, logOutput string, err error) {
	retryReason := ""
	for attempt := 0; attempt <= agentRetryMaxAttempts; attempt++ {
		if attempt > 0 {
			reason := retryReason
			if strings.TrimSpace(reason) == "" {
				reason = "transient agent error"
			}
			r.printf(r.colors.Yellow, "Retrying due to %s (attempt %d/%d)...\n", reason, attempt, agentRetryMaxAttempts)
			time.Sleep(agentRetryDelay)
		}
		exitCode, logOutput, err = r.runAgent(prompt, logPath)
		if err != nil {
			return 0, "", err
		}
		if reason, shouldRetry := detectRetryableAgentError(logOutput, r.opts.Agent, exitCode); shouldRetry && attempt < agentRetryMaxAttempts {
			retryReason = reason
			continue
		}
		break
	}
	return exitCode, logOutput, nil
}

func (r *runner) buildIssueCommitMessage(issue, title string, sessionLimit bool) string {
	if sessionLimit {
		if r.fileMode {
			return fmt.Sprintf("wip: partial work on %s - %s (session limit hit)%s", issue, title, commitCoAuthorSuffix)
		}
		return fmt.Sprintf("wip: partial work on #%s - %s (session limit hit)%s", issue, title, commitCoAuthorSuffix)
	}
	if r.fileMode {
		return fmt.Sprintf("feat: implement %s - %s%s", issue, title, commitCoAuthorSuffix)
	}
	return fmt.Sprintf("feat: implement #%s - %s\n\nCloses #%s%s", issue, title, issue, commitCoAuthorSuffix)
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

// tryMarkCompleted marks the issue as complete. On error, prints failure and returns false.
func (r *runner) tryMarkCompleted(issue string) bool {
	if err := r.markCompleted(issue); err != nil {
		r.printf(r.colors.Red, "FAILED: could not mark %s completed: %v\n", r.issueLabel(issue), err)
		return false
	}
	return true
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
	r.printf(r.colors.Yellow, separatorLine)
	r.printf(r.colors.Yellow, "SESSION LIMIT HIT - waiting until %s (%ds)\n", resetTime.Format("2006-01-02 15:04 UTC"), waitSeconds)
	r.printf(r.colors.Yellow, separatorLine)

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

func (r *runner) defaultBranch() (string, error) {
	if out, err := r.commandOutput(r.opts.GHBin, "repo", "view", "--json", "defaultBranchRef", "-q", ".defaultBranchRef.name"); err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	// Fallback: git symbolic-ref refs/remotes/origin/HEAD
	out, err := r.gitOutput("symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", fmt.Errorf("could not determine default branch (gh repo view and git symbolic-ref both failed): %w", err)
	}
	branch := strings.TrimPrefix(strings.TrimSpace(out), "refs/remotes/origin/")
	if branch == "" || branch == out {
		return "", fmt.Errorf("could not parse default branch from %q", out)
	}
	return branch, nil
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
