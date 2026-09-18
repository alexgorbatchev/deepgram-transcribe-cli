`deepgram-transcribe` turns any audio containing speech into a readable Markdown transcript, labelled with who is speaking and when they spoke. Deepgram responses are cached locally and served from cache on repeated requests, so the same audio is never billed twice, and it reports what each request was billed.

# What It Does

- **Readable transcripts**: Produces Markdown with a metadata table, speaker turns (`### Speaker 0 (00:00 - 00:15)`) and timestamps.
- **Speaker diarization**: Separates and labels each speaker in the audio.
- **Engineering keyterms**: Ships with a built-in vocabulary of engineering and system design terms, so technical words are recognised correctly.
- **Your own keyterms**: Add company, people or product names with `--term` or `--terms-file`.
- **Response caching**: Caches Deepgram responses keyed by audio content, and serves them on repeated requests even when the request options have changed, so the same audio is never billed twice.
- **Cheaper uploads**: Merges stereo to mono and cuts dead air before uploading, which roughly halves the billed audio.
- **Spending history**: Reports the real billed cost of each transcription and the running total.
- **Built for people and for agents**: The same commands produce polished output in a terminal and compact, parseable output when `AGENT=1` is set.

# How It Works

- You give it an audio file, and it checks the cache for a response to that exact audio.
- On a cache hit the stored response is served immediately, with no upload, no waiting and no charge.
- On a miss the audio is made smaller first: stereo channels are merged into one and long silences are trimmed, because Deepgram bills for how much audio is sent.
- The audio is uploaded to Deepgram, which returns the words along with who spoke them and when.
- The result is rendered as Markdown, to stdout or to the file named with `--output`.
- Each request is recorded with its duration and cost, so spending can be reviewed later.

# How it Really Works

