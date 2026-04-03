package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	defaultWidth  = 120
	defaultHeight = 36
)

type Phase string

const (
	PhaseConfigure Phase = "configure"
	PhaseRun       Phase = "run"
	PhaseSummary   Phase = "summary"
)

type LayoutMode string

const (
	LayoutWide    LayoutMode = "wide"
	LayoutStacked LayoutMode = "stacked"
	LayoutCompact LayoutMode = "compact"
)

type TransitionMsg struct {
	Phase Phase
}

type ConfigureStateMsg struct {
	State CommandState
}

type Options struct {
	RepoRoot      string
	Branch        string
	Workflow      string
	Preset        string
	Agent         string
	NoColor       bool
	RunExecutable string
	RunEnv        []string
	StartRun      RunStarter
	RunTickEvery  time.Duration
	Now           func() time.Time
	CommandOutput func(string, ...string) (string, error)
	LookPath      func(string) (string, error)
	CopyCommand   func(string) error
	Getenv        func(string) string
	ExecProcess   func(*exec.Cmd, tea.ExecCallback) tea.Cmd
	RunAction     ActionRunner
}

type LayoutState struct {
	Width   int
	Height  int
	Mode    LayoutMode
	Warning string
}

type KeyHint struct {
	Key   string
	Label string
}

type pane struct {
	Title string
	Body  string
}

type phaseRenderer interface {
	title() string
	keyHints() []KeyHint
	render(*Model, int, int) string
}

type Model struct {
	options         Options
	phase           Phase
	width           int
	height          int
	layout          LayoutState
	styles          Styles
	now             func() time.Time
	phaseStartedAt  time.Time
	configure       configurePhase
	presets         presetState
	command         CommandState
	defaults        configureDefaults
	queueStatus     string
	validation      DiagnosticReport
	preflight       DiagnosticReport
	runBlocked      string
	commandRail     commandRailState
	help            helpState
	search          searchState
	run             runPhase
	summary         summaryPhase
	lastRunCommand  CommandState
	lastRunDefaults configureDefaults
}

