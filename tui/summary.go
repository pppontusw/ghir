package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type summaryPhase struct {
	workflow       string
	command        CommandState
	defaults       configureDefaults
	stream         []string
	succeeded      int
	failed         int
	warnings       []string
	lastLogs       []string
	failedItems    []string
	failureFocus   int
	focusArea      summaryFocusArea
	actionFocus    int
	actionStatus   string
	actionSeverity Severity
}

type summaryFocusArea string

const (
	summaryFocusFailed  summaryFocusArea = "failed"
	summaryFocusActions summaryFocusArea = "actions"
)

func newSummaryPhase(workflow string) summaryPhase {
	return summaryPhase{
		workflow:  normalizeWorkflow(workflow),
		focusArea: summaryFocusFailed,
	}
}

func (p summaryPhase) title() string {
	return "Summary"
}

func (p summaryPhase) keyHints() []KeyHint {
	hints := []KeyHint{
		{Key: "Tab", Label: "Next Pane"},
		{Key: "Shift+Tab", Label: "Prev Pane"},
		{Key: "J/K", Label: "Move"},
		{Key: "Enter", Label: "Apply"},
		{Key: "/", Label: "Search Failed"},
		{Key: "?", Label: "Help"},
		{Key: "Q", Label: "Quit"},
		{Key: "R", Label: "Rerun"},
		{Key: "X", Label: "Reset Item"},
		{Key: "A", Label: "Reset All"},
		{Key: "B", Label: "Configure"},
	}
	return hints
}

func (p summaryPhase) render(model *Model, width, height int) string {
	failedItems := renderSummaryFailedItems(model, p.failedItems, p.failureFocus)
	if len(failedItems) == 0 {
		failedItems = []string{model.styles.dimText("No failed items from the completed run.")}
	}
	warnings := renderSummarySection(model, p.warnings, "No warnings recorded.")
	logs := renderSummarySection(model, p.lastLogs, "No log paths recorded.")
	streamLines := renderSummarySection(model, tailRunLines(p.stream, max(1, height-6)), "No stream output captured.")

	outcome := pane{
		Title: "Outcome",
		Body: strings.Join([]string{
			"Workflow: " + displayWorkflow(p.workflow),
			"Pane focus: " + p.focusAreaLabel(),
			model.styles.okText("Succeeded: " + intLabel(p.succeeded)),
			model.styles.errText("Failed: " + intLabel(p.failed)),
			"Done file: " + fallbackRunLabel(p.defaults.DoneFile, "(default)"),
			"",
			"Failed items",
			strings.Join(failedItems, "\n"),
		}, "\n"),
	}

	actionLines := p.renderActionLines()
	if message := strings.TrimSpace(p.actionStatus); message != "" {
		actionLines = append(actionLines, "", renderSummaryStatus(model, p.actionSeverity, message))
	}
	stream := pane{
		Title: "Stream",
		Body:  strings.Join(streamLines, "\n"),
	}

	actions := pane{
		Title: "Next Actions",
		Body: strings.Join([]string{
			strings.Join(actionLines, "\n"),
			"",
			"Warnings",
			strings.Join(warnings, "\n"),
			"",
			"Recent logs",
			strings.Join(logs, "\n"),
		}, "\n"),
	}
	return model.renderPrimaryLayout([]pane{outcome, stream, actions}, width, height)
}

type summaryResetMsg struct {
	Scope  string
	Output string
	Err    error
}

func (p *summaryPhase) handleKey(model *Model, msg tea.KeyMsg) (bool, tea.Cmd) {
	p.failureFocus = clampSummaryFocus(p.failureFocus, p.failedItems)
	p.actionFocus = clampIndex(p.actionFocus, len(summaryActions))

	switch {
	case keyIs(msg, "tab"):
		p.moveFocus(1)
		return true, nil
	case keyIs(msg, "shift+tab"):
		p.moveFocus(-1)
		return true, nil
	case (keyIs(msg, "up") || keyIsRune(msg, 'k')) && p.focusArea == summaryFocusFailed:
		if len(p.failedItems) == 0 {
			return true, nil
		}
		p.failureFocus = (p.failureFocus - 1 + len(p.failedItems)) % len(p.failedItems)
		return true, nil
	case (keyIs(msg, "down") || keyIsRune(msg, 'j')) && p.focusArea == summaryFocusFailed:
		if len(p.failedItems) == 0 {
			return true, nil
		}
		p.failureFocus = (p.failureFocus + 1) % len(p.failedItems)
		return true, nil
	case (keyIs(msg, "up") || keyIsRune(msg, 'k')) && p.focusArea == summaryFocusActions:
		p.actionFocus = (p.actionFocus - 1 + len(summaryActions)) % len(summaryActions)
		return true, nil
	case (keyIs(msg, "down") || keyIsRune(msg, 'j')) && p.focusArea == summaryFocusActions:
		p.actionFocus = (p.actionFocus + 1) % len(summaryActions)
		return true, nil
	case keyIs(msg, "enter") && p.focusArea == summaryFocusActions:
		return p.executeAction(model, summaryActions[p.actionFocus])
	case keyIsRune(msg, 'r'):
		return p.executeAction(model, summaryActionRerun)
	case keyIsRune(msg, 'x'):
		return p.executeAction(model, summaryActionResetSelected)
	case keyIsRune(msg, 'a'):
		return p.executeAction(model, summaryActionResetAll)
	}

	return false, nil
}

