`deepgram-transcribe` turns recorded speech into Markdown for people reviewing interviews, meetings, lectures or voice notes, or into JSON for programs that process recordings. It labels speakers, cuts audio without speech before upload while keeping every timestamp on the original recording, reuses locally cached transcripts and keeps a history of transcription costs.

# What It Does

- **Readable transcripts:** Writes Markdown with timestamps, speaker turns and recording metadata.
- **Machine-readable transcripts:** Writes versioned JSON with per-word timings, confidences and speakers for programs that consume transcripts.
- **Reusable results:** Recognizes recordings by their contents, so renaming a file still allows a cache hit.
- **Recognition vocabulary:** Combines built-in engineering keyterms with names and terms supplied on the command line or in a file.
- **Audio preparation:** Merges channels and cuts audio without speech before upload, using a built-in speech model or a volume threshold, when ffmpeg is available.
- **Original timeline:** Reports every timestamp against the original recording, even when parts of it were never uploaded.
- **Spending history:** Lists past requests and retrieves their billed costs when credentials permit it.
- **Agent skill:** Publishes an agent skill that `deepgram-transcribe skill` shows how to install, with compact command output available through `AGENT=1`.

# How It Works

- Give `deepgram-transcribe` a local recording. It reads the audio and looks for a cached response.
- A cache hit returns the saved transcript without uploading the recording again.
- Reusing a cached transcript preserves the original request's history and billed cost.
- On a cache miss, it prepares the audio with ffmpeg when available, then sends the recording and recognition options to Deepgram.
- It maps every returned timestamp back to the original recording and writes the transcript, as Markdown or as JSON with `--format json`, to stdout or the file selected with `--output`.
- It saves the response and request details locally so you can reuse the transcript and inspect its cost later.

# How it Really Works

