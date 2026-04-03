package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ghir/streaming"

	tea "github.com/charmbracelet/bubbletea"
)

type runStatus string

const (
	statusPending runStatus = "pending"
	statusQueued  runStatus = "queued"
	statusRunning runStatus = "running"
	statusDone    runStatus = "done"
	statusFailed  runStatus = "failed"
	statusSkipped runStatus = "skipped"
)

type runState string

const (
	runStatePreview   runState = "preview"
	runStateLaunching runState = "launching"
	runStateRunning   runState = "running"
	runStateSucceeded runState = "succeeded"
	runStateFailed    runState = "failed"
	runStateStopped   runState = "stopped"
)

const maxRunStreamLines = 400

var (
	runItemHeaderPattern       = regexp.MustCompile(`^\[(\d+)/(\d+)\]\s+(.+?):\s*(.+)$`)
	runImproveStartPattern     = regexp.MustCompile(`^Starting improve pass (\d+) \(([^)]+)\) with .+\.\.\.$`)
	runLogPattern              = regexp.MustCompile(`^Log:\s+(.+)$`)
	runSuccessPattern          = regexp.MustCompile(`^SUCCESS:\s+(.+?)\s+(committed|completed)`)
	runSkipPattern             = regexp.MustCompile(`^Already completed\s+(.+?), skipping`)
	runDrySkipPattern          = regexp.MustCompile(`^\[DRY RUN\] Already completed\s+(.+?), would skip$`)
	runDryRunPattern           = regexp.MustCompile(`^\[DRY RUN\] Would process\s+(.+)$`)
	runFailurePattern          = regexp.MustCompile(`^FAILED:\s+.+?\s+for\s+(.+?)(?::|$)`)
	runFetchFailurePattern     = regexp.MustCompile(`^FAILED:\s+unable to fetch\s+(.+?):`)
	runImproveDonePattern      = regexp.MustCompile(`^Improvement pass (\d+) \(([^)]+)\)\s+(created new commit\(s\)|committed by runner|produced no changes)`)
	runRetryPattern            = regexp.MustCompile(`^Retrying due to (.+?) \(attempt (\d+)/(\d+)\)\.\.\.$`)
	runSessionLimitPattern     = regexp.MustCompile(`^SESSION LIMIT HIT - waiting until ([^(]+)\s+\((\d+)s\)$`)
	runSessionCountdownPattern = regexp.MustCompile(`^waiting\.\.\.\s+(\d+)\s+minutes remaining$`)
	runSessionRetryPattern     = regexp.MustCompile(`^Retrying\s+.+\s+after session limit reset\.\.\.$`)
	runContinuePattern         = regexp.MustCompile(`^Failed on\s+(.+), continuing due to --continue-on-error$`)
	runStopPattern             = regexp.MustCompile(`^Stopping due to failure on\s+(.+)$`)
	runANSIColorPattern        = regexp.MustCompile(`\x1b\[[0-9;]*m`)
)

type runItem struct {
	Label   string
	Title   string
	Status  runStatus
	LogPath string
}

type runPhase struct {
	workflow          string
	queue             []runItem
	focusArea         runFocusArea
	stream            []string
	pendingStream     []string
	paused            bool
	state             runState
	streamRenderer    streaming.Renderer
	streamNotice      string
	command           string
	startedAt         time.Time
	finishedAt        time.Time
	currentIndex      int
	selectedIndex     int
	currentLogPath    string
	retryStatus       string
	sessionStatus     string
	sessionResetAt    time.Time
	sessionWaiting    bool
	errorMessage      string
	failureContext    string
	stopRequested     bool
	quitAfterExit     bool
	exitCode          *int
	lastLogs          []string
	warningBanners    []string
	logAccessStatus   string
	logAccessSeverity Severity
	handle            *runHandle
}

type runFocusArea string

const (
	runFocusQueue   runFocusArea = "queue"
	runFocusDetails runFocusArea = "details"
)

func newRunPhase(state CommandState) runPhase {
	workflow := normalizeWorkflow(string(normalizeCommandWorkflow(state.Workflow)))
	queue := previewQueue(state)
	return runPhase{
		workflow:      workflow,
		queue:         queue,
		focusArea:     runFocusQueue,
		stream:        previewStream(state),
		state:         runStatePreview,
		command:       BuildCommandString(state),
		currentIndex:  defaultCurrentRunIndex(queue),
		selectedIndex: defaultCurrentRunIndex(queue),
		retryStatus:   "0 / 3",
		sessionStatus: "idle",
	}
}