type summaryAction string

const (
	summaryActionRerun         summaryAction = "rerun"
	summaryActionResetSelected summaryAction = "reset_selected"
	summaryActionResetAll      summaryAction = "reset_all"
	summaryActionConfigure     summaryAction = "configure"
)

var summaryActions = []summaryAction{
	summaryActionRerun,
	summaryActionResetSelected,
	summaryActionResetAll,
	summaryActionConfigure,
}

func (p *summaryPhase) executeAction(model *Model, action summaryAction) (bool, tea.Cmd) {
	switch action {
	case summaryActionRerun:
		state, ok, reason := p.rerunState()
		if !ok {
			p.actionStatus = reason
			p.actionSeverity = SeverityWarning
			return true, nil
		}
		next, cmd := model.startRun(state)
		*model = next
		model.commandRail.copyStatus = ""
		model.commandRail.copySeverity = SeverityInfo
		return true, cmd
	case summaryActionResetSelected:
		target := p.selectedFailedItem()
		if strings.TrimSpace(target) == "" {
			p.actionStatus = "Reset completion is only available for a selected failed issue or file."
			p.actionSeverity = SeverityWarning
			return true, nil
		}
		return true, model.runResetAction(p.command, p.defaults, target)
	case summaryActionResetAll:
		if !summarySupportsReset(p.command) {
			p.actionStatus = "Reset completion is unavailable for Improve runs."
			p.actionSeverity = SeverityWarning
			return true, nil
		}
		return true, model.runResetAction(p.command, p.defaults, "")
	case summaryActionConfigure:
		next, cmd := model.transition(PhaseConfigure)
		*model = next
		return true, cmd
	default:
		return false, nil
	}
}

func (p *summaryPhase) rerunState() (CommandState, bool, string) {
	if normalizeCommandWorkflow(p.command.Workflow) == WorkflowImprove {
		return CommandState{}, false, "Rerun failed subset is unavailable for Improve runs."
	}
	failed := normalizeOrderedItems(p.failedItems)
	if len(failed) == 0 {
		return CommandState{}, false, "No failed items are available to rerun."
	}

	state := cloneCommandState(p.command)
	replaceActiveStagedQueue(&state, failed)
	return state, true, ""
}

func (p *summaryPhase) selectedFailedItem() string {
	if len(p.failedItems) == 0 {
		return ""
	}
	p.failureFocus = clampSummaryFocus(p.failureFocus, p.failedItems)
	return p.failedItems[p.failureFocus]
}

func (p *summaryPhase) consumeResetResult(msg summaryResetMsg) {
	if msg.Err != nil {
		p.actionSeverity = SeverityError
		if strings.TrimSpace(msg.Output) != "" {
			p.actionStatus = fmt.Sprintf("Reset %s failed: %s (%v)", msg.Scope, msg.Output, msg.Err)
			return
		}
		p.actionStatus = fmt.Sprintf("Reset %s failed: %v", msg.Scope, msg.Err)
		return
	}

	p.actionSeverity = SeverityInfo
	p.actionStatus = fmt.Sprintf("Reset %s completed.", msg.Scope)
	if strings.TrimSpace(msg.Output) != "" {
		p.actionStatus += " " + msg.Output
	}
}

func (p *summaryPhase) moveFocus(delta int) {
	areas := []summaryFocusArea{summaryFocusFailed, summaryFocusActions}
	index := 0
	for i, area := range areas {
		if area == p.focusArea {
			index = i
			break
		}
	}
	index = (index + delta + len(areas)) % len(areas)
	p.focusArea = areas[index]
}