- Recordings are matched by their original contents. A cache hit can reuse a response made with a different model, language, speaker configuration, vocabulary or trimming settings (`--trim-mode`, `--trim-padding`, `--vad-*`, `--silence-*`, `--no-mono`, `--no-preprocess`). Use `--force` when changed options require a fresh, billable request; the fresh response is saved for exactly those options, so the next run with them is served it.
- `--no-cache` skips both cache reads and writes, including spending-history storage. `--force` skips reads but saves the fresh result.
- Cache and history default to `$XDG_CACHE_HOME/deepgram-transcribe`, or `~/.cache/deepgram-transcribe` when that variable is unset. `--cache-dir` selects another directory. `cache clear` deletes the cached responses and history there without prompting.
- Audio preparation merges stereo channels to mono and cuts the parts of the recording that hold no speech, which lowers the billed duration. The prepared recording is uploaded only when its measured audio duration is strictly shorter. Trimming measures both recordings by counting decoded samples, which stays exact for formats such as ADTS AAC whose length ffprobe can only estimate and works for recordings whose length ffprobe cannot read at all, such as live WebM captures; a mono merge without trimming is measured with ffprobe. Equal, longer or unavailable durations select the original, including a mono conversion that leaves duration unchanged. Re-encoding can increase bytes even when duration decreases; progress reports both comparisons. `--no-preprocess` uploads the original recording.
- `--trim-mode vad`, the default, runs the Silero VAD speech model inside the binary over a 16 kHz mono copy of the recording, so loud non-speech such as traffic or music is cut and quiet speech is kept. It processes audio roughly 50 times faster than real time on one CPU core, so an hour of audio adds about a minute. `--trim-mode silence` keeps everything louder than `--silence-threshold` instead, and `--trim-mode none` cuts nothing. Each kept range is widened by `--trim-padding` on both sides so that the edges of words survive.
- Cutting decodes the recording to raw samples and joins the kept sample ranges end to end. The decoded samples are streamed straight into the encoder and never written to disk. Prepared audio is uploaded as lossless 16-bit FLAC at the recording's sample rate, with its channels or one channel after the mono merge. FLAC adds no encoder start-up delay, so the upload lasts exactly as long as the kept ranges add up to and every kept range is exact to the sample. The last kept range ends where the decoded audio ends, not at the length ffprobe estimates, so formats without a duration index do not lose their tail.
- Every word and utterance time from Deepgram is mapped back to the original recording before Markdown or JSON is written. A word that straddles a cut is placed in the kept range that holds most of it and clipped to that range, so a word never covers removed audio. An utterance keeps its own start and end, so it can span removed audio. Cached responses keep the kept ranges they were transcribed with, so a cache hit maps exactly as the original run did. A preprocessed cached response that has no kept ranges on record cannot be mapped: with `--format json` it is transcribed again, billed, and saved with its ranges, and the reason is printed on stderr; Markdown serves it with a warning that its times may be on the trimmed audio.
- When the speech model or the volume threshold finds nothing worth keeping, nothing is uploaded or billed: a warning is printed and the transcript is empty. Its JSON has an empty `request_id`, a `transcribed_duration` and `cost_estimate_usd` of `0`, an empty `kept_ranges` array and no words or utterances. The empty transcript is neither cached nor recorded in history, so a later run checks the recording again.
- The only temporary file trimming writes is the prepared FLAC upload.
- Temporary prepared audio is written in the operating system's temporary directory and removed when discarded or after success, errors, or cancellation with Ctrl-C/SIGTERM. Forced termination such as SIGKILL cannot run cleanup. Missing or outdated ffmpeg or ffprobe, or a version banner that cannot be read, produces a warning naming the program and the reason, and the original audio is uploaded. Release numbers are read from distribution and build banners such as `7.1.1-1+b1` and `n9.0.2-47-g…`, and master-branch builds such as `N-118000-g…` count as current; a failure during encoding stops the command. A mono merge without trimming selects original audio with a warning when ffprobe cannot read a duration.
- A fresh transcription sends the audio, model, language and keyterms to Deepgram. Speaker labeling, punctuation, readable formatting and utterance segmentation are requested by default. `--no-diarize` disables speaker labeling.
- A run estimates cost from the returned duration, channel count and rates included in the binary. Models without a known rate show an unknown estimate. `job list` and `job inspect` query Deepgram for unresolved billed charges when credentials are available, then save those charges locally. Pending charges appear as `not billed yet`; totals cover the jobs displayed.
- The transcript, explicitly requested help and `skill` instructions go to stdout. Progress, warnings and errors go to stderr. A successful command exits with code `0`; a command error exits with code `1`.
- `AGENT=1`, `true` or `yes` selects TSV listings, flat detail fields and compact diagnostics. Transcript Markdown, transcript JSON and `skill` output stay the same in both modes. Agent help begins with an instruction to run `AGENT=1 deepgram-transcribe skill`.
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

