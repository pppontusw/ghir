package tui

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type RunRequest struct {
	Executable string
	Args       []string
	Dir        string
	Env        []string
}

type RunStarter func(RunRequest) (runHandle, error)
type ActionRunner func(RunRequest) (string, error)

type runHandle struct {
	Events <-chan tea.Msg
	Stop   func() error
}

type runProcessLineMsg struct {
	Line string
}

type runProcessExitMsg struct {
	ExitCode int
	Err      error
}

type runProcessDrainedMsg struct{}

type runTickMsg struct {
	At time.Time
}

func defaultRunStarter(req RunRequest) (runHandle, error) {
	cmd := exec.Command(req.Executable, req.Args...)
	cmd.Dir = req.Dir
	if len(req.Env) > 0 {
		cmd.Env = req.Env
	}

	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer

	if err := cmd.Start(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return runHandle{}, err
	}

	events := make(chan tea.Msg, 64)
	scanDone := make(chan struct{})

	go func() {
		defer close(scanDone)

		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			events <- runProcessLineMsg{Line: scanner.Text()}
		}
	}()

	go func() {
		err := cmd.Wait()
		_ = writer.Close()
		<-scanDone
		_ = reader.Close()

		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}

		events <- runProcessExitMsg{ExitCode: exitCode, Err: err}
		close(events)
	}()

	return runHandle{
		Events: events,
		Stop: func() error {
			if cmd.Process == nil {
				return nil
			}
			if err := cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return err
			}
			return nil
		},
	}, nil
}

func defaultActionRunner(req RunRequest) (string, error) {
	cmd := exec.Command(req.Executable, req.Args...)
	cmd.Dir = req.Dir
	if len(req.Env) > 0 {
		cmd.Env = req.Env
	}

	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func waitForRunEvent(events <-chan tea.Msg) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return runProcessDrainedMsg{}
		}
		return msg
	}
}

func tickRun(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(at time.Time) tea.Msg {
		return runTickMsg{At: at}
	})
}
