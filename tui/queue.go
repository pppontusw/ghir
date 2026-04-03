package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"ghir/defaults"
)

type queueResolveOptions struct {
	RepoRoot      string
	CommandOutput func(string, ...string) (string, error)
}

func refreshActiveQueue(state *CommandState, opts queueResolveOptions) (string, DiagnosticReport) {
	commandOutput := opts.CommandOutput
	if commandOutput == nil {
		commandOutput = defaultCommandOutput
	}

	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles:
		if len(normalizeOrderedItems(state.Files.ResolvedQueue)) == 0 {
			queue, err := resolveFilesQueue(opts.RepoRoot, state.Files)
			state.Files.ResolvedQueue = queue
			if err != nil {
				state.Files.StagedQueue = nil
				return err.Error(), queueErrorReport(err)
			}
		}
		state.Files.StagedQueue = syncStagedQueue(state.Files.ResolvedQueue, state.Files.StagedQueue)
		stage := activeQueueStage(*state)
		return queuePreviewHint(*state), queueStageReport("file", stage)
	case WorkflowIssues:
		if len(normalizeOrderedItems(state.Issues.ResolvedQueue)) == 0 {
			queue, err := resolveIssuesQueue(opts.RepoRoot, state.Issues, state.Runtime, commandOutput)
			state.Issues.ResolvedQueue = queue
			if err != nil {
				state.Issues.StagedQueue = nil
				return err.Error(), queueErrorReport(err)
			}
		}
		state.Issues.StagedQueue = syncStagedQueue(state.Issues.ResolvedQueue, state.Issues.StagedQueue)
		stage := activeQueueStage(*state)
		return queuePreviewHint(*state), queueStageReport("issue", stage)
	default:
		return "", DiagnosticReport{}
	}
}

func resolveIssuesQueue(repoRoot string, state IssueCommandState, runtime CommandRuntime, commandOutput func(string, ...string) (string, error)) ([]string, error) {
	switch normalizeIssueSource(state.Source) {
	case IssueSourceSingle:
		value := strings.TrimSpace(state.SingleIssue)
		if value == "" {
			return nil, nil
		}
		return []string{value}, nil
	case IssueSourceCSV:
		return normalizeOrderedItems(state.Issues), nil
	case IssueSourceAllOpen:
		return fetchOpenIssueQueue(effectiveGHBin(runtime), strings.TrimSpace(state.Label), commandOutput)
	default:
		path := strings.TrimSpace(state.IssuesFile)
		if path == "" {
			path = defaults.IssuesFile
		}
		return readIssueQueueFile(resolveConfigurePath(repoRoot, path))
	}
}

func resolveFilesQueue(repoRoot string, state FileCommandState) ([]string, error) {
	switch normalizeFileSource(state.Source) {
	case FileSourceAllFiles:
		dir := strings.TrimSpace(state.AllFiles)
		if dir == "" {
			return nil, nil
		}
		return readMarkdownQueue(repoRoot, dir)
	default:
		return loadExplicitFileQueue(repoRoot, state.Files)
	}
}

func readIssueQueueFile(path string) ([]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaults.IssuesFile
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("issue file not found: %s", path)
		}
		return nil, fmt.Errorf("read issues file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	issues := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		id := fields[0]
		if !issueNumberPattern.MatchString(id) {
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

func readMarkdownQueue(repoRoot, dir string) ([]string, error) {
	abs := resolveConfigurePath(repoRoot, dir)
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}

		fullPath := filepath.Join(abs, entry.Name())
		rel, relErr := filepath.Rel(repoRoot, fullPath)
		if relErr != nil {
			rel = fullPath
		}
		paths = append(paths, rel)
	}

	if len(paths) == 0 {
		return nil, fmt.Errorf("no .md files found in %s", dir)
	}

	sortPreviewQueue(paths)
	return paths, nil
}

func loadExplicitFileQueue(repoRoot string, items []string) ([]string, error) {
	paths := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, raw := range items {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		resolved := resolveConfigurePath(repoRoot, value)
		if _, err := os.Stat(resolved); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("file not found: %s", value)
			}
			return nil, fmt.Errorf("inspect file %s: %w", value, err)
		}

		display := value
		if strings.TrimSpace(repoRoot) != "" {
			if rel, err := filepath.Rel(repoRoot, resolved); err == nil {
				display = rel
			}
		}
		if _, ok := seen[display]; ok {
			continue
		}
		paths = append(paths, display)
		seen[display] = struct{}{}
	}
	return paths, nil
}

func fetchOpenIssueQueue(ghBin, label string, commandOutput func(string, ...string) (string, error)) ([]string, error) {
	args := []string{"issue", "list", "--state", "open", "--limit", "4000", "--json", "number"}
	if label != "" {
		args = append(args, "--label", label)
	}

	output, err := commandOutput(ghBin, args...)
	if err != nil {
		return nil, fmt.Errorf("fetch open issues: %w", err)
	}

	var items []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(output), &items); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}

	issues := make([]string, 0, len(items))
	for _, item := range items {
		issues = append(issues, strconv.Itoa(item.Number))
	}

	if len(issues) == 0 {
		return nil, fmt.Errorf("no open issues found")
	}

	sortPreviewQueue(issues)
	return issues, nil
}

