package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLoadFilePathsDeduplicatesResolvedRepoPaths(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	for _, rel := range []string{"tasks/10.md", "tasks/2.md"} {
		path := filepath.Join(repoRoot, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create parent dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(rel), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	r := &runner{repoRoot: repoRoot}
	got, err := r.loadFilePaths("tasks/10.md, " + filepath.Join(repoRoot, "tasks", "10.md") + ", ./tasks/../tasks/2.md")
	if err != nil {
		t.Fatalf("loadFilePaths returned unexpected error: %v", err)
	}

	want := []string{"tasks/10.md", "tasks/2.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("paths mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestLoadFilePathsMissingFile(t *testing.T) {
	t.Parallel()

	r := &runner{repoRoot: t.TempDir()}
	_, err := r.loadFilePaths("missing.md")
	if err == nil {
		t.Fatal("expected missing file error")
	}
	if got, want := err.Error(), "file not found: missing.md"; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestLoadAllFilesReturnsSortedMarkdownFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	tasksDir := filepath.Join(repoRoot, "tasks")
	if err := os.MkdirAll(filepath.Join(tasksDir, "nested"), 0o755); err != nil {
		t.Fatalf("create tasks dir: %v", err)
	}
	for _, name := range []string{"10.md", "2.md", "notes.txt", "nested/3.md", "README.MD"} {
		path := filepath.Join(tasksDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create parent dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	r := &runner{repoRoot: repoRoot}
	got, err := r.loadAllFiles("tasks")
	if err != nil {
		t.Fatalf("loadAllFiles returned unexpected error: %v", err)
	}

	want := []string{"tasks/2.md", "tasks/10.md", "tasks/README.MD"}
	if !slices.Equal(got, want) {
		t.Fatalf("paths mismatch:\n got: %v\nwant: %v", got, want)
	}
}
