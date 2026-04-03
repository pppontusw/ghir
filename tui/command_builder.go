package tui

import (
	"strconv"
	"strings"
)

const (
	defaultCommandExecutable   = "ghir"
	defaultCommandAgent        = "claude"
	defaultCommandStreamView   = "pretty"
	defaultCommandWaitBuffer   = 120
	defaultClaudeBin           = "claude"
	defaultCodexBin            = "codex"
	defaultGeminiBin           = "gemini"
	defaultCursorBin           = "cursor-agent"
	defaultPiBin               = "pi"
	defaultGHBin               = "gh"
	defaultImproveMode         = "cleanup"
	defaultImprovePromptSource = ImprovePromptSourceMode
	defaultImproveStrategy     = DefaultStrategy
	customModelOptionValue     = "__custom_model__"
)

type builtInModel struct {
	Value   string
	Label   string
	Default bool
}

type Workflow string

const (
	WorkflowIssues  Workflow = "issues"
	WorkflowFiles   Workflow = "files"
	WorkflowImprove Workflow = "improve"
)

type IssueSource string

const (
	IssueSourceSingle  IssueSource = "single"
	IssueSourceCSV     IssueSource = "csv"
	IssueSourceFile    IssueSource = "file"
	IssueSourceAllOpen IssueSource = "all-open"
)

type FileSource string

const (
	FileSourceExplicit FileSource = "explicit"
	FileSourceAllFiles FileSource = "all-files"
)

type ImprovePromptSource string

const (
	ImprovePromptSourceMode   ImprovePromptSource = "mode"
	ImprovePromptSourceInline ImprovePromptSource = "prompt"
	ImprovePromptSourceFile   ImprovePromptSource = "prompt-file"
)

type QueueStage struct {
	Original []string
	Staged   []string
}

type CommandRuntime struct {
	Agent         string
	Model         string
	ModelCustom   bool
	StreamView    string
	WaitBufferSec *int
	NoColor       bool
	ClaudeBin     string
	CodexBin      string
	GeminiBin     string
	CursorBin     string
	PiBin         string
	GHBin         string
}

type IssueCommandState struct {
	Source          IssueSource
	Strategy        string
	SingleIssue     string
	Issues          []string
	IssuesFile      string
	Label           string
	DryRun          bool
	Force           bool
	ContinueOnError bool
	Loop            bool
	PromptTemplate  string
	LogDir          string
	DoneFile        string
	ResolvedQueue   []string
	StagedQueue     []string
}

type FileCommandState struct {
	Source          FileSource
	Strategy        string
	Files           []string
	AllFiles        string
	DryRun          bool
	Force           bool
	ContinueOnError bool
	Loop            bool
	PromptTemplate  string
	LogDir          string
	DoneFile        string
	ResolvedQueue   []string
	StagedQueue     []string
}

type ImproveCommandState struct {
	PromptSource ImprovePromptSource
	Mode         string
	Prompt       string
	PromptFile   string
	Iterations   *int
	Loop         bool
	Strategy     string
	Scope        string
}

type CommandState struct {
	Executable string
	Workflow   Workflow
	Runtime    CommandRuntime
	Issues     IssueCommandState
	Files      FileCommandState
	Improve    ImproveCommandState
}

func BuildCommandArgs(state CommandState) []string {
	args := make([]string, 0, 24)

	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles:
		args = append(args, buildFilesArgs(state.Files)...)
	case WorkflowImprove:
		args = append(args, "improve")
		args = append(args, buildImproveArgs(state.Improve)...)
	default:
		args = append(args, buildIssuesArgs(state.Issues)...)
	}

	return appendRuntimeArgs(args, state.Runtime)
}

