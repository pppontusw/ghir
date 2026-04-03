package tui

import (
	"os"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
)

type CommandCopiedMsg struct {
	Err error
}

type commandRailState struct {
	explain      bool
	copyStatus   string
	copySeverity Severity
}

type commandRailSnapshot struct {
	Invocation   string
	Explanations []commandExplanation
	Divergence   *commandRailDivergence
}

type commandExplanation struct {
	Field  string
	Output string
	Detail string
}

type commandRailDivergence struct {
	Summary   string
	Source    string
	Effective string
	Notes     []string
}

func (m Model) copyCurrentCommand() tea.Cmd {
	command := BuildCommandString(m.activeCommandState())
	copyCommand := m.options.CopyCommand
	if copyCommand == nil {
		copyCommand = defaultCopyCommand
	}

	return func() tea.Msg {
		return CommandCopiedMsg{Err: copyCommand(command)}
	}
}

func defaultCopyCommand(command string) error {
	sequence := osc52.New(command)
	switch {
	case os.Getenv("TMUX") != "":
		sequence = sequence.Tmux()
	case os.Getenv("STY") != "", strings.Contains(strings.ToLower(os.Getenv("TERM")), "screen"):
		sequence = sequence.Screen()
	}

	_, err := sequence.WriteTo(os.Stderr)
	return err
}

func renderCommandRail(model *Model) []string {
	snapshot := describeCommandRail(model.activeCommandState())
	lines := []string{
		"Command rail",
		"Actions: C copy full invocation  E " + commandExplainToggleLabel(model.commandRail.explain),
		renderCommandRailCopyStatus(model),
		"",
		"Invocation",
		snapshot.Invocation,
	}

	if snapshot.Divergence != nil {
		lines = append(lines,
			"",
			model.styles.warnText("warn  "+snapshot.Divergence.Summary),
			"Source field: "+snapshot.Divergence.Source,
			"Executed as: "+snapshot.Divergence.Effective,
		)
		for _, note := range snapshot.Divergence.Notes {
			lines = append(lines, model.styles.warnText("warn  "+note))
		}
	}

	lines = append(lines, "")
	if !model.commandRail.explain {
		lines = append(lines, model.styles.dimText("Press E to explain how the current fields map to emitted args."))
		return lines
	}

	lines = append(lines, "Explain")
	if len(snapshot.Explanations) == 0 {
		lines = append(lines, model.styles.dimText("No args are emitted yet. Fill in the active source to build a runnable command."))
		return lines
	}

	for _, item := range snapshot.Explanations {
		line := item.Field + " -> " + item.Output
		if strings.TrimSpace(item.Detail) != "" {
			line += " (" + item.Detail + ")"
		}
		lines = append(lines, line)
	}

	return lines
}

func renderCommandRailCopyStatus(model *Model) string {
	if strings.TrimSpace(model.commandRail.copyStatus) == "" {
		return model.styles.dimText("Copy: ready")
	}

	switch model.commandRail.copySeverity {
	case SeverityError:
		return model.styles.errText(model.commandRail.copyStatus)
	case SeverityWarning:
		return model.styles.warnText(model.commandRail.copyStatus)
	default:
		return model.styles.okText(model.commandRail.copyStatus)
	}
}

func commandExplainToggleLabel(enabled bool) string {
	if enabled {
		return "hide explanation"
	}
	return "show explanation"
}

func describeCommandRail(state CommandState) commandRailSnapshot {
	snapshot := commandRailSnapshot{
		Invocation: BuildCommandString(state),
	}

	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles:
		snapshot.Explanations, snapshot.Divergence = describeFilesCommandRail(state.Files)
	case WorkflowImprove:
		snapshot.Explanations = describeImproveCommandRail(state.Improve)
	default:
		snapshot.Explanations, snapshot.Divergence = describeIssuesCommandRail(state.Issues)
	}

	snapshot.Explanations = append(snapshot.Explanations, describeRuntimeCommandRail(state.Runtime)...)
	return snapshot
}

