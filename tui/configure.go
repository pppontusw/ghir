package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ghir/defaults"
)

const (
	fieldPresetLoad           = "preset_load"
	fieldPresetSaveName       = "preset_save_name"
	fieldWorkflow             = "workflow"
	fieldIssueSource          = "issue_source"
	fieldIssueSingle          = "issue_single"
	fieldIssuesCSV            = "issues_csv"
	fieldIssuesFile           = "issues_file"
	fieldIssueLabel           = "issue_label"
	fieldIssueStrategy        = "issue_strategy"
	fieldIssueDryRun          = "issue_dry_run"
	fieldIssueForce           = "issue_force"
	fieldIssueContinue        = "issue_continue_on_error"
	fieldIssueLoop            = "issue_loop"
	fieldIssuePromptTemplate  = "issue_prompt_template"
	fieldIssueLogDir          = "issue_log_dir"
	fieldIssueDoneFile        = "issue_done_file"
	fieldFileSource           = "file_source"
	fieldFileExplicit         = "file_explicit"
	fieldFileAllFiles         = "file_all_files"
	fieldFileStrategy         = "file_strategy"
	fieldFileDryRun           = "file_dry_run"
	fieldFileForce            = "file_force"
	fieldFileContinue         = "file_continue_on_error"
	fieldFileLoop             = "file_loop"
	fieldFilePromptTemplate   = "file_prompt_template"
	fieldFileLogDir           = "file_log_dir"
	fieldFileDoneFile         = "file_done_file"
	fieldImprovePromptSource  = "improve_prompt_source"
	fieldImproveMode          = "improve_mode"
	fieldImprovePrompt        = "improve_prompt"
	fieldImprovePromptFile    = "improve_prompt_file"
	fieldImproveStrategy      = "improve_strategy"
	fieldImproveIterations    = "improve_iterations"
	fieldImproveLoop          = "improve_loop"
	fieldImproveScope         = "improve_scope"
	fieldRuntimeAgent         = "runtime_agent"
	fieldRuntimeModel         = "runtime_model"
	fieldRuntimeModelCustom   = "runtime_model_custom"
	fieldRuntimeStreamView    = "runtime_stream_view"
	fieldRuntimeWaitBufferSec = "runtime_wait_buffer_sec"
	fieldRuntimeNoColor       = "runtime_no_color"
	fieldRuntimeClaudeBin     = "runtime_claude_bin"
	fieldRuntimeCodexBin      = "runtime_codex_bin"
	fieldRuntimeGeminiBin     = "runtime_gemini_bin"
	fieldRuntimeCursorBin     = "runtime_cursor_bin"
	fieldRuntimePiBin         = "runtime_pi_bin"
	fieldRuntimeGHBin         = "runtime_gh_bin"
)

type configureFieldKind string

const (
	fieldChoice configureFieldKind = "choice"
	fieldText   configureFieldKind = "text"
	fieldBool   configureFieldKind = "bool"
	fieldInt    configureFieldKind = "int"
)

type configureOption struct {
	Value string
	Label string
}

type configureField struct {
	ID          string
	Section     string
	Label       string
	Kind        configureFieldKind
	Options     []configureOption
	Value       string
	EditValue   string
	Diagnostics []Diagnostic
}

type configurePhase struct {
	focus         int
	editing       bool
	editValue     string
	drafts        map[string]string
	parseError    map[string]string
	overlay       configureOverlay
	presetFocus   int
	presetInput   string
	choiceFieldID string
	choiceLabel   string
	choiceOptions []configureOption
	choiceFocus   int
}

type configureOverlay string

const (
	configureOverlayNone       configureOverlay = ""
	configureOverlaySavePreset configureOverlay = "save-preset"
	configureOverlayLoadPreset configureOverlay = "load-preset"
	configureOverlayChoice     configureOverlay = "choice"
)

type configureDefaults struct {
	RepoRoot       string
	IssuesFile     string
	PromptTemplate string
	LogDir         string
	DoneFile       string
}

func newConfigurePhase() configurePhase {
	return configurePhase{
		drafts:     make(map[string]string),
		parseError: make(map[string]string),
	}
}

func (p configurePhase) title() string {
	return "Configure"
}

func (p configurePhase) keyHints() []KeyHint {
	if p.overlay != configureOverlayNone {
		switch p.overlay {
		case configureOverlaySavePreset:
			return []KeyHint{
				{Key: "Type", Label: "Preset Name"},
				{Key: "Enter", Label: "Save"},
				{Key: "Esc", Label: "Cancel"},
				{Key: "Q", Label: "Quit"},
			}
		case configureOverlayChoice:
			return []KeyHint{
				{Key: "J/K", Label: "Select"},
				{Key: "Enter", Label: "Apply"},
				{Key: "Esc", Label: "Cancel"},
				{Key: "Q", Label: "Quit"},
			}
		default:
			return []KeyHint{
				{Key: "J/K", Label: "Select Preset"},
				{Key: "Enter", Label: "Load"},
				{Key: "Esc", Label: "Cancel"},
				{Key: "Q", Label: "Quit"},
			}
		}
	}

	if p.editing {
		return []KeyHint{
			{Key: "Enter", Label: "Apply"},
			{Key: "Esc", Label: "Cancel"},
			{Key: "?", Label: "Help"},
			{Key: "R", Label: "Run"},
			{Key: "S", Label: "Save Preset"},
			{Key: "L", Label: "Load Preset"},
			{Key: "Q", Label: "Quit"},
		}
	}

	return []KeyHint{
		{Key: "J/K", Label: "Field"},
		{Key: "Left/Right", Label: "Change"},
		{Key: "Enter", Label: "Edit"},
		{Key: "Space", Label: "Toggle"},
		{Key: "R", Label: "Run"},
		{Key: "S", Label: "Save Preset"},
		{Key: "L", Label: "Load Preset"},
		{Key: "C", Label: "Copy"},
		{Key: "?", Label: "Help"},
		{Key: "Q", Label: "Quit"},
	}
}