func sortPreviewQueue(values []string) {
	sort.Slice(values, func(i, j int) bool {
		return LessNumericIssueOrPath(values[i], values[j])
	})
}

// LessNumericIssueOrPath reports whether a < b when comparing numeric IDs or path basenames.
// Used for consistent ordering of issue IDs and file paths in both CLI and TUI.
func LessNumericIssueOrPath(a, b string) bool {
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

func syncStagedQueue(resolved, staged []string) []string {
	resolved = normalizeOrderedItems(resolved)
	if len(resolved) == 0 {
		return nil
	}
	if staged == nil {
		return append([]string(nil), resolved...)
	}

	allowed := make(map[string]struct{}, len(resolved))
	for _, item := range resolved {
		allowed[item] = struct{}{}
	}

	next := make([]string, 0, len(staged))
	seen := make(map[string]struct{}, len(staged))
	for _, raw := range staged {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if _, ok := allowed[item]; !ok {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		next = append(next, item)
		seen[item] = struct{}{}
	}

	return next
}

func activeResolvedQueue(state CommandState) []string {
	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles:
		return normalizeOrderedItems(state.Files.ResolvedQueue)
	case WorkflowIssues:
		return normalizeOrderedItems(state.Issues.ResolvedQueue)
	default:
		return nil
	}
}

func activeStagedQueue(state CommandState) []string {
	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles:
		if state.Files.StagedQueue == nil {
			return normalizeOrderedItems(state.Files.ResolvedQueue)
		}
		return normalizeOrderedItems(state.Files.StagedQueue)
	case WorkflowIssues:
		if state.Issues.StagedQueue == nil {
			return normalizeOrderedItems(state.Issues.ResolvedQueue)
		}
		return normalizeOrderedItems(state.Issues.StagedQueue)
	default:
		return nil
	}
}

func activeQueueStage(state CommandState) QueueStage {
	return QueueStage{
		Original: activeResolvedQueue(state),
		Staged:   activeStagedQueue(state),
	}
}

func queueErrorReport(err error) DiagnosticReport {
	var report DiagnosticReport
	if err != nil {
		report.add(SeverityError, err.Error(), "Adjust the selected source until the queue preview can be built.")
	}
	return report
}

func queueStageReport(noun string, stage QueueStage) DiagnosticReport {
	var report DiagnosticReport
	if len(stage.Original) == 0 {
		if hint := queuePreviewHintForEmpty(stage); hint != "" {
			report.add(SeverityInfo, hint, "")
		}
		return report
	}
	if len(stage.Staged) == 0 {
		report.add(SeverityError, fmt.Sprintf("staged %s queue is empty", noun), "Restore the original ordering or keep at least one item staged for this run.")
		return report
	}

	report.add(SeverityInfo, fmt.Sprintf("Queue preview ready: %d %ss staged from %d resolved.", len(stage.Staged), noun, len(stage.Original)), "")
	return report
}

func queuePreviewHint(state CommandState) string {
	stage := activeQueueStage(state)
	if len(stage.Original) > 0 {
		if len(stage.Staged) == 0 {
			return "All items are removed from this run."
		}
		return ""
	}
	return queuePreviewHintForState(state)
}

func queuePreviewHintForState(state CommandState) string {
	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles:
		if normalizeFileSource(state.Files.Source) == FileSourceAllFiles {
			return "Queue preview will load from the selected directory when files are available."
		}
		if len(normalizeOrderedItems(state.Files.Files)) == 0 {
			return "Queue preview is empty until files are provided."
		}
		return ""
	case WorkflowIssues:
		switch normalizeIssueSource(state.Issues.Source) {
		case IssueSourceAllOpen:
			return "Queue preview will load the current open issues for this run."
		case IssueSourceFile:
			return "Queue preview loads from the current issues file."
		case IssueSourceCSV:
			if len(normalizeOrderedItems(state.Issues.Issues)) == 0 {
				return "Queue preview is empty until issue ids are provided."
			}
		case IssueSourceSingle:
			if strings.TrimSpace(state.Issues.SingleIssue) == "" {
				return "Queue preview is empty until an issue number is provided."
			}
		}
	}
	return ""
}

func queuePreviewHintForEmpty(stage QueueStage) string {
	if len(stage.Original) == 0 {
		return "Queue preview is waiting for source input."
	}
	return ""
}

func replaceActiveStagedQueue(state *CommandState, queue []string) {
	staged := make([]string, len(queue))
	copy(staged, queue)
	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles:
		state.Files.StagedQueue = staged
	case WorkflowIssues:
		state.Issues.StagedQueue = staged
	}
}
