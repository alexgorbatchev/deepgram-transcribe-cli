`deepgram-transcribe` turns recorded phone calls and interviews into readable Markdown transcripts, labelled with who is speaking and when they spoke. It keeps a copy of everything it transcribes, so asking for the same recording twice costs nothing, and it can tell you exactly what each recording was billed.

# What It Does

- **Readable transcripts**: Produces Markdown with a metadata table, speaker turns (`### Speaker 0 (00:00 - 00:15)`) and timestamps.
- **Knows who is speaking**: Separates and labels each speaker in the conversation.
- **Engineering vocabulary**: Ships with 80+ common engineering and system design terms so technical interviews come back spelled correctly.
- **Your own vocabulary**: Add company names, people names or product names with `--term` or `--terms-file`.
- **Never pays twice**: Recognises a recording it has already transcribed, even when the settings have changed, and returns the saved transcript instead of buying a new one.
- **Cheaper uploads**: Merges stereo to mono and cuts dead air before uploading, which roughly halves the billed audio.
- **Spending history**: Reports the real billed cost of each transcription and the running total.
- **Built for people and for agents**: The same commands produce polished output in a terminal and compact, parseable output when `AGENT=1` is set.

# How It Works

- You give it a recording, and it checks whether that exact recording has already been transcribed.
- If it has, the saved transcript comes straight back, with no upload, no waiting and no charge.
- If it has not, the recording is made smaller first: the two stereo channels are merged into one and long silences are cut out, because you are billed for how much audio is sent.
- The recording is sent to Deepgram, which returns the words along with who spoke them and when.
- The result is written out as Markdown, either to your screen or to a file you name.
- A note of what was transcribed, how long it was and what it cost is kept, so you can look up your spending later.

# How it Really Works

- Commands are built on Cobra with a subject-verb hierarchy (`transcript create`, `job list`, `job inspect`, `cache status`, `cache clear`), and help screens are rendered as aligned command trees trimmed to the terminal width.
- Transcription posts the audio to Deepgram's pre-recorded REST endpoint (`POST /v1/listen`) with `diarize`, `smart_format`, `utterances` and `punctuate` enabled, and no paid intelligence add-ons.
- Vocabulary is sent as `keyterm` parameters for `nova-3` and `flux`, and as `keywords` for the older `nova-2`, `nova-1` and `base` models.
- Two SHA-256 keys index the local store: one over the raw audio bytes, and one over the audio plus the request options. The first detects the same recording under different settings, the second detects an identical request.
- Responses and job metadata are stored together as a single JSON envelope per key, under `$XDG_CACHE_HOME/deepgram-transcribe` or `~/.cache/deepgram-transcribe`.
- Audio preprocessing shells out to `ffmpeg`, using `-ac 1` for the downmix and the `silenceremove` filter for dead air, and is skipped entirely on a cache hit.
- Billed costs come from Deepgram's management API (`GET /v1/projects/{project}/requests/{request}`), are fetched concurrently for a listing, and are written back so each request is only ever asked about once.
- Transcripts go to stdout and all progress goes to stderr, so redirecting stdout captures the Markdown alone.

# Prerequisites