func (p runPhase) title() string {
	return "Run"
}

func (p runPhase) keyHints() []KeyHint {
	hints := []KeyHint{
		{Key: "Tab", Label: "Next Pane"},
		{Key: "Shift+Tab", Label: "Prev Pane"},
		{Key: "J/K", Label: "Select Item"},
		{Key: "/", Label: "Search Queue"},
		{Key: "?", Label: "Help"},
		{Key: "Q", Label: "Quit"},
		{Key: "P", Label: pauseKeyLabel(p.paused)},
		{Key: "O", Label: "Open Log"},
		{Key: "C", Label: "Copy"},
		{Key: "E", Label: "Explain"},
	}
	if p.active() {
		hints = append(hints,
			KeyHint{Key: "Ctrl+C", Label: "Stop"},
			KeyHint{Key: "Q", Label: "Stop+Quit"},
		)
		return hints
	}
	hints = append(hints,
		KeyHint{Key: "B", Label: "Back"},
		KeyHint{Key: "Q", Label: "Quit"},
	)
	return hints
}

func (p runPhase) render(model *Model, width, height int) string {
	streamTitle := "Stream"
	streamLines := append([]string(nil), p.stream...)
	if p.paused {
		streamTitle = fmt.Sprintf("Stream (paused, %d buffered)", len(p.pendingStream))
		streamLines = append(streamLines, "", "Rendering is paused. Execution continues in the subprocess.")
	}
	streamLines = tailRunLines(streamLines, max(1, height-6))

	rightBody := make([]string, 0, 32)
	if banners := p.renderWarningBanners(model); len(banners) > 0 {
		rightBody = append(rightBody, banners...)
		rightBody = append(rightBody, "")
	}

	rightBody = append(rightBody,
		"Workflow: "+displayWorkflow(p.workflow),
		"Pane focus: "+p.focusAreaLabel(),
		"Agent: "+displayAgent(model.activeCommandState().Runtime.Agent),
		"Model: "+fallbackRunLabel(model.activeCommandState().Runtime.Model, "(default)"),
		"Run state: "+p.renderState(model),
		fmt.Sprintf("Selection: %s", p.selectionLabel()),
		fmt.Sprintf("Selected item: %s", p.selectedItemLabel()),
		fmt.Sprintf("Selected log: %s", fallbackRunLabel(p.selectedLogPath(), "(waiting)")),
		fmt.Sprintf("Current item: %s", p.currentItemLabel()),
		fmt.Sprintf("Current title: %s", p.currentItemTitle()),
		fmt.Sprintf("Progress: %s", p.progressLabel()),
		fmt.Sprintf("Elapsed: %s", formatElapsed(p.elapsed(model.now()))),
		fmt.Sprintf("Log path: %s", fallbackRunLabel(p.currentLogPath, "(waiting)")),
		fmt.Sprintf("Retry count: %s", p.retryStatus),
		fmt.Sprintf("Session wait: %s", p.renderSessionStatus()),
	)
	if p.exitCode != nil {
		rightBody = append(rightBody, fmt.Sprintf("Exit code: %d", *p.exitCode))
	}
	if strings.TrimSpace(p.errorMessage) != "" {
		rightBody = append(rightBody, model.styles.errText("Error: "+p.errorMessage))
	}
	if strings.TrimSpace(p.failureContext) != "" {
		rightBody = append(rightBody,
			"",
			"Failure context",
			model.styles.errText(p.failureContext),
		)
		if strings.TrimSpace(p.currentLogPath) != "" {
			rightBody = append(rightBody, "Log: "+p.currentLogPath)
		}
	}
	if strings.TrimSpace(p.logAccessStatus) != "" {
		rightBody = append(rightBody,
			"",
			"Log access",
			renderRunStatusLine(model, p.logAccessSeverity, p.logAccessStatus),
		)
	}
	rightBody = append(rightBody, "")
	rightBody = append(rightBody, renderCommandRail(model)...)

	panes := []pane{
		{
			Title: "Queue",
			Body:  renderQueue(model, p.queue, p.selectedIndex, p.focusArea),
		},
		{
			Title: streamTitle,
			Body:  strings.Join(streamLines, "\n"),
		},
		{
			Title: "Current Item / Command Rail",
			Body:  strings.Join(rightBody, "\n"),
		},
	}

	return model.renderPrimaryLayout(panes, width, height)
}

