package main

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"ghir/streaming"
)

type consoleStreamWriter struct {
	out      io.Writer
	renderer streaming.Renderer
	pending  []byte
	mu       sync.Mutex
}

func newConsoleStreamWriter(out io.Writer, renderer streaming.Renderer) *consoleStreamWriter {
	return &consoleStreamWriter{
		out:      out,
		renderer: renderer,
	}
}

// trimTrailingCR returns b without a trailing '\r' if present.
func trimTrailingCR(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\r' {
		return b[:len(b)-1]
	}
	return b
}

func (w *consoleStreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pending = append(w.pending, p...)
	for {
		newlineIndex := bytes.IndexByte(w.pending, '\n')
		if newlineIndex < 0 {
			break
		}
		lineBytes := trimTrailingCR(w.pending[:newlineIndex])
		if err := w.emitLineLocked(string(lineBytes)); err != nil {
			return 0, err
		}
		w.pending = w.pending[newlineIndex+1:]
	}

	return len(p), nil
}

func (w *consoleStreamWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.pending) > 0 {
		remaining := trimTrailingCR(w.pending)
		if err := w.emitLineLocked(string(remaining)); err != nil {
			return err
		}
		w.pending = nil
	}

	for _, line := range w.renderer.FinalLines() {
		if _, err := fmt.Fprintln(w.out, line); err != nil {
			return err
		}
	}
	return nil
}

func (w *consoleStreamWriter) emitLineLocked(line string) error {
	for _, formattedLine := range w.renderer.ConsumeLine(line) {
		if _, err := fmt.Fprintln(w.out, formattedLine); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) newStreamRenderer() (streaming.Renderer, string) {
	return streaming.NewRenderer(r.opts.Agent, r.opts.StreamView)
}
