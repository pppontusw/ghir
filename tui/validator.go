package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var issueNumberPattern = regexp.MustCompile(`^\d+$`)

// MatchIssueNumber reports whether s is a valid numeric issue ID (e.g. for --issue).
func MatchIssueNumber(s string) bool {
	return issueNumberPattern.MatchString(s)
}

// ValidImproveModes is the canonical list of improve --mode values (includes "mixed").
var ValidImproveModes = []string{
	"mixed",
	"cleanup",
	"quality",
	"refactor",
	"security",
	"bugfix",
	"dead-code",
	"docs",
	"tests",
	"deps",
	"perf",
	"a11y",
	"errors",
	"types",
	"logging",
}

// DefaultStrategy is the canonical default for non-improve and improve strategy flags.
const DefaultStrategy = "direct"

// ValidStrategies is the canonical list of supported --strategy values.
var ValidStrategies = []string{
	"direct",
	"pr-per-pass",
	"pr-chain",
	"pr-at-end",
}

// ValidImproveStrategies is retained as an alias for improve-specific callers.
var ValidImproveStrategies = ValidStrategies

// ValidImprovePromptSources is the canonical list of improve prompt source values.
var ValidImprovePromptSources = []string{
	string(ImprovePromptSourceMode),
	string(ImprovePromptSourceInline),
	string(ImprovePromptSourceFile),
}

// ValidAgents is the canonical list of --agent values.
var ValidAgents = []string{
	"claude",
	"codex",
	"gemini",
	"cursor-agent",
	"pi",
}

// ValidStreamViews is the canonical list of --stream-view values.
var ValidStreamViews = []string{
	"pretty",
	"raw",
}

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Diagnostic struct {
	Severity Severity
	Message  string
	Hint     string
}

type DiagnosticReport struct {
	items []Diagnostic
}

func (r *DiagnosticReport) add(severity Severity, message, hint string) {
	r.items = append(r.items, Diagnostic{
		Severity: severity,
		Message:  message,
		Hint:     hint,
	})
}

func (r DiagnosticReport) Items() []Diagnostic {
	items := make([]Diagnostic, len(r.items))
	copy(items, r.items)
	return items
}