func (p configurePhase) render(model *Model, width, height int) string {
	base := model.renderPrimaryLayout([]pane{{
		Title: "Run Setup",
		Body:  renderSetupPane(model),
	}}, width, height)
	if p.overlay == configureOverlayNone {
		return base
	}

	title, body := renderConfigureOverlayPane(model)
	modalWidth := minInt(max(36, width*2/3), max(36, width-4))
	modalHeight := minInt(max(8, lipgloss.Height(body)+3), max(8, height-2))
	modal := model.styles.panel(title, body, modalWidth, modalHeight)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func (p *configurePhase) handleKey(model *Model, msg tea.KeyMsg) bool {
	p.ensureMaps()
	fields := p.fields(model)
	if len(fields) == 0 {
		return false
	}
	p.clampFocus(len(fields))

	if p.overlay != configureOverlayNone {
		return p.handleOverlayKey(model, msg)
	}

	if p.editing {
		return p.handleEditingKey(model, msg, fields[p.focus])
	}

	switch {
	case keyIs(msg, "L", "shift+l"):
		p.openLoadPreset(model)
		return true
	case keyIsRune(msg, 's'):
		p.openSavePreset(model)
		return true
	}

	switch {
	case keyIs(msg, "down") || keyIsRune(msg, 'j'):
		p.focus = (p.focus + 1) % len(fields)
		return true
	case keyIs(msg, "up") || keyIsRune(msg, 'k'):
		p.focus = (p.focus - 1 + len(fields)) % len(fields)
		return true
	case keyIs(msg, "left") || keyIsRune(msg, 'h'):
		return p.adjustFocusedField(model, fields[p.focus], -1)
	case keyIs(msg, "right"):
		return p.adjustFocusedField(model, fields[p.focus], 1)
	case keyIs(msg, "enter"):
		return p.activateFocusedField(model, fields[p.focus])
	}

	if msg.Type == tea.KeySpace {
		return p.toggleFocusedField(model, fields[p.focus])
	}

	return false
}

func (p *configurePhase) handleOverlayKey(model *Model, msg tea.KeyMsg) bool {
	switch p.overlay {
	case configureOverlaySavePreset:
		switch msg.Type {
		case tea.KeyEsc:
			p.closeOverlay()
			return true
		case tea.KeyEnter:
			model.presets.saveName = strings.TrimSpace(p.presetInput)
			model.saveCurrentPreset()
			p.closeOverlay()
			return true
		case tea.KeyBackspace:
			p.presetInput = trimLastRune(p.presetInput)
			return true
		case tea.KeyDelete:
			p.presetInput = ""
			return true
		case tea.KeySpace:
			p.presetInput += " "
			return true
		case tea.KeyRunes:
			p.presetInput += string(msg.Runes)
			return true
		default:
			return true
		}
	case configureOverlayLoadPreset:
		switch {
		case keyIs(msg, "esc"):
			p.closeOverlay()
			return true
		case keyIs(msg, "down") || keyIsRune(msg, 'j'):
			p.movePresetFocus(model, 1)
			return true
		case keyIs(msg, "up") || keyIsRune(msg, 'k'):
			p.movePresetFocus(model, -1)
			return true
		case keyIs(msg, "enter"):
			if len(model.presets.entries) == 0 {
				model.presets.setStatus(SeverityWarning, "Preset load skipped: no saved presets available.")
				p.closeOverlay()
				return true
			}
			p.presetFocus = clampInt(p.presetFocus, 0, len(model.presets.entries)-1)
			model.presets.selection = model.presets.entries[p.presetFocus].Name
			model.loadSelectedPreset()
			p.closeOverlay()
			return true
		default:
			return true
		}
	case configureOverlayChoice:
		switch {
		case keyIs(msg, "esc"):
			p.closeOverlay()
			return true
		case keyIs(msg, "down") || keyIsRune(msg, 'j'):
			p.moveChoiceFocus(1)
			return true
		case keyIs(msg, "up") || keyIsRune(msg, 'k'):
			p.moveChoiceFocus(-1)
			return true
		case keyIs(msg, "enter"):
			if len(p.choiceOptions) == 0 {
				p.closeOverlay()
				return true
			}
			p.choiceFocus = clampInt(p.choiceFocus, 0, len(p.choiceOptions)-1)
			p.applyChoiceValue(model, p.choiceFieldID, p.choiceOptions[p.choiceFocus].Value)
			p.closeOverlay()
			return true
		default:
			return true
		}
	default:
		return false
	}
}

func (p configurePhase) fieldActive() bool {
	return true
}

func (p *configurePhase) handleEditingKey(model *Model, msg tea.KeyMsg, field configureField) bool {
	switch msg.Type {
	case tea.KeyEsc:
		p.editing = false
		p.editValue = ""
		return true
	case tea.KeyEnter:
		p.commitEdit(model, field)
		return true
	case tea.KeyBackspace:
		p.editValue = trimLastRune(p.editValue)
		return true
	case tea.KeyDelete:
		p.editValue = ""
		return true
	case tea.KeySpace:
		p.editValue += " "
		return true
	case tea.KeyRunes:
		p.editValue += string(msg.Runes)
		return true
	default:
		if msg.Type == tea.KeyCtrlC {
			return false
		}
		return true
	}
}

func (p *configurePhase) activateFocusedField(model *Model, field configureField) bool {
	switch field.Kind {
	case fieldBool:
		return p.toggleFocusedField(model, field)
	case fieldChoice:
		p.openChoiceOverlay(field)
		return true
	case fieldText, fieldInt:
		p.editing = true
		p.editValue = field.EditValue
		return true
	default:
		return false
	}
}

func (p *configurePhase) adjustFocusedField(model *Model, field configureField, delta int) bool {
	if field.Kind != fieldChoice {
		return false
	}
	if len(field.Options) == 0 {
		return true
	}

	index := 0
	current := strings.TrimSpace(field.Value)
	for i, option := range field.Options {
		if option.Value == current {
			index = i
			break
		}
	}
	index = (index + delta + len(field.Options)) % len(field.Options)
	p.applyChoiceValue(model, field.ID, field.Options[index].Value)
	return true
}

func (p *configurePhase) toggleFocusedField(model *Model, field configureField) bool {
	if field.Kind != fieldBool {
		return false
	}

	p.applyBoolValue(model, field.ID, !p.boolValue(model, field.ID))
	return true
}

func (p *configurePhase) commitEdit(model *Model, field configureField) {
	value := p.editValue
	p.editing = false
	p.editValue = ""

	switch field.Kind {
	case fieldInt:
		p.applyIntValue(model, field.ID, value)
	default:
		p.applyTextValue(model, field.ID, value)
	}
}

func (p *configurePhase) fields(model *Model) []configureField {
	diagnostics := configureDiagnosticsByField(model)
	workflow := normalizeCommandWorkflow(model.command.Workflow)

	fields := []configureField{
		p.choiceField(fieldWorkflow, "Workflow", "Workflow", workflowOptions(), string(workflow), diagnostics[fieldWorkflow]),
	}

	switch workflow {
	case WorkflowFiles:
		source := normalizeFileSource(model.command.Files.Source)
		fields = append(fields,
			p.choiceField(fieldFileSource, "Source", "Source", fileSourceOptions(), string(source), diagnostics[fieldFileSource]),
		)
		if source == FileSourceAllFiles {
			fields = append(fields,
				p.textField(fieldFileAllFiles, "Source", "Value", model.command.Files.AllFiles, diagnostics[fieldFileAllFiles]),
			)
		} else {
			fields = append(fields,
				p.textField(fieldFileExplicit, "Source", "Value", strings.Join(normalizeOrderedItems(model.command.Files.Files), ","), diagnostics[fieldFileExplicit]),
			)
		}
		fields = append(fields,
			p.choiceField(fieldFileStrategy, "Workflow Options", "Strategy", choiceOptions(ValidStrategies, humanizeChoice), p.fieldValue(model, fieldFileStrategy), diagnostics[fieldFileStrategy]),
			p.boolField(fieldFileDryRun, "Workflow Options", "Dry run", model.command.Files.DryRun, diagnostics[fieldFileDryRun]),
		)
	case WorkflowImprove:
		promptSource := normalizeImprovePromptSource(model.command.Improve.PromptSource)
		fields = append(fields,
			p.choiceField(fieldImprovePromptSource, "Workflow Options", "Prompt source", improvePromptSourceOptions(), p.fieldValue(model, fieldImprovePromptSource), diagnostics[fieldImprovePromptSource]),
		)
		switch promptSource {
		case ImprovePromptSourceInline:
			fields = append(fields,
				p.textField(fieldImprovePrompt, "Workflow Options", "Prompt", model.command.Improve.Prompt, diagnostics[fieldImprovePrompt]),
			)
		case ImprovePromptSourceFile:
			fields = append(fields,
				p.textField(fieldImprovePromptFile, "Workflow Options", "Prompt file", model.command.Improve.PromptFile, diagnostics[fieldImprovePromptFile]),
			)
		default:
			fields = append(fields,
				p.choiceField(fieldImproveMode, "Workflow Options", "Mode", choiceOptions(ValidImproveModes, humanizeChoice), p.fieldValue(model, fieldImproveMode), diagnostics[fieldImproveMode]),
			)
		}
		fields = append(fields,
			p.choiceField(fieldImproveStrategy, "Workflow Options", "Strategy", choiceOptions(ValidStrategies, humanizeChoice), p.fieldValue(model, fieldImproveStrategy), diagnostics[fieldImproveStrategy]),
			p.intField(fieldImproveIterations, "Workflow Options", "Iterations", p.fieldEditValue(model, fieldImproveIterations), diagnostics[fieldImproveIterations]),
			p.boolField(fieldImproveLoop, "Workflow Options", "Loop", model.command.Improve.Loop, diagnostics[fieldImproveLoop]),
			p.textField(fieldImproveScope, "Workflow Options", "Scope", model.command.Improve.Scope, diagnostics[fieldImproveScope]),
		)
	default:
		source := normalizeIssueSource(model.command.Issues.Source)
		fields = append(fields,
			p.choiceField(fieldIssueSource, "Source", "Source", issueSourceOptions(), string(source), diagnostics[fieldIssueSource]),
		)
		switch source {
		case IssueSourceSingle:
			fields = append(fields,
				p.textField(fieldIssueSingle, "Source", "Value", model.command.Issues.SingleIssue, diagnostics[fieldIssueSingle]),
			)
		case IssueSourceCSV:
			fields = append(fields,
				p.textField(fieldIssuesCSV, "Source", "Value", strings.Join(normalizeOrderedItems(model.command.Issues.Issues), ","), diagnostics[fieldIssuesCSV]),
			)
		case IssueSourceAllOpen:
			fields = append(fields,
				p.textField(fieldIssueLabel, "Source", "Value", model.command.Issues.Label, diagnostics[fieldIssueLabel]),
			)
		default:
			fields = append(fields,
				p.textField(fieldIssuesFile, "Source", "Value", model.command.Issues.IssuesFile, diagnostics[fieldIssuesFile]),
			)
		}
		fields = append(fields,
			p.choiceField(fieldIssueStrategy, "Workflow Options", "Strategy", choiceOptions(ValidStrategies, humanizeChoice), p.fieldValue(model, fieldIssueStrategy), diagnostics[fieldIssueStrategy]),
			p.boolField(fieldIssueDryRun, "Workflow Options", "Dry run", model.command.Issues.DryRun, diagnostics[fieldIssueDryRun]),
		)
	}

	fields = append(fields,
		p.choiceField(fieldRuntimeAgent, "Runtime", "Agent", agentOptions(), p.fieldValue(model, fieldRuntimeAgent), diagnostics[fieldRuntimeAgent]),
	)
	fields = append(fields,
		p.choiceField(fieldRuntimeModel, "Runtime", "Model", modelOptions(normalizeAgent(model.command.Runtime.Agent)), selectedModelChoice(model.command.Runtime.Agent, model.command.Runtime.Model, model.command.Runtime.ModelCustom), diagnostics[fieldRuntimeModel]),
	)
	if selectedModelChoice(model.command.Runtime.Agent, model.command.Runtime.Model, model.command.Runtime.ModelCustom) == customModelOptionValue {
		fields = append(fields,
			p.textField(fieldRuntimeModelCustom, "Runtime", "Custom model", model.command.Runtime.Model, diagnostics[fieldRuntimeModelCustom]),
		)
	}

	return fields
}

func (p *configurePhase) choiceField(id, section, label string, options []configureOption, value string, diagnostics []Diagnostic) configureField {
	return configureField{
		ID:          id,
		Section:     section,
		Label:       label,
		Kind:        fieldChoice,
		Options:     options,
		Value:       value,
		EditValue:   value,
		Diagnostics: diagnostics,
	}
}

func (p *configurePhase) textField(id, section, label, value string, diagnostics []Diagnostic) configureField {
	return configureField{
		ID:          id,
		Section:     section,
		Label:       label,
		Kind:        fieldText,
		Value:       value,
		EditValue:   p.fieldEditValueFromRaw(id, value),
		Diagnostics: diagnostics,
	}
}

func (p *configurePhase) boolField(id, section, label string, value bool, diagnostics []Diagnostic) configureField {
	return configureField{
		ID:          id,
		Section:     section,
		Label:       label,
		Kind:        fieldBool,
		Value:       strconv.FormatBool(value),
		Diagnostics: diagnostics,
	}
}

func (p *configurePhase) intField(id, section, label, value string, diagnostics []Diagnostic) configureField {
	return configureField{
		ID:          id,
		Section:     section,
		Label:       label,
		Kind:        fieldInt,
		Value:       value,
		EditValue:   p.fieldEditValueFromRaw(id, value),
		Diagnostics: diagnostics,
	}
}

func (p *configurePhase) fieldEditValue(model *Model, id string) string {
	if draft, ok := p.drafts[id]; ok {
		return draft
	}

	switch id {
	case fieldImproveIterations:
		if model.command.Improve.Iterations != nil {
			return strconv.Itoa(*model.command.Improve.Iterations)
		}
		return strconv.Itoa(1)
	case fieldRuntimeWaitBufferSec:
		if model.command.Runtime.WaitBufferSec != nil {
			return strconv.Itoa(*model.command.Runtime.WaitBufferSec)
		}
		return strconv.Itoa(defaultCommandWaitBuffer)
	default:
		return p.fieldValue(model, id)
	}
}

func (p *configurePhase) fieldEditValueFromRaw(id, raw string) string {
	if draft, ok := p.drafts[id]; ok {
		return draft
	}
	return raw
}

func (p *configurePhase) fieldValue(model *Model, id string) string {
	switch id {
	case fieldPresetLoad:
		return model.presets.selection
	case fieldWorkflow:
		return string(normalizeCommandWorkflow(model.command.Workflow))
	case fieldIssueSource:
		return string(normalizeIssueSource(model.command.Issues.Source))
	case fieldFileSource:
		return string(normalizeFileSource(model.command.Files.Source))
	case fieldImprovePromptSource:
		return string(normalizeImprovePromptSource(model.command.Improve.PromptSource))
	case fieldImproveMode:
		return defaultString(model.command.Improve.Mode, defaultImproveMode)
	case fieldIssueStrategy:
		return defaultString(model.command.Issues.Strategy, DefaultStrategy)
	case fieldFileStrategy:
		return defaultString(model.command.Files.Strategy, DefaultStrategy)
	case fieldImproveStrategy:
		return defaultString(model.command.Improve.Strategy, defaultImproveStrategy)
	case fieldRuntimeAgent:
		return normalizeAgent(model.command.Runtime.Agent)
	case fieldRuntimeModel:
		return selectedModelChoice(model.command.Runtime.Agent, model.command.Runtime.Model, model.command.Runtime.ModelCustom)
	case fieldRuntimeStreamView:
		return normalizeStreamView(model.command.Runtime.StreamView)
	case fieldImproveIterations:
		return p.fieldEditValue(model, id)
	case fieldRuntimeWaitBufferSec:
		return p.fieldEditValue(model, id)
	default:
		return ""
	}
}

func (p *configurePhase) boolValue(model *Model, id string) bool {
	switch id {
	case fieldIssueDryRun:
		return model.command.Issues.DryRun
	case fieldIssueForce:
		return model.command.Issues.Force
	case fieldIssueContinue:
		return model.command.Issues.ContinueOnError
	case fieldIssueLoop:
		return model.command.Issues.Loop
	case fieldFileDryRun:
		return model.command.Files.DryRun
	case fieldFileForce:
		return model.command.Files.Force
	case fieldFileContinue:
		return model.command.Files.ContinueOnError
	case fieldFileLoop:
		return model.command.Files.Loop
	case fieldImproveLoop:
		return model.command.Improve.Loop
	case fieldRuntimeNoColor:
		return model.command.Runtime.NoColor
	default:
		return false
	}
}

func (p *configurePhase) applyChoiceValue(model *Model, id, value string) {
	p.clearFieldValidation(id)

	switch id {
	case fieldWorkflow:
		model.command.Workflow = Workflow(value)
		p.seedWorkflowDefaults(&model.command)
	case fieldIssueSource:
		model.command.Issues.Source = IssueSource(value)
		resetIssueQueueState(&model.command.Issues)
		if normalizeIssueSource(model.command.Issues.Source) == IssueSourceAllOpen && strings.TrimSpace(model.command.Issues.Label) == "" {
			model.command.Issues.Label = "ghir"
		}
	case fieldFileSource:
		model.command.Files.Source = FileSource(value)
		resetFileQueueState(&model.command.Files)
		if normalizeFileSource(model.command.Files.Source) == FileSourceAllFiles && strings.TrimSpace(model.command.Files.AllFiles) == "" {
			model.command.Files.AllFiles = "tasks"
		}
	case fieldImprovePromptSource:
		model.command.Improve.PromptSource = ImprovePromptSource(value)
	case fieldImproveMode:
		model.command.Improve.Mode = value
	case fieldIssueStrategy:
		model.command.Issues.Strategy = value
	case fieldFileStrategy:
		model.command.Files.Strategy = value
	case fieldImproveStrategy:
		model.command.Improve.Strategy = value
	case fieldRuntimeAgent:
		model.command.Runtime.Agent = value
		model.command.Runtime.ModelCustom = false
		model.command.Runtime.Model = defaultModelForAgent(value)
	case fieldRuntimeStreamView:
		model.command.Runtime.StreamView = value
	case fieldRuntimeModel:
		if value == customModelOptionValue {
			model.command.Runtime.ModelCustom = true
			if isBuiltInModelForAgent(model.command.Runtime.Agent, model.command.Runtime.Model) {
				model.command.Runtime.Model = ""
			}
		} else {
			model.command.Runtime.ModelCustom = false
			model.command.Runtime.Model = value
		}
	}

	model.refreshConfigureState()
}

func (p *configurePhase) applyBoolValue(model *Model, id string, value bool) {
	p.clearFieldValidation(id)

	switch id {
	case fieldIssueDryRun:
		model.command.Issues.DryRun = value
	case fieldIssueForce:
		model.command.Issues.Force = value
	case fieldIssueContinue:
		model.command.Issues.ContinueOnError = value
	case fieldIssueLoop:
		model.command.Issues.Loop = value
	case fieldFileDryRun:
		model.command.Files.DryRun = value
	case fieldFileForce:
		model.command.Files.Force = value
	case fieldFileContinue:
		model.command.Files.ContinueOnError = value
	case fieldFileLoop:
		model.command.Files.Loop = value
	case fieldImproveLoop:
		model.command.Improve.Loop = value
	case fieldRuntimeNoColor:
		model.command.Runtime.NoColor = value
		model.styles = NewStyles(value || model.options.NoColor)
	}

	model.refreshConfigureState()
}

func (p *configurePhase) applyTextValue(model *Model, id, raw string) {
	p.clearFieldValidation(id)
	value := strings.TrimSpace(raw)

	switch id {
	case fieldIssueSingle:
		model.command.Issues.SingleIssue = value
		resetIssueQueueState(&model.command.Issues)
	case fieldIssuesCSV:
		model.command.Issues.Issues = splitCommaSeparated(raw)
		resetIssueQueueState(&model.command.Issues)
	case fieldIssuesFile:
		model.command.Issues.IssuesFile = value
		resetIssueQueueState(&model.command.Issues)
	case fieldIssueLabel:
		model.command.Issues.Label = value
		resetIssueQueueState(&model.command.Issues)
	case fieldIssuePromptTemplate:
		model.command.Issues.PromptTemplate = value
	case fieldIssueLogDir:
		model.command.Issues.LogDir = value
	case fieldIssueDoneFile:
		model.command.Issues.DoneFile = value
	case fieldFileExplicit:
		model.command.Files.Files = splitCommaSeparated(raw)
		resetFileQueueState(&model.command.Files)
	case fieldFileAllFiles:
		model.command.Files.AllFiles = value
		resetFileQueueState(&model.command.Files)
	case fieldFilePromptTemplate:
		model.command.Files.PromptTemplate = value
	case fieldFileLogDir:
		model.command.Files.LogDir = value
	case fieldFileDoneFile:
		model.command.Files.DoneFile = value
	case fieldImprovePrompt:
		model.command.Improve.Prompt = value
	case fieldImprovePromptFile:
		model.command.Improve.PromptFile = value
	case fieldImproveScope:
		model.command.Improve.Scope = value
	case fieldRuntimeModel:
		model.command.Runtime.Model = value
	case fieldRuntimeModelCustom:
		model.command.Runtime.ModelCustom = true
		model.command.Runtime.Model = value
	case fieldRuntimeClaudeBin:
		model.command.Runtime.ClaudeBin = value
	case fieldRuntimeCodexBin:
		model.command.Runtime.CodexBin = value
	case fieldRuntimeGeminiBin:
		model.command.Runtime.GeminiBin = value
	case fieldRuntimeCursorBin:
		model.command.Runtime.CursorBin = value
	case fieldRuntimePiBin:
		model.command.Runtime.PiBin = value
	case fieldRuntimeGHBin:
		model.command.Runtime.GHBin = value
	}

	model.refreshConfigureState()
}

func (p *configurePhase) applyIntValue(model *Model, id, raw string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		p.clearFieldValidation(id)
		switch id {
		case fieldImproveIterations:
			model.command.Improve.Iterations = nil
		case fieldRuntimeWaitBufferSec:
			model.command.Runtime.WaitBufferSec = nil
		}
		model.refreshConfigureState()
		return
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		p.drafts[id] = value
		switch id {
		case fieldImproveIterations:
			p.parseError[id] = "--iterations must be a non-negative integer"
		case fieldRuntimeWaitBufferSec:
			p.parseError[id] = "--wait-buffer-sec must be a non-negative integer"
		}
		model.refreshConfigureState()
		return
	}

	p.clearFieldValidation(id)
	switch id {
	case fieldImproveIterations:
		model.command.Improve.Iterations = &parsed
	case fieldRuntimeWaitBufferSec:
		model.command.Runtime.WaitBufferSec = &parsed
	}
	model.refreshConfigureState()
}

