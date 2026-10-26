# envguard

A tiny, dependency-free CLI that finds **environment-variable drift** in a codebase — the classic "works on my machine" bug.

## The problem

Every real project grows env vars organically: someone adds `process.env.STRIPE_KEY` in a new feature, forgets to add it to `.env.example`, and a week later a teammate (or CI) fails with a cryptic `undefined` error. Or the reverse: `.env.example` documents a var nobody actually uses anymore. Or — worse — a `.env` file with real secrets gets `git add`-ed by accident.

These are small, boring bugs, but they eat real time, especially onboarding new engineers or spinning up CI. There's no single standard tool that checks this across JS/TS, Go, and Python in one pass.

## What it does

`envguard` scans a project directory and cross-references:

1. **Env vars used in source code** — `process.env.X` (JS/TS, including `const { X, Y: alias } = process.env` destructuring), `os.Getenv("X")` / `os.LookupEnv("X")` (Go), `os.environ["X"]` / `os.getenv("X")` (Python)
2. **Env vars documented** in `.env.example` / `.env.sample` / `.env.template`
3. **Env vars actually set locally** in `.env`

It reports:
- Variables **used in code but missing from `.env.example`** → undocumented config, will break for new devs.
- Variables **documented but missing from your local `.env`** → you're probably about to hit a bug.
- Variables **documented in `.env.example` but never referenced anywhere in source** → dead config left behind after a feature was removed, cluttering onboarding docs.
- **`.env` accidentally tracked by git** → critical risk, flagged loudly.
- **`.env.example` holding a real-looking secret value instead of a placeholder** → the classic "copy-pasted my real .env into .env.example and forgot to blank it out" mistake. `.env.example` is normally the one file meant to be committed and public, so a real key sitting in it is a live leak, not just drift.
- Secret-looking keys (`*_KEY`, `*_SECRET`, `*_TOKEN`, `*_PASSWORD`, ...) that have a value set locally, as a reminder to never commit them.

## Why it matters

- Catches onboarding friction before a new hire hits it.
- Drop into CI (`envguard -ci`) to fail the build when config drifts out of sync with code — cheaper than a runtime crash in staging.
- Zero dependencies, single static binary, runs in milliseconds even on large repos.

## Usage

```bash
# scan current directory
envguard

# scan a specific project
envguard /path/to/project

# CI mode: exit code 1 if any drift/risk is found, 0 if clean
envguard -ci .

# JSON output: machine-readable, for CI annotations or other tooling
envguard -json .
```

Example output:

```
envguard — environment variable drift report
================================================

[MISSING FROM .env.example] (1)
  - FEATURE_FLAG_X                 used in app.js

[MISSING FROM .env] (1) — documented but not set locally
  - JWT_SECRET

[UNUSED] (1) — documented in .env.example but never referenced in source code
  - OLD_FEATURE_TOGGLE

2 issue(s) found.
```

## Install / Build

Requires Go 1.21+.

```bash
git clone https://github.com/AlgamOnline/envguard.git
cd envguard
go build -o envguard .
./envguard .
```

## JSON output

`-json` prints a machine-readable report instead of text, so CI pipelines or
other scripts can consume it without parsing human-readable output:

```bash
envguard -json . | jq '.issue_count'
```

```json
{
  "missing_from_example": ["FEATURE_FLAG_X"],
  "missing_from_env": ["JWT_SECRET"],
  "unused": ["OLD_FEATURE_TOGGLE"],
  "env_tracked_by_git": false,
  "secrets_set_locally": [],
  "used_var_locations": { "FEATURE_FLAG_X": ["app.js"] },
  "issue_count": 2
}
```

`-ci` and `-json` can be combined (`envguard -ci -json .`): exit code still
reflects drift, stdout is JSON.

## Tech stack

- **Go** (stdlib only — `regexp`, `flag`, `bufio`, `path/filepath`, `os/exec`) — no third-party dependencies, so it compiles to one portable static binary for any OS/arch Go supports.
- Shells out to `git ls-files` to detect a tracked `.env` (git itself, no library).

## Supported languages

| Language | Patterns detected |
|---|---|
| JavaScript/TypeScript | `process.env.X`, `process.env["X"]`, destructuring: `const { X, Y: alias } = process.env` |
| Go | `os.Getenv("X")`, `os.LookupEnv("X")` |
| Python | `os.environ["X"]`, `os.environ.get("X")`, `os.getenv("X")` |

## Tests

```bash
go test ./...
```

Covers: undocumented vars, missing local vars, clean projects (no drift),
dead config (a var in `.env.example` that no source file references anymore),
destructured `process.env` usage (`const { X, Y: alias } = process.env`),
a real-looking secret value leaked into `.env.example` (vs. a harmless
placeholder like `your_key_here`), and JSON output correctness.

## License

MIT