func tailRunLines(lines []string, limit int) []string {
	if limit <= 0 || len(lines) <= limit {
		return lines
	}
	return append([]string(nil), lines[len(lines)-limit:]...)
}

func (p *runPhase) handleKey(msg tea.KeyMsg) bool {
	p.clampSelection()

	switch {
	case keyIs(msg, "tab"):
		p.moveFocus(1)
		return true
	case keyIs(msg, "shift+tab"):
		p.moveFocus(-1)
		return true
	}

	if p.focusArea != runFocusQueue {
		return false
	}

	switch {
	case keyIs(msg, "down") || keyIsRune(msg, 'j'):
		p.moveSelection(1)
		return true
	case keyIs(msg, "up") || keyIsRune(msg, 'k'):
		p.moveSelection(-1)
		return true
	default:
		return false
	}
}

func (p runPhase) summary(state CommandState, defaults configureDefaults) summaryPhase {
	succeeded := 0
	failed := 0
	failedItems := make([]string, 0, len(p.queue))
	for _, item := range p.queue {
		switch item.Status {
		case statusDone, statusSkipped:
			succeeded++
		case statusFailed:
			failed++
			failedItems = append(failedItems, item.Label)
		}
	}

	warnings := make([]string, 0, 3)
	if strings.TrimSpace(p.streamNotice) != "" {
		warnings = append(warnings, p.streamNotice)
	}
	warnings = appendUniqueWarnings(warnings, p.warningBanners...)
	if p.state == runStateStopped {
		warnings = append(warnings, "Run was interrupted before the queue completed.")
	}
	if strings.TrimSpace(p.errorMessage) != "" && p.state == runStateFailed {
		warnings = append(warnings, p.errorMessage)
	}
	if strings.TrimSpace(p.failureContext) != "" && p.failureContext != p.errorMessage {
		warnings = append(warnings, p.failureContext)
	}

	lastLogs := append([]string(nil), p.lastLogs...)
	if len(lastLogs) == 0 && strings.TrimSpace(p.currentLogPath) != "" {
		lastLogs = append(lastLogs, p.currentLogPath)
	}

	return summaryPhase{
		workflow:     p.workflow,
		command:      cloneCommandState(state),
		defaults:     defaults,
		stream:       append([]string(nil), p.stream...),
		succeeded:    succeeded,
		failed:       failed,
		warnings:     warnings,
		lastLogs:     lastLogs,
		failedItems:  failedItems,
		failureFocus: clampSummaryFocus(0, failedItems),
		focusArea:    summaryFocusFailed,
		actionFocus:  0,
	}
}

func previewQueue(state CommandState) []runItem {
	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles, WorkflowIssues:
		labels := activeStagedQueue(state)
		queue := make([]runItem, 0, len(labels))
		for index, label := range labels {
			status := statusQueued
			if index == 0 {
				status = statusRunning
			}
			queue = append(queue, runItem{Label: label, Status: status})
		}
		return queue
	case WorkflowImprove:
		iterations := 1
		if state.Improve.Iterations != nil && *state.Improve.Iterations > 0 {
			iterations = *state.Improve.Iterations
		}
		mode := strings.TrimSpace(state.Improve.Mode)
		if normalizeImprovePromptSource(state.Improve.PromptSource) != ImprovePromptSourceMode {
			mode = "custom"
		}
		if mode == "" {
			mode = defaultImproveMode
		}
		queue := make([]runItem, 0, iterations)
		for index := 1; index <= iterations; index++ {
			status := statusQueued
			if index == 1 {
				status = statusRunning
			}
			queue = append(queue, runItem{
				Label:  improveQueueLabel(mode, index),
				Status: status,
			})
		}
		return queue
	}

	return nil
}

func previewStream(state CommandState) []string {
	args := BuildCommandArgs(state)
	lines := []string{
		"Exact subprocess invocation:",
		BuildCommandString(state),
	}
	if len(args) == 0 {
		lines = append(lines, "", "Run output will appear here after launch.")
		return lines
	}
	lines = append(lines, "", "Run output will replace this preview once the subprocess starts.")
	return lines
}

