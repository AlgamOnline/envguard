package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const exampleFileName = ".env.example"

// applyFix appends every variable used in code but missing from .env.example
// to that file (with an empty value, never a guessed one), creating it in
// root if none exists. It returns the sorted names that were added and
// updates f.exampleEnv so later reporting reflects the fix. Idempotent.
func applyFix(root string, f *findings) ([]string, error) {
	var missing []string
	for _, name := range sortedKeys(f.usedVars) {
		if _, ok := f.exampleEnv[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}

	path := f.examplePath
	if path == "" {
		path = filepath.Join(root, exampleFileName)
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var b strings.Builder
	b.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}
	b.WriteString("# added by envguard -fix: fill in values\n")
	for _, name := range missing {
		b.WriteString(name + "=\n")
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", path, err)
	}

	for _, name := range missing {
		f.exampleEnv[name] = ""
	}
	sort.Strings(missing)
	return missing, nil
}
