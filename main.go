// envguard - a CLI that finds environment-variable drift in a codebase.
//
// Problem it solves: "works on my machine" bugs caused by env vars that are
// used in code but never documented in .env.example, or documented but
// missing from the developer's local .env. It also flags secret-looking
// values that are about to be committed in a tracked .env file.
//
// Usage:
//
//	envguard [path]            scan a project (default ".")
//	envguard -ci [path]        same, but exit non-zero on any finding (for CI)
//
// Supports JS/TS (process.env.X), Go (os.Getenv/os.LookupEnv), and
// Python (os.environ / os.getenv) source files, plus .env / .env.example
// / .env.sample files.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	reJSEnv    = regexp.MustCompile(`process\.env\.([A-Za-z_][A-Za-z0-9_]*)`)
	reJSEnvIdx = regexp.MustCompile(`process\.env\[['"]([A-Za-z_][A-Za-z0-9_]*)['"]\]`)
	// reJSEnvDestructure matches `const { A, B: alias, C = "default" } = process.env`
	// (and `let`/`var`), a very common pattern that plain process.env.X scanning
	// misses entirely, producing false "undocumented var" negatives.
	reJSEnvDestructure  = regexp.MustCompile(`(?:const|let|var)\s*\{\s*([^}]+?)\s*\}\s*=\s*process\.env\b`)
	reJSDestructureName = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)`)
	reGoEnv             = regexp.MustCompile(`os\.(?:Getenv|LookupEnv|MustGetenv)\(\s*"([A-Za-z_][A-Za-z0-9_]*)"\s*\)`)
	rePyEnv1            = regexp.MustCompile(`os\.environ\[\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]\s*\]`)
	rePyEnv2            = regexp.MustCompile(`os\.environ\.get\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`)
	rePyEnv3            = regexp.MustCompile(`os\.getenv\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`)
	reEnvFile           = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)
	skipDirs            = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, ".next": true, "__pycache__": true, ".venv": true, "venv": true}
	secretHints         = regexp.MustCompile(`(?i)(key|secret|token|password|pass|pwd|api)`)
	sourceExts          = map[string]bool{".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".mjs": true, ".cjs": true, ".go": true, ".py": true}
)

type findings struct {
	usedVars     map[string][]string // var -> files that reference it
	exampleEnv   map[string]string   // var -> raw value in .env.example
	localEnv     map[string]string   // var -> raw value in .env
	trackedEnv   bool                // .env is tracked by git
	secretsInEnv []string
}

func main() {
	ci := flag.Bool("ci", false, "exit non-zero if any drift or risk is found")
	asJSON := flag.Bool("json", false, "output machine-readable JSON instead of text (for CI/tooling integration)")
	flag.Parse()

	root := "."
	if args := flag.Args(); len(args) > 0 {
		root = args[0]
	}

	f := &findings{
		usedVars:   map[string][]string{},
		exampleEnv: map[string]string{},
		localEnv:   map[string]string{},
	}

	if err := scan(root, f); err != nil {
		fmt.Fprintln(os.Stderr, "envguard: error:", err)
		os.Exit(2)
	}

	var issues int
	if *asJSON {
		issues = reportJSON(f)
	} else {
		issues = report(f)
	}

	if *ci && issues > 0 {
		os.Exit(1)
	}
}

// jsonReport is the machine-readable shape emitted by -json, designed to be
// easy to consume from CI scripts, GitHub Actions annotations, or other
// tooling without scraping the human-readable text output.
type jsonReport struct {
	MissingFromExample []string            `json:"missing_from_example"`
	MissingFromEnv     []string            `json:"missing_from_env"`
	Unused             []string            `json:"unused"`
	EnvTrackedByGit    bool                `json:"env_tracked_by_git"`
	SecretsSetLocally  []string            `json:"secrets_set_locally"`
	UsedVarLocations   map[string][]string `json:"used_var_locations,omitempty"`
	IssueCount         int                 `json:"issue_count"`
}

// buildReport computes the same findings as report() but returns them as a
// struct instead of printing, so both the text and JSON renderers share one
// source of truth.
func buildReport(f *findings) jsonReport {
	r := jsonReport{UsedVarLocations: f.usedVars}

	for _, name := range sortedKeys(f.usedVars) {
		if _, ok := f.exampleEnv[name]; !ok {
			r.MissingFromExample = append(r.MissingFromExample, name)
		}
	}

	if len(f.localEnv) > 0 {
		for name := range f.exampleEnv {
			if _, ok := f.localEnv[name]; !ok {
				r.MissingFromEnv = append(r.MissingFromEnv, name)
			}
		}
		sort.Strings(r.MissingFromEnv)
	}

	for name := range f.exampleEnv {
		if _, ok := f.usedVars[name]; !ok {
			r.Unused = append(r.Unused, name)
		}
	}
	sort.Strings(r.Unused)

	r.EnvTrackedByGit = f.trackedEnv
	r.SecretsSetLocally = append([]string{}, f.secretsInEnv...)
	sort.Strings(r.SecretsSetLocally)

	r.IssueCount = len(r.MissingFromExample) + len(r.MissingFromEnv) + len(r.Unused)
	if r.EnvTrackedByGit {
		r.IssueCount++
	}
	return r
}

// reportJSON prints the findings as JSON to stdout and returns the issue
// count (unused vars and secret hints are informational, not counted as
// issues, matching the text report's exit-code semantics).
func reportJSON(f *findings) int {
	r := buildReport(f)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		fmt.Fprintln(os.Stderr, "envguard: failed to encode JSON:", err)
		return r.IssueCount
	}
	return r.IssueCount
}

func scan(root string, f *findings) error {
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		base := info.Name()
		if info.IsDir() {
			if skipDirs[base] {
				return filepath.SkipDir
			}
			return nil
		}

		switch {
		case base == ".env" || strings.HasPrefix(base, ".env.") && !strings.Contains(base, "example") && !strings.Contains(base, "sample") && !strings.Contains(base, "template"):
			if base == ".env" {
				parseEnvFile(path, f.localEnv)
			}
		case strings.Contains(base, "example") || strings.Contains(base, "sample") || strings.Contains(base, "template"):
			if strings.HasPrefix(base, ".env") {
				parseEnvFile(path, f.exampleEnv)
			}
		}

		ext := filepath.Ext(base)
		if sourceExts[ext] {
			scanSource(path, ext, f)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// is .env tracked by git?
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		out, _ := exec.Command("git", "-C", root, "ls-files", ".env").Output()
		if strings.TrimSpace(string(out)) != "" {
			f.trackedEnv = true
		}
	}

	for k, v := range f.localEnv {
		if v != "" && secretHints.MatchString(k) {
			f.secretsInEnv = append(f.secretsInEnv, k)
		}
	}
	return nil
}

func parseEnvFile(path string, dst map[string]string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := reEnvFile.FindStringSubmatch(trimmed); m != nil {
			dst[m[1]] = strings.Trim(m[2], `"'`)
		}
	}
}

