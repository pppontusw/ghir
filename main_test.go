package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseArgsSupportedAgents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		agent string
	}{
		{name: "claude", agent: "claude"},
		{name: "codex", agent: "codex"},
		{name: "gemini", agent: "gemini"},
		{name: "cursor-agent", agent: "cursor-agent"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseArgs([]string{"--agent", tt.agent})
			if err != nil {
				t.Fatalf("parseArgs returned unexpected error: %v", err)
			}
			if opts.Agent != tt.agent {
				t.Fatalf("agent mismatch: got %q want %q", opts.Agent, tt.agent)
			}
		})
	}
}

func TestParseArgsModelParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		wantModel string
		wantErr   string
	}{
		{
			name:      "model set",
			args:      []string{"--agent", "codex", "--model", "gpt-5"},
			wantModel: "gpt-5",
		},
		{
			name:    "missing model value",
			args:    []string{"--model"},
			wantErr: "--model requires a value",
		},
		{
			name:    "missing model value before next flag",
			args:    []string{"--model", "--agent", "claude"},
			wantErr: "--model requires a value",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseArgs(tt.args)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("unexpected error: got %q want substring %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseArgs returned unexpected error: %v", err)
			}
			if opts.Model != tt.wantModel {
				t.Fatalf("model mismatch: got %q want %q", opts.Model, tt.wantModel)
			}
		})
	}
}

func TestParseArgsIssueAndResetValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		args           []string
		wantSingle     string
		wantReset      bool
		wantResetIssue string
		wantForce      bool
		wantErr        string
	}{
		{
			name:       "valid --issue",
			args:       []string{"--issue", "42"},
			wantSingle: "42",
		},
		{
			name:    "missing --issue value",
			args:    []string{"--issue"},
			wantErr: "--issue requires a value",
		},
		{
			name:    "missing --issue value before next flag",
			args:    []string{"--issue", "--force"},
			wantErr: "--issue requires a value",
		},
		{
			name:    "invalid --issue",
			args:    []string{"--issue", "abc"},
			wantErr: `--issue must be numeric: "abc"`,
		},
		{
			name:      "reset without issue",
			args:      []string{"--reset"},
			wantReset: true,
		},
		{
			name:           "reset with issue",
			args:           []string{"--reset", "99"},
			wantReset:      true,
			wantResetIssue: "99",
		},
		{
			name:      "reset with following flag",
			args:      []string{"--reset", "--force"},
			wantReset: true,
			wantForce: true,
		},
		{
			name:    "reset issue must be numeric",
			args:    []string{"--reset", "bad"},
			wantErr: `--reset issue must be numeric: "bad"`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseArgs(tt.args)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("unexpected error: got %q want substring %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseArgs returned unexpected error: %v", err)
			}
			if opts.SingleIssue != tt.wantSingle {
				t.Fatalf("single issue mismatch: got %q want %q", opts.SingleIssue, tt.wantSingle)
			}
			if opts.Reset != tt.wantReset {
				t.Fatalf("reset mismatch: got %v want %v", opts.Reset, tt.wantReset)
			}
			if opts.ResetIssue != tt.wantResetIssue {
				t.Fatalf("reset issue mismatch: got %q want %q", opts.ResetIssue, tt.wantResetIssue)
			}
			if opts.Force != tt.wantForce {
				t.Fatalf("force mismatch: got %v want %v", opts.Force, tt.wantForce)
			}
		})
	}
}

func TestParseArgsIssuesFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		wantCSV   string
		wantError string
	}{
		{
			name:    "set issues csv",
			args:    []string{"--issues", "1,2,3"},
			wantCSV: "1,2,3",
		},
		{
			name:      "missing value",
			args:      []string{"--issues"},
			wantError: "--issues requires a value",
		},
		{
			name:      "missing value before next flag",
			args:      []string{"--issues", "--force"},
			wantError: "--issues requires a value",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseArgs(tt.args)
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantError)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("unexpected error: got %q want substring %q", err.Error(), tt.wantError)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseArgs returned unexpected error: %v", err)
			}
			if opts.IssuesCSV != tt.wantCSV {
				t.Fatalf("issues csv mismatch: got %q want %q", opts.IssuesCSV, tt.wantCSV)
			}
		})
	}
}

func TestParseCSVIssuesValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		want      []string
		wantError string
	}{
		{
			name:  "valid csv with dedupe",
			input: "1, 2,1,3",
			want:  []string{"1", "2", "3"},
		},
		{
			name:      "invalid numeric value",
			input:     "1,abc,3",
			wantError: `invalid issue in --issues: "abc"`,
		},
		{
			name:      "no issues found",
			input:     " , , ",
			wantError: "no issues found in --issues",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseCSVIssues(tt.input)
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantError)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("unexpected error: got %q want substring %q", err.Error(), tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCSVIssues returned unexpected error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("issues mismatch: got %v want %v", got, tt.want)
			}
		})
	}
}

func TestLoadIssuesFromCSVValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		csv       string
		want      []string
		wantError string
	}{
		{
			name: "valid csv",
			csv:  "10,11",
			want: []string{"10", "11"},
		},
		{
			name:      "invalid csv issue id",
			csv:       "10,abc",
			wantError: `invalid issue in --issues: "abc"`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &runner{opts: options{IssuesCSV: tt.csv}}
			got, err := r.loadIssues()
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantError)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("unexpected error: got %q want substring %q", err.Error(), tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadIssues returned unexpected error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("issues mismatch: got %v want %v", got, tt.want)
			}
		})
	}
}

func TestParseArgsInvalidAgent(t *testing.T) {
	t.Parallel()

	_, err := parseArgs([]string{"--agent", "nope"})
	if err == nil {
		t.Fatal("expected error for invalid agent")
	}
	if !strings.Contains(err.Error(), "--agent must be one of: claude, codex, gemini, cursor-agent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetectSessionLimitByAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		agent    string
		log      string
		exitCode int
		retry    bool
	}{
		{
			name:     "claude retryable when reset text present",
			agent:    "claude",
			log:      "You hit your usage limit. It resets at 5:00 PM UTC.",
			exitCode: 0,
			retry:    true,
		},
		{
			name:     "claude non retryable for unrelated error",
			agent:    "claude",
			log:      "network timeout while contacting upstream",
			exitCode: 1,
			retry:    false,
		},
		{
			name:     "codex retryable for error event even with exit code 0",
			agent:    "codex",
			log:      `{"type":"error","code":"usage_limit_reached"}`,
			exitCode: 0,
			retry:    true,
		},
		{
			name:     "codex retryable for stderr limit text when command failed",
			agent:    "codex",
			log:      `usage limit reached, resets_in_seconds: 120, http 429`,
			exitCode: 1,
			retry:    true,
		},
		{
			name:     "codex non retryable on successful run with incidental limit text",
			agent:    "codex",
			log:      "table includes usage_limit_reached and resets_at fields for tests",
			exitCode: 0,
			retry:    false,
		},
		{
			name:     "gemini retryable when command failed with quota text",
			agent:    "gemini",
			log:      "TerminalQuotaError: quota exceeded, please wait",
			exitCode: 1,
			retry:    true,
		},
		{
			name:     "gemini retryable for is_error payload even with exit code 0",
			agent:    "gemini",
			log:      `{"is_error":true,"result":"TerminalQuotaError: quota exceeded"}`,
			exitCode: 0,
			retry:    true,
		},
		{
			name:     "gemini non retryable for unrelated error",
			agent:    "gemini",
			log:      "authentication failed",
			exitCode: 1,
			retry:    false,
		},
		{
			name:     "cursor agent is always non retryable even with limit text",
			agent:    "cursor-agent",
			log:      "usage_limit_reached resets_in_seconds: 120",
			exitCode: 1,
			retry:    false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := detectSessionLimit(tt.log, tt.agent, tt.exitCode); got != tt.retry {
				t.Fatalf("detectSessionLimit() = %v, want %v", got, tt.retry)
			}
		})
	}
}

