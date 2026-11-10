package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFixAppendsMissingVarsToExistingExample(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env.example"), "PORT=3000\n")
	writeFile(t, filepath.Join(dir, "app.js"), "process.env.PORT; process.env.NEW_VAR; process.env.ANOTHER;\n")

	f := newFindings()
	if err := scan(dir, f); err != nil {
		t.Fatal(err)
	}
	added, err := applyFix(dir, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 || added[0] != "ANOTHER" || added[1] != "NEW_VAR" {
		t.Fatalf("expected [ANOTHER NEW_VAR], got %v", added)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".env.example"))
	got := string(data)
	if !strings.HasPrefix(got, "PORT=3000\n") || !strings.Contains(got, "NEW_VAR=\n") || !strings.Contains(got, "ANOTHER=\n") {
		t.Errorf("unexpected .env.example content:\n%s", got)
	}

	// Re-scanning must now be clean: fix is idempotent.
	f2 := newFindings()
	if err := scan(dir, f2); err != nil {
		t.Fatal(err)
	}
	if again, _ := applyFix(dir, f2); len(again) != 0 {
		t.Errorf("second fix should add nothing, added %v", again)
	}
}

func TestFixCreatesExampleWhenMissing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package x
var _ = os.Getenv("API_HOST")
`)
	f := newFindings()
	if err := scan(dir, f); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFix(dir, f); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".env.example"))
	if err != nil {
		t.Fatalf("expected .env.example to be created: %v", err)
	}
	if !strings.Contains(string(data), "API_HOST=\n") {
		t.Errorf("expected API_HOST= in created file, got:\n%s", data)
	}
}

func TestFixHandlesFileWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env.example"), "PORT=3000")
	writeFile(t, filepath.Join(dir, "app.js"), "process.env.PORT; process.env.EXTRA;\n")
	f := newFindings()
	_ = scan(dir, f)
	if _, err := applyFix(dir, f); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".env.example"))
	if !strings.Contains(string(data), "PORT=3000\n") {
		t.Errorf("existing last line must be newline-terminated before append:\n%s", data)
	}
}
