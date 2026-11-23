package main

import (
	"path/filepath"
	"sort"
	"testing"
)

func scanOne(t *testing.T, name, content string) []string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, name), content)
	f := newFindings()
	if err := scan(dir, f); err != nil {
		t.Fatal(err)
	}
	return sortedKeys(f.usedVars)
}

func assertVars(t *testing.T, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}

func TestRubyEnvDetection(t *testing.T) {
	got := scanOne(t, "app.rb", `
db = ENV['DB_URL']
key = ENV["API_KEY"]
port = ENV.fetch('PORT', 3000)
req = ENV.fetch("REQUIRED_VAR")
`)
	assertVars(t, got, "DB_URL", "API_KEY", "PORT", "REQUIRED_VAR")
}

func TestPHPEnvDetection(t *testing.T) {
	got := scanOne(t, "index.php", `<?php
$a = getenv('APP_NAME');
$b = $_ENV["APP_ENV"];
$c = $_SERVER['SERVER_SECRET'];
$d = env('LARAVEL_KEY', 'x');
`)
	assertVars(t, got, "APP_NAME", "APP_ENV", "SERVER_SECRET", "LARAVEL_KEY")
}

func TestRustEnvDetection(t *testing.T) {
	got := scanOne(t, "main.rs", `
fn main() {
    let a = std::env::var("DATABASE_URL").unwrap();
    let b = env::var_os("HOME_DIR");
    let c = env!("BUILD_TARGET");
    let d = option_env!("OPTIONAL_FLAG");
}
`)
	assertVars(t, got, "DATABASE_URL", "HOME_DIR", "BUILD_TARGET", "OPTIONAL_FLAG")
}

func TestJavaKotlinEnvDetection(t *testing.T) {
	got := scanOne(t, "App.java", `
String a = System.getenv("JAVA_DB");
String b = System.getenv( "JAVA_PORT" );
String c = System.getenv().get("IGNORED_MAP_STYLE");
`)
	assertVars(t, got, "JAVA_DB", "JAVA_PORT")
	got = scanOne(t, "Main.kt", `val k = System.getenv("KOTLIN_KEY") ?: "x"`)
	assertVars(t, got, "KOTLIN_KEY")
}

func TestCSharpEnvDetection(t *testing.T) {
	got := scanOne(t, "Program.cs", `
var a = Environment.GetEnvironmentVariable("CS_CONN");
var b = System.Environment.GetEnvironmentVariable( "CS_TOKEN", EnvironmentVariableTarget.Process);
`)
	assertVars(t, got, "CS_CONN", "CS_TOKEN")
}

func TestDenoEnvDetection(t *testing.T) {
	got := scanOne(t, "main.ts", `const a = Deno.env.get("DENO_KEY"); const b = Deno.env.get('DENO_URL');`)
	assertVars(t, got, "DENO_KEY", "DENO_URL")
}

func TestUnrelatedCallsNotDetected(t *testing.T) {
	got := scanOne(t, "x.rb", `puts ENVIRONMENT['nope']; MYENV['ALSO_NO']`)
	assertVars(t, got)
}