func BuildCommandString(state CommandState) string {
	args := BuildCommandArgs(state)
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(commandExecutable(state.Executable)))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func builtInModelsForAgent(agent string) []builtInModel {
	switch normalizeAgent(agent) {
	case "codex":
		return []builtInModel{
			{Value: "gpt-5.4", Label: "gpt-5.4 (default)", Default: true},
			{Value: "gpt-5.3-codex", Label: "gpt-5.3-codex"},
		}
	case "gemini":
		return []builtInModel{
			{Value: "gemini-3.1-pro-preview", Label: "gemini-3.1-pro-preview (default)", Default: true},
			{Value: "gemini-3-flash-preview", Label: "gemini-3-flash-preview"},
		}
	case "cursor-agent":
		return []builtInModel{
			{Value: "auto", Label: "auto (default)", Default: true},
			{Value: "opus-4.6", Label: "opus-4.6"},
			{Value: "opus-4.6-thinking", Label: "opus-4.6-thinking"},
			{Value: "sonnet-4.6", Label: "sonnet-4.6"},
			{Value: "sonnet-4.6-thinking", Label: "sonnet-4.6-thinking"},
			{Value: "gpt-5.4-high", Label: "gpt-5.4-high"},
			{Value: "gpt-5.4-medium", Label: "gpt-5.4-medium"},
		}
	case "pi":
		return []builtInModel{
			{Value: "github-copilot/gpt-5.4:high", Label: "github-copilot/gpt-5.4:high (default)", Default: true},
		}
	default:
		return []builtInModel{
			{Value: "opus", Label: "Opus (default)", Default: true},
			{Value: "sonnet", Label: "Sonnet"},
		}
	}
}

func defaultModelForAgent(agent string) string {
	for _, model := range builtInModelsForAgent(agent) {
		if model.Default {
			return model.Value
		}
	}
	return ""
}

func isBuiltInModelForAgent(agent, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for _, candidate := range builtInModelsForAgent(agent) {
		if candidate.Value == model {
			return true
		}
	}
	return false
}

func selectedModelChoice(agent, model string, custom bool) string {
	if custom {
		return customModelOptionValue
	}
	model = strings.TrimSpace(model)
	switch {
	case model == "":
		return defaultModelForAgent(agent)
	case isBuiltInModelForAgent(agent, model):
		return model
	default:
		return customModelOptionValue
	}
}

func buildIssuesArgs(state IssueCommandState) []string {
	args := make([]string, 0, 12)
	staged := normalizeOrderedItems(state.StagedQueue)
	resolved := resolvedIssuesQueue(state)
	stagedOverride := hasStagedQueueOverride(resolved, staged)

	if stagedOverride {
		args = append(args, "--issues", strings.Join(staged, ","))
	} else {
		switch normalizeIssueSource(state.Source) {
		case IssueSourceSingle:
			if value := strings.TrimSpace(state.SingleIssue); value != "" {
				args = append(args, "--issue", value)
			}
		case IssueSourceCSV:
			if items := normalizeOrderedItems(state.Issues); len(items) > 0 {
				args = append(args, "--issues", strings.Join(items, ","))
			}
		case IssueSourceAllOpen:
			args = append(args, "--all-open")
			if value := strings.TrimSpace(state.Label); value != "" {
				args = append(args, "--label", value)
			}
		default:
			if value := strings.TrimSpace(state.IssuesFile); value != "" {
				args = append(args, "--issues-file", value)
			}
		}
	}

	return appendRunArgs(args, runOptions{
		strategy:        state.Strategy,
		dryRun:          state.DryRun,
		force:           state.Force,
		continueOnError: state.ContinueOnError,
		loop:            state.Loop && !stagedOverride && normalizeIssueSource(state.Source) == IssueSourceAllOpen,
		promptTemplate:  state.PromptTemplate,
		logDir:          state.LogDir,
		doneFile:        state.DoneFile,
	})
}

