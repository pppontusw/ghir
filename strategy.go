package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"ghir/tui"
)

func strategyOrDefault(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return tui.DefaultStrategy
	}
	return value
}

func runOnceDirect(r *runner, issues []string) (int, error) {
	succeeded, failed := 0, 0
	var failedIssues []string
	for i, issue := range issues {
		idx := i + 1
		result := r.processIssue(idx, len(issues), issue, false)
		for result == resultRetry {
			r.printf(r.colors.Blue, "Retrying %s after session limit reset...\n", r.issueLabel(issue))
			result = r.processIssue(idx, len(issues), issue, true)
		}
		if result == resultSuccess {
			succeeded++
			continue
		}
		failed++
		failedIssues = append(failedIssues, r.issueLabel(issue))
		if r.opts.ContinueOnError {
			r.printf(r.colors.Red, "Failed on %s, continuing due to --continue-on-error\n", r.issueLabel(issue))
			continue
		}
		r.printf(r.colors.Red, "Stopping due to failure on %s\n", r.issueLabel(issue))
		break
	}

	printRunSummary(r, succeeded, failed, failedIssues)
	return failed, nil
}

func runOncePRPerPass(r *runner, issues []string) (int, error) {
	baseBranch, err := r.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return 0, fmt.Errorf("determine base branch: %w", err)
	}
	prBase, err := r.defaultBranch()
	if err != nil {
		return 0, fmt.Errorf("determine default branch: %w", err)
	}

	succeeded, failed := 0, 0
	var failedIssues []string

	for i, issue := range issues {
		idx := i + 1
		if r.opts.DryRun || (r.isCompleted(issue) && !r.opts.Force) {
			execResult := runStrategyItem(r, idx, len(issues), issue, false)
			if execResult.result == resultSuccess {
				succeeded++
				continue
			}
			failed++
			failedIssues = append(failedIssues, r.issueLabel(issue))
			if r.opts.ContinueOnError {
				continue
			}
			break
		}

		if err := ensureCleanWorkingTree(r, baseBranch, false); err != nil {
			return 0, err
		}

		featureBranch, err := makeWorkBranchName(r.fileMode, issue, idx, false)
		if err != nil {
			return 0, fmt.Errorf("generate branch name: %w", err)
		}
		if _, err := r.gitOutput("checkout", "-b", featureBranch); err != nil {
			return 0, fmt.Errorf("create feature branch %s: %w", featureBranch, err)
		}

		execResult := runStrategyItem(r, idx, len(issues), issue, true)
		if execResult.result != resultSuccess {
			failed++
			failedIssues = append(failedIssues, r.issueLabel(issue))
			if continueErr := restoreAfterPerPassFailure(r, baseBranch, issue); continueErr != nil {
				printRunSummary(r, succeeded, failed, failedIssues)
				return failed, continueErr
			}
			if r.opts.ContinueOnError {
				r.printf(r.colors.Red, "Failed on %s, continuing due to --continue-on-error\n", r.issueLabel(issue))
				continue
			}
			r.printf(r.colors.Red, "Stopping due to failure on %s\n", r.issueLabel(issue))
			break
		}

		if execResult.changed {
			title, body := workPRTitleAndBodyFromCommit(r, featureBranch, issue, execResult.details)
			if err := pushAndCreatePR(r, prBase, featureBranch, title, body); err != nil {
				failed++
				failedIssues = append(failedIssues, r.issueLabel(issue))
				if continueErr := restoreAfterPerPassFailure(r, baseBranch, issue); continueErr != nil {
					printRunSummary(r, succeeded, failed, failedIssues)
					return failed, continueErr
				}
				if r.opts.ContinueOnError {
					r.printf(r.colors.Red, "Failed on %s, continuing due to --continue-on-error\n", r.issueLabel(issue))
					continue
				}
				r.printf(r.colors.Red, "Stopping due to failure on %s\n", r.issueLabel(issue))
				break
			}
			if !r.tryMarkCompleted(issue) {
				failed++
				failedIssues = append(failedIssues, r.issueLabel(issue))
				printRunSummary(r, succeeded, failed, failedIssues)
				return failed, nil
			}
			r.printf(r.colors.Green, "PR created for %s on %s\n", r.issueLabel(issue), featureBranch)
		} else {
			if err := deleteBranchAfterCheckout(r, baseBranch, featureBranch); err != nil {
				failed++
				failedIssues = append(failedIssues, r.issueLabel(issue))
				printRunSummary(r, succeeded, failed, failedIssues)
				return failed, err
			}
			succeeded++
			continue
		}

		if _, err := r.gitOutput("checkout", baseBranch); err != nil {
			failed++
			failedIssues = append(failedIssues, r.issueLabel(issue))
			printRunSummary(r, succeeded, failed, failedIssues)
			return failed, fmt.Errorf("switch back to %s: %w", baseBranch, err)
		}
		succeeded++
	}

	printRunSummary(r, succeeded, failed, failedIssues)
	return failed, nil
}