func (r DiagnosticReport) HasErrors() bool {
	for _, item := range r.items {
		if item.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (r DiagnosticReport) Counts() (int, int, int) {
	var errors int
	var warnings int
	var infos int
	for _, item := range r.items {
		switch item.Severity {
		case SeverityError:
			errors++
		case SeverityWarning:
			warnings++
		default:
			infos++
		}
	}
	return errors, warnings, infos
}

func ValidateCommandState(state CommandState) DiagnosticReport {
	var report DiagnosticReport

	if hasIssueFileConflict(state) {
		report.add(SeverityError, "--files/--all-files cannot be combined with --issue/--all-open/--issues/--issues-file", "Clear the inactive source family before running.")
	}

	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles:
		validateFileState(state.Files, &report)
	case WorkflowImprove:
		validateImproveState(state.Improve, &report)
	default:
		validateIssueState(state.Issues, &report)
	}

	validateRuntimeState(state.Runtime, &report)

	if len(report.items) == 0 {
		report.add(SeverityInfo, "Parser-equivalent validation passed.", "")
	}

	return report
}

func validateIssueState(state IssueCommandState, report *DiagnosticReport) {
	strategy := validateStrategy(state.Strategy, report, "Choose one of the supported strategies.")
	if state.Loop && normalizeIssueSource(state.Source) != IssueSourceAllOpen {
		report.add(SeverityError, "--loop requires either --all-open or --all-files", "Use --all-open for looping issue runs.")
	}
	if state.Loop && strategy != DefaultStrategy {
		report.add(SeverityError, "--loop is only supported with --strategy "+DefaultStrategy, "Use direct execution for looping issue runs.")
	}
	if normalizeIssueSource(state.Source) == IssueSourceSingle {
		value := strings.TrimSpace(state.SingleIssue)
		if value != "" && !MatchIssueNumber(value) {
			report.add(SeverityError, fmt.Sprintf("--issue must be numeric: %q", value), "Use a numeric GitHub issue number.")
		}
	}
}

func validateImproveState(state ImproveCommandState, report *DiagnosticReport) {
	promptSource := strings.ToLower(strings.TrimSpace(string(state.PromptSource)))
	if promptSource == "" {
		promptSource = string(defaultImprovePromptSource)
	}
	if !slices.Contains(ValidImprovePromptSources, promptSource) {
		report.add(SeverityError, "--prompt-source must be one of: "+strings.Join(ValidImprovePromptSources, ", "), "Choose a supported improve prompt source.")
	}

	switch ImprovePromptSource(promptSource) {
	case ImprovePromptSourceInline:
		if strings.TrimSpace(state.Prompt) == "" {
			report.add(SeverityError, "--prompt requires a value", "Provide inline improve prompt text.")
		}
	case ImprovePromptSourceFile:
		if strings.TrimSpace(state.PromptFile) == "" {
			report.add(SeverityError, "--prompt-file requires a value", "Provide a path to the improve prompt file.")
		}
	default:
		mode := strings.ToLower(strings.TrimSpace(state.Mode))
		if mode == "" {
			mode = defaultImproveMode
		}
		if !slices.Contains(ValidImproveModes, mode) {
			report.add(SeverityError, "--mode must be one of: "+strings.Join(ValidImproveModes, ", "), "Choose one of the supported improve modes.")
		}
	}

	validateStrategy(state.Strategy, report, "Choose one of the supported improve strategies.")

	if state.Iterations != nil && *state.Iterations < 0 {
		report.add(SeverityError, "--iterations must be a non-negative integer", "Use zero or a positive pass count.")
	}
	if state.Iterations != nil && *state.Iterations == 0 && !state.Loop {
		report.add(SeverityError, "--iterations must be positive unless --loop is set", "Enable --loop or set iterations to at least 1.")
	}
}

func validateRuntimeState(runtime CommandRuntime, report *DiagnosticReport) {
	agent := normalizeAgent(runtime.Agent)
	if !slices.Contains(ValidAgents, agent) {
		report.add(SeverityError, "--agent must be one of: "+strings.Join(ValidAgents, ", "), "Select a supported agent binary.")
	}

	view := normalizeStreamView(runtime.StreamView)
	if !slices.Contains(ValidStreamViews, view) {
		report.add(SeverityError, "--stream-view must be one of: "+strings.Join(ValidStreamViews, ", "), "Use pretty or raw output.")
	}

	if runtime.WaitBufferSec != nil && *runtime.WaitBufferSec < 0 {
		report.add(SeverityError, "--wait-buffer-sec must be a non-negative integer", "Use zero or a positive number of seconds.")
	}
}

func validateFileState(state FileCommandState, report *DiagnosticReport) {
	strategy := validateStrategy(state.Strategy, report, "Choose one of the supported strategies.")
	if state.Loop && normalizeFileSource(state.Source) != FileSourceAllFiles {
		report.add(SeverityError, "--loop requires either --all-open or --all-files", "Use --all-files for looping file runs.")
	}
	if state.Loop && strategy != DefaultStrategy {
		report.add(SeverityError, "--loop is only supported with --strategy "+DefaultStrategy, "Use direct execution for looping file runs.")
	}
}

func hasIssueFileConflict(state CommandState) bool {
	return issueFamilySelected(state.Issues) && fileFamilySelected(state.Files)
}

func issueFamilySelected(state IssueCommandState) bool {
	switch normalizeIssueSource(state.Source) {
	case IssueSourceSingle:
		return strings.TrimSpace(state.SingleIssue) != ""
	case IssueSourceCSV:
		return len(normalizeOrderedItems(state.Issues)) > 0
	case IssueSourceAllOpen:
		return true
	default:
		return strings.TrimSpace(state.IssuesFile) != ""
	}
}

func fileFamilySelected(state FileCommandState) bool {
	if normalizeFileSource(state.Source) == FileSourceAllFiles {
		return strings.TrimSpace(state.AllFiles) != ""
	}
	return len(normalizeOrderedItems(state.Files)) > 0
}

func validateStrategy(value string, report *DiagnosticReport, hint string) string {
	strategy := normalizedLowerDefault(value, DefaultStrategy)
	if !slices.Contains(ValidStrategies, strategy) {
		report.add(SeverityError, "--strategy must be one of: "+strings.Join(ValidStrategies, ", "), hint)
	}
	return strategy
}

func normalizedLowerDefault(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}
