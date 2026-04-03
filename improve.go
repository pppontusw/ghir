package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	mathrand "math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const improveMultiMode = "multi"

type improveModeRandomizer interface {
	Intn(int) int
}

type improveOptions struct {
	agentConfig
	Mode         string
	ModeList     []string
	ModeExplicit bool
	Prompt       string
	PromptFile   string
	Iterations   int
	Loop         bool
	Strategy     string
	Scope        string
	Help         bool
	AvoidFiles   []string
	modeRand     improveModeRandomizer
}

func newImproveRunner(iopts improveOptions, repoRoot string) (*runner, error) {
	opts := options{
		agentConfig: iopts.agentConfig,
	}

	applyRepoDefaults(&opts, repoRoot)
	applyImproveRepoDefaults(&iopts, repoRoot)

	r, err := newRunner(opts, repoRoot)
	if err != nil {
		return nil, err
	}

	if err := r.preflightChecks(); err != nil {
		return nil, err
	}

	return r, nil
}

func runImprove(r *runner, iopts improveOptions) error {
	if len(iopts.ModeList) > 1 && iopts.modeRand == nil {
		iopts.modeRand = mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	}

	switch iopts.Strategy {
	case "direct":
		return runImproveDirect(r, iopts)
	case "pr-per-pass":
		return runImprovePR(r, iopts)
	case "pr-chain":
		return runImprovePRChain(r, iopts)
	case "pr-at-end":
		return runImprovePRAtEnd(r, iopts)
	default:
		return fmt.Errorf("unknown improve strategy: %s", iopts.Strategy)
	}
}

func runImproveDirect(r *runner, iopts improveOptions) error {
	passes := 0
	iteration := 1

	for {
		if iopts.Iterations > 0 && passes >= iopts.Iterations {
			return nil
		}

		branch, err := r.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return fmt.Errorf("determine current branch: %w", err)
		}

		effectiveMode := selectImproveMode(iopts, iteration)
		noChanges, err := runImproveOnCurrentBranch(r, iopts, iteration, branch, effectiveMode)
		if err != nil {
			return err
		}

		passes++
		iteration++

		if noChanges {
			return nil
		}
		if !iopts.Loop && iopts.Iterations > 0 && passes >= iopts.Iterations {
			return nil
		}
		if !iopts.Loop && iopts.Iterations == 0 {
			return nil
		}
	}
}

func runImprovePR(r *runner, iopts improveOptions) error {
	passes := 0
	iteration := 1

	for {
		if iopts.Iterations > 0 && passes >= iopts.Iterations {
			return nil
		}

		baseBranch, err := r.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return fmt.Errorf("determine base branch: %w", err)
		}

		dirty, err := r.workingTreeDirty()
		if err != nil {
			return fmt.Errorf("check git status: %w", err)
		}
		if dirty {
			return fmt.Errorf("uncommitted changes detected on %s; commit or stash before running improve", baseBranch)
		}

		prBase, err := r.defaultBranch()
		if err != nil {
			return fmt.Errorf("determine default branch: %w", err)
		}

		effectiveMode := selectImproveMode(iopts, iteration)
		featureBranch, err := makeImproveBranchName(effectiveMode, iteration)
		if err != nil {
			return fmt.Errorf("generate branch name: %w", err)
		}
		if _, err := r.gitOutput("checkout", "-b", featureBranch); err != nil {
			return fmt.Errorf("create feature branch %s: %w", featureBranch, err)
		}

		noChanges := false
		runErr := func() error {
			defer func() {
				if _, err := r.gitOutput("checkout", baseBranch); err != nil {
					r.printf(r.colors.Red, "WARNING: failed to switch back to %s: %v\n", baseBranch, err)
				}
			}()

			var err error
			noChanges, err = runImproveOnCurrentBranch(r, iopts, iteration, featureBranch, effectiveMode)
			return err
		}()
		if runErr != nil {
			return runErr
		}

		if noChanges {
			if _, err := r.gitOutput("branch", "-D", featureBranch); err != nil {
				r.printf(r.colors.Yellow, "Warning: failed to delete empty branch %s: %v\n", featureBranch, err)
			}
			return nil
		}

		out, err := r.gitOutput("diff", "--name-only", baseBranch+"..."+featureBranch)
		if err == nil && out != "" {
			lines := strings.Split(out, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					found := false
					for _, f := range iopts.AvoidFiles {
						if f == line {
							found = true
							break
						}
					}
					if !found {
						iopts.AvoidFiles = append(iopts.AvoidFiles, line)
					}
				}
			}
		}

		if _, err := r.gitOutput("push", "-u", "origin", featureBranch); err != nil {
			return fmt.Errorf("push feature branch %s: %w", featureBranch, err)
		}

		title, body := improvePRTitleAndBodyFromCommit(r, featureBranch, effectiveMode, iteration)
		if _, err := r.commandOutput(r.opts.GHBin, "pr", "create",
			"--title", title,
			"--body", body,
			"--base", prBase,
			"--head", featureBranch,
		); err != nil {
			return fmt.Errorf("create pull request: %w", err)
		}

		passes++
		iteration++

		if !iopts.Loop && iopts.Iterations > 0 && passes >= iopts.Iterations {
			return nil
		}
		if !iopts.Loop && iopts.Iterations == 0 {
			return nil
		}
	}
}

