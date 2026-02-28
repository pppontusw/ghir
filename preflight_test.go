package main

import (
	"os"
	"strings"
	"testing"
)

func TestCheckBinary(t *testing.T) {
	t.Parallel()

	r := &runner{}

	// Case 1: Existing binary (go should be present in the environment running tests)
	if err := r.checkBinary("go", "go"); err != nil {
		t.Errorf("checkBinary('go', 'go') failed: %v", err)
	}

	// Case 2: Non-existent binary
	if err := r.checkBinary("nonexistent", "nonexistent-binary-xyz"); err == nil {
		t.Error("checkBinary('nonexistent', ...) succeeded, expected error")
	} else if !strings.Contains(err.Error(), "missing required binary 'nonexistent'") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPreflightChecks(t *testing.T) {
	// We cannot parallelize this easily if we modify PATH, but we can rely on
	// specific binary paths in options which is safer.

	// Helper to create a dummy executable
	createDummyBin := func(name string) string {
		dir := t.TempDir()
		path := dir + "/" + name
		// Create an executable file (shell script)
		content := "#!/bin/sh\nexit 0"
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}

	dummyGH := createDummyBin("gh")
	dummyClaude := createDummyBin("claude")

	tests := []struct {
		name      string
		opts      options
		fileMode  bool
		wantError string
	}{
		{
			name: "all binaries present",
			opts: options{
				Agent:     "claude",
				ClaudeBin: dummyClaude,
				GHBin:     dummyGH,
			},
			fileMode: false,
		},
		{
			name: "missing gh in issue mode",
			opts: options{
				Agent:     "claude",
				ClaudeBin: dummyClaude,
				GHBin:     "/path/to/missing/gh",
			},
			fileMode:  false,
			wantError: "missing required binary 'gh'",
		},
		{
			name: "missing gh in file mode (should be ignored)",
			opts: options{
				Agent:     "claude",
				ClaudeBin: dummyClaude,
				GHBin:     "/path/to/missing/gh",
			},
			fileMode: true,
		},
		{
			name: "missing agent binary",
			opts: options{
				Agent:     "claude",
				ClaudeBin: "/path/to/missing/claude",
				GHBin:     dummyGH,
			},
			fileMode:  false,
			wantError: "missing required binary 'claude'",
		},
		{
			name: "missing specific agent binary (gemini)",
			opts: options{
				Agent:     "gemini",
				GeminiBin: "/path/to/missing/gemini",
				GHBin:     dummyGH,
			},
			fileMode:  false,
			wantError: "missing required binary 'gemini'",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			r := &runner{
				opts:     tt.opts,
				fileMode: tt.fileMode,
				colors:   palette{},
			}

			// We need to bypass the 'git' check in preflightChecks because we can't easily validly mock "git" in PATH
			// without affecting the test runner or using a complex setup.
			// However, r.checkBinary("git", "git") is hardcoded.
			// To test this effectively, we should probably make the git binary path configurable in runner or opts,
			// or just accept that "git" check will pass (since we are running tests, git is likely installed).
			// If git is NOT installed, this test will fail on "all binaries present" case too.
			//
			// If we want to test "missing git", we'd need to modify PATH.
			// For now, let's assume 'git' is present (since we are in a git repo) and focus on testing GHBin and AgentBin.

			err := r.preflightChecks()
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantError)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Errorf("unexpected error: got %q, want substring %q", err.Error(), tt.wantError)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