func TestWaitDurationClaude(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		log         string
		now         time.Time
		bufferSec   int
		wantWaitSec int
		wantReset   time.Time
	}{
		{
			name:        "parses 24 hour reset time",
			log:         "You are out of usage. Resets at 16:30 UTC.",
			now:         time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC),
			bufferSec:   120,
			wantWaitSec: 5520,
			wantReset:   time.Date(2026, 1, 2, 16, 32, 0, 0, time.UTC),
		},
		{
			name:        "parses 12 hour reset time with minutes",
			log:         "Usage limit hit, resets at 3:05 pm",
			now:         time.Date(2026, 1, 2, 14, 55, 0, 0, time.UTC),
			bufferSec:   120,
			wantWaitSec: 720,
			wantReset:   time.Date(2026, 1, 2, 15, 7, 0, 0, time.UTC),
		},
		{
			name:        "rolls reset to next day when time already passed",
			log:         "hit your usage limit, resets at 12:10 am UTC",
			now:         time.Date(2026, 1, 2, 23, 50, 0, 0, time.UTC),
			bufferSec:   120,
			wantWaitSec: 1320,
			wantReset:   time.Date(2026, 1, 3, 0, 12, 0, 0, time.UTC),
		},
		{
			name:        "falls back when reset text missing",
			log:         "hit your usage limit; try again later",
			now:         time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC),
			bufferSec:   120,
			wantWaitSec: defaultFallbackWaitSec,
			wantReset:   time.Date(2026, 1, 2, 15, 30, 0, 0, time.UTC),
		},
		{
			name:        "falls back on malformed minute",
			log:         "usage limit exceeded, resets at 8:99 pm",
			now:         time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC),
			bufferSec:   120,
			wantWaitSec: defaultFallbackWaitSec,
			wantReset:   time.Date(2026, 1, 2, 15, 30, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotWait, gotReset := waitDurationClaude(tt.log, tt.now, tt.bufferSec)
			if gotWait != tt.wantWaitSec {
				t.Fatalf("waitDurationClaude() wait = %d, want %d", gotWait, tt.wantWaitSec)
			}
			if !gotReset.Equal(tt.wantReset) {
				t.Fatalf("waitDurationClaude() reset = %s, want %s", gotReset.UTC().Format(time.RFC3339), tt.wantReset.UTC().Format(time.RFC3339))
			}
		})
	}
}

func TestWaitDurationCodex(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)
	futureResetUnix := now.Add(20 * time.Minute).Unix()
	pastResetUnix := now.Add(-5 * time.Minute).Unix()

	tests := []struct {
		name        string
		log         string
		bufferSec   int
		wantWaitSec int
		wantReset   time.Time
	}{
		{
			name:        "uses resets_at when timestamp is in the future",
			log:         fmt.Sprintf(`{"code":"usage_limit_reached","resets_at": %d}`, futureResetUnix),
			bufferSec:   120,
			wantWaitSec: 1320,
			wantReset:   now.Add(22 * time.Minute),
		},
		{
			name:        "supports escaped resets_at key",
			log:         fmt.Sprintf(`{"message":"resets_at\": %d"}`, futureResetUnix),
			bufferSec:   120,
			wantWaitSec: 1320,
			wantReset:   now.Add(22 * time.Minute),
		},
		{
			name:        "falls through to resets_in_seconds when resets_at already passed",
			log:         fmt.Sprintf(`{"resets_at": %d, "resets_in_seconds": 90}`, pastResetUnix),
			bufferSec:   120,
			wantWaitSec: 210,
			wantReset:   now.Add(210 * time.Second),
		},
		{
			name:        "uses resets_in_seconds when present",
			log:         `usage limit; resets_in_seconds: 45`,
			bufferSec:   120,
			wantWaitSec: 165,
			wantReset:   now.Add(165 * time.Second),
		},
		{
			name:        "falls back on malformed values",
			log:         `usage limit; resets_in_seconds: nope`,
			bufferSec:   120,
			wantWaitSec: defaultFallbackWaitSec,
			wantReset:   now.Add(defaultFallbackWaitSec * time.Second),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotWait, gotReset := waitDurationCodex(tt.log, now, tt.bufferSec)
			if gotWait != tt.wantWaitSec {
				t.Fatalf("waitDurationCodex() wait = %d, want %d", gotWait, tt.wantWaitSec)
			}
			if !gotReset.Equal(tt.wantReset) {
				t.Fatalf("waitDurationCodex() reset = %s, want %s", gotReset.UTC().Format(time.RFC3339), tt.wantReset.UTC().Format(time.RFC3339))
			}
		})
	}
}

