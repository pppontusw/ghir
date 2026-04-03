package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestIntegrationSmoke(t *testing.T) {
	t.Parallel()

	// 1. Create a temporary directory for the test workspace
	dir := t.TempDir()

	// 2. Set up a dummy git repository
	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.name", "Test User")
	runCmd(t, dir, "git", "config", "user.email", "test@example.com")

	// Create an initial commit so HEAD exists
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("fake-*\n.ticket-runs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, dir, "git", "add", "README.md", ".gitignore")
	runCmd(t, dir, "git", "commit", "-m", "Initial commit")

	// 3. Stub the `gh` binary
	ghStubPath := filepath.Join(dir, "fake-gh")
	ghStubScript := `#!/bin/sh
if [ "$1" = "issue" ] && [ "$2" = "view" ] && [ "$3" = "42" ]; then
	echo '{"title": "Smoke Test Issue", "body": "This is a deterministic body for testing."}'
	exit 0
fi
if [ "$1" = "issue" ] && [ "$2" = "list" ]; then
	echo '[{"number": 42}]'
	exit 0
fi
echo "Unexpected gh arguments: $@" >&2
exit 1
`
	if err := os.WriteFile(ghStubPath, []byte(ghStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	// 4. Stub the agent binary (e.g. claude)
	agentStubPath := filepath.Join(dir, "fake-claude")
	agentStubScript := `#!/bin/sh
# The agent modifies a file to simulate work
echo "Modified by fake agent" >> README.md
# Output some mock response
echo "I have completed the task."
exit 0
`
	if err := os.WriteFile(agentStubPath, []byte(agentStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	// 5. Run the ticket-runner flow via the test helper process
	// This simulates running the compiled binary.

	cmdArgs := []string{
		"-test.run=TestMainHelperProcess",
		"--",
		"--issue", "42",
		"--agent", "claude",
		"--claude-bin", agentStubPath,
		"--gh-bin", ghStubPath,
	}

	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir // Run inside the dummy repo
	cmd.Env = append(os.Environ(), "GHIR_TEST_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	outStr := string(output)

	if err != nil {
		t.Fatalf("ticket-runner failed with error: %v\nOutput:\n%s", err, outStr)
	}

	// 6. Assertions

	// Assert the log indicates success
	if !strings.Contains(outStr, "SUCCESS: #42 committed by runner") {
		t.Errorf("Expected runner to commit changes successfully, got output:\n%s", outStr)
	}

	// Assert the done file was created and contains the issue number
	doneFilePath := filepath.Join(dir, ".ticket-runs", ".completed")
	doneData, err := os.ReadFile(doneFilePath)
	if err != nil {
		t.Fatalf("Failed to read done file: %v", err)
	}
	if !strings.Contains(string(doneData), "42") {
		t.Errorf("Expected done file to contain '42', got: %q", string(doneData))
	}

	// Assert the git history has the new commit
	logOut := runCmdOutput(t, dir, "git", "log", "-1", "--pretty=format:%s")
	if !strings.Contains(logOut, "feat: implement #42 - Smoke Test Issue") {
		t.Errorf("Expected commit message to contain issue title, got: %q", logOut)
	}
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to run %s %v: %v\nOutput: %s", name, args, err, out)
	}
}

func runCmdOutput(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run %s %v: %v\nOutput: %s", name, args, err, out)
	}
	return string(out)
}

func TestIntegrationContinueOnError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.name", "Test User")
	runCmd(t, dir, "git", "config", "user.email", "test@example.com")

	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("fake-*\n.ticket-runs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, dir, "git", "add", "README.md", ".gitignore")
	runCmd(t, dir, "git", "commit", "-m", "Initial commit")

	ghStubPath := filepath.Join(dir, "fake-gh")
	ghStubScript := "#!/bin/sh\n" +
		"if [ \"$1\" = \"issue\" ] && [ \"$2\" = \"view\" ]; then\n" +
		"	if [ \"$3\" = \"42\" ]; then\n" +
		"		echo '{\"title\": \"Fail Issue\", \"body\": \"Fails.\"}'\n" +
		"		exit 0\n" +
		"	elif [ \"$3\" = \"43\" ]; then\n" +
		"		echo '{\"title\": \"Success Issue\", \"body\": \"Succeeds.\"}'\n" +
		"		exit 0\n" +
		"	fi\n" +
		"fi\n" +
		"if [ \"$1\" = \"issue\" ] && [ \"$2\" = \"list\" ]; then\n" +
		"	echo '[{\"number\": 42}, {\"number\": 43}]'\n" +
		"	exit 0\n" +
		"fi\n" +
		"echo \"Unexpected gh arguments: $@\" >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(ghStubPath, []byte(ghStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	agentStubPath := filepath.Join(dir, "fake-claude")
	agentStubScript := "#!/bin/sh\n" +
		"prompt=$(cat)\n" +
		"if echo \"$prompt\" | grep -q \"Fail Issue\"; then\n" +
		"	echo \"Agent fails.\"\n" +
		"	exit 1\n" +
		"fi\n" +
		"echo \"Modified by fake agent\" >> README.md\n" +
		"echo \"I have completed the task.\"\n" +
		"exit 0\n"
	if err := os.WriteFile(agentStubPath, []byte(agentStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmdArgs := []string{
		"-test.run=TestMainHelperProcess",
		"--",
		"--all-open",
		"--continue-on-error",
		"--agent", "claude",
		"--claude-bin", agentStubPath,
		"--gh-bin", ghStubPath,
	}

	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GHIR_TEST_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	outStr := string(output)

	if err == nil {
		t.Fatalf("Expected ticket-runner to exit with error due to failed issue 42, but it succeeded.\nOutput:\n%s", outStr)
	}

	if !strings.Contains(outStr, "Succeeded: 1") {
		t.Errorf("Expected 1 success, got output:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Failed: 1") {
		t.Errorf("Expected 1 failure, got output:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Failed issues: #42") {
		t.Errorf("Expected Failed issues: #42 in summary, got output:\n%s", outStr)
	}
}

func TestIntegrationIssuePRPerPassContinuesAfterNoChange(t *testing.T) {
	t.Parallel()

	dir, baseBranch := initPRStrategyRepo(t)
	ghStubPath, prLogPath := writeNormalPRGHStub(t, dir, map[string]string{
		"42": "No-op Issue",
		"43": "Real Issue",
	})
	agentStubPath := filepath.Join(dir, "fake-claude-pr")
	agentStubScript := "#!/bin/sh\n" +
		"prompt=$(cat)\n" +
		"if echo \"$prompt\" | grep -q \"No-op Issue\"; then\n" +
		"  echo \"No changes needed\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo \"pr per pass change\" >> README.md\n" +
		"echo \"Updated\"\n" +
		"exit 0\n"
	if err := os.WriteFile(agentStubPath, []byte(agentStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmdArgs := []string{
		"-test.run=TestMainHelperProcess",
		"--",
		"--issues", "42,43",
		"--strategy", "pr-per-pass",
		"--agent", "claude",
		"--claude-bin", agentStubPath,
		"--gh-bin", ghStubPath,
	}
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GHIR_TEST_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if err != nil {
		t.Fatalf("ghir pr-per-pass failed: %v\nOutput:\n%s", err, outStr)
	}

	doneData, readErr := os.ReadFile(filepath.Join(dir, ".ticket-runs", ".completed"))
	if readErr != nil {
		t.Fatalf("read done file: %v", readErr)
	}
	if !strings.Contains(string(doneData), "42") || !strings.Contains(string(doneData), "43") {
		t.Fatalf("expected both issues in done file, got %q", string(doneData))
	}

	prData, readErr := os.ReadFile(prLogPath)
	if readErr != nil {
		t.Fatalf("read PR log: %v", readErr)
	}
	lines := nonEmptyLines(string(prData))
	if got, want := len(lines), 1; got != want {
		t.Fatalf("expected one PR for the changed item, got %d (%q)", got, string(prData))
	}

	currentBranch := strings.TrimSpace(runCmdOutput(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"))
	if currentBranch != baseBranch {
		t.Fatalf("expected to return to %q, got %q", baseBranch, currentBranch)
	}
}

func TestIntegrationIssuePRChain(t *testing.T) {
	t.Parallel()

	dir, baseBranch := initPRStrategyRepo(t)
	ghStubPath, prLogPath := writeNormalPRGHStub(t, dir, map[string]string{
		"42": "Chain Issue One",
		"43": "Chain Issue Two",
	})
	agentStubPath := filepath.Join(dir, "fake-claude-chain")
	agentStubScript := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"echo \"chain change\" >> README.md\n" +
		"exit 0\n"
	if err := os.WriteFile(agentStubPath, []byte(agentStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmdArgs := []string{
		"-test.run=TestMainHelperProcess",
		"--",
		"--issues", "42,43",
		"--strategy", "pr-chain",
		"--agent", "claude",
		"--claude-bin", agentStubPath,
		"--gh-bin", ghStubPath,
	}
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GHIR_TEST_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if err != nil {
		t.Fatalf("ghir pr-chain failed: %v\nOutput:\n%s", err, outStr)
	}

	lines := nonEmptyLines(runFile(t, prLogPath))
	if got, want := len(lines), 2; got != want {
		t.Fatalf("expected two PRs, got %d (%q)", got, strings.Join(lines, "\n"))
	}
	firstBase, firstHead := parsePRLogLine(t, lines[0])
	secondBase, _ := parsePRLogLine(t, lines[1])
	if firstBase != "main" {
		t.Fatalf("expected first PR base main, got %q", firstBase)
	}
	if secondBase != firstHead {
		t.Fatalf("expected second PR to target %q, got %q", firstHead, secondBase)
	}

	currentBranch := strings.TrimSpace(runCmdOutput(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"))
	if currentBranch == baseBranch {
		t.Fatalf("expected chain run to leave HEAD off base branch %q", baseBranch)
	}
}

func TestIntegrationIssuePRAtEnd(t *testing.T) {
	t.Parallel()

	dir, baseBranch := initPRStrategyRepo(t)
	ghStubPath, prLogPath := writeNormalPRGHStub(t, dir, map[string]string{
		"42": "Batch Issue One",
		"43": "Batch Issue Two",
	})
	agentStubPath := filepath.Join(dir, "fake-claude-batch")
	agentStubScript := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"echo \"batch change\" >> README.md\n" +
		"exit 0\n"
	if err := os.WriteFile(agentStubPath, []byte(agentStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmdArgs := []string{
		"-test.run=TestMainHelperProcess",
		"--",
		"--issues", "42,43",
		"--strategy", "pr-at-end",
		"--agent", "claude",
		"--claude-bin", agentStubPath,
		"--gh-bin", ghStubPath,
	}
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GHIR_TEST_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if err != nil {
		t.Fatalf("ghir pr-at-end failed: %v\nOutput:\n%s", err, outStr)
	}

	lines := nonEmptyLines(runFile(t, prLogPath))
	if got, want := len(lines), 1; got != want {
		t.Fatalf("expected one batch PR, got %d (%q)", got, strings.Join(lines, "\n"))
	}

	doneData := runFile(t, filepath.Join(dir, ".ticket-runs", ".completed"))
	if !strings.Contains(doneData, "42") || !strings.Contains(doneData, "43") {
		t.Fatalf("expected both issues marked complete after batch PR, got %q", doneData)
	}

	currentBranch := strings.TrimSpace(runCmdOutput(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"))
	if currentBranch == baseBranch {
		t.Fatalf("expected batch run to leave HEAD off base branch %q", baseBranch)
	}
}

func TestIntegrationFilePRPerPass(t *testing.T) {
	t.Parallel()

	dir, baseBranch := initPRStrategyRepo(t)
	taskDir := filepath.Join(dir, "tasks")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(taskDir, "1.md")
	if err := os.WriteFile(taskPath, []byte("# Task One\n\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, dir, "git", "add", "tasks/1.md")
	runCmd(t, dir, "git", "commit", "-m", "Add task")

	ghStubPath, prLogPath := writeFilePRGHStub(t, dir)
	agentStubPath := filepath.Join(dir, "fake-claude-file-pr")
	agentStubScript := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"echo \"file strategy change\" >> README.md\n" +
		"exit 0\n"
	if err := os.WriteFile(agentStubPath, []byte(agentStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmdArgs := []string{
		"-test.run=TestMainHelperProcess",
		"--",
		"--files", "tasks/1.md",
		"--strategy", "pr-per-pass",
		"--agent", "claude",
		"--claude-bin", agentStubPath,
		"--gh-bin", ghStubPath,
	}
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GHIR_TEST_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if err != nil {
		t.Fatalf("ghir file pr-per-pass failed: %v\nOutput:\n%s", err, outStr)
	}

	lines := nonEmptyLines(runFile(t, prLogPath))
	if got, want := len(lines), 1; got != want {
		t.Fatalf("expected one file PR, got %d (%q)", got, strings.Join(lines, "\n"))
	}

	doneData := runFile(t, filepath.Join(dir, ".ticket-runs", ".completed"))
	if !strings.Contains(doneData, "tasks/1.md") {
		t.Fatalf("expected file path in done file, got %q", doneData)
	}

	currentBranch := strings.TrimSpace(runCmdOutput(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"))
	if currentBranch != baseBranch {
		t.Fatalf("expected to return to %q, got %q", baseBranch, currentBranch)
	}
}

func initPRStrategyRepo(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.name", "Test User")
	runCmd(t, dir, "git", "config", "user.email", "test@example.com")

	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("fake-*\n.ticket-runs/\n*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, dir, "git", "add", "README.md", ".gitignore")
	runCmd(t, dir, "git", "commit", "-m", "Initial commit")

	baseBranch := strings.TrimSpace(runCmdOutput(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"))
	remoteRoot := t.TempDir()
	remotePath := filepath.Join(remoteRoot, "origin.git")
	runCmd(t, remoteRoot, "git", "init", "--bare", remotePath)
	runCmd(t, dir, "git", "remote", "add", "origin", remotePath)
	return dir, baseBranch
}

func writeNormalPRGHStub(t *testing.T, dir string, issues map[string]string) (string, string) {
	t.Helper()

	prLogPath := filepath.Join(dir, "prs.log")
	var issueCases strings.Builder
	for number, title := range issues {
		issueCases.WriteString("if [ \"$3\" = \"" + number + "\" ]; then\n")
		issueCases.WriteString("  echo '{\"title\": \"" + title + "\", \"body\": \"Body for " + title + "\"}'\n")
		issueCases.WriteString("  exit 0\n")
		issueCases.WriteString("fi\n")
	}

	ghStubPath := filepath.Join(dir, "fake-gh-normal-pr")
	ghStubScript := "#!/bin/sh\n" +
		"if [ \"$1\" = \"issue\" ] && [ \"$2\" = \"view\" ]; then\n" +
		issueCases.String() +
		"fi\n" +
		"if [ \"$1\" = \"repo\" ] && [ \"$2\" = \"view\" ]; then\n" +
		"  echo \"main\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"pr\" ] && [ \"$2\" = \"create\" ]; then\n" +
		"  shift 2\n" +
		"  base=\"\"\n" +
		"  head=\"\"\n" +
		"  while [ $# -gt 0 ]; do\n" +
		"    case \"$1\" in\n" +
		"      --base) base=\"$2\"; shift 2 ;;\n" +
		"      --head) head=\"$2\"; shift 2 ;;\n" +
		"      *) shift ;;\n" +
		"    esac\n" +
		"  done\n" +
		"  echo \"base=$base head=$head\" >> \"" + prLogPath + "\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(ghStubPath, []byte(ghStubScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return ghStubPath, prLogPath
}

func writeFilePRGHStub(t *testing.T, dir string) (string, string) {
	t.Helper()

	prLogPath := filepath.Join(dir, "prs.log")
	ghStubPath := filepath.Join(dir, "fake-gh-file-pr")
	ghStubScript := "#!/bin/sh\n" +
		"if [ \"$1\" = \"repo\" ] && [ \"$2\" = \"view\" ]; then\n" +
		"  echo \"main\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"pr\" ] && [ \"$2\" = \"create\" ]; then\n" +
		"  echo \"file-pr\" >> \"" + prLogPath + "\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(ghStubPath, []byte(ghStubScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return ghStubPath, prLogPath
}

func runFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func parsePRLogLine(t *testing.T, line string) (string, string) {
	t.Helper()
	re := regexp.MustCompile(`base=([^ ]+) head=(.+)$`)
	match := re.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) != 3 {
		t.Fatalf("unexpected PR log line: %q", line)
	}
	return match[1], match[2]
}

func TestIntegrationImproveDirect(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.name", "Test User")
	runCmd(t, dir, "git", "config", "user.email", "test@example.com")

	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("fake-*\n.ticket-runs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, dir, "git", "add", "README.md", ".gitignore")
	runCmd(t, dir, "git", "commit", "-m", "Initial commit")

	agentStubPath := filepath.Join(dir, "fake-claude-improve")
	agentStubScript := "#!/bin/sh\n" +
		"# Append to README before reading prompt so tree is dirty when improve starts\n" +
		"echo \"// improved\" >> README.md\n" +
		"# Read prompt from stdin (unused in stub)\n" +
		"cat >/dev/null\n" +
		"echo \"Improvement pass running\" >&2\n" +
		"exit 0\n"
	if err := os.WriteFile(agentStubPath, []byte(agentStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmdArgs := []string{
		"-test.run=TestMainHelperProcess",
		"--",
		"improve",
		"--mode", "cleanup",
		"--iterations", "1",
		"--agent", "claude",
		"--claude-bin", agentStubPath,
	}

	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GHIR_TEST_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if err != nil {
		t.Fatalf("ghir improve direct failed: %v\nOutput:\n%s", err, outStr)
	}

	logDir := filepath.Join(dir, ".ticket-runs")
	matches, _ := filepath.Glob(filepath.Join(logDir, "improve-cleanup-1-*.log"))
	if len(matches) == 0 {
		t.Fatalf("expected improve log matching improve-cleanup-1-*.log in %s", logDir)
	}

	logOut := runCmdOutput(t, dir, "git", "log", "-1", "--pretty=format:%s")
	if !strings.Contains(logOut, "chore: cleanup pass 1") {
		t.Errorf("expected cleanup commit message, got: %q", logOut)
	}
}

func TestIntegrationImproveDirectCustomPromptFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.name", "Test User")
	runCmd(t, dir, "git", "config", "user.email", "test@example.com")

	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("fake-*\n.ticket-runs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	promptFilePath := filepath.Join(dir, "prompts", "improve.txt")
	if err := os.MkdirAll(filepath.Dir(promptFilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptFilePath, []byte("Custom improve prompt from file."), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, dir, "git", "add", "README.md", ".gitignore", "prompts/improve.txt")
	runCmd(t, dir, "git", "commit", "-m", "Initial commit")

	capturedPromptPath := filepath.Join(dir, "captured-prompt.txt")
	agentStubPath := filepath.Join(dir, "fake-claude-improve-custom")
	agentStubScript := "#!/bin/sh\n" +
		"cat >\"" + capturedPromptPath + "\"\n" +
		"echo \"// improved\" >> README.md\n" +
		"echo \"Improvement pass running\" >&2\n" +
		"exit 0\n"
	if err := os.WriteFile(agentStubPath, []byte(agentStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmdArgs := []string{
		"-test.run=TestMainHelperProcess",
		"--",
		"improve",
		"--prompt-file", "prompts/improve.txt",
		"--iterations", "1",
		"--agent", "claude",
		"--claude-bin", agentStubPath,
	}

	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GHIR_TEST_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if err != nil {
		t.Fatalf("ghir improve direct custom prompt failed: %v\nOutput:\n%s", err, outStr)
	}

	capturedPrompt, err := os.ReadFile(capturedPromptPath)
	if err != nil {
		t.Fatalf("read captured prompt: %v", err)
	}
	if string(capturedPrompt) != "Custom improve prompt from file." {
		t.Fatalf("captured prompt mismatch: got %q", string(capturedPrompt))
	}

	logDir := filepath.Join(dir, ".ticket-runs")
	matches, _ := filepath.Glob(filepath.Join(logDir, "improve-custom-1-*.log"))
	if len(matches) == 0 {
		t.Fatalf("expected improve log matching improve-custom-1-*.log in %s", logDir)
	}

	logOut := runCmdOutput(t, dir, "git", "log", "-1", "--pretty=format:%s")
	if !strings.Contains(logOut, "chore: custom prompt pass 1") {
		t.Errorf("expected custom prompt commit message, got: %q", logOut)
	}
}

func TestIntegrationImprovePRPerPass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.name", "Test User")
	runCmd(t, dir, "git", "config", "user.email", "test@example.com")

	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("fake-*\n.ticket-runs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, dir, "git", "add", "README.md", ".gitignore")
	runCmd(t, dir, "git", "commit", "-m", "Initial commit")

	baseBranch := strings.TrimSpace(runCmdOutput(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"))

	remoteRoot := t.TempDir()
	remotePath := filepath.Join(remoteRoot, "origin.git")
	runCmd(t, remoteRoot, "git", "init", "--bare", remotePath)
	runCmd(t, dir, "git", "remote", "add", "origin", remotePath)

	agentStubPath := filepath.Join(dir, "fake-claude-improve-pr")
	agentStubScript := "#!/bin/sh\n" +
		"# Append to README before reading prompt so tree is dirty when improve starts\n" +
		"echo \"// improved via pr\" >> README.md\n" +
		"# Read prompt from stdin (unused in stub)\n" +
		"cat >/dev/null\n" +
		"echo \"Improvement pass running\" >&2\n" +
		"exit 0\n"
	if err := os.WriteFile(agentStubPath, []byte(agentStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	ghStubPath := filepath.Join(dir, "fake-gh-improve")
	ghStubScript := "#!/bin/sh\n" +
		"if [ \"$1\" = \"repo\" ] && [ \"$2\" = \"view\" ]; then\n" +
		"  echo \"main\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"pr\" ] && [ \"$2\" = \"create\" ]; then\n" +
		"  echo \"PR created: $@\" >&2\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo \"Unexpected gh arguments: $@\" >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(ghStubPath, []byte(ghStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmdArgs := []string{
		"-test.run=TestMainHelperProcess",
		"--",
		"improve",
		"--mode", "cleanup",
		"--iterations", "1",
		"--strategy", "pr-per-pass",
		"--agent", "claude",
		"--claude-bin", agentStubPath,
		"--gh-bin", ghStubPath,
	}

	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GHIR_TEST_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if err != nil {
		t.Fatalf("ghir improve pr-per-pass failed: %v\nOutput:\n%s", err, outStr)
	}

	currentBranch := strings.TrimSpace(runCmdOutput(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"))
	if currentBranch != baseBranch {
		t.Errorf("expected to return to base branch %q, got %q", baseBranch, currentBranch)
	}
}

func TestIntegrationImprovePRChain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.name", "Test User")
	runCmd(t, dir, "git", "config", "user.email", "test@example.com")

	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("fake-*\n.ticket-runs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, dir, "git", "add", "README.md", ".gitignore")
	runCmd(t, dir, "git", "commit", "-m", "Initial commit")

	baseBranch := strings.TrimSpace(runCmdOutput(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"))

	remoteRoot := t.TempDir()
	remotePath := filepath.Join(remoteRoot, "origin.git")
	runCmd(t, remoteRoot, "git", "init", "--bare", remotePath)
	runCmd(t, dir, "git", "remote", "add", "origin", remotePath)

	agentStubPath := filepath.Join(dir, "fake-claude-improve-pr")
	agentStubScript := "#!/bin/sh\n" +
		"# Append to README before reading prompt so tree is dirty when improve starts\n" +
		"echo \"// improved via pr\" >> README.md\n" +
		"# Read prompt from stdin (unused in stub)\n" +
		"cat >/dev/null\n" +
		"echo \"Improvement pass running\" >&2\n" +
		"exit 0\n"
	if err := os.WriteFile(agentStubPath, []byte(agentStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	ghStubPath := filepath.Join(dir, "fake-gh-improve")
	ghStubScript := "#!/bin/sh\n" +
		"if [ \"$1\" = \"repo\" ] && [ \"$2\" = \"view\" ]; then\n" +
		"  echo \"main\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"pr\" ] && [ \"$2\" = \"create\" ]; then\n" +
		"  echo \"PR created: $@\" >&2\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo \"Unexpected gh arguments: $@\" >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(ghStubPath, []byte(ghStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmdArgs := []string{
		"-test.run=TestMainHelperProcess",
		"--",
		"improve",
		"--mode", "cleanup",
		"--iterations", "2",
		"--strategy", "pr-chain",
		"--agent", "claude",
		"--claude-bin", agentStubPath,
		"--gh-bin", ghStubPath,
	}

	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GHIR_TEST_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if err != nil {
		t.Fatalf("ghir improve pr-chain failed: %v\nOutput:\n%s", err, outStr)
	}

	currentBranch := strings.TrimSpace(runCmdOutput(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"))
	if currentBranch == baseBranch {
		t.Errorf("expected NOT to return to base branch %q, but got %q", baseBranch, currentBranch)
	}
}

func TestIntegrationImprovePRAtEnd(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.name", "Test User")
	runCmd(t, dir, "git", "config", "user.email", "test@example.com")

	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("fake-*\n.ticket-runs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, dir, "git", "add", "README.md", ".gitignore")
	runCmd(t, dir, "git", "commit", "-m", "Initial commit")

	baseBranch := strings.TrimSpace(runCmdOutput(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"))

	remoteRoot := t.TempDir()
	remotePath := filepath.Join(remoteRoot, "origin.git")
	runCmd(t, remoteRoot, "git", "init", "--bare", remotePath)
	runCmd(t, dir, "git", "remote", "add", "origin", remotePath)

	agentStubPath := filepath.Join(dir, "fake-claude-improve-pr")
	agentStubScript := "#!/bin/sh\n" +
		"# Append to README before reading prompt so tree is dirty when improve starts\n" +
		"echo \"// improved via pr\" >> README.md\n" +
		"# Read prompt from stdin (unused in stub)\n" +
		"cat >/dev/null\n" +
		"echo \"Improvement pass running\" >&2\n" +
		"exit 0\n"
	if err := os.WriteFile(agentStubPath, []byte(agentStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	ghStubPath := filepath.Join(dir, "fake-gh-improve")
	ghStubScript := "#!/bin/sh\n" +
		"if [ \"$1\" = \"repo\" ] && [ \"$2\" = \"view\" ]; then\n" +
		"  echo \"main\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"pr\" ] && [ \"$2\" = \"create\" ]; then\n" +
		"  echo \"PR created: $@\" >&2\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo \"Unexpected gh arguments: $@\" >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(ghStubPath, []byte(ghStubScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cmdArgs := []string{
		"-test.run=TestMainHelperProcess",
		"--",
		"improve",
		"--mode", "cleanup",
		"--iterations", "2",
		"--strategy", "pr-at-end",
		"--agent", "claude",
		"--claude-bin", agentStubPath,
		"--gh-bin", ghStubPath,
	}

	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GHIR_TEST_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if err != nil {
		t.Fatalf("ghir improve pr-at-end failed: %v\nOutput:\n%s", err, outStr)
	}

	currentBranch := strings.TrimSpace(runCmdOutput(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD"))
	if currentBranch == baseBranch {
		t.Errorf("expected NOT to return to base branch %q, but got %q", baseBranch, currentBranch)
	}
}