func renderQueue(model *Model, queue []runItem, selected int, focusArea runFocusArea) string {
	if len(queue) == 0 {
		return model.styles.dimText("No staged items selected for this run.")
	}

	selected = clampIndex(selected, len(queue))
	lines := make([]string, 0, len(queue)+2)
	lines = append(lines, "Selection: "+intLabel(selected+1)+" / "+intLabel(len(queue)))
	lines = append(lines, "Focus: "+string(focusArea))
	for index, item := range queue {
		lines = append(lines, renderRunItem(model, item, index == selected, focusArea == runFocusQueue))
	}
	return strings.Join(lines, "\n")
}

func renderRunItem(model *Model, item runItem, selected, active bool) string {
	status := string(item.Status)
	switch item.Status {
	case statusDone:
		status = model.styles.okText(string(item.Status))
	case statusFailed:
		status = model.styles.errText(string(item.Status))
	case statusRunning:
		status = model.styles.focusText(string(item.Status))
	case statusSkipped:
		status = model.styles.warnText(string(item.Status))
	default:
		status = model.styles.dimText(string(item.Status))
	}

	label := item.Label
	if strings.TrimSpace(item.Title) != "" {
		label += " - " + item.Title
	}

	prefix := " "
	if selected {
		prefix = ":"
		if active {
			prefix = ">"
		}
	}
	return prefix + " [" + status + "] " + label
}

func improveQueueLabel(mode string, index int) string {
	return fmt.Sprintf("%s pass %d", strings.TrimSpace(mode), index)
}

func defaultCurrentRunIndex(queue []runItem) int {
	if len(queue) == 0 {
		return -1
	}
	return 0
}

func pauseKeyLabel(paused bool) string {
	if paused {
		return "Resume"
	}
	return "Pause"
}

func (p *runPhase) start(now time.Time, opts Options, state CommandState) tea.Cmd {
	p.command = BuildCommandString(state)
	p.startedAt = now
	p.finishedAt = time.Time{}
	p.exitCode = nil
	p.errorMessage = ""
	p.failureContext = ""
	p.retryStatus = "0 / 3"
	p.sessionStatus = "idle"
	p.sessionResetAt = time.Time{}
	p.sessionWaiting = false
	p.currentLogPath = ""
	p.lastLogs = nil
	p.pendingStream = nil
	p.quitAfterExit = false
	p.stopRequested = false
	p.warningBanners = nil
	p.logAccessStatus = ""
	p.logAccessSeverity = SeverityInfo
	p.currentIndex = defaultCurrentRunIndex(p.queue)
	p.selectedIndex = p.currentIndex
	p.focusArea = runFocusQueue
	p.streamRenderer, p.streamNotice = streaming.NewRenderer(normalizeAgent(state.Runtime.Agent), normalizeStreamView(state.Runtime.StreamView))
	p.stream = nil
	if strings.TrimSpace(p.streamNotice) != "" {
		p.appendVisibleLine("warn  " + p.streamNotice)
	}

	starter := opts.StartRun
	runExecutable := strings.TrimSpace(opts.RunExecutable)
	if starter == nil && runExecutable == "" {
		p.state = runStatePreview
		p.stream = previewStream(state)
		return nil
	}
	if starter == nil {
		starter = defaultRunStarter
	}

	p.state = runStateLaunching
	if p.currentIndex >= 0 && p.currentIndex < len(p.queue) {
		p.queue[p.currentIndex].Status = statusRunning
	}

	handle, err := starter(RunRequest{
		Executable: runExecutable,
		Args:       BuildCommandArgs(state),
		Dir:        opts.RepoRoot,
		Env:        opts.RunEnv,
	})
	if err != nil {
		p.fail(now, err)
		return nil
	}

	p.state = runStateRunning
	p.handle = &handle
	if opts.RunTickEvery > 0 {
		return tea.Batch(waitForRunEvent(handle.Events), tickRun(opts.RunTickEvery))
	}
	return waitForRunEvent(handle.Events)
}

func (p *runPhase) active() bool {
	return p.state == runStateLaunching || p.state == runStateRunning
}