func runOncePRChain(r *runner, issues []string) (int, error) {
	prBase, err := r.defaultBranch()
	if err != nil {
		return 0, fmt.Errorf("determine default branch: %w", err)
	}

	succeeded, failed := 0, 0
	var failedIssues []string

	for i, issue := range issues {
		idx := i + 1
		currentBranch, err := r.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return 0, fmt.Errorf("determine current branch: %w", err)
		}

		if r.opts.DryRun || (r.isCompleted(issue) && !r.opts.Force) {
			execResult := runStrategyItem(r, idx, len(issues), issue, false)
			if execResult.result == resultSuccess {
				succeeded++
				continue
			}
			failed++
			failedIssues = append(failedIssues, r.issueLabel(issue))
			r.printf(r.colors.Red, "Stopping due to failure on %s\n", r.issueLabel(issue))
			break
		}

		if err := ensureCleanWorkingTree(r, currentBranch, false); err != nil {
			return 0, err
		}

		featureBranch, err := makeWorkBranchName(r.fileMode, issue, idx, false)
		if err != nil {
			return 0, fmt.Errorf("generate branch name: %w", err)
		}
		if _, err := r.gitOutput("checkout", "-b", featureBranch); err != nil {
			return 0, fmt.Errorf("create feature branch %s: %w", featureBranch, err)
		}

		execResult := runStrategyItem(r, idx, len(issues), issue, true)
		if execResult.result != resultSuccess {
			failed++
			failedIssues = append(failedIssues, r.issueLabel(issue))
			r.printf(r.colors.Red, "Stopping due to failure on %s\n", r.issueLabel(issue))
			printRunSummary(r, succeeded, failed, failedIssues)
			return failed, nil
		}

		if !execResult.changed {
			if err := deleteBranchAfterCheckout(r, currentBranch, featureBranch); err != nil {
				failed++
				failedIssues = append(failedIssues, r.issueLabel(issue))
				printRunSummary(r, succeeded, failed, failedIssues)
				return failed, err
			}
			succeeded++
			continue
		}

		title, body := workPRTitleAndBodyFromCommit(r, featureBranch, issue, execResult.details)
		if err := pushAndCreatePR(r, prBase, featureBranch, title, body); err != nil {
			failed++
			failedIssues = append(failedIssues, r.issueLabel(issue))
			r.printf(r.colors.Red, "Stopping due to failure on %s\n", r.issueLabel(issue))
			printRunSummary(r, succeeded, failed, failedIssues)
			return failed, nil
		}
		if !r.tryMarkCompleted(issue) {
			failed++
			failedIssues = append(failedIssues, r.issueLabel(issue))
			printRunSummary(r, succeeded, failed, failedIssues)
			return failed, nil
		}
		r.printf(r.colors.Green, "PR created for %s on %s\n", r.issueLabel(issue), featureBranch)
		prBase = featureBranch
		succeeded++
	}

	printRunSummary(r, succeeded, failed, failedIssues)
	return failed, nil
}