func (p *configurePhase) localValidation() DiagnosticReport {
	var report DiagnosticReport
	for _, id := range orderedParseErrorFields() {
		if message := strings.TrimSpace(p.parseError[id]); message != "" {
			report.add(SeverityError, message, "Fix the field value or clear it to restore the default.")
		}
	}
	return report
}

func (p *configurePhase) seedWorkflowDefaults(state *CommandState) {
	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles:
		if normalizeFileSource(state.Files.Source) == FileSourceExplicit && len(normalizeOrderedItems(state.Files.Files)) == 0 && strings.TrimSpace(state.Files.AllFiles) == "" {
			state.Files.Source = FileSourceAllFiles
			state.Files.AllFiles = "tasks"
		}
		if strings.TrimSpace(state.Files.Strategy) == "" {
			state.Files.Strategy = DefaultStrategy
		}
	case WorkflowImprove:
		state.Improve.PromptSource = normalizeImprovePromptSource(state.Improve.PromptSource)
		if strings.TrimSpace(state.Improve.Mode) == "" {
			state.Improve.Mode = defaultImproveMode
		}
		if strings.TrimSpace(state.Improve.Strategy) == "" {
			state.Improve.Strategy = defaultImproveStrategy
		}
		if state.Improve.Iterations == nil {
			iterations := 1
			state.Improve.Iterations = &iterations
		}
	default:
		if strings.TrimSpace(state.Issues.IssuesFile) == "" && strings.TrimSpace(state.Issues.SingleIssue) == "" && len(normalizeOrderedItems(state.Issues.Issues)) == 0 && strings.TrimSpace(state.Issues.Label) == "" {
			state.Issues.Source = IssueSourceFile
		}
		if strings.TrimSpace(state.Issues.Strategy) == "" {
			state.Issues.Strategy = DefaultStrategy
		}
	}
}