func NewModel(opts Options) Model {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	opts.Workflow = normalizeWorkflow(opts.Workflow)
	if opts.Agent == "" {
		opts.Agent = "claude"
	}

	model := Model{
		options:        opts,
		phase:          PhaseConfigure,
		width:          defaultWidth,
		height:         defaultHeight,
		styles:         NewStyles(opts.NoColor),
		now:            now,
		phaseStartedAt: now(),
		configure:      newConfigurePhase(),
		command:        defaultConfigureCommandState(opts.Workflow, opts.Agent),
		run:            newRunPhase(defaultConfigureCommandState(opts.Workflow, opts.Agent)),
		summary:        newSummaryPhase(opts.Workflow),
	}
	model.layout = classifyLayout(model.width, model.height)
	model.initializePresets()
	model.refreshConfigureState()
	return model
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		m.layout = classifyLayout(m.width, m.height)
	case ConfigureStateMsg:
		m.command = typed.State
		m.refreshConfigureState()
	case CommandCopiedMsg:
		if typed.Err != nil {
			m.commandRail.copyStatus = fmt.Sprintf("Copy failed: %v", typed.Err)
			m.commandRail.copySeverity = SeverityError
			break
		}
		m.commandRail.copyStatus = "Copy: full invocation copied to the terminal clipboard."
		m.commandRail.copySeverity = SeverityInfo
	case TransitionMsg:
		var cmd tea.Cmd
		m, cmd = m.transition(typed.Phase)
		return m, cmd
	case runProcessLineMsg:
		if m.phase == PhaseRun {
			m.run.consumeLine(m.now(), typed.Line)
			return m, waitForRunEvent(m.runEvents())
		}
	case runProcessExitMsg:
		if m.phase == PhaseRun {
			m.run.complete(m.now(), typed)
			if m.run.quitAfterExit {
				return m, tea.Quit
			}
			if !m.run.active() {
				return m.transition(PhaseSummary)
			}
		}
	case runProcessDrainedMsg:
		return m, nil
	case summaryResetMsg:
		if m.phase == PhaseSummary {
			m.summary.consumeResetResult(typed)
		}
	case runTickMsg:
		if m.phase == PhaseRun && m.run.active() && m.options.RunTickEvery > 0 {
			m.run.tick(typed.At)
			return m, tickRun(m.options.RunTickEvery)
		}
	case logAccessMsg:
		if m.phase == PhaseRun {
			m.run.consumeLogAccessResult(typed)
		}
	case tea.KeyMsg:
		if m.help.visible {
			switch {
			case keyIsHelp(typed), keyIs(typed, "esc"), keyIsRune(typed, 'q'), keyIs(typed, "enter"):
				m.help.visible = false
				return m, nil
			case keyIs(typed, "ctrl+c"):
				return m, tea.Quit
			default:
				return m, nil
			}
		}
		if m.search.active {
			if handled := m.handleSearchKey(typed); handled {
				return m, nil
			}
		}
		if keyIsHelp(typed) {
			m.help.visible = true
			m.search.active = false
			return m, nil
		}
		if keyIsSlash(typed) {
			if m.startSearch() {
				return m, nil
			}
		}
		if m.phase == PhaseConfigure {
			if handled := m.configure.handleKey(&m, typed); handled {
				return m, nil
			}
		}
		if m.phase == PhaseRun {
			if handled := m.run.handleKey(typed); handled {
				return m, nil
			}
		}
		if m.phase == PhaseSummary {
			if handled, cmd := m.summary.handleKey(&m, typed); handled {
				return m, cmd
			}
		}
		switch {
		case keyIs(typed, "ctrl+c"):
			if m.phase == PhaseRun && m.run.active() {
				if err := m.run.requestStop(false); err != nil {
					m.run.errorMessage = err.Error()
				}
				return m, waitForRunEvent(m.runEvents())
			}
			return m, tea.Quit
		case keyIsRune(typed, 'q'):
			if m.phase == PhaseRun && m.run.active() {
				if err := m.run.requestStop(true); err != nil {
					m.run.errorMessage = err.Error()
				}
				return m, waitForRunEvent(m.runEvents())
			}
			return m, tea.Quit
		case keyIsRune(typed, 'c'):
			if m.supportsCommandRail() {
				return m, m.copyCurrentCommand()
			}
		case keyIsRune(typed, 'e'):
			if m.supportsCommandRail() {
				m.commandRail.explain = !m.commandRail.explain
				return m, nil
			}
		case keyIsRune(typed, 'r'):
			if m.phase == PhaseConfigure {
				m.refreshConfigureState()
				if !m.canRun() {
					m.runBlocked = "Run blocked until all validation and preflight errors are resolved."
					return m, nil
				}
				m.runBlocked = ""
				return m.transition(PhaseRun)
			}
		case keyIsRune(typed, 'b'):
			if m.phase == PhaseRun && m.run.active() {
				return m, nil
			}
			if m.phase == PhaseRun || m.phase == PhaseSummary {
				return m.transition(PhaseConfigure)
			}
		case keyIsRune(typed, 'p'):
			if m.phase == PhaseRun {
				m.run.paused = !m.run.paused
				if !m.run.paused && len(m.run.pendingStream) > 0 {
					for _, line := range m.run.pendingStream {
						m.run.appendVisibleLine(line)
					}
					m.run.pendingStream = nil
				}
			}
		case keyIsRune(typed, 'o'):
			if m.phase == PhaseRun {
				return m, m.openCurrentLog()
			}
		}
	}

	return m, nil
}

func (m Model) View() string {
	topBar := m.renderTopBar()
	footer := m.renderFooter()
	mainHeight := max(8, m.height-lipgloss.Height(topBar)-lipgloss.Height(footer))
	main := m.renderMain(mainHeight)
	return lipgloss.JoinVertical(lipgloss.Left, topBar, main, footer)
}

func (m Model) Phase() Phase {
	return m.phase
}

func (m Model) Layout() LayoutState {
	return m.layout
}

func (m Model) Snapshot(width, height int) string {
	m.width = width
	m.height = height
	m.layout = classifyLayout(width, height)
	return m.View()
}

func Snapshot(opts Options, width, height int) string {
	model := NewModel(opts)
	return model.Snapshot(width, height)
}

func Transition(phase Phase) tea.Cmd {
	return func() tea.Msg {
		return TransitionMsg{Phase: phase}
	}
}

func SetConfigureState(state CommandState) tea.Cmd {
	return func() tea.Msg {
		return ConfigureStateMsg{State: state}
	}
}