func (p summaryPhase) renderActionLines() []string {
	lines := make([]string, 0, len(summaryActions))
	for index, action := range summaryActions {
		label := summaryActionLabel(action)
		prefix := " "
		if index == p.actionFocus {
			prefix = ":"
			if p.focusArea == summaryFocusActions {
				prefix = ">"
			}
		}
		lines = append(lines, prefix+" "+label)
	}
	return lines
}

func (p summaryPhase) focusAreaLabel() string {
	switch p.focusArea {
	case summaryFocusActions:
		return "Actions"
	default:
		return "Failed items"
	}
}

func summaryActionLabel(action summaryAction) string {
	switch action {
	case summaryActionResetSelected:
		return "X reset selected completion"
	case summaryActionResetAll:
		return "A reset all completion markers"
	case summaryActionConfigure:
		return "B return to configure with preserved state"
	default:
		return "R rerun failed subset"
	}
}

func renderSummaryFailedItems(model *Model, failedItems []string, focus int) []string {
	lines := make([]string, 0, len(failedItems))
	focus = clampSummaryFocus(focus, failedItems)
	for index, item := range failedItems {
		line := item
		if normalizeCommandWorkflow(model.summary.command.Workflow) == WorkflowIssues {
			line = "#" + strings.TrimPrefix(item, "#")
		}
		if index == focus {
			prefix := ": "
			if model.summary.focusArea == summaryFocusFailed {
				prefix = "> "
			}
			line = model.styles.focusText(prefix + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	return lines
}

func renderSummarySection(model *Model, values []string, empty string) []string {
	if len(values) == 0 {
		return []string{model.styles.dimText(empty)}
	}
	return values
}

func renderSummaryStatus(model *Model, severity Severity, message string) string {
	switch severity {
	case SeverityError:
		return model.styles.errText(message)
	case SeverityWarning:
		return model.styles.warnText(message)
	default:
		return model.styles.okText(message)
	}
}

func clampSummaryFocus(focus int, failedItems []string) int {
	if len(failedItems) == 0 {
		return 0
	}
	if focus < 0 {
		return 0
	}
	if focus >= len(failedItems) {
		return len(failedItems) - 1
	}
	return focus
}

func summarySupportsReset(state CommandState) bool {
	return normalizeCommandWorkflow(state.Workflow) != WorkflowImprove
}

func buildSummaryResetArgs(state CommandState, defaults configureDefaults, target string) (string, []string) {
	args := []string{"--reset"}
	scope := "all completion markers"
	if value := strings.TrimSpace(target); value != "" {
		args = append(args, value)
		scope = summaryResetScope(state, value)
	}
	args = append(args, buildSummaryResetWorkflowArgs(state)...)
	if value := strings.TrimSpace(defaults.DoneFile); value != "" {
		args = append(args, "--done-file", value)
	}
	args = append(args, appendRuntimeArgs(nil, state.Runtime)...)
	return scope, args
}

func buildSummaryResetWorkflowArgs(state CommandState) []string {
	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles:
		queue := activeStagedQueue(state)
		if len(queue) == 0 {
			queue = activeResolvedQueue(state)
		}
		if len(queue) > 0 {
			return []string{"--files", strings.Join(queue, ",")}
		}
		if value := strings.TrimSpace(state.Files.AllFiles); value != "" {
			return []string{"--all-files", value}
		}
	case WorkflowIssues:
		queue := activeStagedQueue(state)
		if len(queue) == 1 && normalizeIssueSource(state.Issues.Source) == IssueSourceSingle {
			return []string{"--issue", queue[0]}
		}
		if len(queue) > 0 {
			return []string{"--issues", strings.Join(queue, ",")}
		}
		if value := strings.TrimSpace(state.Issues.SingleIssue); value != "" {
			return []string{"--issue", value}
		}
		if items := normalizeOrderedItems(state.Issues.Issues); len(items) > 0 {
			return []string{"--issues", strings.Join(items, ",")}
		}
		if value := strings.TrimSpace(state.Issues.IssuesFile); value != "" {
			return []string{"--issues-file", value}
		}
		if normalizeIssueSource(state.Issues.Source) == IssueSourceAllOpen {
			args := []string{"--all-open"}
			if value := strings.TrimSpace(state.Issues.Label); value != "" {
				args = append(args, "--label", value)
			}
			return args
		}
	}
	return nil
}

func summaryResetScope(state CommandState, target string) string {
	if normalizeCommandWorkflow(state.Workflow) == WorkflowIssues {
		return "#" + strings.TrimPrefix(target, "#")
	}
	return target
}

func intLabel(v int) string {
	return strconv.Itoa(v)
}