func runImprovePRChain(r *runner, iopts improveOptions) error {
	passes := 0
	iteration := 1

	prBase, err := r.defaultBranch()
	if err != nil {
		return fmt.Errorf("determine default branch: %w", err)
	}

	for {
		if iopts.Iterations > 0 && passes >= iopts.Iterations {
			return nil
		}

		currentBranch, err := r.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return fmt.Errorf("determine current branch: %w", err)
		}

		dirty, err := r.workingTreeDirty()
		if err != nil {
			return fmt.Errorf("check git status: %w", err)
		}
		if dirty {
			return fmt.Errorf("uncommitted changes detected on %s; commit or stash before running improve", currentBranch)
		}

		effectiveMode := selectImproveMode(iopts, iteration)
		featureBranch, err := makeImproveBranchName(effectiveMode, iteration)
		if err != nil {
			return fmt.Errorf("generate branch name: %w", err)
		}
		if _, err := r.gitOutput("checkout", "-b", featureBranch); err != nil {
			return fmt.Errorf("create feature branch %s: %w", featureBranch, err)
		}

		noChanges, err := runImproveOnCurrentBranch(r, iopts, iteration, featureBranch, effectiveMode)
		if err != nil {
			return err
		}

		if noChanges {
			if _, err := r.gitOutput("checkout", currentBranch); err != nil {
				r.printf(r.colors.Red, "WARNING: failed to switch back to %s: %v\n", currentBranch, err)
			}
			if _, err := r.gitOutput("branch", "-D", featureBranch); err != nil {
				r.printf(r.colors.Yellow, "Warning: failed to delete empty branch %s: %v\n", featureBranch, err)
			}
			return nil
		}

		if _, err := r.gitOutput("push", "-u", "origin", featureBranch); err != nil {
			return fmt.Errorf("push feature branch %s: %w", featureBranch, err)
		}

		title, body := improvePRTitleAndBodyFromCommit(r, featureBranch, effectiveMode, iteration)
		if _, err := r.commandOutput(r.opts.GHBin, "pr", "create",
			"--title", title,
			"--body", body,
			"--base", prBase,
			"--head", featureBranch,
		); err != nil {
			return fmt.Errorf("create pull request: %w", err)
		}

		prBase = featureBranch

		passes++
		iteration++

		if !iopts.Loop && iopts.Iterations > 0 && passes >= iopts.Iterations {
			return nil
		}
		if !iopts.Loop && iopts.Iterations == 0 {
			return nil
		}
	}
}

