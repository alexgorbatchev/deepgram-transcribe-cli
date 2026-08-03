# deepgram-transcribe-cli Agent Guidelines

Workspace for `deepgram-transcribe-cli` — a Go CLI utility (`deepgram-transcribe`) that transcribes phone call audio recordings (mp3, m4a, wav) into formatted Markdown with speaker diarization, tech interview keyterm boosting, and post-transcription cost tracking subcommands.

## Repository & GitHub
- Public GitHub Repository: `github.com/alexgorbatchev/deepgram-transcribe-cli`
- Binary name: `deepgram-transcribe`

## Shared Commands
- Build binary: `just build` (outputs to `bin/deepgram-transcribe`)
- Run unit tests: `just test`
- Run coverage report: `just coverage` (generates `coverage.out`)
- Clean build artifacts: `just clean`

## Architecture Map
- `cmd/deepgram-transcribe/main.go`: Cobra CLI entrypoint, argument parsing, subcommands (`cost`, `history`, `cache`), MIME detection, and execution flow.
- `pkg/deepgram/`: Deepgram API client (`HTTPClient`), URL builder, options, local SHA-256 response caching (`cache.go`), job history persistence (`job.go`), and JSON response models (`v1/listen`).
- `pkg/audio/`: Audio preprocessing (`PreprocessAudio`) via `ffmpeg` for stereo-to-mono downmixing (`--mono`) and silent pause removal (`--trim-silence`).
- `pkg/terms/`: Tech interview keyterm vocabulary (`DefaultTechTerms`), custom term parser, and file loader.
- `pkg/markdown/`: Markdown transcript formatter (`Format`) with speaker turn grouping and metadata table generation.

## CLI Usage (Assuming `deepgram-transcribe` is in PATH)
- `deepgram-transcribe <audio-file>`: Default transcription command (outputs Markdown to stdout or `-o`).
- `deepgram-transcribe cost <file|request-id>`: Query Deepgram Request ID, duration, channel count, model, and calculated cost for any completed job.
- `deepgram-transcribe history`: View a chronological table of all past transcriptions and cumulative total spending.
- `deepgram-transcribe cache [status|clear]`: Inspect or clear the local transcript cache.

## Mandatory Maintenance Boundaries
1. **NO LIVE API CALLS IN TESTS**: Unit tests in `deepgram-transcribe-cli/` MUST mock the Deepgram API via `httptest.NewServer`. Tests must never consume live Deepgram credits or make external network calls.
2. **HIGH FUNCTION COVERAGE**: Maintain statement and function coverage across all domain packages (`pkg/deepgram`, `pkg/audio`, `pkg/terms`, `pkg/markdown`).
3. **NO BINARIES IN GIT**: Compiled binaries (`bin/`) MUST be excluded via `.gitignore` and never committed to the repository.