func describeIssuesCommandRail(state IssueCommandState) ([]commandExplanation, *commandRailDivergence) {
	sourceArgs, sourceDetail := issueSourceExplanation(state)
	staged := normalizeOrderedItems(state.StagedQueue)
	resolved := resolvedIssuesQueue(state)
	override := hasStagedQueueOverride(resolved, staged)

	explanations := make([]commandExplanation, 0, 10)
	var divergence *commandRailDivergence

	if override {
		effective := commandFragment("--issues", strings.Join(staged, ","))
		if sourceArgs != "" {
			explanations = append(explanations, commandExplanation{
				Field:  "Source selection",
				Output: sourceArgs,
				Detail: sourceDetail + " before staging rewrote execution",
			})
		}
		explanations = append(explanations, commandExplanation{
			Field:  "Staged queue",
			Output: effective,
			Detail: "run-scoped ordered issue subset emitted for execution",
		})
		divergence = &commandRailDivergence{
			Summary:   "Queue staging rewrote the issue source for this run.",
			Source:    fallbackCommandFragment(sourceArgs),
			Effective: effective,
		}
		if state.Loop && normalizeIssueSource(state.Source) == IssueSourceAllOpen {
			explanations = append(explanations, commandExplanation{
				Field:  "Loop",
				Output: "(omitted)",
				Detail: "staged issues no longer preserve --all-open semantics",
			})
			divergence.Notes = append(divergence.Notes, "Loop is omitted because staged issues no longer preserve --all-open semantics.")
		}
	} else if sourceArgs != "" {
		explanations = append(explanations, commandExplanation{
			Field:  "Source selection",
			Output: sourceArgs,
			Detail: sourceDetail,
		})
	}

	explanations = append(explanations, describeRunOptionCommandRail(runOptions{
		strategy:        state.Strategy,
		dryRun:          state.DryRun,
		force:           state.Force,
		continueOnError: state.ContinueOnError,
		loop:            state.Loop && !override && normalizeIssueSource(state.Source) == IssueSourceAllOpen,
		promptTemplate:  state.PromptTemplate,
		logDir:          state.LogDir,
		doneFile:        state.DoneFile,
	})...)

	return explanations, divergence
}

func describeFilesCommandRail(state FileCommandState) ([]commandExplanation, *commandRailDivergence) {
	sourceArgs, sourceDetail := fileSourceExplanation(state)
	staged := normalizeOrderedItems(state.StagedQueue)
	resolved := resolvedFilesQueue(state)
	override := hasStagedQueueOverride(resolved, staged)

	explanations := make([]commandExplanation, 0, 10)
	var divergence *commandRailDivergence

	if override {
		effective := commandFragment("--files", strings.Join(staged, ","))
		if sourceArgs != "" {
			explanations = append(explanations, commandExplanation{
				Field:  "Source selection",
				Output: sourceArgs,
				Detail: sourceDetail + " before staging rewrote execution",
			})
		}
		explanations = append(explanations, commandExplanation{
			Field:  "Staged queue",
			Output: effective,
			Detail: "run-scoped ordered file subset emitted for execution",
		})
		divergence = &commandRailDivergence{
			Summary:   "Queue staging rewrote the file source for this run.",
			Source:    fallbackCommandFragment(sourceArgs),
			Effective: effective,
		}
		if state.Loop && normalizeFileSource(state.Source) == FileSourceAllFiles {
			explanations = append(explanations, commandExplanation{
				Field:  "Loop",
				Output: "(omitted)",
				Detail: "staged files no longer preserve --all-files semantics",
			})
			divergence.Notes = append(divergence.Notes, "Loop is omitted because staged files no longer preserve --all-files semantics.")
		}
	} else if sourceArgs != "" {
		explanations = append(explanations, commandExplanation{
			Field:  "Source selection",
			Output: sourceArgs,
			Detail: sourceDetail,
		})
	}

	explanations = append(explanations, describeRunOptionCommandRail(runOptions{
		strategy:        state.Strategy,
		dryRun:          state.DryRun,
		force:           state.Force,
		continueOnError: state.ContinueOnError,
		loop:            state.Loop && !override && normalizeFileSource(state.Source) == FileSourceAllFiles,
		promptTemplate:  state.PromptTemplate,
		logDir:          state.LogDir,
		doneFile:        state.DoneFile,
	})...)

	return explanations, divergence
}