func runImprovePRAtEnd(r *runner, iopts improveOptions) error {
	passes := 0
	iteration := 1

	baseBranch, err := r.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("determine base branch: %w", err)
	}

	dirty, err := r.workingTreeDirty()
	if err != nil {
		return fmt.Errorf("check git status: %w", err)
	}
	if dirty {
		return fmt.Errorf("uncommitted changes detected on %s; commit or stash before running improve", baseBranch)
	}

	prBase, err := r.defaultBranch()
	if err != nil {
		return fmt.Errorf("determine default branch: %w", err)
	}

	featureBranch, err := makeImproveBranchName(iopts.aggregateLabelMode(), 1)
	if err != nil {
		return fmt.Errorf("generate branch name: %w", err)
	}
	if _, err := r.gitOutput("checkout", "-b", featureBranch); err != nil {
		return fmt.Errorf("create feature branch %s: %w", featureBranch, err)
	}

	var commitMessages []string
	madeChanges := false

	for {
		if iopts.Iterations > 0 && passes >= iopts.Iterations {
			break
		}

		effectiveMode := selectImproveMode(iopts, iteration)
		noChanges, err := runImproveOnCurrentBranch(r, iopts, iteration, featureBranch, effectiveMode)
		if err != nil {
			return err
		}

		if !noChanges {
			madeChanges = true
			subject, err := r.gitOutput("log", "-1", "--format=%s", featureBranch)
			if err == nil && strings.TrimSpace(subject) != "" {
				commitMessages = append(commitMessages, strings.TrimSpace(subject))
			}
		} else {
			break
		}

		passes++
		iteration++

		if !iopts.Loop && iopts.Iterations > 0 && passes >= iopts.Iterations {
			break
		}
		if !iopts.Loop && iopts.Iterations == 0 {
			break
		}
	}

	if !madeChanges {
		if _, err := r.gitOutput("checkout", baseBranch); err != nil {
			r.printf(r.colors.Red, "WARNING: failed to switch back to %s: %v\n", baseBranch, err)
		}
		if _, err := r.gitOutput("branch", "-D", featureBranch); err != nil {
			r.printf(r.colors.Yellow, "Warning: failed to delete empty branch %s: %v\n", featureBranch, err)
		}
		return nil
	}

	if _, err := r.gitOutput("push", "-u", "origin", featureBranch); err != nil {
		return fmt.Errorf("push feature branch %s: %w", featureBranch, err)
	}

	title, body := buildImprovePRAtEndSummary(iopts, commitMessages)

	if _, err := r.commandOutput(r.opts.GHBin, "pr", "create",
		"--title", title,
		"--body", body,
		"--base", prBase,
		"--head", featureBranch,
	); err != nil {
		return fmt.Errorf("create pull request: %w", err)
	}

	return nil
}

func runImproveOnCurrentBranch(r *runner, iopts improveOptions, iteration int, branch, effectiveMode string) (bool, error) {
	dirty, err := r.workingTreeDirty()
	if err != nil {
		return false, fmt.Errorf("check git status: %w", err)
	}
	if dirty {
		return false, fmt.Errorf("uncommitted changes detected; commit or stash before running improve")
	}

	startHead, err := r.gitOutput("rev-parse", "HEAD")
	if err != nil {
		return false, fmt.Errorf("determine pre-run git HEAD: %w", err)
	}

	prompt, effectiveMode, err := buildImprovePromptForMode(iopts, r.repoRoot, branch, iteration, effectiveMode)
	if err != nil {
		return false, err
	}

	runSuffix := improveRunSuffixFromBranch(branch)
	if runSuffix == "" {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err == nil {
			runSuffix = hex.EncodeToString(b)
		}
	}
	logFileName := fmt.Sprintf("improve-%s-%d-%s.log", strings.ReplaceAll(effectiveMode, "_", "-"), iteration, runSuffix)

	for {
		exitCode, _, logPath, retry, runErr := runImprovePassOnce(r, prompt, logFileName, effectiveMode, iteration)
		if runErr != nil {
			return false, runErr
		}
		if retry {
			continue
		}
		if exitCode != 0 {
			return false, fmt.Errorf("improve agent exited with code %d (see log: %s)", exitCode, logPath)
		}
		break
	}

	endHead, err := r.gitOutput("rev-parse", "HEAD")
	if err != nil {
		return false, fmt.Errorf("determine post-run git HEAD: %w", err)
	}

	if endHead != startHead {
		r.printf(r.colors.Green, "Improvement pass %d (%s) created new commit(s) on %s\n", iteration, effectiveMode, branch)
		return false, nil
	}

	dirty, err = r.workingTreeDirty()
	if err != nil {
		return false, fmt.Errorf("determine post-run git status: %w", err)
	}
	if dirty {
		message := improveCommitMessage(effectiveMode, iteration)
		if err := r.commitAll(message); err != nil {
			return false, fmt.Errorf("fallback commit failed for improve pass %d: %w", iteration, err)
		}
		r.printf(r.colors.Green, "Improvement pass %d (%s) committed by runner on %s\n", iteration, effectiveMode, branch)
		return false, nil
	}

	r.printf(r.colors.Yellow, "Improvement pass %d (%s) produced no changes on %s\n", iteration, effectiveMode, branch)
	return true, nil
}