func runOncePRAtEnd(r *runner, issues []string) (int, error) {
	baseBranch, err := r.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return 0, fmt.Errorf("determine base branch: %w", err)
	}
	if err := ensureCleanWorkingTree(r, baseBranch, false); err != nil {
		return 0, err
	}

	prBase, err := r.defaultBranch()
	if err != nil {
		return 0, fmt.Errorf("determine default branch: %w", err)
	}

	featureBranch, err := makeWorkBranchName(r.fileMode, "", 0, true)
	if err != nil {
		return 0, fmt.Errorf("generate batch branch name: %w", err)
	}
	if _, err := r.gitOutput("checkout", "-b", featureBranch); err != nil {
		return 0, fmt.Errorf("create feature branch %s: %w", featureBranch, err)
	}

	succeeded, failed := 0, 0
	var failedIssues []string
	var pendingCompletion []string
	var commitMessages []string
	madeChanges := false

	for i, issue := range issues {
		idx := i + 1
		execResult := runStrategyItem(r, idx, len(issues), issue, true)
		if execResult.result != resultSuccess {
			failed++
			failedIssues = append(failedIssues, r.issueLabel(issue))
			r.printf(r.colors.Red, "Stopping due to failure on %s\n", r.issueLabel(issue))
			printRunSummary(r, succeeded, failed, failedIssues)
			return failed, nil
		}

		if execResult.changed {
			madeChanges = true
			pendingCompletion = append(pendingCompletion, issue)
			if subject, err := r.gitOutput("log", "-1", "--format=%s", featureBranch); err == nil && strings.TrimSpace(subject) != "" {
				commitMessages = append(commitMessages, strings.TrimSpace(subject))
			}
		}
		succeeded++
	}

	if !madeChanges {
		if err := deleteBranchAfterCheckout(r, baseBranch, featureBranch); err != nil {
			return failed, err
		}
		printRunSummary(r, succeeded, failed, failedIssues)
		return failed, nil
	}

	title, body := batchPRTitleAndBody(r.fileMode, commitMessages)
	if err := pushAndCreatePR(r, prBase, featureBranch, title, body); err != nil {
		failed++
		failedIssues = append(failedIssues, "batch")
		printRunSummary(r, succeeded, failed, failedIssues)
		return failed, nil
	}
	for _, issue := range pendingCompletion {
		if !r.tryMarkCompleted(issue) {
			failed++
			failedIssues = append(failedIssues, r.issueLabel(issue))
			printRunSummary(r, succeeded, failed, failedIssues)
			return failed, nil
		}
	}
	r.printf(r.colors.Green, "PR created for batch branch %s\n", featureBranch)
	printRunSummary(r, succeeded, failed, failedIssues)
	return failed, nil
}

func runStrategyItem(r *runner, idx, total int, issue string, deferChangedCompletion bool) issueExecution {
	isResume := false
	result := r.processIssueWithStrategy(idx, total, issue, isResume, deferChangedCompletion)
	for result.result == resultRetry {
		r.printf(r.colors.Blue, "Retrying %s after session limit reset...\n", r.issueLabel(issue))
		isResume = true
		result = r.processIssueWithStrategy(idx, total, issue, isResume, deferChangedCompletion)
	}
	return result
}

func printRunSummary(r *runner, succeeded, failed int, failedIssues []string) {
	fmt.Println()
	r.printf(r.colors.Blue, separatorLine)
	r.printf(r.colors.Green, "Succeeded: %d\n", succeeded)
	r.printf(r.colors.Red, "Failed: %d\n", failed)
	if len(failedIssues) > 0 {
		r.printf(r.colors.Red, "Failed issues: %s\n", strings.Join(failedIssues, ", "))
	}
	r.printf(r.colors.Blue, separatorLine)
}

func ensureCleanWorkingTree(r *runner, branch string, improve bool) error {
	dirty, err := r.workingTreeDirty()
	if err != nil {
		return fmt.Errorf("check git status: %w", err)
	}
	if dirty {
		if improve {
			return fmt.Errorf("uncommitted changes detected on %s; commit or stash before running improve", branch)
		}
		return fmt.Errorf("uncommitted changes detected on %s; commit or stash before running", branch)
	}
	return nil
}

