`deepgram-transcribe` turns recorded speech into Markdown for people reviewing interviews, meetings, lectures or voice notes. It labels speakers, reuses locally cached transcripts and keeps a history of transcription costs.

# What It Does

- **Readable transcripts:** Writes Markdown with timestamps, speaker turns and recording metadata.
- **Reusable results:** Recognizes recordings by their contents, so renaming a file still allows a cache hit.
- **Recognition vocabulary:** Combines built-in engineering keyterms with names and terms supplied on the command line or in a file.
- **Audio preparation:** Uses ffmpeg to merge channels and trim silence before upload when available.
- **Spending history:** Lists past requests and retrieves their billed costs when credentials permit it.
- **Agent skill:** Publishes an agent skill that `deepgram-transcribe skill` shows how to install, with compact command output available through `AGENT=1`.

# How It Works

- Give `deepgram-transcribe` a local recording. It reads the audio and looks for a cached response.
- A cache hit returns the saved transcript without uploading the recording again.
- Reusing a cached transcript preserves the original request's history and billed cost.
- On a cache miss, it prepares the audio with ffmpeg when available, then sends the recording and recognition options to Deepgram.
- It writes the transcript to stdout or the file selected with `--output`.
- It saves the response and request details locally so you can reuse the transcript and inspect its cost later.

# How it Really Works

- Recordings are matched by their original contents. A cache hit can reuse a response made with a different model, language, speaker configuration or vocabulary. Use `--force` when changed options require a fresh, billable request.
- `--no-cache` skips both cache reads and writes, including spending-history storage. `--force` skips reads but saves the fresh result.
- Cache and history default to `$XDG_CACHE_HOME/deepgram-transcribe`, or `~/.cache/deepgram-transcribe` when that variable is unset. `--cache-dir` selects another directory. `cache clear` deletes the cached responses and history there without prompting.
- Audio preparation merges stereo channels to mono and trims silence. The prepared recording is uploaded only when its measured audio duration is strictly shorter. Equal, longer or unavailable durations select the original, including a mono conversion that leaves duration unchanged. Re-encoding can increase bytes even when duration decreases; progress reports both comparisons. `--no-preprocess` uploads the original recording.
- Temporary prepared audio is written in the operating system's temporary directory and removed when discarded or after success, errors, or cancellation with Ctrl-C/SIGTERM. Forced termination such as SIGKILL cannot run cleanup. Missing or outdated ffmpeg or ffprobe produces a warning and the original audio is uploaded; a failure during encoding stops the command. An unreadable duration selects original audio with a warning.
- A fresh transcription sends the audio, model, language and keyterms to Deepgram. Speaker labeling, punctuation, readable formatting and utterance segmentation are requested by default. `--no-diarize` disables speaker labeling.
- A run estimates cost from the returned duration, channel count and rates included in the binary. Models without a known rate show an unknown estimate. `job list` and `job inspect` query Deepgram for unresolved billed charges when credentials are available, then save those charges locally. Pending charges appear as `not billed yet`; totals cover the jobs displayed.
- The transcript, explicitly requested help and `skill` instructions go to stdout. Progress, warnings and errors go to stderr. A successful command exits with code `0`; a command error exits with code `1`.
- `AGENT=1`, `true` or `yes` selects TSV listings, flat detail fields and compact diagnostics. Transcript Markdown and `skill` output stay the same in both modes. Agent help begins with an instruction to run `AGENT=1 deepgram-transcribe skill`.
- `dependency install` and `dependency update` invoke the host package manager and can change packages outside the tool's managed directory. The update command requests installation of declared packages; whether that upgrades an existing installation depends on the package manager.

# Installation

