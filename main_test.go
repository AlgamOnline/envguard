package main

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// buildAndRun compiles envguard (if not already) and runs it against a dir,
// returning combined stdout.
func runEnvguard(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("go", "run", ".", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("envguard run failed: %v\noutput:\n%s", err, out)
	}
	return string(out)
}

func TestFixtureDetectsDrift(t *testing.T) {
	out := runEnvguard(t, "testdata/fixture")
	if !strings.Contains(out, "FEATURE_FLAG_X") {
		t.Errorf("expected undocumented var FEATURE_FLAG_X to be flagged, got:\n%s", out)
	}
	if !strings.Contains(out, "JWT_SECRET") {
		t.Errorf("expected JWT_SECRET missing-from-local to be flagged, got:\n%s", out)
	}
}

func TestCleanProjectHasNoDrift(t *testing.T) {
	out := runEnvguard(t, "testdata/clean")
	if !strings.Contains(out, "No drift detected") {
		t.Errorf("expected clean project to report no drift, got:\n%s", out)
	}
}

// TestJSONOutputIsValidAndMatchesTextFindings covers the -json flag: output
// must be parseable JSON and report the same drift as the text mode, so CI
// scripts/tooling can consume it reliably instead of scraping text.
func TestJSONOutputIsValidAndMatchesTextFindings(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "-json", "testdata/fixture")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("envguard -json run failed: %v\noutput:\n%s", err, out)
	}

	var parsed struct {
		MissingFromExample []string `json:"missing_from_example"`
		MissingFromEnv     []string `json:"missing_from_env"`
		IssueCount         int      `json:"issue_count"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("expected valid JSON, got error %v\noutput:\n%s", err, out)
	}

	if !contains(parsed.MissingFromExample, "FEATURE_FLAG_X") {
		t.Errorf("expected FEATURE_FLAG_X in missing_from_example, got %v", parsed.MissingFromExample)
	}
	if !contains(parsed.MissingFromEnv, "JWT_SECRET") {
		t.Errorf("expected JWT_SECRET in missing_from_env, got %v", parsed.MissingFromEnv)
	}
	if parsed.IssueCount != 2 {
		t.Errorf("expected issue_count 2, got %d", parsed.IssueCount)
	}
}

// TestDestructuredEnvVarsAreDetected covers object-destructuring of
// process.env (const { A, B: alias, C = "default" } = process.env), a
// common JS/TS pattern the plain process.env.X regex misses.
func TestDestructuredEnvVarsAreDetected(t *testing.T) {
	out := runEnvguard(t, "testdata/destructure")
	if !strings.Contains(out, "STRIPE_KEY") {
		t.Errorf("expected destructured STRIPE_KEY to be flagged as undocumented, got:\n%s", out)
	}
	if strings.Contains(out, "DB_URL") && strings.Contains(out, "MISSING FROM .env.example") && strings.Contains(out, "- DB_URL") {
		t.Errorf("DB_URL is documented via alias destructuring, should not be flagged undocumented, got:\n%s", out)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// TestUnusedEnvVarIsFlagged covers a previously untested edge case: a var
// documented in .env.example that is never referenced in any source file
// (dead config left behind after a feature was removed).
func TestUnusedEnvVarIsFlagged(t *testing.T) {
	out := runEnvguard(t, "testdata/unused")
	if !strings.Contains(out, "[UNUSED]") || !strings.Contains(out, "LEGACY_FEATURE_TOGGLE") {
		t.Errorf("expected LEGACY_FEATURE_TOGGLE to be flagged as unused, got:\n%s", out)
	}
	if strings.Contains(out, "OK] every variable in .env.example is actually used") {
		t.Fatalf("PORT should NOT be flagged as unused, got:\n%s", out)
	}
}

// TestLeakedSecretInExampleIsFlagged covers a real-world mistake: someone
// copies their real .env to .env.example and forgets to blank out the
// values, leaking a live credential into a file that gets committed to git.
// A clear placeholder value (e.g. "your_database_url_here") must NOT trigger
// this, only values that actually look like a real token/key.
func TestLeakedSecretInExampleIsFlagged(t *testing.T) {
	out := runEnvguard(t, "testdata/leaked")
	if !strings.Contains(out, "RISK") || !strings.Contains(out, "STRIPE_KEY") {
		t.Errorf("expected STRIPE_KEY leaked value to be flagged as RISK, got:\n%s", out)
	}
	if strings.Contains(out, "- DB_URL") {
		t.Errorf("DB_URL has a placeholder value, should NOT be flagged as leaked, got:\n%s", out)
	}
}