func (p *runPhase) elapsed(now time.Time) time.Duration {
	if p.startedAt.IsZero() {
		return 0
	}
	if !p.finishedAt.IsZero() {
		return p.finishedAt.Sub(p.startedAt)
	}
	return now.Sub(p.startedAt)
}

func (p *runPhase) renderState(model *Model) string {
	label := string(p.state)
	switch p.state {
	case runStateSucceeded:
		return model.styles.okText(label)
	case runStateFailed, runStateStopped:
		return model.styles.errText(label)
	case runStateRunning, runStateLaunching:
		return model.styles.focusText(label)
	default:
		return model.styles.dimText(label)
	}
}

func (p *runPhase) currentItemLabel() string {
	if p.currentIndex < 0 || p.currentIndex >= len(p.queue) {
		return "(waiting)"
	}
	return p.queue[p.currentIndex].Label
}

func (p *runPhase) currentItemTitle() string {
	if p.currentIndex < 0 || p.currentIndex >= len(p.queue) {
		return "(waiting)"
	}
	if strings.TrimSpace(p.queue[p.currentIndex].Title) == "" {
		return "(waiting)"
	}
	return p.queue[p.currentIndex].Title
}

func (p *runPhase) progressLabel() string {
	if len(p.queue) == 0 || p.currentIndex < 0 {
		return fmt.Sprintf("0 / %d", len(p.queue))
	}
	return fmt.Sprintf("%d / %d", p.currentIndex+1, len(p.queue))
}

func (p *runPhase) fail(now time.Time, err error) {
	p.finishedAt = now
	p.state = runStateFailed
	if err != nil {
		p.errorMessage = err.Error()
		p.failureContext = err.Error()
		p.appendVisibleLine("[error] " + err.Error())
	}
	if p.currentIndex >= 0 && p.currentIndex < len(p.queue) {
		p.queue[p.currentIndex].Status = statusFailed
	}
	p.selectedIndex = p.currentIndex
}

func (p *runPhase) requestStop(quitAfterExit bool) error {
	if !p.active() || p.handle == nil || p.stopRequested {
		if quitAfterExit {
			p.quitAfterExit = true
		}
		return nil
	}
	p.stopRequested = true
	p.quitAfterExit = quitAfterExit
	p.sessionStatus = "stopping"
	p.addWarning("Graceful stop requested. Waiting for the subprocess to exit cleanly.")
	p.appendVisibleLine("Stopping subprocess...")
	return p.handle.Stop()
}

func (p *runPhase) consumeLine(now time.Time, line string) {
	clean := stripRunANSI(line)
	if p.streamRenderer == nil {
		p.streamRenderer = &streaming.RawRenderer{}
	}
	for _, rendered := range p.streamRenderer.ConsumeLine(clean) {
		p.appendStreamLine(rendered)
	}
	plain := strings.TrimSpace(clean)
	if plain == "" {
		return
	}

	p.consumeStateLine(now, plain)
}

func (p *runPhase) complete(now time.Time, msg runProcessExitMsg) {
	p.finishedAt = now
	code := msg.ExitCode
	p.exitCode = &code
	if p.streamRenderer != nil {
		for _, rendered := range p.streamRenderer.FinalLines() {
			p.appendStreamLine(rendered)
		}
	}

	if p.stopRequested {
		p.state = runStateStopped
		p.sessionStatus = "stopped"
		p.sessionWaiting = false
		p.sessionResetAt = time.Time{}
		p.markCurrentIfTerminal(statusSkipped)
		return
	}
	if msg.Err != nil && msg.ExitCode == 0 {
		p.errorMessage = msg.Err.Error()
		p.failureContext = msg.Err.Error()
		p.state = runStateFailed
		p.markCurrentIfTerminal(statusFailed)
		return
	}
	if msg.ExitCode != 0 {
		p.state = runStateFailed
		p.sessionStatus = "failed"
		p.sessionWaiting = false
		p.sessionResetAt = time.Time{}
		if strings.TrimSpace(p.errorMessage) == "" && msg.Err != nil {
			p.errorMessage = msg.Err.Error()
		}
		if strings.TrimSpace(p.failureContext) == "" {
			p.failureContext = failureContextForExit(msg)
		}
		p.markCurrentIfTerminal(statusFailed)
		return
	}

	p.state = runStateSucceeded
	p.sessionStatus = "complete"
	p.sessionWaiting = false
	p.sessionResetAt = time.Time{}
	p.markRemainingQueuedAsDone()
}

