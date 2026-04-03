package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultSessionBufferSec  = 120
	countdownIntervalSeconds = 300
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type options struct {
	agentConfig
	DryRun          bool
	Strategy        string
	SingleIssue     string
	AllOpen         bool
	Label           string
	Force           bool
	Status          bool
	Reset           bool
	ResetIssue      string
	IssuesCSV       string
	IssuesFile      string
	Files           string
	AllFiles        string
	LogDir          string
	DoneFile        string
	PromptTemplate  string
	Help            bool
	Version         bool
	ContinueOnError bool
	Loop            bool
}

type issueResult int

const (
	resultSuccess issueResult = iota
	resultFailed
	resultRetry
)

// printErr writes an error message to stderr.
func printErr(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
}

// printParseErr writes an error message to stderr with an extra newline (e.g. before usage).
func printParseErr(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
}

// withRepoRoot finds the git repo root; on success it runs fn(repoRoot), exiting with code 1 on any error.
func withRepoRoot(fn func(repoRoot string) error) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		printErr(err)
		os.Exit(1)
	}
	if err := fn(repoRoot); err != nil {
		printErr(err)
		os.Exit(1)
	}
}

func main() {
	args := os.Args[1:]

	if len(args) > 0 {
		switch args[0] {
		case "improve":
			iopts, err := parseImproveArgs(args[1:])
			if err != nil {
				printParseErr(err)
				printImproveUsage()
				os.Exit(2)
			}
			if iopts.Help {
				printImproveUsage()
				return
			}
			withRepoRoot(func(repoRoot string) error {
				applyImproveRepoDefaults(&iopts, repoRoot)
				r, err := newImproveRunner(iopts, repoRoot)
				if err != nil {
					return err
				}
				return runImprove(r, iopts)
			})
			return
		case "tui":
			topts, err := parseTUIArgs(args[1:])
			if err != nil {
				printParseErr(err)
				printTUIUsage()
				os.Exit(2)
			}
			if topts.Help {
				printTUIUsage()
				return
			}
			withRepoRoot(func(repoRoot string) error {
				return runTUI(topts, repoRoot)
			})
			return
		}
	}

	opts, err := parseArgs(args)
	if err != nil {
		printParseErr(err)
		printUsage()
		os.Exit(2)
	}
	if opts.Help {
		printUsage()
		return
	}
	if opts.Version {
		fmt.Printf("ghir version %s (commit: %s, built at: %s)\n", version, commit, date)
		return
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		printErr(err)
		os.Exit(1)
	}

	applyRepoDefaults(&opts, repoRoot)

	r, err := newRunner(opts, repoRoot)
	if err != nil {
		printErr(err)
		os.Exit(1)
	}

	if err := r.preflightChecks(); err != nil {
		printErr(err)
		os.Exit(1)
	}

	if opts.Reset {
		if err := r.handleReset(); err != nil {
			printErr(err)
			os.Exit(1)
		}
		return
	}

	loop := func() bool {
		failed, err := runOnce(r)
		if err != nil {
			if r.opts.Loop && (strings.Contains(err.Error(), "no open issues found") || strings.Contains(err.Error(), "no .md files found")) {
				r.printf(r.colors.Yellow, "No issues found.\n")
				return true
			}
			printErr(err)
			return false
		}
		if !r.opts.Loop && failed > 0 {
			return false
		}
		return true
	}

	if opts.Loop {
		for {
			if !loop() {
				os.Exit(1)
			}
			r.printf(r.colors.Yellow, "Waiting 5 minutes before checking for new issues...\n")
			time.Sleep(time.Duration(countdownIntervalSeconds) * time.Second)
		}
	}

	if !loop() {
		os.Exit(1)
	}
}

func runOnce(r *runner) (int, error) {
	issues, err := r.loadIssues()
	if err != nil {
		return 0, err
	}

	if r.opts.Status {
		r.printStatus(issues)
		return 0, nil
	}

	r.printBanner(issues)

	var wg sync.WaitGroup
	for _, issue := range issues {
		if !r.isCompleted(issue) {
			wg.Add(1)
			go func(i string) {
				defer wg.Done()
				r.setIssueLabel(i, "ghir:queued")
			}(issue)
		}
	}
	wg.Wait()

	if r.opts.SingleIssue != "" {
		r.opts.Force = true
	}
	if r.opts.DryRun {
		return runOnceDirect(r, issues)
	}

	switch strategyOrDefault(r.opts.Strategy) {
	case "pr-per-pass":
		return runOncePRPerPass(r, issues)
	case "pr-chain":
		return runOncePRChain(r, issues)
	case "pr-at-end":
		return runOncePRAtEnd(r, issues)
	default:
		return runOnceDirect(r, issues)
	}
}