func TestWaitDurationGemini(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		log         string
		bufferSec   int
		wantWaitSec int
		wantReset   time.Time
	}{
		{
			name:        "parses hour and minute duration",
			log:         "rate limit reached, resets after 2h30m",
			bufferSec:   120,
			wantWaitSec: 9120,
			wantReset:   now.Add(9120 * time.Second),
		},
		{
			name:        "parses minute duration",
			log:         "quota exceeded; resets after 45m",
			bufferSec:   120,
			wantWaitSec: 2820,
			wantReset:   now.Add(2820 * time.Second),
		},
		{
			name:        "parses second duration",
			log:         "quota exceeded; resets after 30s",
			bufferSec:   120,
			wantWaitSec: 150,
			wantReset:   now.Add(150 * time.Second),
		},
		{
			name:        "falls back when duration is malformed",
			log:         "quota exceeded; resets after soon",
			bufferSec:   120,
			wantWaitSec: defaultFallbackWaitSec,
			wantReset:   now.Add(defaultFallbackWaitSec * time.Second),
		},
		{
			name:        "falls back when parsed duration is zero",
			log:         "quota exceeded; resets after 0m",
			bufferSec:   120,
			wantWaitSec: defaultFallbackWaitSec,
			wantReset:   now.Add(defaultFallbackWaitSec * time.Second),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotWait, gotReset := waitDurationGemini(tt.log, now, tt.bufferSec)
			if gotWait != tt.wantWaitSec {
				t.Fatalf("waitDurationGemini() wait = %d, want %d", gotWait, tt.wantWaitSec)
			}
			if !gotReset.Equal(tt.wantReset) {
				t.Fatalf("waitDurationGemini() reset = %s, want %s", gotReset.UTC().Format(time.RFC3339), tt.wantReset.UTC().Format(time.RFC3339))
			}
		})
	}
}

func TestMainInvalidFlagsExitNonZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "unknown option",
			args:    []string{"--not-a-flag"},
			wantErr: "unknown option: --not-a-flag",
		},
		{
			name:    "missing model value",
			args:    []string{"--model"},
			wantErr: "--model requires a value",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmdArgs := append([]string{"-test.run=TestMainHelperProcess", "--"}, tt.args...)
			cmd := exec.Command(os.Args[0], cmdArgs...)
			cmd.Env = append(os.Environ(), "GHIR_TEST_HELPER_PROCESS=1")

			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected non-zero exit, output: %s", string(output))
			}

			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("expected *exec.ExitError, got %T (%v)", err, err)
			}
			if exitErr.ExitCode() == 0 {
				t.Fatalf("expected non-zero exit code, got 0; output: %s", string(output))
			}
			if !strings.Contains(string(output), tt.wantErr) {
				t.Fatalf("output mismatch: got %q want substring %q", string(output), tt.wantErr)
			}
		})
	}
}

func TestMainHelperProcess(t *testing.T) {
	if os.Getenv("GHIR_TEST_HELPER_PROCESS") != "1" {
		return
	}

	idx := -1
	for i, arg := range os.Args {
		if arg == "--" {
			idx = i
			break
		}
	}
	if idx == -1 {
		os.Exit(3)
	}

	os.Args = append([]string{os.Args[0]}, os.Args[idx+1:]...)
	main()
	os.Exit(0)
}