func (p *runPhase) tick(now time.Time) {
	p.updateSessionCountdown(now)
}

func (p *runPhase) consumeStateLine(now time.Time, line string) {
	if match := runRetryPattern.FindStringSubmatch(line); match != nil {
		reason := strings.TrimSpace(match[1])
		p.retryStatus = fmt.Sprintf("%s / %s", match[2], match[3])
		p.addWarning(fmt.Sprintf("Agent retry in progress (%s/%s) after %s.", match[2], match[3], reason))
		return
	}
	if match := runSessionLimitPattern.FindStringSubmatch(line); match != nil {
		p.sessionWaiting = true
		p.sessionStatus = strings.TrimSpace(line)
		p.sessionResetAt = parseRunResetTime(match[1], now, match[2])
		p.updateSessionCountdown(now)
		p.addWarning("Session limit hit. Waiting for the reported reset window before retrying.")
		return
	}
	if match := runSessionCountdownPattern.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
		if !p.sessionWaiting {
			p.sessionStatus = fmt.Sprintf("waiting (%s minutes remaining)", match[1])
		}
		return
	}
	if strings.Contains(line, "Session limit should be reset") {
		p.sessionWaiting = false
		p.sessionResetAt = time.Time{}
		p.sessionStatus = "resuming"
		return
	}
	if runSessionRetryPattern.MatchString(line) {
		p.sessionStatus = "retrying after reset"
		return
	}
	if match := runLogPattern.FindStringSubmatch(line); match != nil {
		p.currentLogPath = strings.TrimSpace(match[1])
		p.addLogPath(p.currentLogPath)
		if p.currentIndex >= 0 && p.currentIndex < len(p.queue) {
			p.queue[p.currentIndex].LogPath = p.currentLogPath
		}
		return
	}
	if match := runItemHeaderPattern.FindStringSubmatch(line); match != nil {
		index, _ := strconv.Atoi(match[1])
		label := normalizeRunItemLabel(p.workflow, match[3])
		p.startQueueItem(index-1, label, match[4])
		return
	}
	if match := runImproveStartPattern.FindStringSubmatch(line); match != nil {
		index, _ := strconv.Atoi(match[1])
		label := improveQueueLabel(match[2], index)
		p.startQueueItem(index-1, label, "")
		return
	}
	if match := runSuccessPattern.FindStringSubmatch(line); match != nil {
		p.finishQueueItem(normalizeRunItemLabel(p.workflow, match[1]), statusDone, "")
		return
	}
	if match := runSkipPattern.FindStringSubmatch(line); match != nil {
		p.finishQueueItem(normalizeRunItemLabel(p.workflow, match[1]), statusSkipped, "")
		return
	}
	if match := runDrySkipPattern.FindStringSubmatch(line); match != nil {
		p.finishQueueItem(normalizeRunItemLabel(p.workflow, match[1]), statusSkipped, "")
		return
	}
	if match := runDryRunPattern.FindStringSubmatch(line); match != nil {
		p.finishQueueItem(normalizeRunItemLabel(p.workflow, match[1]), statusDone, "")
		return
	}
	if match := runFetchFailurePattern.FindStringSubmatch(line); match != nil {
		p.finishQueueItem(normalizeRunItemLabel(p.workflow, match[1]), statusFailed, line)
		return
	}
	if match := runFailurePattern.FindStringSubmatch(line); match != nil {
		p.finishQueueItem(normalizeRunItemLabel(p.workflow, match[1]), statusFailed, line)
		return
	}
	if match := runContinuePattern.FindStringSubmatch(line); match != nil {
		p.addWarning("Failure recorded for " + strings.TrimSpace(match[1]) + ", continuing because --continue-on-error is enabled.")
		return
	}
	if match := runStopPattern.FindStringSubmatch(line); match != nil {
		p.addWarning("Run is stopping after a failure on " + strings.TrimSpace(match[1]) + ".")
		return
	}
	if match := runImproveDonePattern.FindStringSubmatch(line); match != nil {
		index, _ := strconv.Atoi(match[1])
		p.finishQueueItem(improveQueueLabel(match[2], index), statusDone, "")
		return
	}
	if strings.HasPrefix(line, "error: ") {
		p.errorMessage = strings.TrimPrefix(line, "error: ")
		if p.currentIndex >= 0 {
			p.finishQueueItem(p.queue[p.currentIndex].Label, statusFailed, p.errorMessage)
		}
	}
}