func describeImproveCommandRail(state ImproveCommandState) []commandExplanation {
	explanations := []commandExplanation{
		{
			Field:  "Workflow",
			Output: "improve",
			Detail: "selects the improve subcommand",
		},
	}

	switch normalizeImprovePromptSource(state.PromptSource) {
	case ImprovePromptSourceInline:
		if prompt := strings.TrimSpace(state.Prompt); prompt != "" {
			explanations = append(explanations, commandExplanation{
				Field:  "Prompt source",
				Output: commandFragment("--prompt", prompt),
				Detail: "uses inline custom improve prompt text",
			})
		}
	case ImprovePromptSourceFile:
		if path := strings.TrimSpace(state.PromptFile); path != "" {
			explanations = append(explanations, commandExplanation{
				Field:  "Prompt source",
				Output: commandFragment("--prompt-file", path),
				Detail: "loads custom improve prompt text from a file",
			})
		}
	default:
		if mode := strings.TrimSpace(state.Mode); mode != "" && mode != defaultImproveMode {
			explanations = append(explanations, commandExplanation{
				Field:  "Mode",
				Output: commandFragment("--mode", mode),
				Detail: "improve mode override",
			})
		}
	}
	if state.Iterations != nil && *state.Iterations != 1 {
		explanations = append(explanations, commandExplanation{
			Field:  "Iterations",
			Output: commandFragment("--iterations", intString(*state.Iterations)),
			Detail: "pass count for improve execution",
		})
	}
	if state.Loop {
		explanations = append(explanations, commandExplanation{
			Field:  "Loop",
			Output: commandFragment("--loop"),
			Detail: "continuous improve mode",
		})
	}
	if strategy := strings.TrimSpace(state.Strategy); strategy != "" && strategy != defaultImproveStrategy {
		explanations = append(explanations, commandExplanation{
			Field:  "Strategy",
			Output: commandFragment("--strategy", strategy),
			Detail: "improve execution strategy",
		})
	}
	if scope := strings.TrimSpace(state.Scope); scope != "" {
		explanations = append(explanations, commandExplanation{
			Field:  "Scope",
			Output: commandFragment("--scope", scope),
			Detail: "limits improve to the requested path",
		})
	}

	return explanations
}

func describeRunOptionCommandRail(options runOptions) []commandExplanation {
	explanations := make([]commandExplanation, 0, 8)
	if strategy := strings.TrimSpace(options.strategy); strategy != "" && strategy != DefaultStrategy {
		explanations = append(explanations, commandExplanation{
			Field:  "Strategy",
			Output: commandFragment("--strategy", strategy),
			Detail: "changes how queue work is published",
		})
	}
	if options.dryRun {
		explanations = append(explanations, commandExplanation{
			Field:  "Dry run",
			Output: commandFragment("--dry-run"),
			Detail: "preview mode",
		})
	}
	if options.force {
		explanations = append(explanations, commandExplanation{
			Field:  "Force",
			Output: commandFragment("--force"),
			Detail: "bypasses confirmation safeguards",
		})
	}
	if options.continueOnError {
		explanations = append(explanations, commandExplanation{
			Field:  "Continue on error",
			Output: commandFragment("--continue-on-error"),
			Detail: "keeps processing the queue after failures",
		})
	}
	if options.loop {
		explanations = append(explanations, commandExplanation{
			Field:  "Loop",
			Output: commandFragment("--loop"),
			Detail: "repeats the source-driven queue",
		})
	}
	if value := strings.TrimSpace(options.promptTemplate); value != "" {
		explanations = append(explanations, commandExplanation{
			Field:  "Prompt template",
			Output: commandFragment("--prompt-template", value),
			Detail: "custom prompt template override",
		})
	}
	if value := strings.TrimSpace(options.logDir); value != "" {
		explanations = append(explanations, commandExplanation{
			Field:  "Log directory",
			Output: commandFragment("--log-dir", value),
			Detail: "writes logs under the requested directory",
		})
	}
	if value := strings.TrimSpace(options.doneFile); value != "" {
		explanations = append(explanations, commandExplanation{
			Field:  "Done file",
			Output: commandFragment("--done-file", value),
			Detail: "tracks completed queue items",
		})
	}

	return explanations
}