func (p *configurePhase) clearFieldValidation(id string) {
	delete(p.drafts, id)
	delete(p.parseError, id)
}

func (p *configurePhase) ensureMaps() {
	if p.drafts == nil {
		p.drafts = make(map[string]string)
	}
	if p.parseError == nil {
		p.parseError = make(map[string]string)
	}
}

func (p *configurePhase) clampFocus(length int) {
	if length <= 0 {
		p.focus = 0
		return
	}
	if p.focus < 0 {
		p.focus = 0
	}
	if p.focus >= length {
		p.focus = length - 1
	}
}

func (p *configurePhase) focusedField(model *Model) configureField {
	fields := p.fields(model)
	if len(fields) == 0 {
		return configureField{}
	}
	p.clampFocus(len(fields))
	return fields[p.focus]
}

func buildCommandPreview(state CommandState) string {
	return BuildCommandString(state)
}

func presetLabel(preset string) string {
	if strings.TrimSpace(preset) == "" {
		return "(none)"
	}
	return preset
}

func defaultConfigureCommandState(workflow, agent string) CommandState {
	state := CommandState{
		Workflow: Workflow(normalizeWorkflow(workflow)),
		Runtime: CommandRuntime{
			Agent: normalizeAgent(agent),
			Model: defaultModelForAgent(agent),
		},
	}

	switch normalizeWorkflow(workflow) {
	case "files":
		state.Workflow = WorkflowFiles
		state.Files = FileCommandState{
			Source:   FileSourceAllFiles,
			Strategy: DefaultStrategy,
			AllFiles: "tasks",
		}
	case "improve":
		state.Workflow = WorkflowImprove
		iterations := 1
		state.Improve = ImproveCommandState{
			PromptSource: defaultImprovePromptSource,
			Mode:         defaultImproveMode,
			Iterations:   &iterations,
			Strategy:     defaultImproveStrategy,
		}
	default:
		state.Workflow = WorkflowIssues
		state.Issues = IssueCommandState{
			Source:   IssueSourceFile,
			Strategy: DefaultStrategy,
		}
	}

	return state
}