func restoreAfterPerPassFailure(r *runner, baseBranch, issue string) error {
	if _, err := r.gitOutput("checkout", baseBranch); err != nil {
		return fmt.Errorf("failed to switch back to %s after %s failure: %w", baseBranch, r.issueLabel(issue), err)
	}
	dirty, err := r.workingTreeDirty()
	if err != nil {
		return fmt.Errorf("check git status after %s failure: %w", r.issueLabel(issue), err)
	}
	if dirty {
		return fmt.Errorf("uncommitted changes remain after failure on %s; resolve them before continuing", r.issueLabel(issue))
	}
	return nil
}

func deleteBranchAfterCheckout(r *runner, targetBranch, featureBranch string) error {
	if _, err := r.gitOutput("checkout", targetBranch); err != nil {
		return fmt.Errorf("switch back to %s: %w", targetBranch, err)
	}
	if _, err := r.gitOutput("branch", "-D", featureBranch); err != nil {
		r.printf(r.colors.Yellow, "Warning: failed to delete empty branch %s: %v\n", featureBranch, err)
	}
	return nil
}

func pushAndCreatePR(r *runner, prBase, featureBranch, title, body string) error {
	if _, err := r.gitOutput("push", "-u", "origin", featureBranch); err != nil {
		return fmt.Errorf("push feature branch %s: %w", featureBranch, err)
	}
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

func workPRTitleAndBodyFromCommit(r *runner, branch, issue string, details issueDetails) (title, body string) {
	subject, err := r.gitOutput("log", "-1", "--format=%s", branch)
	if err == nil && strings.TrimSpace(subject) != "" {
		rawBody, bodyErr := r.gitOutput("log", "-1", "--format=%b", branch)
		if bodyErr == nil && strings.TrimSpace(rawBody) != "" {
			return strings.TrimSpace(subject), strings.TrimSpace(rawBody)
		}
		return strings.TrimSpace(subject), workPRBody(r.fileMode, issue, details.Title)
	}
	return workPRTitle(r.fileMode, issue, details.Title), workPRBody(r.fileMode, issue, details.Title)
}

func workPRTitle(fileMode bool, issue, title string) string {
	if fileMode {
		return fmt.Sprintf("feat: %s - %s", issue, title)
	}
	return fmt.Sprintf("feat: implement #%s - %s", issue, title)
}

func workPRBody(fileMode bool, issue, title string) string {
	if fileMode {
		return fmt.Sprintf("Automated ghir PR for `%s`.\n\nTitle: %s", issue, title)
	}
	return fmt.Sprintf("Automated ghir PR for issue #%s.\n\nTitle: %s", issue, title)
}

func batchPRTitleAndBody(fileMode bool, commitMessages []string) (string, string) {
	var title string
	if fileMode {
		title = "chore: file queue batch"
	} else {
		title = "chore: issue queue batch"
	}
	var bodyBuilder strings.Builder
	if fileMode {
		bodyBuilder.WriteString("Automated ghir batch PR for file queue processing.\n\n")
	} else {
		bodyBuilder.WriteString("Automated ghir batch PR for issue queue processing.\n\n")
	}
	if len(commitMessages) > 0 {
		bodyBuilder.WriteString("## Commits\n")
		for _, msg := range commitMessages {
			bodyBuilder.WriteString("- " + msg + "\n")
		}
	}
	return title, strings.TrimSpace(bodyBuilder.String())
}

func makeWorkBranchName(fileMode bool, issue string, index int, batch bool) (string, error) {
	suffix, err := randomHex(4)
	if err != nil {
		return "", err
	}
	if batch {
		if fileMode {
			return fmt.Sprintf("ghir/files-batch-%s", suffix), nil
		}
		return fmt.Sprintf("ghir/issues-batch-%s", suffix), nil
	}
	if fileMode {
		return fmt.Sprintf("ghir/file-%s-%d-%s", slugifyBranchSegment(issue), index, suffix), nil
	}
	return fmt.Sprintf("ghir/issue-%s-%d-%s", slugifyBranchSegment(issue), index, suffix), nil
}

func randomHex(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func slugifyBranchSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "item"
	}
	value = strings.TrimSuffix(value, filepath.Ext(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return "item"
	}
	return slug
}
