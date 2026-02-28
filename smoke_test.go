package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