func runImprovePassOnce(r *runner, prompt, logFileName, mode string, iteration int) (int, string, string, bool, error) {
	logPath := filepath.Join(r.opts.LogDir, logFileName)

	r.printf(r.colors.Yellow, "Starting improve pass %d (%s) with %s...\n", iteration, mode, agentDisplayName(r.opts.Agent))
	fmt.Printf("Log: %s\n", logPath)

	exitCode, logOutput, err := r.runAgentWithRetry(prompt, logPath)
	if err != nil {
		return 0, "", logPath, false, fmt.Errorf("improve %s invocation failed: %w", r.opts.Agent, err)
	}

	if retry, commitErr := r.handleSessionLimitRetry(logOutput, exitCode, "mid-improve", improvePartialCommitMessage(mode, iteration)); commitErr != nil {
		return 0, "", logPath, false, commitErr
	} else if retry {
		return 0, "", logPath, true, nil
	}

	return exitCode, logOutput, logPath, false, nil
}

func loadImproveTemplate(mode, repoRoot string) (string, error) {
	tmpl, ok := improveTemplateByMode[mode]
	if !ok {
		tmpl = improveTemplateByMode["cleanup"]
	}
	candidate := filepath.Join(repoRoot, ".ticket-runner", tmpl.fileName)
	if data, err := os.ReadFile(candidate); err == nil {
		return string(data), nil
	}
	return tmpl.builtIn, nil
}

func buildImprovePrompt(iopts improveOptions, repoRoot, branch string, iteration int) (string, string, error) {
	return buildImprovePromptForMode(iopts, repoRoot, branch, iteration, selectImproveMode(iopts, iteration))
}

func buildImprovePromptForMode(iopts improveOptions, repoRoot, branch string, iteration int, effectiveMode string) (string, string, error) {
	if iopts.hasCustomPrompt() {
		templateBody, err := loadCustomImprovePrompt(iopts)
		if err != nil {
			return "", "", err
		}
		prompt := templateBody
		if len(iopts.AvoidFiles) > 0 {
			prompt += fmt.Sprintf("\n\ndo not touch these files, as they have open PRs with changes already: %s", strings.Join(iopts.AvoidFiles, ", "))
		}
		return prompt, improveCustomMode, nil
	}

	templateBody, err := loadImproveTemplate(effectiveMode, repoRoot)
	if err != nil {
		return "", "", fmt.Errorf("load improve template: %w", err)
	}

	replacer := strings.NewReplacer(
		"{{MODE}}", effectiveMode,
		"{{ITERATION_NUMBER}}", strconv.Itoa(iteration),
		"{{BRANCH_NAME}}", branch,
		"{{SCOPE}}", iopts.Scope,
	)
	prompt := replacer.Replace(templateBody)

	if len(iopts.AvoidFiles) > 0 {
		prompt += fmt.Sprintf("\n\ndo not touch these files, as they have open PRs with changes already: %s", strings.Join(iopts.AvoidFiles, ", "))
	}

	return prompt, effectiveMode, nil
}

func (o improveOptions) hasCustomPrompt() bool {
	return strings.TrimSpace(o.Prompt) != "" || strings.TrimSpace(o.PromptFile) != ""
}

func (o improveOptions) aggregateLabelMode() string {
	if o.hasCustomPrompt() {
		return improveCustomMode
	}
	if len(o.ModeList) > 1 {
		return improveMultiMode
	}
	if len(o.ModeList) == 1 {
		return o.ModeList[0]
	}
	return o.Mode
}

func (o improveOptions) configuredModeSummary() string {
	if o.hasCustomPrompt() {
		return improveCustomMode
	}
	if len(o.ModeList) > 1 {
		return strings.Join(o.ModeList, ", ")
	}
	if len(o.ModeList) == 1 {
		return o.ModeList[0]
	}
	return o.Mode
}

func applyImproveRepoDefaults(iopts *improveOptions, repoRoot string) {
	if strings.TrimSpace(iopts.PromptFile) != "" {
		iopts.PromptFile = resolvePath(repoRoot, iopts.PromptFile)
	}
}