func resolveConfigureDefaults(repoRoot string, state CommandState) configureDefaults {
	logDir := strings.TrimSpace(state.Issues.LogDir)
	if normalizeCommandWorkflow(state.Workflow) == WorkflowFiles {
		logDir = strings.TrimSpace(state.Files.LogDir)
	}
	if logDir == "" {
		logDir = resolveConfigurePath(repoRoot, defaults.LogDir)
	} else {
		logDir = resolveConfigurePath(repoRoot, logDir)
	}

	doneFile := strings.TrimSpace(state.Issues.DoneFile)
	if normalizeCommandWorkflow(state.Workflow) == WorkflowFiles {
		doneFile = strings.TrimSpace(state.Files.DoneFile)
	}
	if doneFile == "" {
		doneFile = filepath.Join(logDir, defaults.DoneFileName)
	} else {
		doneFile = resolveConfigurePath(repoRoot, doneFile)
	}

	issuesFile := strings.TrimSpace(state.Issues.IssuesFile)
	if issuesFile == "" {
		issuesFile = resolveConfigurePath(repoRoot, defaults.IssuesFile)
	} else {
		issuesFile = resolveConfigurePath(repoRoot, issuesFile)
	}

	promptTemplate := strings.TrimSpace(state.Issues.PromptTemplate)
	if normalizeCommandWorkflow(state.Workflow) == WorkflowFiles {
		promptTemplate = strings.TrimSpace(state.Files.PromptTemplate)
	}
	if promptTemplate != "" {
		promptTemplate = resolveConfigurePath(repoRoot, promptTemplate)
	} else {
		candidate := resolveConfigurePath(repoRoot, defaults.PromptTemplate)
		if _, err := os.Stat(candidate); err == nil {
			promptTemplate = candidate
		} else {
			promptTemplate = "(none)"
		}
	}

	return configureDefaults{
		RepoRoot:       repoRoot,
		IssuesFile:     issuesFile,
		PromptTemplate: promptTemplate,
		LogDir:         logDir,
		DoneFile:       doneFile,
	}
}