func (m Model) transition(phase Phase) (Model, tea.Cmd) {
	if phase == "" || phase == m.phase {
		return m, nil
	}

	switch phase {
	case PhaseRun:
		return m.startRun(m.command)
	case PhaseSummary:
		m.phase = phase
		m.phaseStartedAt = m.now()
		m.summary = m.run.summary(m.lastRunCommand, m.lastRunDefaults)
		return m, nil
	}

	m.phase = phase
	m.phaseStartedAt = m.now()
	return m, nil
}

func (m Model) currentPhase() phaseRenderer {
	switch m.phase {
	case PhaseRun:
		return m.run
	case PhaseSummary:
		return m.summary
	default:
		return m.configure
	}
}

func (m Model) renderTopBar() string {
	parts := []string{
		m.styles.badge("GHIR TUI"),
		m.styles.focusText(fmt.Sprintf("Phase %s", m.currentPhase().title())),
		fmt.Sprintf("Repo %s", filepath.Base(m.options.RepoRoot)),
		fmt.Sprintf("Branch %s", m.options.Branch),
		fmt.Sprintf("Agent %s", displayAgent(m.activeCommandState().Runtime.Agent)),
		fmt.Sprintf("Elapsed %s", formatElapsed(m.now().Sub(m.phaseStartedAt))),
	}
	if m.layout.Mode != LayoutWide {
		parts = append(parts, "Layout "+strings.ToUpper(string(m.layout.Mode)))
	}
	if m.layout.Mode == LayoutCompact {
		parts = []string{
			m.styles.badge("GHIR TUI"),
			m.styles.focusText(m.currentPhase().title()),
			filepath.Base(m.options.RepoRoot),
			displayAgent(m.activeCommandState().Runtime.Agent),
			formatElapsed(m.now().Sub(m.phaseStartedAt)),
			"compact",
		}
	}

	line := strings.Join(fitSegments(parts, m.styles.separator(), m.width), m.styles.separator())
	return m.styles.topBar.Width(m.width).Render(line)
}

func (m Model) renderFooter() string {
	hints := m.currentPhase().keyHints()
	segments := make([]string, 0, len(hints))
	for _, hint := range hints {
		segments = append(segments, m.styles.keyHint(hint))
	}
	return m.styles.footer.Width(m.width).Render(strings.Join(fitSegments(segments, "  ", m.width), "  "))
}

func (m *Model) refreshConfigureState() {
	m.command.Workflow = normalizeCommandWorkflow(m.command.Workflow)
	if strings.TrimSpace(m.command.Runtime.Agent) == "" {
		m.command.Runtime.Agent = defaultCommandAgent
	}
	if !m.command.Runtime.ModelCustom && strings.TrimSpace(m.command.Runtime.Model) == "" {
		m.command.Runtime.Model = defaultModelForAgent(m.command.Runtime.Agent)
	}
	m.configure.ensureMaps()
	queueStatus, queueDiagnostics := refreshActiveQueue(&m.command, queueResolveOptions{
		RepoRoot:      m.options.RepoRoot,
		CommandOutput: m.options.CommandOutput,
	})
	m.queueStatus = queueStatus
	effective := effectiveCommandState(m.command)
	m.defaults = resolveConfigureDefaults(m.options.RepoRoot, effective)
	m.validation = mergeDiagnosticReports(ValidateCommandState(effective), m.configure.localValidation(), queueDiagnostics)
	m.preflight = RunPreflightChecks(effective, PreflightOptions{
		RepoRoot:      m.options.RepoRoot,
		LookPath:      m.options.LookPath,
		CommandOutput: m.options.CommandOutput,
	})
	m.options.Workflow = m.currentWorkflow()
	m.options.Agent = normalizeAgent(m.command.Runtime.Agent)
	m.styles = NewStyles(m.options.NoColor || m.command.Runtime.NoColor)
}

func (m Model) canRun() bool {
	return !m.validation.HasErrors() && !m.preflight.HasErrors()
}

func (m Model) supportsCommandRail() bool {
	return m.phase == PhaseConfigure || m.phase == PhaseRun
}

func (m Model) runEvents() <-chan tea.Msg {
	if m.run.handle == nil {
		return nil
	}
	return m.run.handle.Events
}

func (m Model) currentWorkflow() string {
	return string(normalizeCommandWorkflow(m.command.Workflow))
}