Download the prebuilt binary for your platform from the [latest release](https://github.com/alexgorbatchev/deepgram-transcribe-cli/releases/latest), replacing `X.X.X` below with the version shown on that page.

```bash
# macOS (Apple Silicon)
mkdir -p ~/.local/bin
curl -fSL https://github.com/alexgorbatchev/deepgram-transcribe-cli/releases/latest/download/deepgram-transcribe_X.X.X_darwin_arm64.tar.gz | tar -xz -C ~/.local/bin
export PATH="$HOME/.local/bin:$PATH"
```

# Setup

- **[Deepgram account and API key](https://console.deepgram.com/):** Required for fresh transcriptions. Set `DEEPGRAM_API_KEY` or pass `--api-key`. Cached responses and local history remain readable without a key; billed-cost lookups require credentials with management API access.
- **[ffmpeg and ffprobe](https://ffmpeg.org/download.html):** Version 4.4 or newer enables audio preparation and duration measurement. Check them with `deepgram-transcribe dependency list`. `dependency install` requests their shared ffmpeg package through your host package manager, using that manager's permissions and installation locations. Transcription uploads original audio when these tools are unavailable.

# Quick Start

```bash
export DEEPGRAM_API_KEY="your-deepgram-api-key"

# Print a transcript
deepgram-transcribe transcript create interview.m4a

# Save the transcript; progress remains on stderr
deepgram-transcribe transcript create interview.m4a > interview.md

# Request a fresh transcript with additional names
deepgram-transcribe transcript create interview.m4a --force -t Envoy -t Gorbatchev -o interview.md

# Inspect one request or recent spending
deepgram-transcribe job inspect interview.m4a
deepgram-transcribe job list --limit 10
```

Sample Output (excerpt from transcript stdout):

```markdown
# Transcript: interview.m4a

## Metadata

| Property | Value |
| :--- | :--- |
| **Duration** | 00:05 |
| **Model** | `nova-3` |
| **Diarization** | `true` |

---

## Transcript

### Speaker 0 (00:00 - 00:05)

Hello world from Deepgram
```

# Options & Flags

Global flags:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--api-key <key>` | | Empty; uses `DEEPGRAM_API_KEY` | Deepgram credentials |
| `--cache-dir <path>` | | Empty; resolves to the XDG cache directory or `~/.cache/deepgram-transcribe` | Response cache and history directory |

`deepgram-transcribe`:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--version` | `-v` | `false` | Print the bare version and exit; root command only |

Every command:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--help` | `-h` | `false` | Print help for the selected command |

`deepgram-transcribe transcript create <audio-file>`:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--term <word>` | `-t` | `[]` | Additional recognition keyterms; repeat or separate with commas |
| `--terms-file <path>` | | Empty | Additional keyterms, one per line |
| `--model <name>` | `-m` | `nova-3` | Deepgram transcription model |
| `--language <code>` | `-l` | `en` | Language spoken in the audio |
| `--output <path>` | `-o` | Empty; writes to stdout | Create or overwrite a transcript file |
| `--force` | `-f` | `false` | Request a fresh transcription even when a cached result exists |
| `--no-diarize` | | `false` | Disable speaker labeling |
| `--no-tech-terms` | | `false` | Exclude built-in engineering keyterms |
| `--no-cache` | | `false` | Skip cache reads and writes, including job history |
| `--no-preprocess` | | `false` | Upload audio without either preparation step |
| `--no-mono` | | `false` | Retain channels during preparation |
| `--no-trim-silence` | | `false` | Retain silence during preparation |
| `--silence-threshold <level>` | | `-30dB` | ffmpeg threshold below which audio counts as silence |
| `--silence-duration <seconds>` | | `2.0` | Silence duration before trimming |

`deepgram-transcribe job list`:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--limit <count>` | | `0` (all jobs) | Positive values limit the listing and its totals; zero or negative values show all |

`deepgram-transcribe completion bash|zsh|fish|powershell`:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--no-descriptions` | | `false` | Generate shell completions without descriptions |

# Commands

| Command | Result or side effect |
| :--- | :--- |
| `transcript create <audio-file>` | Print or save a transcript |
| `job list` | List stored requests and spending for displayed jobs |
| `job inspect <audio-file\|request-id>` | Show one stored request and its cost |
| `cache status` | Show the cache directory and recorded response count |
| `cache clear` | Delete cached responses and history without prompting |
| `dependency list` | Check ffmpeg and ffprobe versions and status |
| `dependency install` | Request the shared ffmpeg package for missing or outdated tools |
| `dependency update` | Request host-package-manager installation for declared programs |
| `skill` | Print the Skills CLI install command and the skill's URL |
| `help [command]` | Print help for a command path |
| `completion bash\|zsh\|fish\|powershell` | Print a shell completion script |

The audio must be supported by Deepgram; common recording formats include MP3, M4A, MP4, WAV, FLAC, Ogg, AAC and Opus. Paths with spaces work when quoted.

# Agent Mode

Install the agent skill with the [Skills CLI](https://github.com/vercel-labs/skills) before operating the CLI. `deepgram-transcribe skill` prints the same command and the skill's location:

```bash
npx skills add alexgorbatchev/deepgram-transcribe-cli --skill deepgram-transcribe
AGENT=1 deepgram-transcribe job list --limit 10
```

The skill covers all commands, generated shell completion commands, argument and flag contracts, environment variables, output formats, errors and side effects. It is published at [`skills/deepgram-transcribe/SKILL.md`](skills/deepgram-transcribe/SKILL.md).

# License

[MIT License](LICENSE) © 2026 Alex Gorbatchev
