# deepgram-transcribe-cli

`deepgram-transcribe` is a Go CLI helper tool for transcribing phone call audio recordings (`.mp3`, `.m4a`, `.wav`, `.flac`, `.ogg`, `.aac`) into structured Markdown documents using the Deepgram API (`v1/listen`).

It features preconfigured tech interview vocabulary, speaker diarization, custom term boosting, post-transcription cost tracking subcommands, source audio caching with term diff warnings, and default cost-reduction optimizations.

## Features

- **Markdown Output**: Formats transcripts into structured Markdown with header metadata, speaker turn grouping (`### Speaker 0 (00:00 - 00:15)`), and timestamps.
- **Tech Interview Vocabulary**: Preconfigured with 80+ common engineering and system design terms across languages, frameworks, cloud infrastructure, databases, and protocols (see full vocabulary in [`pkg/terms/tech.go`](./pkg/terms/tech.go)).
- **Custom Term Boosting**: Pass extra company names, people names, or terms via `-t/--term` or `--terms-file`.
- **Source-Audio Caching & `--force` Flag**:
  - Automatically matches raw source audio content (`sha256(audioBytes)`).
  - If a file was previously transcribed with slightly different terms/flags, `deepgram-transcribe` returns the cached transcript, displays a clear term diff warning on `stderr`, and prevents accidental $0.27+ API re-charges.
  - Pass `-f/--force` to re-transcribe with new terms.
- **Post-Transcription Cost Tracking**:
  - `deepgram-transcribe cost <file|request-id>`: Query Deepgram Request ID, duration, channel count, model, and calculated cost for any completed job.
  - `deepgram-transcribe history`: View a chronological table of all past transcriptions and cumulative total spending.
- **Automatic Cost Reduction**:
  - **Stereo-to-Mono Downmixing**: Automatically downmixes stereo audio to single-channel (mono) via `ffmpeg` when available, reducing billable audio minutes by 50%.
  - **Silence & Dead-Air Trimming**: Automatically strips dead-air pauses (>2.0s) via `ffmpeg silenceremove` filter before upload to minimize billable audio duration.
  - **No Intelligence Add-Ons**: Uses pure speech-to-text with speaker diarization (`diarize=true`), avoiding paid intelligence add-ons (`summarize`, `sentiment`).
  - **Batch Pre-Recorded REST API**: Uses Deepgram's cost-effective pre-recorded batch API (`POST https://api.deepgram.com/v1/listen`).

## Comparison: `deepgram-transcribe` vs. Official Deepgram CLI ([`deepgram/cli`](https://github.com/deepgram/cli))