func (m Model) activeCommandState() CommandState {
	if (m.phase == PhaseRun || m.phase == PhaseSummary) && strings.TrimSpace(string(m.lastRunCommand.Workflow)) != "" {
		return m.lastRunCommand
	}
	return m.command
}

func (m Model) startRun(state CommandState) (Model, tea.Cmd) {
	state = cloneCommandState(state)
	m.phase = PhaseRun
	m.phaseStartedAt = m.now()
	m.lastRunCommand = cloneCommandState(state)
	m.lastRunDefaults = resolveConfigureDefaults(m.options.RepoRoot, effectiveCommandState(state))
	m.run = newRunPhase(state)
	return m, m.run.start(m.now(), m.options, state)
}

func effectiveCommandState(state CommandState) CommandState {
	effective := state
	effective.Workflow = normalizeCommandWorkflow(state.Workflow)

	switch effective.Workflow {
	case WorkflowFiles:
		effective.Issues = IssueCommandState{}
	case WorkflowImprove:
		effective.Issues = IssueCommandState{}
		effective.Files = FileCommandState{}
	default:
		effective.Files = FileCommandState{}
	}

	return effective
}

func cloneCommandState(state CommandState) CommandState {
	state.Issues.Issues = cloneStringSlice(state.Issues.Issues)
	state.Issues.ResolvedQueue = cloneStringSlice(state.Issues.ResolvedQueue)
	state.Issues.StagedQueue = cloneStringSlice(state.Issues.StagedQueue)
	state.Files.Files = cloneStringSlice(state.Files.Files)
	state.Files.ResolvedQueue = cloneStringSlice(state.Files.ResolvedQueue)
	state.Files.StagedQueue = cloneStringSlice(state.Files.StagedQueue)
	if state.Improve.Iterations != nil {
		iterations := *state.Improve.Iterations
		state.Improve.Iterations = &iterations
	}
	if state.Runtime.WaitBufferSec != nil {
		waitBuffer := *state.Runtime.WaitBufferSec
		state.Runtime.WaitBufferSec = &waitBuffer
	}
	return state
}