func (p *runPhase) startQueueItem(index int, label, title string) {
	p.markCurrentIfTerminal(statusDone)
	if !p.sessionWaiting && (p.sessionStatus == "resuming" || p.sessionStatus == "retrying after reset") {
		p.sessionStatus = "idle"
	}

	if resolved := p.resolveQueueIndex(index, label); resolved >= 0 {
		p.currentIndex = resolved
		if p.selectedIndex < 0 || p.selectedIndex >= len(p.queue) || p.selectedIndex == p.currentIndex {
			p.selectedIndex = resolved
		}
		p.queue[resolved].Status = statusRunning
		p.currentLogPath = p.queue[resolved].LogPath
		if strings.TrimSpace(title) != "" {
			p.queue[resolved].Title = strings.TrimSpace(title)
		}
		return
	}

	p.currentIndex = -1
	p.currentLogPath = ""
}

func (p *runPhase) finishQueueItem(label string, status runStatus, message string) {
	index := p.resolveQueueIndex(-1, label)
	if index < 0 {
		index = p.currentIndex
	}
	if index < 0 || index >= len(p.queue) {
		if status == statusFailed && strings.TrimSpace(message) != "" {
			p.errorMessage = message
			p.failureContext = message
		}
		return
	}
	p.currentIndex = index
	if p.selectedIndex < 0 || p.selectedIndex == p.currentIndex {
		p.selectedIndex = index
	}
	p.queue[index].Status = status
	if status == statusFailed && strings.TrimSpace(message) != "" {
		p.errorMessage = message
		p.failureContext = message
	}
}

func (p *runPhase) markCurrentIfTerminal(status runStatus) {
	if p.currentIndex < 0 || p.currentIndex >= len(p.queue) {
		return
	}
	if p.queue[p.currentIndex].Status != statusRunning {
		return
	}
	p.queue[p.currentIndex].Status = status
}