func buildFilesArgs(state FileCommandState) []string {
	args := make([]string, 0, 12)
	staged := normalizeOrderedItems(state.StagedQueue)
	resolved := resolvedFilesQueue(state)
	stagedOverride := hasStagedQueueOverride(resolved, staged)

	if stagedOverride {
		args = append(args, "--files", strings.Join(staged, ","))
	} else {
		switch normalizeFileSource(state.Source) {
		case FileSourceAllFiles:
			if value := strings.TrimSpace(state.AllFiles); value != "" {
				args = append(args, "--all-files", value)
			}
		default:
			if items := normalizeOrderedItems(state.Files); len(items) > 0 {
				args = append(args, "--files", strings.Join(items, ","))
			}
		}
	}

	return appendRunArgs(args, runOptions{
		strategy:        state.Strategy,
		dryRun:          state.DryRun,
		force:           state.Force,
		continueOnError: state.ContinueOnError,
		loop:            state.Loop && !stagedOverride && normalizeFileSource(state.Source) == FileSourceAllFiles,
		promptTemplate:  state.PromptTemplate,
		logDir:          state.LogDir,
		doneFile:        state.DoneFile,
	})
}

func buildImproveArgs(state ImproveCommandState) []string {
	args := make([]string, 0, 12)

	switch normalizeImprovePromptSource(state.PromptSource) {
	case ImprovePromptSourceInline:
		if prompt := strings.TrimSpace(state.Prompt); prompt != "" {
			args = append(args, "--prompt", prompt)
		}
	case ImprovePromptSourceFile:
		if path := strings.TrimSpace(state.PromptFile); path != "" {
			args = append(args, "--prompt-file", path)
		}
	default:
		if mode := strings.TrimSpace(state.Mode); mode != "" && mode != defaultImproveMode {
			args = append(args, "--mode", mode)
		}
	}
	if state.Iterations != nil && *state.Iterations != 1 {
		args = append(args, "--iterations", intString(*state.Iterations))
	}
	if state.Loop {
		args = append(args, "--loop")
	}
	if strategy := strings.TrimSpace(state.Strategy); strategy != "" && strategy != defaultImproveStrategy {
		args = append(args, "--strategy", strategy)
	}
	if scope := strings.TrimSpace(state.Scope); scope != "" {
		args = append(args, "--scope", scope)
	}

	return args
}

type runOptions struct {
	strategy        string
	dryRun          bool
	force           bool
	continueOnError bool
	loop            bool
	promptTemplate  string
	logDir          string
	doneFile        string
}

func appendRunArgs(args []string, options runOptions) []string {
	if value := strings.TrimSpace(options.strategy); value != "" && value != DefaultStrategy {
		args = append(args, "--strategy", value)
	}
	if options.dryRun {
		args = append(args, "--dry-run")
	}
	if options.force {
		args = append(args, "--force")
	}
	if options.continueOnError {
		args = append(args, "--continue-on-error")
	}
	if options.loop {
		args = append(args, "--loop")
	}
	if value := strings.TrimSpace(options.promptTemplate); value != "" {
		args = append(args, "--prompt-template", value)
	}
	if value := strings.TrimSpace(options.logDir); value != "" {
		args = append(args, "--log-dir", value)
	}
	if value := strings.TrimSpace(options.doneFile); value != "" {
		args = append(args, "--done-file", value)
	}
	return args
}

func appendRuntimeArgs(args []string, runtime CommandRuntime) []string {
	if agent := normalizeAgent(runtime.Agent); agent != defaultCommandAgent {
		args = append(args, "--agent", agent)
	}
	if model := strings.TrimSpace(runtime.Model); model != "" {
		args = append(args, "--model", model)
	}
	if view := normalizeStreamView(runtime.StreamView); view != defaultCommandStreamView {
		args = append(args, "--stream-view", view)
	}
	if runtime.WaitBufferSec != nil && *runtime.WaitBufferSec != defaultCommandWaitBuffer {
		args = append(args, "--wait-buffer-sec", intString(*runtime.WaitBufferSec))
	}
	if runtime.NoColor {
		args = append(args, "--no-color")
	}
	if value := strings.TrimSpace(runtime.ClaudeBin); value != "" && value != defaultClaudeBin {
		args = append(args, "--claude-bin", value)
	}
	if value := strings.TrimSpace(runtime.CodexBin); value != "" && value != defaultCodexBin {
		args = append(args, "--codex-bin", value)
	}
	if value := strings.TrimSpace(runtime.GeminiBin); value != "" && value != defaultGeminiBin {
		args = append(args, "--gemini-bin", value)
	}
	if value := strings.TrimSpace(runtime.CursorBin); value != "" && value != defaultCursorBin {
		args = append(args, "--cursor-bin", value)
	}
	if value := strings.TrimSpace(runtime.PiBin); value != "" && value != defaultPiBin {
		args = append(args, "--pi-bin", value)
	}
	if value := strings.TrimSpace(runtime.GHBin); value != "" && value != defaultGHBin {
		args = append(args, "--gh-bin", value)
	}
	return args
}