- Transcription posts the audio to Deepgram's pre-recorded REST endpoint (`POST /v1/listen`), asking for speaker diarization, smart formatting, utterance segmentation and punctuation. No intelligence add-ons such as summarisation or sentiment are requested.
- Keyterms travel as `keyterm` query parameters on `nova-3` and `flux`, and as `keywords` on the older model families, which is the same distinction Deepgram's API draws.
- Two SHA-256 keys index the cache: one over the raw audio bytes, one over the audio plus the request options. The first recognises the same audio under changed options, the second recognises an identical request.
- A response and its job record are stored together as a single JSON envelope per key, under `$XDG_CACHE_HOME/deepgram-transcribe` or `~/.cache/deepgram-transcribe`.
- Preprocessing shells out to `ffmpeg`, using `-ac 1` to downmix and the `silenceremove` filter to cut dead air, and is skipped entirely on a cache hit, so a repeat request touches neither ffmpeg nor the network.
- What a request was actually billed comes from Deepgram's management API, is fetched concurrently when listing history, and is written back so the same request is never looked up twice.
- Until that real charge arrives, cost is estimated from a published rate compiled into the binary, and a model with no published rate reports an unknown cost rather than a guess. The estimate ignores negotiated and volume pricing, and published rates change, so [Deepgram's pricing page](https://deepgram.com/pricing) is the authority and `job inspect` is what reports the real charge.
- The transcript goes to stdout and everything else to stderr, so redirecting stdout captures the Markdown alone.

# Prerequisites

- [Deepgram API key](https://console.deepgram.com/) - Required to transcribe. Set `DEEPGRAM_API_KEY` in your environment or pass `--api-key`. Reading history and cached responses does not need one.
- [ffmpeg](https://ffmpeg.org/download.html) - Version 4.4 or newer, optional but recommended. Without it, recordings are uploaded at full size and cost roughly twice as much. Run `deepgram-transcribe dependency install` and the tool will install it for you through your system package manager.

# Installation

Download the prebuilt binary for your platform from the [latest release](https://github.com/alexgorbatchev/deepgram-transcribe-cli/releases/latest), replacing `X.X.X` below with the version shown on that page.

```bash
# macOS (Apple Silicon)
curl -sSL https://github.com/alexgorbatchev/deepgram-transcribe-cli/releases/latest/download/deepgram-transcribe_X.X.X_darwin_arm64.tar.gz | tar -xz -C ~/.local/bin
```

# Quick Start

```bash
export DEEPGRAM_API_KEY="your-deepgram-api-key"

# Transcribe an audio file and save it as Markdown
deepgram-transcribe transcript create interview.m4a > interview.md

# Boost recognition of names that would otherwise be misheard
deepgram-transcribe transcript create interview.m4a -t Envoy -t Gorbatchev -o interview.md

# See what one request cost
deepgram-transcribe job inspect interview.m4a

# See every request so far, and the running total
deepgram-transcribe job list
```

# Options & Flags

Available on every command:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--api-key <key>` | | `$DEEPGRAM_API_KEY` | Deepgram API key |
| `--cache-dir <path>` | | `~/.cache/deepgram-transcribe` | Directory for the local response cache |
| `--version` | `-v` | `false` | Print the version and exit |
| `--help` | `-h` | `false` | Print command line help |

`deepgram-transcribe transcript create <audio-file>`:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--term <word>` | `-t` | none | Additional keyterm to boost recognition, such as a company or person name (repeatable or comma-separated) |
| `--terms-file <path>` | | none | File of additional keyterms, one per line |
| `--model <name>` | `-m` | `nova-3` | Transcription model to use |
| `--language <code>` | `-l` | `en` | Language spoken in the audio |
| `--output <path>` | `-o` | stdout | Write the transcript to this file instead of stdout |
| `--force` | `-f` | `false` | Re-transcribe and be billed again, even on a cache hit |
| `--no-diarize` | | `false` | Disable speaker diarization |
| `--no-tech-terms` | | `false` | Disable the built-in engineering keyterms |
| `--no-cache` | | `false` | Bypass the response cache, for both reads and writes |
| `--no-preprocess` | | `false` | Disable ffmpeg preprocessing before upload |
| `--no-mono` | | `false` | Disable stereo-to-mono downmixing |
| `--no-trim-silence` | | `false` | Disable silence trimming |
| `--silence-threshold <level>` | | `-30dB` | Noise threshold below which audio counts as silence |
| `--silence-duration <seconds>` | | `2.0` | Minimum silence duration, in seconds, before it is trimmed |

`deepgram-transcribe job list`:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--limit <count>` | | all | Show only this many of the most recent transcriptions |

# Commands

```
deepgram-transcribe
├─ cache                               Manage the local response cache
│  ├─ clear                            Delete all cached responses and job history
│  ╰─ status                           Show the cache location and how many responses are stored
├─ dependency                          Manage required external programs
│  ├─ install                          Install missing or outdated programs
│  ├─ list                             List required programs and their status
│  ╰─ update                           Update required programs to their latest versions
├─ job                                 Review past transcriptions and what they cost
│  ├─ inspect <audio-file|request-id>  Show the details and cost of one past transcription
│  ╰─ list                             List past transcriptions and total spending
╰─ transcript                          Create transcripts from audio
   ╰─ create <audio-file>              Transcribe an audio file to Markdown
```

Supported audio formats are `.mp3`, `.m4a`, `.mp4`, `.wav`, `.flac`, `.ogg` and `.aac`.

# Required Programs

Transcribing needs nothing but the binary itself. Making recordings smaller before upload needs `ffmpeg`, and the tool manages that for you rather than leaving you to work out what is missing.

```bash
# What is needed, what is installed, and whether it is good enough
deepgram-transcribe dependency list

# Install anything missing or too old, through brew, apt, pacman, dnf or winget
deepgram-transcribe dependency install

# Move everything to its newest version
deepgram-transcribe dependency update
```

Anything the tool installs itself goes into its own directory, under `$XDG_DATA_HOME/deepgram-transcribe/bin` or `~/.local/share/deepgram-transcribe/bin`, which is added to the path for its own runs only. Nothing outside that directory is touched. If `ffmpeg` is missing when you transcribe, the recording is uploaded at full size and the run says so rather than failing.

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
| **Response caching** | Built in. A repeat request is served from cache, costs nothing and returns immediately. | None. Every run calls the API and is billed. |
| **Cache key** | Keyed on the audio content itself, so a cache hit survives changed request options. | None. The audio is uploaded every time. |
| **Forcing a fresh run** | `-f / --force` when new keyterms or a different model are needed. | Not applicable, since it always re-transcribes. |
| **Cheaper uploads** | Merges stereo to mono and cuts dead air with `ffmpeg` before uploading. | None. The file is uploaded as it is. |
| **Spending history** | `job inspect` and `job list` track request IDs, duration and running spend. | None. Requires the management API or the web dashboard. |
| **Output** | Markdown with a metadata table and speaker turns. | Raw JSON, VTT, SRT or plain text. |

# License

MIT License (c) 2026 Alex Gorbatchev