func (p *runPhase) moveFocus(delta int) {
	areas := []runFocusArea{runFocusQueue, runFocusDetails}
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

func (p *runPhase) clampSelection() {
	if len(p.queue) == 0 {
		p.selectedIndex = -1
		return
	}
	p.selectedIndex = clampIndex(p.selectedIndex, len(p.queue))
}

func (p *runPhase) moveSelection(delta int) {
	if len(p.queue) == 0 {
		p.selectedIndex = -1
		return
	}
	p.clampSelection()
	p.selectedIndex = (p.selectedIndex + delta + len(p.queue)) % len(p.queue)
}

func (p *runPhase) selectedLogPath() string {
	if p.selectedIndex >= 0 && p.selectedIndex < len(p.queue) && strings.TrimSpace(p.queue[p.selectedIndex].LogPath) != "" {
		return p.queue[p.selectedIndex].LogPath
	}
	return p.currentLogPath
}

func (p *runPhase) selectedItemLabel() string {
	if p.selectedIndex < 0 || p.selectedIndex >= len(p.queue) {
		return "(waiting)"
	}
	return p.queue[p.selectedIndex].Label
}

func (p *runPhase) selectionLabel() string {
	if len(p.queue) == 0 || p.selectedIndex < 0 {
		return fmt.Sprintf("0 / %d", len(p.queue))
	}
	return fmt.Sprintf("%d / %d", p.selectedIndex+1, len(p.queue))
}

func (p runPhase) focusAreaLabel() string {
	switch p.focusArea {
	case runFocusDetails:
		return "Details"
	default:
		return "Queue"
	}
}

func (p *runPhase) markRemainingQueuedAsDone() {
	for index := range p.queue {
		if p.queue[index].Status == statusQueued || p.queue[index].Status == statusRunning {
			p.queue[index].Status = statusDone
		}
	}
}

func (p *runPhase) resolveQueueIndex(index int, label string) int {
	if index >= 0 && index < len(p.queue) {
		return index
	}
	normalized := normalizeRunItemLabel(p.workflow, label)
	for i := range p.queue {
		if normalizeRunItemLabel(p.workflow, p.queue[i].Label) == normalized {
			return i
		}
	}
	return -1
}

func (p *runPhase) addLogPath(logPath string) {
	if strings.TrimSpace(logPath) == "" {
		return
	}
	if len(p.lastLogs) == 0 || p.lastLogs[len(p.lastLogs)-1] != logPath {
		p.lastLogs = append(p.lastLogs, logPath)
	}
}

func (p *runPhase) addWarning(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if len(p.warningBanners) > 0 && p.warningBanners[len(p.warningBanners)-1] == message {
		return
	}
	p.warningBanners = appendCapped(p.warningBanners, message)
}

func (p *runPhase) renderWarningBanners(model *Model) []string {
	if len(p.warningBanners) == 0 {
		return nil
	}

	lines := make([]string, 0, len(p.warningBanners))
	for _, banner := range p.warningBanners {
		lines = append(lines, model.styles.warnText("warn  "+banner))
	}
	return lines
}

func (p *runPhase) renderSessionStatus() string {
	if strings.TrimSpace(p.sessionStatus) == "" {
		return "idle"
	}
	return p.sessionStatus
}

func (p *runPhase) updateSessionCountdown(now time.Time) {
	if !p.sessionWaiting || p.sessionResetAt.IsZero() {
		return
	}

	remaining := p.sessionResetAt.Sub(now)
	if remaining < 0 {
		remaining = 0
	}
	p.sessionStatus = fmt.Sprintf("waiting (%s remaining until %s)", formatCountdown(remaining), p.sessionResetAt.Format("2006-01-02 15:04 UTC"))
}

func (p *runPhase) consumeLogAccessResult(msg logAccessMsg) {
	if msg.Err != nil {
		p.logAccessSeverity = SeverityError
		p.logAccessStatus = "Log open failed: " + msg.Err.Error()
		p.addWarning("Log open failed. Use the printed log path directly.")
		return
	}
	if msg.Fallback {
		p.logAccessSeverity = SeverityWarning
		p.logAccessStatus = "Pager unavailable; use log path directly: " + msg.Path
		return
	}
	p.logAccessSeverity = SeverityInfo
	p.logAccessStatus = "Opened log with pager: " + msg.Path
}

func renderRunStatusLine(model *Model, severity Severity, message string) string {
	switch severity {
	case SeverityError:
		return model.styles.errText(message)
	case SeverityWarning:
		return model.styles.warnText(message)
	default:
		return model.styles.okText(message)
	}
}

func parseRunResetTime(raw string, now time.Time, waitSecondsRaw string) time.Time {
	resetAt, err := time.Parse("2006-01-02 15:04 UTC", strings.TrimSpace(raw))
	if err == nil {
		return resetAt
	}

	waitSeconds, err := strconv.Atoi(waitSecondsRaw)
	if err != nil || waitSeconds <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(waitSeconds) * time.Second)
}

func formatCountdown(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	totalSeconds := int(value.Round(time.Second).Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func failureContextForExit(msg runProcessExitMsg) string {
	if msg.ExitCode != 0 {
		return fmt.Sprintf("subprocess exited with code %d", msg.ExitCode)
	}
	if msg.Err != nil {
		return msg.Err.Error()
	}
	return ""
}

func appendUniqueWarnings(existing []string, warnings ...string) []string {
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" {
			continue
		}
		found := false
		for _, current := range existing {
			if current == warning {
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, warning)
		}
	}
	return existing
}

func (p *runPhase) appendStreamLine(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	if p.paused {
		p.pendingStream = appendCapped(p.pendingStream, line)
		return
	}
	p.appendVisibleLine(line)
}

func (p *runPhase) appendVisibleLine(line string) {
	p.stream = appendCapped(p.stream, line)
}

func appendCapped(lines []string, line string) []string {
	lines = append(lines, line)
	if len(lines) > maxRunStreamLines {
		lines = append([]string(nil), lines[len(lines)-maxRunStreamLines:]...)
	}
	return lines
}

func stripRunANSI(line string) string {
	return runANSIColorPattern.ReplaceAllString(strings.TrimRight(line, "\r"), "")
}

func normalizeRunItemLabel(workflow, label string) string {
	label = strings.TrimSpace(label)
	if normalizeWorkflow(workflow) == "issues" {
		return strings.TrimPrefix(label, "#")
	}
	return label
}

func fallbackRunLabel(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