func cloneStringSlice(values []string) []string {
	if values == nil {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func (m Model) runResetAction(state CommandState, defaults configureDefaults, target string) tea.Cmd {
	scope, args := buildSummaryResetArgs(state, defaults, target)
	executable := strings.TrimSpace(m.options.RunExecutable)
	if executable == "" {
		return func() tea.Msg {
			return summaryResetMsg{Scope: scope, Err: fmt.Errorf("ghir executable unavailable for reset action")}
		}
	}

	runner := m.options.RunAction
	if runner == nil {
		runner = defaultActionRunner
	}

	return func() tea.Msg {
		output, err := runner(RunRequest{
			Executable: executable,
			Args:       args,
			Dir:        m.options.RepoRoot,
			Env:        m.options.RunEnv,
		})
		return summaryResetMsg{Scope: scope, Output: output, Err: err}
	}
}

func mergeDiagnosticReports(reports ...DiagnosticReport) DiagnosticReport {
	var merged DiagnosticReport
	for _, report := range reports {
		for _, item := range report.Items() {
			merged.add(item.Severity, item.Message, item.Hint)
		}
	}
	return merged
}

func (m Model) renderPrimaryLayout(panes []pane, width, height int) string {
	switch m.layout.Mode {
	case LayoutCompact:
		return m.renderCompactLayout(panes, width, height)
	case LayoutStacked:
		return m.renderTwoPaneLayout(panes, width, height)
	default:
		return m.renderWideLayout(panes, width, height)
	}
}

func (m Model) renderWideLayout(panes []pane, width, height int) string {
	usableWidth := max(40, width)
	gap := 1
	columnWidth := max(24, (usableWidth-(gap*(len(panes)-1)))/len(panes))
	columns := make([]string, 0, len(panes))
	for _, current := range panes {
		columns = append(columns, m.styles.panel(current.Title, current.Body, columnWidth, height))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, columns...)
}

func (m Model) renderStackedLayout(panes []pane, width, height int) string {
	if len(panes) == 0 {
		return ""
	}

	panelHeight := max(6, height/len(panes))
	sections := make([]string, 0, len(panes))
	for _, current := range panes {
		sections = append(sections, m.styles.panel(current.Title, current.Body, width, panelHeight))
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m Model) renderMain(mainHeight int) string {
	if m.help.visible {
		return m.renderHelpOverlay(mainHeight)
	}

	searchHeight := 0
	searchPanel := ""
	if m.search.active {
		searchHeight = 6
		searchPanel = m.styles.panel("Search", m.renderSearchPrompt(), m.width, searchHeight)
	}

	contentHeight := max(8, mainHeight-searchHeight)
	main := m.currentPhase().render(&m, m.width, contentHeight)
	if searchPanel == "" {
		return main
	}
	return lipgloss.JoinVertical(lipgloss.Left, searchPanel, main)
}

func (m Model) renderHelpOverlay(height int) string {
	lines := []string{
		"Keyboard help",
		"",
		"Global",
		"  ? toggle help",
		"  Q quit (or stop + quit during a run)",
		"  C copy command rail invocation",
		"  E toggle command rail explanation",
		"",
		fmt.Sprintf("Phase: %s", m.currentPhase().title()),
		fmt.Sprintf("Layout: %s (%d x %d)", strings.ToUpper(string(m.layout.Mode)), m.layout.Width, m.layout.Height),
	}

	switch m.phase {
	case PhaseRun:
		lines = append(lines,
			"",
			"Run phase",
			"  Tab / Shift+Tab switch between queue and details",
			"  J/K move the selected queue item",
			"  / search the run queue",
			"  P pause or resume stream rendering",
			"  O open the selected log path",
			"  Ctrl+C request graceful stop",
		)
	case PhaseSummary:
		lines = append(lines,
			"",
			"Summary phase",
			"  Tab / Shift+Tab switch between failed items and actions",
			"  J/K move within the active pane",
			"  Enter applies the selected action",
			"  / search failed items",
			"  R rerun failed subset",
			"  X reset selected completion",
			"  A reset all completion markers",
			"  B return to configure",
		)
	default:
		lines = append(lines,
			"",
			"Configure phase",
			"  J/K move between setup fields",
			"  Enter edits the active field",
			"  Space toggles booleans",
			"  Left/Right changes choice fields",
			"  L opens preset load",
			"  S opens preset save",
			"  R run when validation is clear",
		)
	}

	lines = append(lines, "", "Close: Esc, Enter, ?, or Q")
	return m.styles.panel("Help", strings.Join(lines, "\n"), m.width, height)
}

func (m Model) renderSearchPrompt() string {
	lines := []string{
		"Scope: " + searchScopeLabel(m.search.scope),
		"Query: " + m.search.query + "_",
	}
	status := strings.TrimSpace(m.search.status)
	if status == "" {
		status = searchPlaceholder(m.search.scope)
	}
	lines = append(lines, status, "Enter keeps the current match. Esc cancels.")
	return strings.Join(lines, "\n")
}

func (m *Model) handleSearchKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyEsc:
		m.search.active = false
		m.search.query = ""
		m.search.status = ""
		return true
	case tea.KeyEnter:
		m.search.active = false
		return true
	case tea.KeyBackspace:
		m.search.query = trimLastRune(m.search.query)
		m.applySearch()
		return true
	case tea.KeyDelete:
		m.search.query = ""
		m.applySearch()
		return true
	case tea.KeySpace:
		m.search.query += " "
		m.applySearch()
		return true
	case tea.KeyRunes:
		m.search.query += string(msg.Runes)
		m.applySearch()
		return true
	default:
		return true
	}
}

func (m *Model) startSearch() bool {
	scope, _ := m.searchItems()
	if scope == "" {
		return false
	}
	m.search.active = true
	m.search.scope = scope
	m.search.query = ""
	m.search.status = searchPlaceholder(scope)
	m.applySearch()
	return true
}

func (m *Model) applySearch() {
	scope, items := m.searchItems()
	if scope == "" {
		m.search.status = "Search is unavailable in the current phase."
		return
	}
	m.search.scope = scope
	if len(items) == 0 {
		m.search.status = "No queue items are available to search."
		return
	}
	query := strings.ToLower(strings.TrimSpace(m.search.query))
	if query == "" {
		m.search.status = searchPlaceholder(scope)
		return
	}
	for index, item := range items {
		if strings.Contains(strings.ToLower(item), query) {
			switch scope {
			case searchScopeRunQueue:
				m.run.selectedIndex = index
			case searchScopeSummaryFailed:
				m.summary.failureFocus = index
			}
			m.search.status = describeSearchResult(m.search.query, index, len(items))
			return
		}
	}
	m.search.status = fmt.Sprintf("No items match %q.", strings.TrimSpace(m.search.query))
}

func (m Model) searchItems() (searchScope, []string) {
	switch m.phase {
	case PhaseRun:
		items := make([]string, 0, len(m.run.queue))
		for _, item := range m.run.queue {
			label := item.Label
			if strings.TrimSpace(item.Title) != "" {
				label += " " + item.Title
			}
			items = append(items, label)
		}
		return searchScopeRunQueue, items
	case PhaseSummary:
		return searchScopeSummaryFailed, append([]string(nil), m.summary.failedItems...)
	case PhaseConfigure:
		return "", nil
	default:
		return "", nil
	}
}

func (m Model) renderTwoPaneLayout(panes []pane, width, height int) string {
	if len(panes) <= 2 {
		return m.renderStackedLayout(panes, width, height)
	}

	top := panes[0]
	bottomTitleParts := make([]string, 0, len(panes)-1)
	bottomBodyParts := make([]string, 0, len(panes)-1)
	for _, current := range panes[1:] {
		bottomTitleParts = append(bottomTitleParts, current.Title)
		bottomBodyParts = append(bottomBodyParts, current.Title, current.Body)
	}

	bottom := pane{
		Title: strings.Join(bottomTitleParts, " / "),
		Body:  strings.Join(bottomBodyParts, "\n\n"),
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.styles.panel(top.Title, top.Body, width, max(8, height/2)),
		m.styles.panel(bottom.Title, bottom.Body, width, max(8, height-(height/2))),
	)
}

func (m Model) renderCompactLayout(panes []pane, width, height int) string {
	warning := pane{
		Title: "Compact Mode",
		Body: strings.Join([]string{
			"warn  Terminal width is under 80 columns.",
			"Critical state remains visible, but panes are merged for narrow terminals.",
		}, "\n"),
	}

	merged := append([]pane{warning}, panes...)
	return m.renderStackedLayout(merged, width, height)
}

func classifyLayout(width, height int) LayoutState {
	if width <= 0 {
		width = defaultWidth
	}
	if height <= 0 {
		height = defaultHeight
	}

	layout := LayoutState{
		Width:  width,
		Height: height,
	}

	switch {
	case width < 80:
		layout.Mode = LayoutCompact
		layout.Warning = "compact"
	case width < 100:
		layout.Mode = LayoutStacked
		layout.Warning = "stacked"
	default:
		layout.Mode = LayoutWide
	}

	return layout
}

func fitSegments(segments []string, separator string, width int) []string {
	available := max(1, width-2)
	fitted := make([]string, 0, len(segments))
	used := 0
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			continue
		}
		next := lipgloss.Width(segment)
		if len(fitted) > 0 {
			next += lipgloss.Width(separator)
		}
		if used+next > available && len(fitted) > 0 {
			break
		}
		fitted = append(fitted, segment)
		used += next
	}
	if len(fitted) == 0 && len(segments) > 0 {
		return segments[:1]
	}
	return fitted
}

func normalizeWorkflow(workflow string) string {
	switch strings.ToLower(strings.TrimSpace(workflow)) {
	case "files":
		return "files"
	case "improve":
		return "improve"
	default:
		return "issues"
	}
}

func displayWorkflow(workflow string) string {
	switch normalizeWorkflow(workflow) {
	case "files":
		return "Files"
	case "improve":
		return "Improve"
	default:
		return "Issues"
	}
}

func displayAgent(agent string) string {
	value := strings.ToLower(strings.TrimSpace(agent))
	switch value {
	case "codex":
		return "Codex"
	case "gemini":
		return "Gemini"
	case "cursor-agent":
		return "Cursor Agent"
	case "pi":
		return "pi"
	case "", "claude":
		return "Claude"
	default:
		return value
	}
}

func formatElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	minutes := int(elapsed / time.Minute)
	seconds := int((elapsed % time.Minute) / time.Second)
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampIndex(index, length int) int {
	if length <= 0 {
		return -1
	}
	if index < 0 {
		return 0
	}
	if index >= length {
		return length - 1
	}
	return index
}
