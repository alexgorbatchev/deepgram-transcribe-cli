# deepgram-transcribe-cli Agent Guidelines

Workspace for `deepgram-transcribe-cli` — a Go CLI utility (`deepgram-transcribe`) that turns recorded calls into Markdown transcripts with speaker labelling, custom vocabulary boosting, local transcript reuse and spending history.

## Repository & GitHub Releases
- Public GitHub Repository: [`github.com/alexgorbatchev/deepgram-transcribe-cli`](https://github.com/alexgorbatchev/deepgram-transcribe-cli)
- GitHub Releases (Pre-compiled Binaries): [`github.com/alexgorbatchev/deepgram-transcribe-cli/releases`](https://github.com/alexgorbatchev/deepgram-transcribe-cli/releases)
- Binary name: `deepgram-transcribe`
- Release archives are named `deepgram-transcribe_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows).

## Installation
- **Pre-compiled Release Binaries**: Download from the [latest release](https://github.com/alexgorbatchev/deepgram-transcribe-cli/releases/latest). The README documents this path only; do not add `gh` CLI or build-from-source instructions to it.

## Shared Commands
- Build binary: `just build` (outputs to `bin/deepgram-transcribe`)
- Run in human mode: `just run <args>`
- Run in agent mode: `just run-ai <args>` (sets `AGENT=1`)
- Run unit tests: `just test`
- Lint and static analysis: `just lint` (`gofmt`, `go vet`, `golangci-lint`)
- Full pre-commit gate: `just check`
- Coverage report: `just coverage` (generates `coverage.out`)
- Clean build artifacts: `just clean`

## Architecture Map
- `cmd/deepgram-transcribe/`: Cobra command tree, one file per subject.
  - `main.go`: entrypoint, signal-aware context, error reporting.
  - `root.go`: root command, persistent `--cache-dir` and `--api-key`, tree help setup, version template.
  - `transcript.go`: the `transcript create` verb and the steps of one transcription run.
  - `job.go`: the `job list` and `job inspect` verbs, plus concurrent billed-cost lookups.
  - `cache.go`: the `cache status` and `cache clear` verbs.
  - `dependency.go`: the `dependency list`, `dependency install` and `dependency update` verbs.
  - `format.go`: shared human-readable formatting.
- `internal/cliout/`: the dual-mode output layer (`AGENT=1`), covering statuses, fields, tables, rules and hinted errors.
- `internal/deps/`: the external programs this CLI needs, declared for `godeps`. Currently only `ffmpeg`, with its minimum version, install strategy and managed PATH setup.
- `pkg/deepgram/`: Deepgram API client (`Client`), URL builder, options, local SHA-256 response store (`cache.go`), job history (`job.go`) and response models (`types.go`).
- `pkg/audio/`: `ffmpeg` preprocessing (`PreprocessAudio`) for stereo-to-mono downmixing and silence trimming, plus MIME detection.
- `pkg/terms/`: engineering vocabulary (`DefaultTechTerms`), custom term parsing and file loading.
- `pkg/markdown/`: transcript formatter (`Format`) with speaker turn grouping and a metadata table.

## CLI Structure (Assuming `deepgram-transcribe` is on PATH)
Commands follow a subject-verb hierarchy, at most three levels deep:
- `deepgram-transcribe transcript create <audio-file>`: transcribe a recording to stdout or `--output`.
- `deepgram-transcribe job list [--limit N]`: list past transcriptions and total spending.
- `deepgram-transcribe job inspect <audio-file|request-id>`: show details and cost for one transcription.
- `deepgram-transcribe cache status`: show where transcripts are saved and how many are kept.
- `deepgram-transcribe cache clear`: delete saved transcripts and history.
- `deepgram-transcribe dependency list|install|update`: inspect and manage the external programs, currently just `ffmpeg`.

## Mandatory Maintenance Boundaries
1. **NO LIVE API CALLS IN TESTS**: Unit tests MUST mock the Deepgram API via `httptest.NewServer`. Tests must never consume live Deepgram credits or make external network calls.
   Likewise, tests MUST inject a stub `godeps.CommandRunner` rather than run real external programs, and MUST NOT exercise `dependency install` or `dependency update` down to a branch that actually installs, because `godeps.SystemPackageManager` shells out to `brew`, `apt-get` and friends with a runner that cannot be replaced from outside that library.
2. **HIGH FUNCTION COVERAGE**: Maintain statement and function coverage across all domain packages (`internal/cliout`, `pkg/deepgram`, `pkg/audio`, `pkg/terms`, `pkg/markdown`).
3. **NO BINARIES IN GIT**: Compiled binaries (`bin/`) MUST be excluded via `.gitignore` and never committed to the repository.
4. **DUAL-MODE OUTPUT**: Every new output path MUST go through `internal/cliout` so that `AGENT=1` stays token-conservative. Never print tables, rules or padding directly.
5. **SUBJECT-VERB COMMANDS**: New commands MUST nest a verb under a subject, and help screens MUST stay on `cobra-help-tree`.
6. **CLEAN VERSION OUTPUT**: `--version` MUST print only the bare version string, because scripts parse it.
7. **STDOUT IS THE RESULT**: Progress, warnings and errors belong on stderr, so that redirecting stdout captures the transcript alone.
