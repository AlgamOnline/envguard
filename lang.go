package main

import "regexp"

// Extra language detectors: Ruby, PHP, Rust. Each pattern captures the env
// var name in group 1. A leading word-boundary-ish guard (`(?:^|[^A-Za-z0-9_])`)
// stops look-alikes such as MYENV['X'] or ENVIRONMENT['X'] from matching.
const (
	name      = `([A-Za-z_][A-Za-z0-9_]*)`
	notIdent  = `(?:^|[^A-Za-z0-9_])`
	quote     = `['"]`
	optSpaces = `\s*`
)

var (
	// Ruby: ENV['X'], ENV["X"], ENV.fetch('X'...)
	reRubyEnv = regexp.MustCompile(notIdent + `ENV(?:\[` + optSpaces + quote + name + quote + `|\.fetch\(` + optSpaces + quote + name + quote + `)`)
	// PHP: getenv('X'), $_ENV['X'], $_SERVER['X'], Laravel env('X')
	rePHPEnv = regexp.MustCompile(`(?:` + notIdent + `(?:getenv|env)\(` + optSpaces + quote + name + quote + `|\$_(?:ENV|SERVER)\[` + optSpaces + quote + name + quote + `)`)
	// Rust: env::var("X"), env::var_os("X"), env!("X"), option_env!("X")
	reRustEnv = regexp.MustCompile(`(?:env::var(?:_os)?\(` + optSpaces + `"` + name + `"|(?:option_)?env!\(` + optSpaces + `"` + name + `")`)

	// Java/Kotlin: System.getenv("X")
	reJavaEnv = regexp.MustCompile(notIdent + `System\.getenv\(` + optSpaces + `"` + name + `"`)
	// C#: Environment.GetEnvironmentVariable("X")
	reCSharpEnv = regexp.MustCompile(`Environment\.GetEnvironmentVariable\(` + optSpaces + `"` + name + `"`)
	// Deno (TS/JS): Deno.env.get("X")
	reDenoEnv = regexp.MustCompile(notIdent + `Deno\.env\.get\(` + optSpaces + quote + name + quote)

	// Docker Compose variable substitution: ${VAR}, ${VAR:-default}, ${VAR:?err}.
	// This is the #1 real-world "works on my machine" trap with Compose: the
	// var silently defaults to an empty string if missing, instead of erroring
	// (see docker-compose "variable is not set, defaulting to a blank string").
	reComposeEnv = regexp.MustCompile(notIdent + `\$\{` + name + `(?::[-?][^}]*)?\}`)
)

// firstNonEmptyGroup returns the first non-empty capture group of a match,
// since the patterns above use alternation with one group per branch.
func firstNonEmptyGroup(m []string) string {
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

// findAllNames runs re over content and returns every captured env var name.
func findAllNames(re *regexp.Regexp, content string) [][]string {
	var out [][]string
	for _, m := range re.FindAllStringSubmatch(content, -1) {
		if n := firstNonEmptyGroup(m); n != "" {
			out = append(out, []string{m[0], n})
		}
	}
	return out
}