func scanSource(path, ext string, f *findings) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	content := string(data)
	var matches [][]string

	switch ext {
	case ".go":
		matches = reGoEnv.FindAllStringSubmatch(content, -1)
	case ".py":
		matches = append(matches, rePyEnv1.FindAllStringSubmatch(content, -1)...)
		matches = append(matches, rePyEnv2.FindAllStringSubmatch(content, -1)...)
		matches = append(matches, rePyEnv3.FindAllStringSubmatch(content, -1)...)
	default: // js/ts family
		matches = append(matches, reJSEnv.FindAllStringSubmatch(content, -1)...)
		matches = append(matches, reJSEnvIdx.FindAllStringSubmatch(content, -1)...)
		for _, name := range extractDestructuredNames(content) {
			f.usedVars[name] = appendUnique(f.usedVars[name], path)
		}
	}

	for _, m := range matches {
		name := m[1]
		f.usedVars[name] = appendUnique(f.usedVars[name], path)
	}
}

// extractDestructuredNames finds env var names pulled out via object
// destructuring, e.g. `const { PORT, DB_URL: url, TIMEOUT = 30 } = process.env`.
// Only the real env var name (left of any `: alias` or `= default`) is kept.
func extractDestructuredNames(content string) []string {
	var names []string
	for _, block := range reJSEnvDestructure.FindAllStringSubmatch(content, -1) {
		for _, field := range strings.Split(block[1], ",") {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			if m := reJSDestructureName.FindStringSubmatch(field); m != nil {
				names = append(names, m[1])
			}
		}
	}
	return names
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func report(f *findings) int {
	issues := 0

	fmt.Println("envguard — environment variable drift report")
	fmt.Println(strings.Repeat("=", 48))

	// 1. used in code but missing from .env.example
	var undocumented []string
	for _, name := range sortedKeys(f.usedVars) {
		if _, ok := f.exampleEnv[name]; !ok {
			undocumented = append(undocumented, name)
		}
	}
	if len(undocumented) > 0 {
		issues += len(undocumented)
		fmt.Printf("\n[MISSING FROM .env.example] (%d)\n", len(undocumented))
		for _, name := range undocumented {
			fmt.Printf("  - %-30s used in %s\n", name, strings.Join(f.usedVars[name], ", "))
		}
	} else {
		fmt.Println("\n[OK] every env var used in code is documented in .env.example")
	}

	// 2. documented in .env.example but not set in local .env
	if len(f.localEnv) > 0 {
		var missingLocal []string
		for name := range f.exampleEnv {
			if _, ok := f.localEnv[name]; !ok {
				missingLocal = append(missingLocal, name)
			}
		}
		sort.Strings(missingLocal)
		if len(missingLocal) > 0 {
			issues += len(missingLocal)
			fmt.Printf("\n[MISSING FROM .env] (%d) — documented but not set locally\n", len(missingLocal))
			for _, name := range missingLocal {
				fmt.Printf("  - %s\n", name)
			}
		} else {
			fmt.Println("\n[OK] local .env has every variable documented in .env.example")
		}
	} else {
		fmt.Println("\n[SKIP] no local .env file found, skipping local-completeness check")
	}

	// 3. documented in .env.example but never referenced in any source file (dead config)
	if len(f.exampleEnv) > 0 {
		var unused []string
		for name := range f.exampleEnv {
			if _, ok := f.usedVars[name]; !ok {
				unused = append(unused, name)
			}
		}
		sort.Strings(unused)
		if len(unused) > 0 {
			fmt.Printf("\n[UNUSED] (%d) — documented in .env.example but never referenced in source code\n", len(unused))
			for _, name := range unused {
				fmt.Printf("  - %s\n", name)
			}
		} else {
			fmt.Println("\n[OK] every variable in .env.example is actually used in code")
		}
	}

	// 4. .env tracked by git (should never happen)
	if f.trackedEnv {
		issues++
		fmt.Println("\n[RISK] .env is tracked by git! Add it to .gitignore and rotate any committed secrets.")
	}

	// 5. secret-looking keys that have a non-empty local value (informational)
	if len(f.secretsInEnv) > 0 {
		sort.Strings(f.secretsInEnv)
		fmt.Printf("\n[INFO] secret-looking keys with a local value set (%d) — make sure these never get committed\n", len(f.secretsInEnv))
		for _, name := range f.secretsInEnv {
			fmt.Printf("  - %s\n", name)
		}
	}

	fmt.Println()
	if issues == 0 {
		fmt.Println("No drift detected. ✔")
	} else {
		fmt.Printf("%d issue(s) found.\n", issues)
	}
	return issues
}