func resolveConfigurePath(repoRoot, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if repoRoot == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(repoRoot, value)
}

func renderSetupPane(model *Model) string {
	fields := model.configure.fields(model)
	model.configure.clampFocus(len(fields))

	lines := []string{
		renderPresetSummary(model),
	}
	if status := strings.TrimSpace(renderPresetStatus(model)); status != "" && strings.TrimSpace(model.presets.status) != "" {
		lines = append(lines, status)
	}
	lines = append(lines, "")
	lines = append(lines, renderConfigureFields(model, fields)...)
	lines = append(lines,
		"",
		"Queue",
		renderQueueSummary(model),
		"",
		"Command",
		buildCommandPreview(model.command),
		"",
		"Status",
	)
	lines = append(lines, renderSetupDiagnostics(model)...)

	return strings.Join(lines, "\n")
}

func renderPresetSummary(model *Model) string {
	active := presetLabel(model.presets.active)
	if active == "(none)" && strings.TrimSpace(model.presets.loadedFrom) != "" {
		active = model.presets.loadedFrom
	}
	return fmt.Sprintf("Preset: %s  Saved: %d  [L load] [S save]", active, model.presets.count())
}

func renderQueueSummary(model *Model) string {
	if strings.TrimSpace(model.queueStatus) != "" {
		return model.queueStatus
	}
	queue := activeStagedQueue(model.command)
	if len(queue) == 0 {
		return "(empty)"
	}
	if len(queue) > 5 {
		return fmt.Sprintf("%d items: %s", len(queue), strings.Join(append(queue[:5], "..."), ", "))
	}
	return fmt.Sprintf("%d items: %s", len(queue), strings.Join(queue, ", "))
}

func renderPresetStatus(model *Model) string {
	if strings.TrimSpace(model.presets.status) == "" {
		return ""
	}
	switch model.presets.severity {
	case SeverityError:
		return model.styles.errText("Preset status: " + model.presets.status)
	case SeverityWarning:
		return model.styles.warnText("Preset status: " + model.presets.status)
	default:
		return model.styles.dimText("Preset status: " + model.presets.status)
	}
}

func renderConfigureFields(model *Model, fields []configureField) []string {
	lines := make([]string, 0, len(fields)*3)
	currentSection := ""

	for index, field := range fields {
		if field.Section != currentSection {
			if currentSection != "" {
				lines = append(lines, "")
			}
			lines = append(lines, field.Section)
			currentSection = field.Section
		}

		prefix := " "
		if index == model.configure.focus {
			prefix = ":"
			if model.configure.fieldActive() {
				prefix = ">"
			}
			if model.configure.editing && model.configure.fieldActive() {
				prefix = "*"
			}
		}

		displayValue := formatConfigureFieldValue(model, field)
		status := ""
		if severity, ok := highestSeverity(field.Diagnostics); ok {
			status = " " + renderInlineSeverityLabel(model.styles, severity)
		}
		lines = append(lines, fmt.Sprintf("%s %-18s %s%s", prefix, field.Label+":", displayValue, status))

		for _, diagnostic := range field.Diagnostics {
			lines = append(lines, "  "+renderInlineDiagnostic(model.styles, diagnostic))
		}
	}

	return lines
}

func renderSetupDiagnostics(model *Model) []string {
	report := mergeDiagnosticReports(model.validation, model.preflight)
	items := report.Items()
	if len(items) == 0 {
		return []string{model.styles.okText("Ready to run.")}
	}

	lines := make([]string, 0, minInt(len(items), 4)+1)
	limit := minInt(len(items), 3)
	for _, item := range items[:limit] {
		lines = append(lines, renderInlineDiagnostic(model.styles, item))
	}
	if len(items) > limit {
		lines = append(lines, model.styles.dimText(fmt.Sprintf("%d more checks hidden.", len(items)-limit)))
	}
	return lines
}

func renderConfigureOverlay(model *Model) string {
	switch model.configure.overlay {
	case configureOverlaySavePreset:
		return strings.Join([]string{
			"Save preset",
			"Enter saves. Esc cancels.",
			"Name: " + model.configure.presetInput + "_",
		}, "\n")
	case configureOverlayLoadPreset:
		lines := []string{
			"Load preset",
			"J/K selects. Enter loads. Esc cancels.",
		}
		if len(model.presets.entries) == 0 {
			lines = append(lines, "(no saved presets)")
			return strings.Join(lines, "\n")
		}
		for index, preset := range model.presets.entries {
			prefix := " "
			if index == clampInt(model.configure.presetFocus, 0, len(model.presets.entries)-1) {
				prefix = ">"
			}
			label := preset.Name
			if preset.Name == model.presets.active {
				label += " [active]"
			}
			lines = append(lines, prefix+" "+label)
		}
		return strings.Join(lines, "\n")
	case configureOverlayChoice:
		lines := []string{
			fieldOverlayTitle(model.configure.choiceLabel),
			"J/K selects. Enter applies. Esc cancels.",
		}
		if len(model.configure.choiceOptions) == 0 {
			lines = append(lines, "(no options)")
			return strings.Join(lines, "\n")
		}
		focus := clampInt(model.configure.choiceFocus, 0, len(model.configure.choiceOptions)-1)
		for index, option := range model.configure.choiceOptions {
			prefix := " "
			if index == focus {
				prefix = ">"
			}
			lines = append(lines, prefix+" "+option.Label)
		}
		return strings.Join(lines, "\n")
	default:
		return ""
	}
}