- **[Deepgram account and API key](https://console.deepgram.com/):** Required for fresh transcriptions; a run that needs one fails before any audio is prepared. Set `DEEPGRAM_API_KEY` or pass `--api-key`. Cached responses and local history remain readable without a key; billed-cost lookups require credentials with management API access.
- **[ffmpeg and ffprobe](https://ffmpeg.org/download.html):** Version 4.4 or newer enables audio preparation and duration measurement. Check them with `deepgram-transcribe dependency list`. `dependency install` requests their shared ffmpeg package through your host package manager, using that manager's permissions and installation locations. Transcription uploads original audio when these tools are unavailable.

# Quick Start

```bash
export DEEPGRAM_API_KEY="your-deepgram-api-key"

# Print a transcript
deepgram-transcribe transcript create interview.m4a

# Save the transcript; progress remains on stderr
deepgram-transcribe transcript create interview.m4a > interview.md

# Write JSON for another program
deepgram-transcribe transcript create interview.m4a --format json > interview.json

# Trim by volume instead of with the speech model
deepgram-transcribe transcript create interview.m4a --trim-mode silence --silence-threshold=-35dB

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
| `--format <name>` | | `markdown` | Transcript format: `markdown` or `json` |
| `--force` | `-f` | `false` | Request a fresh transcription even when a cached result exists |
| `--no-diarize` | | `false` | Disable speaker labeling |
| `--no-tech-terms` | | `false` | Exclude built-in engineering keyterms |
| `--no-cache` | | `false` | Skip cache reads and writes, including job history |
| `--no-preprocess` | | `false` | Upload audio without either preparation step |
| `--no-mono` | | `false` | Retain channels during preparation |
| `--trim-mode <mode>` | | `vad` | How to find audio worth uploading: `vad` (speech model), `silence` (volume threshold) or `none` |
| `--trim-padding <seconds>` | | `0.3` | Audio kept on each side of each kept range; not accepted with `--trim-mode none` |
| `--vad-threshold <probability>` | | `0.5` | Speech probability, between 0 and 1 exclusive, at which `vad` hears speech |
| `--vad-min-speech <seconds>` | | `0.25` | Shortest speech `vad` keeps |
| `--vad-min-silence <seconds>` | | `1` | Shortest pause `vad` cuts |
| `--silence-threshold <level>` | | `-30dB` | ffmpeg volume below which `silence` counts audio as silent |
| `--silence-duration <seconds>` | | `2.0` | Shortest silence `silence` cuts |

The `--vad-*` flags are accepted only with `--trim-mode vad`, and `--silence-*` only with `--trim-mode silence`.

`deepgram-transcribe job list`:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--limit <count>` | | `0` (all jobs) | Positive values limit the listing and its totals; zero or negative values show all |

`deepgram-transcribe completion bash|zsh|fish|powershell`:

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `--no-descriptions` | | `false` | Generate shell completions without descriptions |

# JSON Output

`--format json` writes one JSON document. All times are seconds on the original recording's timeline. `schema` changes when a field is removed, renamed or changes meaning; new fields can appear without a change.

```json
{
  "schema": 1,
  "source": {"filename": "interview.m4a", "size_bytes": 1843200},
  "request_id": "5f1c9a2e-7b0d-4e8b-9c51-0d6c2e7f3a10",
  "model": "nova-3",
  "language": "en",
  "diarized": true,
  "duration": 60,
  "transcribed_duration": 25,
  "cache_hit": false,
  "cost_estimate_usd": 0.0023333333333333335,
  "kept_ranges": [{"start": 0, "end": 5}, {"start": 40, "end": 60}],
  "transcript": "Hello again world.",
  "words": [
    {"word": "hello", "punctuated_word": "Hello", "start": 1, "end": 1.5, "confidence": 0.99, "speaker": 0}
  ],
  "utterances": [
    {"start": 1, "end": 40.4, "confidence": 0.95, "speaker": 0, "transcript": "Hello again"}
  ]
}
```

| Field | Meaning |
| :--- | :--- |
| `schema` | Document layout version, `1` |
| `source.filename`, `source.size_bytes` | Name and size of the transcribed file |
| `request_id` | Deepgram request ID of the response; empty when no speech was found and nothing was uploaded |
| `model`, `language` | Model and language of the request that produced the transcript; for a cache hit on audio content alone, the cached request's |
| `diarized` | Whether the request that produced the transcript labeled speakers; `speaker` fields are `null` when `false` |
| `duration` | Length of the original recording, counted from its decoded samples when trimming ran, from ffprobe for a mono merge without trimming, and Deepgram's processed length when the recording was not measured |
| `transcribed_duration` | Length of the audio Deepgram transcribed and billed; `0` when no speech was found and nothing was uploaded |
| `cache_hit` | Whether the response came from the local cache; the rest of the document is identical either way |
| `cost_estimate_usd` | Estimated list-price cost of the request that produced the transcript, or `null` when Deepgram publishes no rate for its model |
| `kept_ranges` | Ranges of the original recording that were uploaded, in order; `null` when nothing was cut, and empty when no speech was found and nothing was uploaded |
| `transcript` | Full text of the best transcription |
| `words[]` | Each word with `start`, `end`, `confidence`, `speaker` and `punctuated_word` |
| `utterances[]` | Each continuous stretch of speech with `start`, `end`, `confidence`, `speaker` and `transcript` |

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
