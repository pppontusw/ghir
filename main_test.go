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

func TestParseArgsStreamView(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		wantView  string
		wantError string
	}{
		{
			name:     "default stream view is pretty",
			args:     []string{},
			wantView: streamViewPretty,
		},
		{
			name:     "explicit pretty",
			args:     []string{"--stream-view", "pretty"},
			wantView: streamViewPretty,
		},
		{
			name:     "explicit raw",
			args:     []string{"--stream-view", "raw"},
			wantView: streamViewRaw,
		},
		{
			name:      "invalid stream view",
			args:      []string{"--stream-view", "minimal"},
			wantError: "--stream-view must be one of: pretty, raw",
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
			if opts.StreamView != tt.wantView {
				t.Fatalf("stream view mismatch: got %q want %q", opts.StreamView, tt.wantView)
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
			name:           "reset with non-numeric value (file path)",
			args:           []string{"--reset", "tasks/foo.md"},
			wantReset:      true,
			wantResetIssue: "tasks/foo.md",
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

func TestParseArgsAllOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		want      bool
		wantLabel string
		wantErr   string
	}{
		{
			name: "flag present",
			args: []string{"--all-open"},
			want: true,
		},
		{
			name:      "flag and label present",
			args:      []string{"--all-open", "--label", "ghir"},
			want:      true,
			wantLabel: "ghir",
		},
		{
			name:      "label present without all-open",
			args:      []string{"--label", "bug"},
			want:      false,
			wantLabel: "bug",
		},
		{
			name: "flag absent",
			args: []string{},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs returned unexpected error: %v", err)
			}
			if opts.AllOpen != tt.want {
				t.Fatalf("AllOpen mismatch: got %v want %v", opts.AllOpen, tt.want)
			}
			if opts.Label != tt.wantLabel {
				t.Fatalf("Label mismatch: got %v want %v", opts.Label, tt.wantLabel)
			}
		})
	}
}

func TestParseArgsVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "version flag present",
			args: []string{"--version"},
			want: true,
		},
		{
			name: "version flag absent",
			args: []string{},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs returned unexpected error: %v", err)
			}
			if opts.Version != tt.want {
				t.Fatalf("Version mismatch: got %v want %v", opts.Version, tt.want)
			}
		})
	}
}

func TestParseArgsContinueOnError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "flag present",
			args: []string{"--continue-on-error"},
			want: true,
		},
		{
			name: "flag absent",
			args: []string{},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs returned unexpected error: %v", err)
			}
			if opts.ContinueOnError != tt.want {
				t.Fatalf("ContinueOnError mismatch: got %v want %v", opts.ContinueOnError, tt.want)
			}
		})
	}
}

