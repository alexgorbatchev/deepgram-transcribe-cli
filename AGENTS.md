# deepgram-transcribe-cli Agent Guidelines

Workspace for `deepgram-transcribe-cli` — a Go CLI utility (`deepgram-transcribe`) that transcribes any audio containing speech into Markdown, with speaker diarization, custom keyterm boosting, a local Deepgram response cache and spending history.

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
  - `skill.go` and `SKILL.md`: embedded operational reference, printed verbatim by `skill`.
  - `transcript.go`: the `transcript create` verb and the steps of one transcription run.
  - `job.go`: the `job list` and `job inspect` verbs, plus concurrent billed-cost lookups.
  - `cache.go`: the `cache status` and `cache clear` verbs.
  - `dependency.go`: the `dependency list`, `dependency install` and `dependency update` verbs.
  - `format.go`: shared human-readable formatting.
- `internal/cliout/`: the dual-mode output layer (`AGENT=1`), covering statuses, fields, tables, rules and hinted errors.
- `internal/deps/`: `ffmpeg` and `ffprobe`, declared for `godeps` with minimum versions, the shared ffmpeg package installation strategy and managed PATH setup.
- `pkg/deepgram/`: Deepgram API client (`Client`), URL builder, options, local SHA-256 response store (`cache.go`), job history (`job.go`) and response models (`types.go`).
- `pkg/audio/`: `ffmpeg` preprocessing (`PreprocessAudio`) for stereo-to-mono downmixing and silence trimming, plus MIME detection.
- `pkg/terms/`: built-in engineering keyterms (`DefaultTechTerms`), custom keyterm parsing and file loading.
- `pkg/markdown/`: transcript formatter (`Format`) with speaker turn grouping and a metadata table.

## CLI Structure (Assuming `deepgram-transcribe` is on PATH)
Commands follow a subject-verb hierarchy, at most three levels deep:
- `deepgram-transcribe transcript create <audio-file>`: transcribe a recording to stdout or `--output`.
- `deepgram-transcribe job list [--limit N]`: list past transcriptions and total spending.
- `deepgram-transcribe job inspect <audio-file|request-id>`: show details and cost for one transcription.
- `deepgram-transcribe cache status`: show the cache location and how many responses are stored.
- `deepgram-transcribe cache clear`: delete all cached responses and job history.
- `deepgram-transcribe dependency list|install|update`: inspect and manage `ffmpeg` and `ffprobe`.

## Mandatory Maintenance Boundaries
1. **NO LIVE API CALLS IN TESTS**: Unit tests MUST mock the Deepgram API via `httptest.NewServer`. Tests must never consume live Deepgram credits or make external network calls.
   Likewise, tests MUST inject a stub `godeps.CommandRunner` rather than run real external programs, and MUST NOT exercise `dependency install` or `dependency update` down to a branch that actually installs, because `godeps.SystemPackageManager` shells out to `brew`, `apt-get` and friends with a runner that cannot be replaced from outside that library.
   Pass the runner through `audio.PreprocessOptions.Runner` for preprocessing tests. When testing invalid arguments on install/update before validation exists, replace the command's RunE with a test handler so the red phase cannot reach the package manager.
2. **HIGH FUNCTION COVERAGE**: Maintain statement and function coverage across all domain packages (`internal/cliout`, `pkg/deepgram`, `pkg/audio`, `pkg/terms`, `pkg/markdown`).
3. **NO BINARIES IN GIT**: Compiled binaries (`bin/`) MUST be excluded via `.gitignore` and never committed to the repository.
4. **DUAL-MODE OUTPUT**: Every new output path MUST go through `internal/cliout` so that `AGENT=1` stays token-conservative. Never print tables, rules or padding directly.
5. **SUBJECT-VERB COMMANDS**: Domain commands MUST nest a verb under a subject; the top-level `skill` command is the metadata exception. Help screens MUST stay on `cobra-help-tree`. A new command that takes a positional argument MUST describe it in `helpCatalog`, since Cobra has no field for that and `Use` alone leaves a reader guessing what the placeholder accepts. Commands without positional arguments MUST use `cobra.NoArgs`.
6. **CLEAN VERSION OUTPUT**: `--version` MUST print only the bare version string, because scripts parse it.
7. **STDOUT IS THE RESULT**: Progress, warnings and errors belong on stderr, so that redirecting stdout captures the transcript alone. Help that the user explicitly asked for is a result and goes to stdout; usage printed after a failure is a diagnostic and stays on stderr. `cobra-help-tree` v2 splits them that way itself, so `setTreeHelp` only passes options. Do not reintroduce a hand-rolled help func, and keep the tests that capture the real process streams: a command with an output writer set never exercises cobra's stderr fallback, so buffer-based tests cannot see a regression here.
8. **EMBEDDED REFERENCE**: Update `cmd/deepgram-transcribe/SKILL.md` and its `last_modified` in the same change as any public command, argument, flag, alias, default, environment variable, output or side-effect change. Include generated help/completion commands. Keep interface metadata synchronized with the live Cobra tree and preserve byte-identical `skill` output in both modes.
9. **MEASURED UPLOAD SIZE**: Report whether preprocessing makes the upload smaller, larger or unchanged based on actual bytes. Re-encoding compressed audio can increase file size; byte count does not establish the billed duration or channel count.
   Upload processed audio only when ffprobe establishes a strictly shorter audio-stream duration. Equal, longer or unavailable durations select original audio and remove the discarded temporary file, including mono-only processing. Inject the duration probe runner in tests.
10. **TESTING**: Change a corresponding test whenever runtime behavior changes; develop red/green and verify regressions by temporarily disabling the implementation. Maintain at least 90% statement coverage across domain packages.
11. **PUBLICATION**: Publish releases, tags, packages or deployments only with explicit user authorization.
12. **CACHE-HIT HISTORY**: Serving an existing response must preserve its original job record and billed cost, including source-content hits under different options. History-only envelopes must not decode as empty transcripts. Cover repeated cache hits, not just the first run.
