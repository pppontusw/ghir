package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	presetSchemaVersion = 1
	presetRelativePath  = ".ticket-runner/tui-presets.json"
	presetDirectoryName = ".ticket-runner"
	presetFileName      = "tui-presets.json"
)

type Preset struct {
	Name  string
	State CommandState
}

type presetCatalog struct {
	Path    string
	Presets []Preset
}

type presetState struct {
	path       string
	entries    []Preset
	active     string
	selection  string
	saveName   string
	status     string
	severity   Severity
	loadedFrom string
}

type presetSaveResult struct {
	Path          string
	RecoveredPath string
}

type invalidPresetFileError struct {
	Path   string
	Reason string
}

func (e invalidPresetFileError) Error() string {
	return fmt.Sprintf("invalid preset file %s: %s", e.Path, e.Reason)
}

type presetDocument struct {
	Version int            `json:"version"`
	Presets []presetRecord `json:"presets"`
}

type presetRecord struct {
	Name     string       `json:"name"`
	Workflow string       `json:"workflow"`
	Fields   presetFields `json:"fields"`
}

type presetFields struct {
	Source          string   `json:"source,omitempty"`
	SingleIssue     string   `json:"single_issue,omitempty"`
	Issues          []string `json:"issues,omitempty"`
	IssuesFile      string   `json:"issues_file,omitempty"`
	Label           string   `json:"label,omitempty"`
	Files           []string `json:"files,omitempty"`
	AllFiles        string   `json:"all_files,omitempty"`
	DryRun          bool     `json:"dry_run,omitempty"`
	Force           bool     `json:"force,omitempty"`
	ContinueOnError bool     `json:"continue_on_error,omitempty"`
	Loop            bool     `json:"loop,omitempty"`
	PromptTemplate  string   `json:"prompt_template,omitempty"`
	LogDir          string   `json:"log_dir,omitempty"`
	DoneFile        string   `json:"done_file,omitempty"`
	PromptSource    string   `json:"prompt_source,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	Prompt          string   `json:"prompt,omitempty"`
	PromptFile      string   `json:"prompt_file,omitempty"`
	Strategy        string   `json:"strategy,omitempty"`
	Iterations      *int     `json:"iterations,omitempty"`
	Scope           string   `json:"scope,omitempty"`
	Agent           string   `json:"agent,omitempty"`
	Model           string   `json:"model,omitempty"`
	StreamView      string   `json:"stream_view,omitempty"`
	WaitBufferSec   *int     `json:"wait_buffer_sec,omitempty"`
	NoColor         bool     `json:"no_color,omitempty"`
	ClaudeBin       string   `json:"claude_bin,omitempty"`
	CodexBin        string   `json:"codex_bin,omitempty"`
	GeminiBin       string   `json:"gemini_bin,omitempty"`
	CursorBin       string   `json:"cursor_bin,omitempty"`
	PiBin           string   `json:"pi_bin,omitempty"`
	GHBin           string   `json:"gh_bin,omitempty"`
}

func newPresetState(repoRoot, requested string) presetState {
	requested = strings.TrimSpace(requested)
	return presetState{
		path:     presetsFilePath(repoRoot),
		saveName: requested,
	}
}

func presetsFilePath(repoRoot string) string {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, presetDirectoryName, presetFileName)
	}
	return legacyPresetsFilePath(repoRoot)
}

func legacyPresetsFilePath(repoRoot string) string {
	return resolveConfigurePath(repoRoot, presetRelativePath)
}

func loadPresetCatalog(repoRoot string) (presetCatalog, error) {
	path := presetsFilePath(repoRoot)
	doc, err := readPresetDocument(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			legacyPath := legacyPresetsFilePath(repoRoot)
			if legacyPath != path {
				doc, legacyErr := readPresetDocument(legacyPath)
				if legacyErr == nil {
					return presetCatalogFromDocument(legacyPath, doc)
				}
				if !errors.Is(legacyErr, os.ErrNotExist) {
					return presetCatalog{Path: legacyPath}, legacyErr
				}
			}
			return presetCatalog{Path: path}, nil
		}
		return presetCatalog{Path: path}, err
	}

	return presetCatalogFromDocument(path, doc)
}

func presetCatalogFromDocument(path string, doc presetDocument) (presetCatalog, error) {
	presets := make([]Preset, 0, len(doc.Presets))
	seen := make(map[string]struct{}, len(doc.Presets))
	for index, record := range doc.Presets {
		name := strings.TrimSpace(record.Name)
		if name == "" {
			return presetCatalog{Path: path}, invalidPresetFileError{
				Path:   path,
				Reason: fmt.Sprintf("preset %d has an empty name", index+1),
			}
		}
		if _, exists := seen[name]; exists {
			return presetCatalog{Path: path}, invalidPresetFileError{
				Path:   path,
				Reason: fmt.Sprintf("duplicate preset name %q", name),
			}
		}

		preset, presetErr := presetFromRecord(record)
		if presetErr != nil {
			return presetCatalog{Path: path}, invalidPresetFileError{
				Path:   path,
				Reason: fmt.Sprintf("preset %q: %v", name, presetErr),
			}
		}
		presets = append(presets, preset)
		seen[name] = struct{}{}
	}

	sortByName(presets, func(preset Preset) string {
		return preset.Name
	})

	return presetCatalog{
		Path:    path,
		Presets: presets,
	}, nil
}

func saveNamedPreset(repoRoot, name string, state CommandState) (presetSaveResult, error) {
	name = strings.TrimSpace(name)
	path := presetsFilePath(repoRoot)
	result := presetSaveResult{Path: path}
	if name == "" {
		return result, fmt.Errorf("preset name is required")
	}

	doc := presetDocument{Version: presetSchemaVersion}
	existing, err := readPresetDocument(path)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
		case isInvalidPresetFileError(err):
			recoveredPath, moveErr := moveInvalidPresetFile(path)
			if moveErr != nil {
				return result, moveErr
			}
			result.RecoveredPath = recoveredPath
		default:
			return result, err
		}
	} else {
		doc = existing
	}

	record := recordFromPreset(Preset{
		Name:  name,
		State: effectiveCommandState(cloneCommandState(state)),
	})

	replaced := false
	for index := range doc.Presets {
		if doc.Presets[index].Name == name {
			doc.Presets[index] = record
			replaced = true
			break
		}
	}
	if !replaced {
		doc.Presets = append(doc.Presets, record)
	}

	sortByName(doc.Presets, func(record presetRecord) string {
		return record.Name
	})

	if err := writePresetDocument(path, doc); err != nil {
		return result, err
	}

	return result, nil
}

func readPresetDocument(path string) (presetDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return presetDocument{}, err
	}

	var doc presetDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return presetDocument{}, invalidPresetFileError{
			Path:   path,
			Reason: fmt.Sprintf("parse JSON: %v", err),
		}
	}
	if decoder.More() {
		return presetDocument{}, invalidPresetFileError{
			Path:   path,
			Reason: "unexpected trailing JSON content",
		}
	}
	if doc.Version != presetSchemaVersion {
		return presetDocument{}, invalidPresetFileError{
			Path:   path,
			Reason: fmt.Sprintf("unsupported version %d", doc.Version),
		}
	}
	if doc.Presets == nil {
		doc.Presets = []presetRecord{}
	}
	return doc, nil
}

func sortByName[T any](items []T, name func(T) string) {
	sort.Slice(items, func(i, j int) bool {
		return name(items[i]) < name(items[j])
	})
}

func writePresetDocument(path string, doc presetDocument) error {
	doc.Version = presetSchemaVersion
	if doc.Presets == nil {
		doc.Presets = []presetRecord{}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create preset directory: %w", err)
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preset file: %w", err)
	}
	data = append(data, '\n')

	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return fmt.Errorf("write preset file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace preset file: %w", err)
	}

	return nil
}

func moveInvalidPresetFile(path string) (string, error) {
	candidate := path + ".invalid"
	if _, err := os.Stat(candidate); err == nil {
		for index := 1; ; index++ {
			next := candidate + "." + strconv.Itoa(index)
			if _, nextErr := os.Stat(next); os.IsNotExist(nextErr) {
				candidate = next
				break
			} else if nextErr != nil {
				return "", fmt.Errorf("inspect preset recovery target: %w", nextErr)
			}
		}
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect preset recovery target: %w", err)
	}

	if err := os.Rename(path, candidate); err != nil {
		return "", fmt.Errorf("move invalid preset file aside: %w", err)
	}
	return candidate, nil
}

func presetFromRecord(record presetRecord) (Preset, error) {
	workflow, err := parsePresetWorkflow(record.Workflow)
	if err != nil {
		return Preset{}, err
	}

	fields := record.Fields
	agent, err := parsePresetAgent(fields.Agent)
	if err != nil {
		return Preset{}, err
	}

	state := defaultConfigureCommandState(string(workflow), agent)
	state.Workflow = workflow
	state.Runtime.Agent = agent
	state.Runtime.Model = strings.TrimSpace(fields.Model)
	state.Runtime.StreamView, err = parsePresetStreamView(fields.StreamView)
	if err != nil {
		return Preset{}, err
	}
	state.Runtime.NoColor = fields.NoColor
	state.Runtime.ClaudeBin = strings.TrimSpace(fields.ClaudeBin)
	state.Runtime.CodexBin = strings.TrimSpace(fields.CodexBin)
	state.Runtime.GeminiBin = strings.TrimSpace(fields.GeminiBin)
	state.Runtime.CursorBin = strings.TrimSpace(fields.CursorBin)
	state.Runtime.PiBin = strings.TrimSpace(fields.PiBin)
	state.Runtime.GHBin = strings.TrimSpace(fields.GHBin)
	if fields.WaitBufferSec != nil {
		if *fields.WaitBufferSec < 0 {
			return Preset{}, fmt.Errorf("--wait_buffer_sec must be a non-negative integer")
		}
		waitBuffer := *fields.WaitBufferSec
		state.Runtime.WaitBufferSec = &waitBuffer
	}

	switch workflow {
	case WorkflowFiles:
		source, err := parsePresetFileSource(fields.Source)
		if err != nil {
			return Preset{}, err
		}
		strategy, err := parsePresetStrategy(fields.Strategy)
		if err != nil {
			return Preset{}, err
		}
		state.Files.Source = source
		state.Files.Strategy = strategy
		state.Files.Files = normalizeOrderedItems(fields.Files)
		state.Files.AllFiles = strings.TrimSpace(fields.AllFiles)
		state.Files.DryRun = fields.DryRun
		state.Files.Force = fields.Force
		state.Files.ContinueOnError = fields.ContinueOnError
		state.Files.Loop = fields.Loop
		state.Files.PromptTemplate = strings.TrimSpace(fields.PromptTemplate)
		state.Files.LogDir = strings.TrimSpace(fields.LogDir)
		state.Files.DoneFile = strings.TrimSpace(fields.DoneFile)
	case WorkflowImprove:
		mode, err := parsePresetImproveMode(fields.Mode)
		if err != nil {
			return Preset{}, err
		}
		strategy, err := parsePresetStrategy(fields.Strategy)
		if err != nil {
			return Preset{}, err
		}
		state.Improve.PromptSource = normalizeImprovePromptSource(ImprovePromptSource(fields.PromptSource))
		state.Improve.Mode = mode
		state.Improve.Prompt = strings.TrimSpace(fields.Prompt)
		state.Improve.PromptFile = strings.TrimSpace(fields.PromptFile)
		state.Improve.Strategy = strategy
		state.Improve.Scope = strings.TrimSpace(fields.Scope)
		state.Improve.Loop = fields.Loop
		if fields.Iterations != nil {
			if *fields.Iterations < 0 {
				return Preset{}, fmt.Errorf("--iterations must be a non-negative integer")
			}
			iterations := *fields.Iterations
			state.Improve.Iterations = &iterations
		}
	default:
		source, err := parsePresetIssueSource(fields.Source)
		if err != nil {
			return Preset{}, err
		}
		strategy, err := parsePresetStrategy(fields.Strategy)
		if err != nil {
			return Preset{}, err
		}
		state.Issues.Source = source
		state.Issues.Strategy = strategy
		state.Issues.SingleIssue = strings.TrimSpace(fields.SingleIssue)
		state.Issues.Issues = normalizeOrderedItems(fields.Issues)
		state.Issues.IssuesFile = strings.TrimSpace(fields.IssuesFile)
		state.Issues.Label = strings.TrimSpace(fields.Label)
		state.Issues.DryRun = fields.DryRun
		state.Issues.Force = fields.Force
		state.Issues.ContinueOnError = fields.ContinueOnError
		state.Issues.Loop = fields.Loop
		state.Issues.PromptTemplate = strings.TrimSpace(fields.PromptTemplate)
		state.Issues.LogDir = strings.TrimSpace(fields.LogDir)
		state.Issues.DoneFile = strings.TrimSpace(fields.DoneFile)
		if issue := strings.TrimSpace(state.Issues.SingleIssue); issue != "" && !issueNumberPattern.MatchString(issue) {
			return Preset{}, fmt.Errorf("--single_issue must be numeric")
		}
	}

	return Preset{
		Name:  strings.TrimSpace(record.Name),
		State: state,
	}, nil
}

func recordFromPreset(preset Preset) presetRecord {
	state := effectiveCommandState(cloneCommandState(preset.State))
	record := presetRecord{
		Name:     strings.TrimSpace(preset.Name),
		Workflow: string(normalizeCommandWorkflow(state.Workflow)),
		Fields: presetFields{
			Agent:      normalizeAgent(state.Runtime.Agent),
			Model:      strings.TrimSpace(state.Runtime.Model),
			StreamView: normalizeStreamView(state.Runtime.StreamView),
			NoColor:    state.Runtime.NoColor,
			ClaudeBin:  strings.TrimSpace(state.Runtime.ClaudeBin),
			CodexBin:   strings.TrimSpace(state.Runtime.CodexBin),
			GeminiBin:  strings.TrimSpace(state.Runtime.GeminiBin),
			CursorBin:  strings.TrimSpace(state.Runtime.CursorBin),
			PiBin:      strings.TrimSpace(state.Runtime.PiBin),
			GHBin:      strings.TrimSpace(state.Runtime.GHBin),
			WaitBufferSec: func() *int {
				if state.Runtime.WaitBufferSec != nil {
					waitBuffer := *state.Runtime.WaitBufferSec
					return &waitBuffer
				}
				waitBuffer := defaultCommandWaitBuffer
				return &waitBuffer
			}(),
		},
	}

	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles:
		record.Fields.Source = string(normalizeFileSource(state.Files.Source))
		record.Fields.Strategy = defaultString(state.Files.Strategy, DefaultStrategy)
		record.Fields.Files = normalizeOrderedItems(state.Files.Files)
		record.Fields.AllFiles = strings.TrimSpace(state.Files.AllFiles)
		record.Fields.DryRun = state.Files.DryRun
		record.Fields.Force = state.Files.Force
		record.Fields.ContinueOnError = state.Files.ContinueOnError
		record.Fields.Loop = state.Files.Loop
		record.Fields.PromptTemplate = strings.TrimSpace(state.Files.PromptTemplate)
		record.Fields.LogDir = strings.TrimSpace(state.Files.LogDir)
		record.Fields.DoneFile = strings.TrimSpace(state.Files.DoneFile)
	case WorkflowImprove:
		record.Fields.PromptSource = string(normalizeImprovePromptSource(state.Improve.PromptSource))
		record.Fields.Mode = defaultString(state.Improve.Mode, defaultImproveMode)
		record.Fields.Prompt = strings.TrimSpace(state.Improve.Prompt)
		record.Fields.PromptFile = strings.TrimSpace(state.Improve.PromptFile)
		record.Fields.Strategy = defaultString(state.Improve.Strategy, defaultImproveStrategy)
		record.Fields.Loop = state.Improve.Loop
		record.Fields.Scope = strings.TrimSpace(state.Improve.Scope)
		if state.Improve.Iterations != nil {
			iterations := *state.Improve.Iterations
			record.Fields.Iterations = &iterations
		} else {
			iterations := 1
			record.Fields.Iterations = &iterations
		}
	default:
		record.Fields.Source = string(normalizeIssueSource(state.Issues.Source))
		record.Fields.Strategy = defaultString(state.Issues.Strategy, DefaultStrategy)
		record.Fields.SingleIssue = strings.TrimSpace(state.Issues.SingleIssue)
		record.Fields.Issues = normalizeOrderedItems(state.Issues.Issues)
		record.Fields.IssuesFile = strings.TrimSpace(state.Issues.IssuesFile)
		record.Fields.Label = strings.TrimSpace(state.Issues.Label)
		record.Fields.DryRun = state.Issues.DryRun
		record.Fields.Force = state.Issues.Force
		record.Fields.ContinueOnError = state.Issues.ContinueOnError
		record.Fields.Loop = state.Issues.Loop
		record.Fields.PromptTemplate = strings.TrimSpace(state.Issues.PromptTemplate)
		record.Fields.LogDir = strings.TrimSpace(state.Issues.LogDir)
		record.Fields.DoneFile = strings.TrimSpace(state.Issues.DoneFile)
	}

	return record
}

func (p presetState) count() int {
	return len(p.entries)
}

func (p presetState) lookup(name string) (Preset, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Preset{}, false
	}
	for _, preset := range p.entries {
		if preset.Name == name {
			return preset, true
		}
	}
	return Preset{}, false
}

func (p *presetState) setStatus(severity Severity, message string) {
	p.status = strings.TrimSpace(message)
	p.severity = severity
}

func (m *Model) initializePresets() {
	m.presets = newPresetState(m.options.RepoRoot, m.options.Preset)
	catalog, err := loadPresetCatalog(m.options.RepoRoot)
	if err != nil {
		m.presets.setStatus(SeverityWarning, fmt.Sprintf("Preset file skipped: %v", err))
	} else {
		m.presets.path = catalog.Path
		m.presets.entries = catalog.Presets
		if len(catalog.Presets) > 0 {
			m.presets.selection = catalog.Presets[0].Name
		}
	}

	requested := strings.TrimSpace(m.options.Preset)
	if requested == "" {
		return
	}
	if err := m.applyPresetByName(requested, true); err != nil {
		m.presets.setStatus(SeverityWarning, fmt.Sprintf("Preset %q was not loaded: %v", requested, err))
	}
}

func (m *Model) applyPresetByName(name string, bootstrap bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("no preset selected")
	}

	preset, ok := m.presets.lookup(name)
	if !ok {
		return fmt.Errorf("not found in %s", m.presets.path)
	}

	m.command = cloneCommandState(preset.State)
	m.options.Workflow = string(normalizeCommandWorkflow(m.command.Workflow))
	m.options.Preset = name
	m.presets.selection = name
	m.presets.active = name
	m.presets.loadedFrom = name
	if bootstrap || strings.TrimSpace(m.presets.saveName) == "" {
		m.presets.saveName = name
	}
	m.runBlocked = ""
	m.configure.editing = false
	m.configure.editValue = ""
	m.configure.drafts = make(map[string]string)
	m.configure.parseError = make(map[string]string)
	m.refreshConfigureState()
	if bootstrap {
		m.presets.setStatus(SeverityInfo, fmt.Sprintf("Preset %q loaded from %s.", name, m.presets.path))
	} else {
		m.presets.setStatus(SeverityInfo, fmt.Sprintf("Preset %q loaded.", name))
	}
	return nil
}

func (m *Model) saveCurrentPreset() {
	name := strings.TrimSpace(m.presets.saveName)
	if name == "" {
		m.presets.setStatus(SeverityWarning, "Preset save skipped: enter a preset name first.")
		return
	}

	result, err := saveNamedPreset(m.options.RepoRoot, name, m.command)
	if err != nil {
		m.presets.setStatus(SeverityWarning, fmt.Sprintf("Preset save failed: %v", err))
		return
	}

	catalog, err := loadPresetCatalog(m.options.RepoRoot)
	if err != nil {
		m.presets.setStatus(SeverityWarning, fmt.Sprintf("Preset %q saved, but reload failed: %v", name, err))
		return
	}

	m.presets.path = catalog.Path
	m.presets.entries = catalog.Presets
	m.presets.active = name
	m.presets.selection = name
	m.presets.saveName = name
	m.options.Preset = name

	if result.RecoveredPath != "" {
		m.presets.setStatus(SeverityWarning, fmt.Sprintf("Preset %q saved. Invalid preset file moved to %s.", name, result.RecoveredPath))
		return
	}
	m.presets.setStatus(SeverityInfo, fmt.Sprintf("Preset %q saved to %s.", name, result.Path))
}

func (m *Model) loadSelectedPreset() {
	name := strings.TrimSpace(m.presets.selection)
	if name == "" {
		m.presets.setStatus(SeverityWarning, "Preset load skipped: select a saved preset first.")
		return
	}

	if err := m.applyPresetByName(name, false); err != nil {
		m.presets.setStatus(SeverityWarning, fmt.Sprintf("Preset %q was not loaded: %v", name, err))
	}
}

func parsePresetWorkflow(value string) (Workflow, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(WorkflowIssues):
		return WorkflowIssues, nil
	case string(WorkflowFiles):
		return WorkflowFiles, nil
	case string(WorkflowImprove):
		return WorkflowImprove, nil
	default:
		return "", fmt.Errorf("workflow must be one of: issues, files, improve")
	}
}

func parsePresetIssueSource(value string) (IssueSource, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(IssueSourceSingle):
		return IssueSourceSingle, nil
	case string(IssueSourceCSV):
		return IssueSourceCSV, nil
	case string(IssueSourceFile):
		return IssueSourceFile, nil
	case string(IssueSourceAllOpen):
		return IssueSourceAllOpen, nil
	default:
		return "", fmt.Errorf("issues.source must be one of: single, csv, file, all-open")
	}
}

func parsePresetFileSource(value string) (FileSource, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(FileSourceExplicit):
		return FileSourceExplicit, nil
	case string(FileSourceAllFiles):
		return FileSourceAllFiles, nil
	default:
		return "", fmt.Errorf("files.source must be one of: explicit, all-files")
	}
}

func parsePresetImproveMode(value string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "" {
		return defaultImproveMode, nil
	}
	if !slices.Contains(ValidImproveModes, mode) {
		return "", fmt.Errorf("--mode must be one of: %s", strings.Join(ValidImproveModes, ", "))
	}
	return mode, nil
}

func parsePresetStrategy(value string) (string, error) {
	strategy := strings.ToLower(strings.TrimSpace(value))
	if strategy == "" {
		return DefaultStrategy, nil
	}
	if !slices.Contains(ValidStrategies, strategy) {
		return "", fmt.Errorf("--strategy must be one of: %s", strings.Join(ValidStrategies, ", "))
	}
	return strategy, nil
}

func parsePresetAgent(value string) (string, error) {
	agent := normalizeAgent(value)
	if !slices.Contains(ValidAgents, agent) {
		return "", fmt.Errorf("--agent must be one of: %s", strings.Join(ValidAgents, ", "))
	}
	return agent, nil
}

func parsePresetStreamView(value string) (string, error) {
	view := normalizeStreamView(value)
	if !slices.Contains(ValidStreamViews, view) {
		return "", fmt.Errorf("--stream_view must be one of: %s", strings.Join(ValidStreamViews, ", "))
	}
	return view, nil
}

func isInvalidPresetFileError(err error) bool {
	var invalid invalidPresetFileError
	return errors.As(err, &invalid)
}