While the official Deepgram CLI ([`deepgram/cli`](https://github.com/deepgram/cli)) streams audio directly to Deepgram's API on every execution, `deepgram-transcribe` is specifically engineered as a cost-optimized, AI-agent-friendly local wrapper:

| Capability | `deepgram-transcribe` | Official Deepgram CLI (`dg`) |
| :--- | :--- | :--- |
| **Local Response Caching** | **Built-In (Default)** — Hashes raw source audio (`sha256`). Re-runs cost **$0.00** and return in **<1ms**. | **None** — Every run calls Deepgram API and incurs full billing charges. |
| **Source Audio Matching** | **Smart Matching** — Detects previously transcribed audio files even if custom terms or flags differ, preventing accidental $0.27+ API re-charges. | **None** — Sends raw audio file stream on every invocation. |
| **Force Re-Transcription** | **`-f / --force`** — Forces a fresh Deepgram API call when new terms or models are needed. | N/A (Always re-transcribes). |
| **Audio Preprocessing** | **Automatic (`ffmpeg`)** — Downmixes stereo to mono (50% cost savings) and trims dead-air silence (>2s) before transmission. | **None** — Uploads raw audio file as-is. |
| **Post-Call Cost Tracking** | **`deepgram-transcribe cost` & `deepgram-transcribe history`** — Tracks Request IDs, duration, and cumulative USD spending across all transcribed calls. | **None** — Requires separate Management API calls or logging into the web dashboard. |
| **Output Format** | **Structured Markdown** — Formatted with Metadata tables and speaker turn blocks (`### Speaker 0 (00:00 - 00:15)`). | Raw JSON, VTT, SRT, or plain unformatted text. |

## Prerequisites

- **Go**: Version 1.22+
- **Deepgram API Key**: Set `DEEPGRAM_API_KEY` environment variable or pass `--api-key`.
- **ffmpeg** (Optional, recommended): Enables automatic stereo-to-mono downmixing and dead-air silence trimming.

## Installation

### Option A: Pre-Compiled Release Binaries (Recommended)
Download pre-compiled binaries for macOS, Linux, and Windows from [GitHub Releases](https://github.com/alexgorbatchev/deepgram-transcribe-cli/releases) or via `gh`:

```bash
gh release download --repo alexgorbatchev/deepgram-transcribe-cli --pattern "*Darwin_arm64*"
```

### Option B: Via `go install`
```bash
go install github.com/alexgorbatchev/deepgram-transcribe-cli/cmd/deepgram-transcribe@latest
```

### Option C: Build from Source
```bash
cd deepgram-transcribe-cli
just build
# Binary output at bin/deepgram-transcribe
```

## Quick Start (Assuming `deepgram-transcribe` on PATH)

```bash
export DEEPGRAM_API_KEY="your-deepgram-api-key"

# Transcribe audio file to stdout and redirect to Markdown
deepgram-transcribe interview.m4a > interview.md

# View post-transcription cost & Deepgram Request ID for a file
deepgram-transcribe cost interview.m4a

# View history table of all completed calls & cumulative spending
deepgram-transcribe history
```

## Subcommands & Usage

### 1. Transcribe Audio File (Default)
```bash
deepgram-transcribe [flags] <audio-file>
```

| Flag | Short | Description | Default |
| :--- | :--- | :--- | :--- |
| `--force` | `-f` | Force fresh Deepgram re-transcription even if cached source audio exists | `false` |
| `--term` | `-t` | Additional terms to boost (can pass multiple times or comma-separated) | `nil` |
| `--terms-file` | | Path to a text file containing custom terms (one per line) | `""` |
| `--model` | `-m` | Deepgram transcription model (`nova-3`, `nova-2`, `flux`) | `"nova-3"` |
| `--language` | `-l` | Audio language code | `"en"` |
| `--output` | `-o` | Output file path (default stdout) | `""` |
| `--api-key` | | Deepgram API key | `$DEEPGRAM_API_KEY` |
| `--no-cache` | | Bypass local SHA-256 transcript cache | `false` |
| `--no-diarize` | | Disable speaker diarization | `false` |
| `--no-preprocess` | | Disable automatic `ffmpeg` audio preprocessing (mono + silence trim) | `false` |
| `--no-mono` | | Disable stereo-to-mono downmixing | `false` |
| `--no-trim-silence` | | Disable dead-air silence trimming | `false` |
| `--silence-threshold` | | Noise threshold for silence removal | `"-30dB"` |
| `--silence-duration` | | Minimum silence duration in seconds to trim | `"2.0"` |
| `--no-tech-terms` | | Disable preconfigured tech interview terms | `false` |
| `--version` | `-v` | Print version information | `false` |

### 2. Query Post-Transcription Cost (`deepgram-transcribe cost`)
```bash
deepgram-transcribe cost <audio-file|request-id>
```

### 3. View Spending History (`deepgram-transcribe history`)
```bash
deepgram-transcribe history [--limit N]
```

### 4. Manage Cache (`deepgram-transcribe cache`)
```bash
deepgram-transcribe cache status
deepgram-transcribe cache clear
```

## Development & Testing

```bash
just test       # Run unit tests
just coverage   # Run coverage report
```