func normalizeCommandWorkflow(workflow Workflow) Workflow {
	switch strings.ToLower(strings.TrimSpace(string(workflow))) {
	case string(WorkflowFiles):
		return WorkflowFiles
	case string(WorkflowImprove):
		return WorkflowImprove
	default:
		return WorkflowIssues
	}
}

func normalizeImprovePromptSource(source ImprovePromptSource) ImprovePromptSource {
	switch strings.ToLower(strings.TrimSpace(string(source))) {
	case string(ImprovePromptSourceInline):
		return ImprovePromptSourceInline
	case string(ImprovePromptSourceFile):
		return ImprovePromptSourceFile
	default:
		return ImprovePromptSourceMode
	}
}

func normalizeIssueSource(source IssueSource) IssueSource {
	switch strings.ToLower(strings.TrimSpace(string(source))) {
	case string(IssueSourceSingle):
		return IssueSourceSingle
	case string(IssueSourceCSV):
		return IssueSourceCSV
	case string(IssueSourceAllOpen):
		return IssueSourceAllOpen
	default:
		return IssueSourceFile
	}
}

func normalizeFileSource(source FileSource) FileSource {
	if strings.ToLower(strings.TrimSpace(string(source))) == string(FileSourceAllFiles) {
		return FileSourceAllFiles
	}
	return FileSourceExplicit
}

func normalizeOrderedItems(values []string) []string {
	if values == nil {
		return nil
	}
	items := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		items = append(items, value)
		seen[value] = struct{}{}
	}
	return items
}

func resolvedIssuesQueue(state IssueCommandState) []string {
	if items := normalizeOrderedItems(state.ResolvedQueue); len(items) > 0 {
		return items
	}

	switch normalizeIssueSource(state.Source) {
	case IssueSourceSingle:
		if value := strings.TrimSpace(state.SingleIssue); value != "" {
			return []string{value}
		}
	case IssueSourceCSV:
		return normalizeOrderedItems(state.Issues)
	}

	return nil
}

func resolvedFilesQueue(state FileCommandState) []string {
	if items := normalizeOrderedItems(state.ResolvedQueue); len(items) > 0 {
		return items
	}

	if normalizeFileSource(state.Source) == FileSourceExplicit {
		return normalizeOrderedItems(state.Files)
	}

	return nil
}

func hasStagedQueueOverride(resolved, staged []string) bool {
	resolved = normalizeOrderedItems(resolved)
	if len(resolved) == 0 {
		return false
	}
	if staged == nil {
		return false
	}
	staged = normalizeOrderedItems(staged)
	if len(staged) != len(resolved) {
		return true
	}
	for i := range resolved {
		if resolved[i] != staged[i] {
			return true
		}
	}
	return false
}

func normalizeAgent(agent string) string {
	value := strings.ToLower(strings.TrimSpace(agent))
	if value == "" {
		return defaultCommandAgent
	}
	return value
}

func normalizeStreamView(view string) string {
	value := strings.ToLower(strings.TrimSpace(view))
	if value == "" {
		return defaultCommandStreamView
	}
	return value
}

func commandExecutable(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultCommandExecutable
	}
	return value
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if isShellSafe(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func isShellSafe(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("-._/:,=@+%", r):
		default:
			return false
		}
	}
	return true
}

func intString(value int) string {
	return strconv.Itoa(value)
}