- [Deepgram API key](https://console.deepgram.com/) - Required to transcribe. Set `DEEPGRAM_API_KEY` in your environment or pass `--api-key`. Reading your history and saved transcripts does not need one.
- [ffmpeg](https://ffmpeg.org/download.html) - Optional but recommended. Without it, recordings are uploaded at full size and cost roughly twice as much.

# Installation

Download the prebuilt binary for your platform from the [latest release](https://github.com/alexgorbatchev/deepgram-transcribe-cli/releases/latest), replacing `X.X.X` below with the version shown on that page.

```bash
# macOS (Apple Silicon)
curl -sSL https://github.com/alexgorbatchev/deepgram-transcribe-cli/releases/latest/download/deepgram-transcribe_X.X.X_darwin_arm64.tar.gz | tar -xz -C ~/.local/bin
```

# Quick Start

```bash
export DEEPGRAM_API_KEY="your-deepgram-api-key"

# Transcribe a recording and save it as Markdown
deepgram-transcribe transcript create interview.m4a > interview.md

# Listen for names that would otherwise be misheard
deepgram-transcribe transcript create interview.m4a -t Envoy -t Gorbatchev -o interview.md

# See what one recording cost
deepgram-transcribe job inspect interview.m4a

# See everything transcribed so far, and the running total
deepgram-transcribe job list
```

# Options & Flags

Available on every command:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--api-key <key>` | | `$DEEPGRAM_API_KEY` | Deepgram API key |
| `--cache-dir <path>` | | `~/.cache/deepgram-transcribe` | Folder that saved transcripts are kept in |
| `--version` | `-v` | `false` | Print the version and exit |
| `--help` | `-h` | `false` | Print command line help |

`deepgram-transcribe transcript create <audio-file>`:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--term <word>` | `-t` | none | Extra word or name to listen for (repeat or separate with commas) |
| `--terms-file <path>` | | none | File of extra words to listen for, one per line |
| `--model <name>` | `-m` | `nova-3` | Transcription model to use |
| `--language <code>` | `-l` | `en` | Language spoken in the recording |
| `--output <path>` | `-o` | screen | Write the transcript to this file instead of the screen |
| `--force` | `-f` | `false` | Transcribe the recording again even if a saved transcript exists |
| `--no-diarize` | | `false` | Do not label who is speaking |
| `--no-tech-terms` | | `false` | Do not listen for the built-in engineering vocabulary |
| `--no-cache` | | `false` | Do not reuse or keep a saved copy of the transcript |
| `--no-preprocess` | | `false` | Upload the recording as it is, without making it smaller first |
| `--no-mono` | | `false` | Keep both stereo channels instead of merging them into one |
| `--no-trim-silence` | | `false` | Keep long silences instead of cutting them out |
| `--silence-threshold <level>` | | `-30dB` | How quiet a passage has to be to count as silence |
| `--silence-duration <seconds>` | | `2.0` | How many seconds a silence has to last before it is cut |

`deepgram-transcribe job list`:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--limit <count>` | | all | Show only this many of the most recent transcriptions |

# Commands

```
deepgram-transcribe
├─ cache                               Manage the transcripts saved on this computer
│  ├─ clear                            Delete every saved transcript and its history entry
│  ╰─ status                           Show where transcripts are saved and how many are kept
├─ job                                 Review past transcriptions and what they cost
│  ├─ inspect <audio-file|request-id>  Show the details and cost of one past transcription
│  ╰─ list                             List past transcriptions and total spending
╰─ transcript                          Create transcripts from recorded audio
   ╰─ create <audio-file>              Turn a recording into a Markdown transcript
```

Supported audio formats are `.mp3`, `.m4a`, `.mp4`, `.wav`, `.flac`, `.ogg` and `.aac`.

# Agent Mode

Set `AGENT=1` to swap the polished output for a compact, parseable form: no rules, no borders, no padding. Listings become tab separated, details become flat `key: value` lines, and help screens become compact command summaries.

```bash
AGENT=1 deepgram-transcribe job list
```

```
date	file	duration	channels	model	cost	request_id
2026-03-31	interview.m4a	10:00	1	nova-3	$0.043	req-actual-123
transcriptions: 1
total_duration: 10:00
total_billed: $0.043
```

# Comparison With The Official Deepgram CLI

The official [`deepgram/cli`](https://github.com/deepgram/cli) streams audio to Deepgram on every run. `deepgram-transcribe` is built instead around not paying for the same recording twice.

| Capability | `deepgram-transcribe` | Official Deepgram CLI (`dg`) |
| :--- | :--- | :--- |
| **Saved transcripts** | Built in. A repeat run costs nothing and returns immediately. | None. Every run calls the API and is billed. |
| **Recognising a recording** | Matches the audio itself, so it is recognised even when the settings differ. | None. The audio is uploaded every time. |
| **Forcing a fresh run** | `-f / --force` when new vocabulary or a new model is needed. | Not applicable, since it always re-transcribes. |
| **Cheaper uploads** | Merges stereo to mono and cuts dead air with `ffmpeg` before uploading. | None. The file is uploaded as it is. |
| **Spending history** | `job inspect` and `job list` track request IDs, duration and running spend. | None. Requires the management API or the web dashboard. |
| **Output** | Markdown with a metadata table and speaker turns. | Raw JSON, VTT, SRT or plain text. |

# Development

```bash
just build      # Build the binary into bin/
just run        # Run it in human mode
just run-ai     # Run it in agent mode
just test       # Run the test suite
just lint       # Check formatting and run static analysis
just check      # Lint, test and verify module hygiene
just coverage   # Report test coverage
```

# License

MIT License (c) 2026 Alex Gorbatchev