func renderConfigureOverlayPane(model *Model) (string, string) {
	switch model.configure.overlay {
	case configureOverlaySavePreset:
		return "Save Preset", renderConfigureOverlay(model)
	case configureOverlayLoadPreset:
		return "Load Preset", renderConfigureOverlay(model)
	case configureOverlayChoice:
		return "Select Option", renderConfigureOverlay(model)
	default:
		return "", ""
	}
}

func modelOptions(agent string) []configureOption {
	options := make([]configureOption, 0, len(builtInModelsForAgent(agent))+1)
	for _, model := range builtInModelsForAgent(agent) {
		options = append(options, configureOption{
			Value: model.Value,
			Label: model.Label,
		})
	}
	options = append(options, configureOption{
		Value: customModelOptionValue,
		Label: "Custom...",
	})
	return options
}

func (p *configurePhase) openSavePreset(model *Model) {
	p.overlay = configureOverlaySavePreset
	p.presetInput = strings.TrimSpace(model.presets.saveName)
	if p.presetInput == "" {
		p.presetInput = strings.TrimSpace(model.presets.active)
	}
}

func (p *configurePhase) openLoadPreset(model *Model) {
	p.overlay = configureOverlayLoadPreset
	p.presetFocus = 0
	selection := strings.TrimSpace(model.presets.selection)
	for index, preset := range model.presets.entries {
		if preset.Name == selection {
			p.presetFocus = index
			break
		}
	}
}

func (p *configurePhase) openChoiceOverlay(field configureField) {
	p.overlay = configureOverlayChoice
	p.choiceFieldID = field.ID
	p.choiceLabel = field.Label
	p.choiceOptions = append([]configureOption(nil), field.Options...)
	p.choiceFocus = 0
	current := strings.TrimSpace(field.Value)
	for index, option := range p.choiceOptions {
		if option.Value == current {
			p.choiceFocus = index
			break
		}
	}
}

func (p *configurePhase) movePresetFocus(model *Model, delta int) {
	if len(model.presets.entries) == 0 {
		p.presetFocus = 0
		return
	}
	p.presetFocus = (p.presetFocus + delta + len(model.presets.entries)) % len(model.presets.entries)
}

func (p *configurePhase) moveChoiceFocus(delta int) {
	if len(p.choiceOptions) == 0 {
		p.choiceFocus = 0
		return
	}
	p.choiceFocus = (p.choiceFocus + delta + len(p.choiceOptions)) % len(p.choiceOptions)
}

func (p *configurePhase) closeOverlay() {
	p.overlay = configureOverlayNone
	p.presetInput = ""
	p.presetFocus = 0
	p.choiceFieldID = ""
	p.choiceLabel = ""
	p.choiceOptions = nil
	p.choiceFocus = 0
}

func fieldOverlayTitle(label string) string {
	if strings.TrimSpace(label) == "" {
		return "Select option"
	}
	return "Select " + strings.ToLower(strings.TrimSpace(label))
}