func describeRuntimeCommandRail(runtime CommandRuntime) []commandExplanation {
	explanations := make([]commandExplanation, 0, 8)
	if agent := normalizeAgent(runtime.Agent); agent != defaultCommandAgent {
		explanations = append(explanations, commandExplanation{
			Field:  "Agent",
			Output: commandFragment("--agent", agent),
			Detail: "selects the execution agent",
		})
	}
	if model := strings.TrimSpace(runtime.Model); model != "" {
		explanations = append(explanations, commandExplanation{
			Field:  "Model",
			Output: commandFragment("--model", model),
			Detail: "pins the requested model",
		})
	}
	if view := normalizeStreamView(runtime.StreamView); view != defaultCommandStreamView {
		explanations = append(explanations, commandExplanation{
			Field:  "Stream view",
			Output: commandFragment("--stream-view", view),
			Detail: "changes stream rendering mode",
		})
	}
	if runtime.WaitBufferSec != nil && *runtime.WaitBufferSec != defaultCommandWaitBuffer {
		explanations = append(explanations, commandExplanation{
			Field:  "Wait buffer sec",
			Output: commandFragment("--wait-buffer-sec", intString(*runtime.WaitBufferSec)),
			Detail: "overrides the session wait buffer",
		})
	}
	if runtime.NoColor {
		explanations = append(explanations, commandExplanation{
			Field:  "No color",
			Output: commandFragment("--no-color"),
			Detail: "forces colorless output",
		})
	}
	if value := strings.TrimSpace(runtime.ClaudeBin); value != "" && value != defaultClaudeBin {
		explanations = append(explanations, commandExplanation{
			Field:  "Claude bin",
			Output: commandFragment("--claude-bin", value),
			Detail: "overrides the Claude binary path",
		})
	}
	if value := strings.TrimSpace(runtime.CodexBin); value != "" && value != defaultCodexBin {
		explanations = append(explanations, commandExplanation{
			Field:  "Codex bin",
			Output: commandFragment("--codex-bin", value),
			Detail: "overrides the Codex binary path",
		})
	}
	if value := strings.TrimSpace(runtime.GeminiBin); value != "" && value != defaultGeminiBin {
		explanations = append(explanations, commandExplanation{
			Field:  "Gemini bin",
			Output: commandFragment("--gemini-bin", value),
			Detail: "overrides the Gemini binary path",
		})
	}
	if value := strings.TrimSpace(runtime.CursorBin); value != "" && value != defaultCursorBin {
		explanations = append(explanations, commandExplanation{
			Field:  "Cursor bin",
			Output: commandFragment("--cursor-bin", value),
			Detail: "overrides the Cursor Agent binary path",
		})
	}
	if value := strings.TrimSpace(runtime.PiBin); value != "" && value != defaultPiBin {
		explanations = append(explanations, commandExplanation{
			Field:  "pi bin",
			Output: commandFragment("--pi-bin", value),
			Detail: "overrides the pi binary path",
		})
	}
	if value := strings.TrimSpace(runtime.GHBin); value != "" && value != defaultGHBin {
		explanations = append(explanations, commandExplanation{
			Field:  "GH bin",
			Output: commandFragment("--gh-bin", value),
			Detail: "overrides the GitHub CLI binary path",
		})
	}

	return explanations
}

func issueSourceExplanation(state IssueCommandState) (string, string) {
	switch normalizeIssueSource(state.Source) {
	case IssueSourceSingle:
		if value := strings.TrimSpace(state.SingleIssue); value != "" {
			return commandFragment("--issue", value), "single issue selection"
		}
	case IssueSourceCSV:
		if items := normalizeOrderedItems(state.Issues); len(items) > 0 {
			return commandFragment("--issues", strings.Join(items, ",")), "explicit issue CSV"
		}
	case IssueSourceAllOpen:
		if value := strings.TrimSpace(state.Label); value != "" {
			return commandFragment("--all-open", "--label", value), "open-issue discovery filtered by label"
		}
		return commandFragment("--all-open"), "open-issue discovery"
	default:
		if value := strings.TrimSpace(state.IssuesFile); value != "" {
			return commandFragment("--issues-file", value), "issues file source"
		}
	}
	return "", ""
}

func fileSourceExplanation(state FileCommandState) (string, string) {
	switch normalizeFileSource(state.Source) {
	case FileSourceAllFiles:
		if value := strings.TrimSpace(state.AllFiles); value != "" {
			return commandFragment("--all-files", value), "directory scan source"
		}
	default:
		if items := normalizeOrderedItems(state.Files); len(items) > 0 {
			return commandFragment("--files", strings.Join(items, ",")), "explicit file list"
		}
	}
	return "", ""
}

func commandFragment(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		filtered = append(filtered, shellQuote(part))
	}
	return strings.Join(filtered, " ")
}

func fallbackCommandFragment(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(not emitted)"
	}
	return value
}