func TestSortStringsNumeric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "mixed numeric",
			input: []string{"10", "1", "2"},
			want:  []string{"1", "2", "10"},
		},
		{
			name:  "mixed alphanumeric",
			input: []string{"a", "10", "1", "b"},
			want:  []string{"1", "10", "a", "b"},
		},
		{
			name:  "already sorted",
			input: []string{"1", "2", "3"},
			want:  []string{"1", "2", "3"},
		},
		{
			name:  "reverse sorted",
			input: []string{"3", "2", "1"},
			want:  []string{"1", "2", "3"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := make([]string, len(tt.input))
			copy(got, tt.input)
			sortStringsNumeric(got)

			if !slices.Equal(got, tt.want) {
				t.Fatalf("sortStringsNumeric mismatch: got %v want %v", got, tt.want)
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

func TestIssueMentionedInSubjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		subjects string
		issue    string
		want     bool
	}{
		{
			name:     "single subject matches issue",
			subjects: "feat: implement thing (closes #1)",
			issue:    "1",
			want:     true,
		},
		{
			name: "multi-commit range contains issue reference",
			subjects: strings.Join([]string{
				"fix: remove python cache artifacts from backend scaffold",
				"feat: scaffold backend and compose foundation (closes #1)",
			}, "\n"),
			issue: "1",
			want:  true,
		},
		{
			name:     "issue one does not match issue ten",
			subjects: "feat: closes #10",
			issue:    "1",
			want:     false,
		},
		{
			name:     "empty issue never matches",
			subjects: "feat: closes #1",
			issue:    "",
			want:     false,
		},
		{
			name:     "no subject mentions issue",
			subjects: "chore: cleanup",
			issue:    "1",
			want:     false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := issueMentionedInSubjects(tt.subjects, tt.issue); got != tt.want {
				t.Fatalf("issueMentionedInSubjects() = %v, want %v", got, tt.want)
			}
		})
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
			name:     "gemini retryable for no capacity 429",
			agent:    "gemini",
			log:      "RetryableQuotaError: No capacity available for model gemini-3-pro-preview on the server\n  cause: { code: 429 }",
			exitCode: 1,
			retry:    true,
		},
		{
			name:     "gemini retryable for 429 capacity in stack trace and JSON",
			agent:    "gemini",
			log:      "cause: { code: 429, message: 'No capacity available for model gemini-3-pro-preview on the server' }\n{\"session_id\":\"x\",\"error\":{\"message\":\"[object Object]\",\"code\":1}}",
			exitCode: 1,
			retry:    true,
		},
		{
			name:     "gemini retryable for real GaxiosError log with MODEL_CAPACITY_EXHAUSTED",
			agent:    "gemini",
			log:      "Attempt 1 failed with status 429. Retrying with backoff... GaxiosError: [{\"error\":{\"code\":429,\"message\":\"No capacity available for model gemini-3-pro-preview on the server\",\"reason\":\"rateLimitExceeded\"},\"status\":\"RESOURCE_EXHAUSTED\",\"reason\":\"MODEL_CAPACITY_EXHAUSTED\"}}]",
			exitCode: 1,
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

func TestDetectInternalServerError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		log  string
		want bool
	}{
		{
			name: "generic internal server error",
			log:  "Something went wrong. Internal Server Error.",
			want: true,
		},
		{
			name: "500 internal",
			log:  "HTTP 500 Internal Error",
			want: true,
		},
		{
			name: "502 bad gateway",
			log:  "Error: 502 Bad Gateway",
			want: true,
		},
		{
			name: "503 service unavailable",
			log:  "Service is overloaded, 503 Service Unavailable",
			want: true,
		},
		{
			name: "504 gateway timeout",
			log:  "Request timed out: 504 Gateway Timeout",
			want: true,
		},
		{
			name: "overloaded",
			log:  "Model is overloaded",
			want: true,
		},
		{
			name: "no error",
			log:  "Everything is fine",
			want: false,
		},
		{
			name: "different error",
			log:  "Syntax error in code",
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := detectInternalServerError(tt.log); got != tt.want {
				t.Fatalf("detectInternalServerError(%q) = %v, want %v", tt.log, got, tt.want)
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
		{
			name:        "no capacity 429 uses 15 min wait",
			log:         "No capacity available for model gemini-3-pro-preview on the server. RetryableQuotaError",
			bufferSec:   120,
			wantWaitSec: geminiCapacity429WaitSec + 120,
			wantReset:   now.Add(time.Duration(geminiCapacity429WaitSec+120) * time.Second),
		},
		{
			name:        "code 429 uses 15 min wait",
			log:         "Error: cause: { code: 429, message: 'No capacity available' }",
			bufferSec:   0,
			wantWaitSec: geminiCapacity429WaitSec,
			wantReset:   now.Add(time.Duration(geminiCapacity429WaitSec) * time.Second),
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

func TestNewStreamRenderer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		agent                 string
		streamView            string
		wantCodexPretty       bool
		wantCursorAgentPretty bool
		wantGeminiPretty      bool
		wantRaw               bool
		wantNoticeSubstr      string
	}{
		{
			name:            "codex pretty renderer for codex pretty view",
			agent:           "codex",
			streamView:      streamViewPretty,
			wantCodexPretty: true,
		},
		{
			name:                  "cursor-agent pretty renderer for cursor-agent pretty view",
			agent:                 "cursor-agent",
			streamView:            streamViewPretty,
			wantCursorAgentPretty: true,
		},
		{
			name:             "gemini pretty renderer for gemini pretty view",
			agent:            "gemini",
			streamView:       streamViewPretty,
			wantGeminiPretty: true,
		},
		{
			name:       "raw renderer for raw view",
			agent:      "codex",
			streamView: streamViewRaw,
			wantRaw:    true,
		},
		{
			name:             "non-codex pretty falls back to raw with notice",
			agent:            "claude",
			streamView:       streamViewPretty,
			wantRaw:          true,
			wantNoticeSubstr: "not implemented",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &runner{
				opts: options{
					Agent:      tt.agent,
					StreamView: tt.streamView,
				},
			}

			gotRenderer, gotNotice := r.newStreamRenderer()
			if tt.wantCodexPretty {
				if _, ok := gotRenderer.(*codexPrettyRenderer); !ok {
					t.Fatalf("renderer type mismatch: got %T want *codexPrettyRenderer", gotRenderer)
				}
			}
			if tt.wantCursorAgentPretty {
				if _, ok := gotRenderer.(*cursorAgentPrettyRenderer); !ok {
					t.Fatalf("renderer type mismatch: got %T want *cursorAgentPrettyRenderer", gotRenderer)
				}
			}
			if tt.wantGeminiPretty {
				if _, ok := gotRenderer.(*geminiPrettyRenderer); !ok {
					t.Fatalf("renderer type mismatch: got %T want *geminiPrettyRenderer", gotRenderer)
				}
			}
			if tt.wantRaw {
				if _, ok := gotRenderer.(*rawStreamRenderer); !ok {
					t.Fatalf("renderer type mismatch: got %T want *rawStreamRenderer", gotRenderer)
				}
			}
			if tt.wantNoticeSubstr == "" {
				if gotNotice != "" {
					t.Fatalf("expected no notice, got %q", gotNotice)
				}
				return
			}
			if !strings.Contains(gotNotice, tt.wantNoticeSubstr) {
				t.Fatalf("notice mismatch: got %q want substring %q", gotNotice, tt.wantNoticeSubstr)
			}
		})
	}
}

func TestCodexPrettyRenderer(t *testing.T) {
	t.Parallel()

	renderer := &codexPrettyRenderer{}

	t.Run("shows command start", func(t *testing.T) {
		t.Parallel()
		got := renderer.ConsumeLine(`{"type":"item.started","item":{"type":"command_execution","command":"echo hello"}}`)
		if len(got) != 1 || got[0] != "[cmd] echo hello" {
			t.Fatalf("unexpected output: %v", got)
		}
	})

	t.Run("suppresses successful command completion", func(t *testing.T) {
		t.Parallel()
		got := renderer.ConsumeLine(`{"type":"item.completed","item":{"type":"command_execution","command":"echo hello","status":"completed","exit_code":0}}`)
		if len(got) != 0 {
			t.Fatalf("expected no output, got %v", got)
		}
	})

	t.Run("shows failed command completion", func(t *testing.T) {
		t.Parallel()
		got := renderer.ConsumeLine(`{"type":"item.completed","item":{"type":"command_execution","command":"/bin/sh -lc \"exit 1\"","status":"failed","exit_code":1,"aggregated_output":"line 1\nline 2"}}`)
		if len(got) < 2 {
			t.Fatalf("expected multiline output, got %v", got)
		}
		if !strings.Contains(got[0], "[cmd failed exit=1]") {
			t.Fatalf("missing failure header: %v", got)
		}
		if !strings.Contains(got[1], "line 1") {
			t.Fatalf("missing output snippet: %v", got)
		}
	})

	t.Run("shows assistant message", func(t *testing.T) {
		t.Parallel()
		got := renderer.ConsumeLine(`{"type":"item.completed","item":{"type":"agent_message","text":"hello\nworld"}}`)
		if len(got) != 2 {
			t.Fatalf("unexpected line count: %v", got)
		}
		if got[0] != "[assistant] hello" {
			t.Fatalf("unexpected first line: %q", got[0])
		}
		if got[1] != "  world" {
			t.Fatalf("unexpected second line: %q", got[1])
		}
	})

	t.Run("passes non-json lines through", func(t *testing.T) {
		t.Parallel()
		got := renderer.ConsumeLine("plain text output")
		if len(got) != 1 || got[0] != "plain text output" {
			t.Fatalf("unexpected output: %v", got)
		}
	})
}

func TestCursorAgentPrettyRenderer(t *testing.T) {
	t.Parallel()

	renderer := &cursorAgentPrettyRenderer{}

	t.Run("shows success result with duration and content", func(t *testing.T) {
		t.Parallel()
		line := `{"type":"result","subtype":"success","is_error":false,"duration_ms":56885,"result":"Checking layout structure.\n\n## Summary\nFix applied."}`
		got := renderer.ConsumeLine(line)
		if len(got) < 3 {
			t.Fatalf("expected multiline output, got %v", got)
		}
		if !strings.Contains(got[0], "[done]") {
			t.Fatalf("missing [done] header: %q", got[0])
		}
		if !strings.Contains(got[0], "56.9") {
			t.Fatalf("missing duration: %q", got[0])
		}
		if !strings.Contains(got[1], "Checking layout structure") {
			t.Fatalf("missing result content: %q", got[1])
		}
		content := strings.Join(got, "\n")
		if !strings.Contains(content, "## Summary") {
			t.Fatalf("missing markdown content: %q", content)
		}
	})

	t.Run("shows error result", func(t *testing.T) {
		t.Parallel()
		got := renderer.ConsumeLine(`{"type":"result","subtype":"error","is_error":true,"result":"Something went wrong"}`)
		if len(got) < 2 {
			t.Fatalf("expected output, got %v", got)
		}
		if got[0] != "[error] error" {
			t.Fatalf("unexpected error header: %q", got[0])
		}
		if !strings.Contains(got[1], "Something went wrong") {
			t.Fatalf("missing error content: %q", got[1])
		}
	})

	t.Run("suppresses non-result events", func(t *testing.T) {
		t.Parallel()
		got := renderer.ConsumeLine(`{"type":"item.started","item":{"type":"command_execution"}}`)
		if len(got) != 0 {
			t.Fatalf("expected no output for non-result, got %v", got)
		}
	})

	t.Run("passes non-json lines through", func(t *testing.T) {
		t.Parallel()
		got := renderer.ConsumeLine("plain text output")
		if len(got) != 1 || got[0] != "plain text output" {
			t.Fatalf("unexpected output: %v", got)
		}
	})
}

func TestGeminiPrettyRenderer(t *testing.T) {
	t.Parallel()

	t.Run("suppresses YOLO and credentials", func(t *testing.T) {
		t.Parallel()
		r := &geminiPrettyRenderer{}
		if got := r.ConsumeLine("YOLO mode is enabled. All tool calls will be automatically approved."); got != nil {
			t.Fatalf("expected suppress, got %v", got)
		}
		if got := r.ConsumeLine("Loaded cached credentials."); got != nil {
			t.Fatalf("expected suppress, got %v", got)
		}
	})

	t.Run("passes through tool errors", func(t *testing.T) {
		t.Parallel()
		r := &geminiPrettyRenderer{}
		line := "Error executing tool replace: Error: Failed to edit, expected 4 occurrences but found 1."
		got := r.ConsumeLine(line)
		if len(got) != 1 || got[0] != line {
			t.Fatalf("expected tool error line, got %v", got)
		}
	})

	t.Run("formats single-line result JSON", func(t *testing.T) {
		t.Parallel()
		r := &geminiPrettyRenderer{}
		line := `{"session_id":"abc","response":"Done.","stats":{"models":{"m1":{"api":{"totalRequests":2,"totalErrors":0,"totalLatencyMs":5000},"tokens":{"input":100,"prompt":200,"candidates":50,"total":350,"cached":0,"thoughts":0,"tool":0}}},"tools":{"totalCalls":5,"totalSuccess":5,"totalFail":0,"totalDurationMs":1200},"files":{"totalLinesAdded":10,"totalLinesRemoved":2}}}`
		got := r.ConsumeLine(line)
		if len(got) < 3 {
			t.Fatalf("expected formatted lines, got %v", got)
		}
		joined := strings.Join(got, "\n")
		if !strings.Contains(joined, "[assistant]") {
			t.Fatalf("missing [assistant] in %s", joined)
		}
		if !strings.Contains(joined, "tokens:") {
			t.Fatalf("missing tokens stats in %s", joined)
		}
		if !strings.Contains(joined, "tools:") {
			t.Fatalf("missing tools stats in %s", joined)
		}
		if !strings.Contains(joined, "files:") {
			t.Fatalf("missing files stats in %s", joined)
		}
	})
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

func TestParseArgsFileFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		wantFiles    string
		wantAllFiles string
		wantErr      string
	}{
		{
			name:      "files flag",
			args:      []string{"--files", "a.md,b.md"},
			wantFiles: "a.md,b.md",
		},
		{
			name:         "all-files flag",
			args:         []string{"--all-files", "tasks"},
			wantAllFiles: "tasks",
		},
		{
			name:    "missing files value",
			args:    []string{"--files"},
			wantErr: "--files requires a value",
		},
		{
			name:    "missing all-files value",
			args:    []string{"--all-files"},
			wantErr: "--all-files requires a value",
		},
		{
			name:    "files with issue is invalid",
			args:    []string{"--files", "a.md", "--issue", "1"},
			wantErr: "--files/--all-files cannot be combined with --issue/--all-open/--issues/--issues-file",
		},
		{
			name:    "all-files with all-open is invalid",
			args:    []string{"--all-files", "tasks", "--all-open"},
			wantErr: "--files/--all-files cannot be combined with --issue/--all-open/--issues/--issues-file",
		},
		{
			name:    "files with issues csv is invalid",
			args:    []string{"--files", "a.md", "--issues", "1,2"},
			wantErr: "--files/--all-files cannot be combined with --issue/--all-open/--issues/--issues-file",
		},
		{
			name:    "all-files with issues-file is invalid",
			args:    []string{"--all-files", "tasks", "--issues-file", "issues.txt"},
			wantErr: "--files/--all-files cannot be combined with --issue/--all-open/--issues/--issues-file",
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
			if opts.Files != tt.wantFiles {
				t.Fatalf("files mismatch: got %q want %q", opts.Files, tt.wantFiles)
			}
			if opts.AllFiles != tt.wantAllFiles {
				t.Fatalf("all-files mismatch: got %q want %q", opts.AllFiles, tt.wantAllFiles)
			}
		})
	}
}

func TestLoadIssuesFromFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create test files
	for _, name := range []string{"task1.md", "task2.md"} {
		if err := os.WriteFile(fmt.Sprintf("%s/%s", dir, name), []byte("# "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r := &runner{
		opts:     options{Files: "task1.md,task2.md"},
		repoRoot: dir,
	}

	got, err := r.loadIssues()
	if err != nil {
		t.Fatalf("loadIssues returned unexpected error: %v", err)
	}

	want := []string{"task1.md", "task2.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("issues mismatch: got %v want %v", got, want)
	}
}

func TestLoadIssuesFromFilesDedup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(fmt.Sprintf("%s/a.md", dir), []byte("# a"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &runner{
		opts:     options{Files: "a.md,a.md"},
		repoRoot: dir,
	}

	got, err := r.loadIssues()
	if err != nil {
		t.Fatalf("loadIssues returned unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(got), got)
	}
}

func TestLoadIssuesFromFilesMissing(t *testing.T) {
	t.Parallel()

	r := &runner{
		opts:     options{Files: "nonexistent.md"},
		repoRoot: t.TempDir(),
	}

	_, err := r.loadIssues()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "file not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadIssuesFromAllFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tasksDir := fmt.Sprintf("%s/tasks", dir)
	if err := os.Mkdir(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create files with numeric names to test sort order
	for _, name := range []string{"10.md", "2.md", "1.md", "readme.txt"} {
		if err := os.WriteFile(fmt.Sprintf("%s/%s", tasksDir, name), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r := &runner{
		opts:     options{AllFiles: "tasks"},
		repoRoot: dir,
	}

	got, err := r.loadIssues()
	if err != nil {
		t.Fatalf("loadIssues returned unexpected error: %v", err)
	}

	// Should filter out readme.txt and sort numerically
	want := []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("issues mismatch: got %v want %v", got, want)
	}
}

func TestLoadIssuesFromAllFilesEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	emptyDir := fmt.Sprintf("%s/empty", dir)
	if err := os.Mkdir(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}

	r := &runner{
		opts:     options{AllFiles: "empty"},
		repoRoot: dir,
	}

	_, err := r.loadIssues()
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
	if !strings.Contains(err.Error(), "no .md files found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchFileDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantTitle string
		wantBody  string
	}{
		{
			name:      "with H1 title",
			content:   "# Add authentication\n\nImplement JWT auth for the API.\n\n## Details\n\nUse RS256.",
			wantTitle: "Add authentication",
			wantBody:  "Implement JWT auth for the API.\n\n## Details\n\nUse RS256.",
		},
		{
			name:      "without H1 title uses filename",
			content:   "Just some content\nwith no heading.",
			wantTitle: "task",
			wantBody:  "Just some content\nwith no heading.",
		},
		{
			name:      "H1 not on first line",
			content:   "Some preamble\n# The Real Title\n\nBody here.",
			wantTitle: "The Real Title",
			wantBody:  "Body here.",
		},
		{
			name:      "empty body after title",
			content:   "# Title Only",
			wantTitle: "Title Only",
			wantBody:  "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			filePath := fmt.Sprintf("%s/task.md", dir)
			if err := os.WriteFile(filePath, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}

			r := &runner{repoRoot: dir, fileMode: true}
			details, err := r.fetchIssueDetails("task.md")
			if err != nil {
				t.Fatalf("fetchIssueDetails returned unexpected error: %v", err)
			}
			if details.Title != tt.wantTitle {
				t.Fatalf("title mismatch: got %q want %q", details.Title, tt.wantTitle)
			}
			if details.Body != tt.wantBody {
				t.Fatalf("body mismatch: got %q want %q", details.Body, tt.wantBody)
			}
		})
	}
}

func TestBuildPromptFileMode(t *testing.T) {
	t.Parallel()

	r := &runner{fileMode: true}
	details := issueDetails{Title: "Add auth", Body: "Implement JWT."}
	prompt, err := r.buildPrompt("tasks/auth.md", details, false)
	if err != nil {
		t.Fatalf("buildPrompt returned unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "tasks/auth.md") {
		t.Fatalf("prompt should contain file path, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Add auth") {
		t.Fatalf("prompt should contain title, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Implement JWT.") {
		t.Fatalf("prompt should contain body, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "GitHub issue") {
		t.Fatalf("file mode prompt should not mention GitHub issue, got:\n%s", prompt)
	}
}

func TestBuildPromptIssueModeUnchanged(t *testing.T) {
	t.Parallel()

	r := &runner{fileMode: false}
	details := issueDetails{Title: "Fix bug", Body: "Something is broken."}
	prompt, err := r.buildPrompt("42", details, false)
	if err != nil {
		t.Fatalf("buildPrompt returned unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "#42") {
		t.Fatalf("issue mode prompt should contain #42, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "GitHub issue") {
		t.Fatalf("issue mode prompt should mention GitHub issue, got:\n%s", prompt)
	}
}

func TestLogFileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fileMode bool
		issue    string
		want     string
	}{
		{
			name:     "issue mode",
			fileMode: false,
			issue:    "42",
			want:     "42.log",
		},
		{
			name:     "file mode simple",
			fileMode: true,
			issue:    "task.md",
			want:     "task.md.log",
		},
		{
			name:     "file mode with directory",
			fileMode: true,
			issue:    "tasks/add-auth.md",
			want:     "tasks__add-auth.md.log",
		},
		{
			name:     "file mode nested directory",
			fileMode: true,
			issue:    "a/b/c.md",
			want:     "a__b__c.md.log",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &runner{fileMode: tt.fileMode}
			got := r.logFileName(tt.issue)
			if got != tt.want {
				t.Fatalf("logFileName mismatch: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestIssueLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fileMode bool
		issue    string
		want     string
	}{
		{
			name:     "issue mode",
			fileMode: false,
			issue:    "42",
			want:     "#42",
		},
		{
			name:     "file mode",
			fileMode: true,
			issue:    "tasks/add-auth.md",
			want:     "tasks/add-auth.md",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &runner{fileMode: tt.fileMode}
			got := r.issueLabel(tt.issue)
			if got != tt.want {
				t.Fatalf("issueLabel mismatch: got %q want %q", got, tt.want)
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

func TestParseArgsLoop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "flag present",
			args: []string{"--loop", "--all-open"},
			want: true,
		},
		{
			name: "flag absent",
			args: []string{},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs returned unexpected error: %v", err)
			}
			if opts.Loop != tt.want {
				t.Fatalf("Loop mismatch: got %v want %v", opts.Loop, tt.want)
			}
		})
	}
}

func TestParseArgsLoopValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "loop with all-open",
			args:    []string{"--loop", "--all-open"},
			wantErr: "",
		},
		{
			name:    "loop with all-files",
			args:    []string{"--loop", "--all-files", "dir"},
			wantErr: "",
		},
		{
			name:    "loop without all-open or all-files",
			args:    []string{"--loop"},
			wantErr: "--loop requires either --all-open or --all-files",
		},
		{
			name:    "loop with files (invalid)",
			args:    []string{"--loop", "--files", "a.md"},
			wantErr: "--loop requires either --all-open or --all-files",
		},
		{
			name:    "loop with issue (invalid)",
			args:    []string{"--loop", "--issue", "1"},
			wantErr: "--loop requires either --all-open or --all-files",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseArgs(tt.args)
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
		})
	}
}