func clampInt(value, lower, upper int) int {
	if value < lower {
		return lower
	}
	if value > upper {
		return upper
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func renderInlineDiagnostic(styles Styles, diagnostic Diagnostic) string {
	message := diagnostic.Message
	if strings.TrimSpace(diagnostic.Hint) != "" {
		message += " Hint: " + diagnostic.Hint
	}
	switch diagnostic.Severity {
	case SeverityError:
		return styles.errText("error " + message)
	case SeverityWarning:
		return styles.warnText("warn  " + message)
	default:
		return styles.dimText("info  " + message)
	}
}

func renderInlineSeverityLabel(styles Styles, severity Severity) string {
	switch severity {
	case SeverityError:
		return styles.errText("[error]")
	case SeverityWarning:
		return styles.warnText("[warn]")
	default:
		return styles.dimText("[info]")
	}
}

func boolLabel(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func configureDiagnosticsByField(model *Model) map[string][]Diagnostic {
	grouped := make(map[string][]Diagnostic)
	appendDiagnostics := func(report DiagnosticReport) {
		for _, diagnostic := range report.Items() {
			for _, fieldID := range diagnosticFieldsForMessage(model.command, diagnostic.Message) {
				grouped[fieldID] = append(grouped[fieldID], diagnostic)
			}
		}
	}

	appendDiagnostics(model.validation)
	appendDiagnostics(model.preflight)
	return grouped
}

func diagnosticFieldsForMessage(state CommandState, message string) []string {
	switch {
	case strings.Contains(message, "--files/--all-files cannot be combined"):
		return []string{fieldWorkflow}
	case strings.Contains(message, "--issue must be numeric"):
		return []string{fieldIssueSingle}
	case strings.Contains(message, "--prompt-source must be one of"):
		return []string{fieldImprovePromptSource}
	case strings.Contains(message, "--mode must be one of"):
		return []string{fieldImproveMode}
	case strings.Contains(message, "--prompt requires a value"):
		return []string{fieldImprovePrompt}
	case strings.Contains(message, "--prompt-file requires a value"):
		return []string{fieldImprovePromptFile}
	case strings.Contains(message, "--strategy must be one of"):
		switch normalizeCommandWorkflow(state.Workflow) {
		case WorkflowFiles:
			return []string{fieldFileStrategy}
		case WorkflowImprove:
			return []string{fieldImproveStrategy}
		default:
			return []string{fieldIssueStrategy}
		}
	case strings.Contains(message, "--iterations"):
		return []string{fieldImproveIterations}
	case strings.Contains(message, "--agent must be one of"):
		return []string{fieldRuntimeAgent}
	case strings.Contains(message, "--stream-view must be one of"):
		return []string{fieldRuntimeStreamView}
	case strings.Contains(message, "--wait-buffer-sec"):
		return []string{fieldRuntimeWaitBufferSec}
	case strings.Contains(message, "--loop requires either --all-open or --all-files"):
		if normalizeCommandWorkflow(state.Workflow) == WorkflowFiles {
			return []string{fieldFileLoop}
		}
		return []string{fieldIssueLoop}
	case strings.Contains(message, "--loop is only supported with --strategy"):
		if normalizeCommandWorkflow(state.Workflow) == WorkflowFiles {
			return []string{fieldFileLoop, fieldFileStrategy}
		}
		return []string{fieldIssueLoop, fieldIssueStrategy}
	case strings.Contains(message, "staged issue queue is empty"):
		return []string{fieldIssueSource}
	case strings.Contains(message, "staged file queue is empty"):
		return []string{fieldFileSource}
	case strings.Contains(message, "issue file not found:"), strings.Contains(message, "read issues file:"), strings.Contains(message, "invalid issue id at"), strings.Contains(message, "no issue ids found in"):
		return []string{fieldIssuesFile}
	case strings.Contains(message, "fetch open issues:"), strings.Contains(message, "no open issues found"):
		return []string{fieldIssueSource, fieldIssueLabel}
	case strings.Contains(message, "read directory:"), strings.Contains(message, "no .md files found in"):
		return []string{fieldFileAllFiles}
	case strings.Contains(message, "file not found:"), strings.Contains(message, "inspect file "):
		return []string{fieldFileExplicit}
	case strings.Contains(message, "missing required binary 'gh'"):
		return []string{fieldRuntimeGHBin}
	case strings.Contains(message, "missing required binary 'claude'"):
		return []string{fieldRuntimeClaudeBin, fieldRuntimeAgent}
	case strings.Contains(message, "missing required binary 'codex'"):
		return []string{fieldRuntimeCodexBin, fieldRuntimeAgent}
	case strings.Contains(message, "missing required binary 'gemini'"):
		return []string{fieldRuntimeGeminiBin, fieldRuntimeAgent}
	case strings.Contains(message, "missing required binary 'cursor-agent'"):
		return []string{fieldRuntimeCursorBin, fieldRuntimeAgent}
	case strings.Contains(message, "missing required binary 'pi'"):
		return []string{fieldRuntimePiBin, fieldRuntimeAgent}
	case strings.Contains(message, "must run inside a git repository"):
		return []string{fieldWorkflow}
	case strings.Contains(message, "Working tree is clean"), strings.Contains(message, "uncommitted changes detected"), strings.Contains(message, "Working tree cleanliness check is skipped"):
		return []string{fieldWorkflow}
	default:
		return nil
	}
}

func formatConfigureFieldValue(model *Model, field configureField) string {
	raw := field.Value
	if model.configure.editing && field.ID == model.configure.focusedField(model).ID {
		raw = model.configure.editValue
	}

	switch field.Kind {
	case fieldBool:
		return boolLabel(raw == "true")
	case fieldChoice:
		for _, option := range field.Options {
			if option.Value == raw {
				return option.Label
			}
		}
	case fieldInt:
		if strings.TrimSpace(raw) == "" {
			return configureEmptyLabel(field.ID)
		}
		if model.configure.editing && field.ID == model.configure.focusedField(model).ID {
			return raw + "_"
		}
		return raw
	case fieldText:
		if model.configure.editing && field.ID == model.configure.focusedField(model).ID {
			return raw + "_"
		}
		if strings.TrimSpace(raw) == "" {
			return configureEmptyLabel(field.ID)
		}
		return raw
	}

	if strings.TrimSpace(raw) == "" {
		return configureEmptyLabel(field.ID)
	}
	return raw
}

func configureEmptyLabel(fieldID string) string {
	switch fieldID {
	case fieldPresetSaveName:
		return "(preset name)"
	case fieldIssueSingle:
		return "(issue number)"
	case fieldIssuesCSV, fieldFileExplicit:
		return "(comma-separated)"
	case fieldIssueLabel:
		return "(optional)"
	case fieldImprovePrompt:
		return "(inline prompt)"
	case fieldImprovePromptFile:
		return "(path)"
	case fieldImproveScope:
		return "(repo root)"
	case fieldRuntimeModel:
		return "(default model)"
	case fieldRuntimeClaudeBin:
		return defaultClaudeBin
	case fieldRuntimeCodexBin:
		return defaultCodexBin
	case fieldRuntimeGeminiBin:
		return defaultGeminiBin
	case fieldRuntimeCursorBin:
		return defaultCursorBin
	case fieldRuntimePiBin:
		return defaultPiBin
	case fieldRuntimeGHBin:
		return defaultGHBin
	case fieldImproveIterations:
		return "1"
	case fieldRuntimeWaitBufferSec:
		return intString(defaultCommandWaitBuffer)
	default:
		return "(default)"
	}
}

func highestSeverity(diagnostics []Diagnostic) (Severity, bool) {
	if len(diagnostics) == 0 {
		return "", false
	}

	severity := SeverityInfo
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == SeverityError {
			return SeverityError, true
		}
		if diagnostic.Severity == SeverityWarning {
			severity = SeverityWarning
		}
	}
	return severity, true
}

func workflowOptions() []configureOption {
	return []configureOption{
		{Value: string(WorkflowIssues), Label: "Issues"},
		{Value: string(WorkflowFiles), Label: "Files"},
		{Value: string(WorkflowImprove), Label: "Improve"},
	}
}

func issueSourceOptions() []configureOption {
	return []configureOption{
		{Value: string(IssueSourceSingle), Label: "Single issue"},
		{Value: string(IssueSourceCSV), Label: "CSV issues"},
		{Value: string(IssueSourceFile), Label: "Issues file"},
		{Value: string(IssueSourceAllOpen), Label: "All open"},
	}
}

func fileSourceOptions() []configureOption {
	return []configureOption{
		{Value: string(FileSourceExplicit), Label: "Explicit list"},
		{Value: string(FileSourceAllFiles), Label: "Directory scan"},
	}
}

func improvePromptSourceOptions() []configureOption {
	return []configureOption{
		{Value: string(ImprovePromptSourceMode), Label: "Built-in mode"},
		{Value: string(ImprovePromptSourceInline), Label: "Inline prompt"},
		{Value: string(ImprovePromptSourceFile), Label: "Prompt file"},
	}
}

func agentOptions() []configureOption {
	return []configureOption{
		{Value: "claude", Label: displayAgent("claude")},
		{Value: "codex", Label: displayAgent("codex")},
		{Value: "gemini", Label: displayAgent("gemini")},
		{Value: "cursor-agent", Label: displayAgent("cursor-agent")},
		{Value: "pi", Label: displayAgent("pi")},
	}
}

func choiceOptions(values []string, label func(string) string) []configureOption {
	options := make([]configureOption, 0, len(values))
	for _, value := range values {
		options = append(options, configureOption{Value: value, Label: label(value)})
	}
	return options
}

func humanizeChoice(value string) string {
	parts := strings.Split(strings.ReplaceAll(value, "-", " "), " ")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func splitCommaSeparated(value string) []string {
	items := strings.Split(value, ",")
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		result = append(result, item)
	}
	return result
}

func trimLastRune(value string) string {
	if value == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(value)
	return value[:len(value)-size]
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func orderedParseErrorFields() []string {
	return []string{
		fieldImproveIterations,
		fieldRuntimeWaitBufferSec,
	}
}

func resetIssueQueueState(state *IssueCommandState) {
	state.ResolvedQueue = nil
	state.StagedQueue = nil
}

func resetFileQueueState(state *FileCommandState) {
	state.ResolvedQueue = nil
	state.StagedQueue = nil
}