func loadCustomImprovePrompt(iopts improveOptions) (string, error) {
	if strings.TrimSpace(iopts.Prompt) != "" {
		return iopts.Prompt, nil
	}
	if path := strings.TrimSpace(iopts.PromptFile); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read improve prompt file: %w", err)
		}
		return string(data), nil
	}
	return "", fmt.Errorf("custom improve prompt source is empty")
}

func selectImproveMode(iopts improveOptions, iteration int) string {
	if iopts.hasCustomPrompt() {
		return improveCustomMode
	}
	if len(iopts.ModeList) > 1 {
		if iopts.modeRand == nil {
			return iopts.ModeList[0]
		}
		return iopts.ModeList[iopts.modeRand.Intn(len(iopts.ModeList))]
	}
	mode := iopts.Mode
	if len(iopts.ModeList) == 1 {
		mode = iopts.ModeList[0]
	}
	if mode == "mixed" {
		idx := (iteration - 1) % len(improveModes)
		return improveModes[idx]
	}
	return mode
}

func makeImproveBranchName(mode string, iteration int) (string, error) {
	if mode == "mixed" || mode == "" {
		mode = "mixed"
	}
	safeMode := strings.ReplaceAll(mode, "_", "-")
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("ghir/improve-%s-%d-%s", safeMode, iteration, hex.EncodeToString(b)), nil
}

func improveRunSuffixFromBranch(branch string) string {
	// ghir/improve-{mode}-{iteration}-{hex}
	if !strings.HasPrefix(branch, "ghir/improve-") {
		return ""
	}
	parts := strings.Split(branch, "-")
	if len(parts) < 2 {
		return ""
	}
	last := parts[len(parts)-1]
	if len(last) == 8 {
		return last
	}
	return ""
}

func improveCommitMessage(mode string, iteration int) string {
	return fmt.Sprintf("chore: %s pass %d%s", improveModeLabel(mode), iteration, commitCoAuthorSuffix)
}

func improvePartialCommitMessage(mode string, iteration int) string {
	return fmt.Sprintf("wip: partial %s pass %d (session limit hit)%s", improveModeLabel(mode), iteration, commitCoAuthorSuffix)
}

func improveCommitTitle(mode string, iteration int) string {
	base := improveModeLabel(mode)
	return fmt.Sprintf("chore: %s pass %d", base, iteration)
}

func improveModeLabel(mode string) string {
	if label, ok := improveModeLabels[mode]; ok {
		return label
	}
	return "improvement"
}

func improvePRBody(mode string, iteration int) string {
	return fmt.Sprintf("Automated ghir improve pass %d in %s mode.\n\nThis PR was generated by ghir's continuous improvement mode. It applies a focused set of changes for this pass and should be reviewed like any other code change.", iteration, mode)
}

func improvePRTitleAndBodyFromCommit(r *runner, branch, mode string, iteration int) (title, body string) {
	subject, err := r.gitOutput("log", "-1", "--format=%s", branch)
	if err != nil || strings.TrimSpace(subject) == "" {
		return improveCommitTitle(mode, iteration), improvePRBody(mode, iteration)
	}
	rawBody, err := r.gitOutput("log", "-1", "--format=%b", branch)
	if err != nil {
		return subject, improvePRBody(mode, iteration)
	}
	body = strings.TrimSpace(rawBody)
	if body == "" {
		body = improvePRBody(mode, iteration)
	}
	return subject, body
}

func buildImprovePRAtEndSummary(iopts improveOptions, commitMessages []string) (title, body string) {
	var bodyBuilder strings.Builder
	bodyBuilder.WriteString(fmt.Sprintf("Automated ghir improve passes in %s mode.\n\n", iopts.aggregateLabelMode()))
	if len(iopts.ModeList) > 1 && !iopts.hasCustomPrompt() {
		bodyBuilder.WriteString(fmt.Sprintf("Configured modes: %s\n\n", iopts.configuredModeSummary()))
	}
	bodyBuilder.WriteString("## Commits\n")
	for _, msg := range commitMessages {
		bodyBuilder.WriteString(fmt.Sprintf("- %s\n", msg))
	}

	return fmt.Sprintf("chore: %s improvements", improveModeLabel(iopts.aggregateLabelMode())), bodyBuilder.String()
}
