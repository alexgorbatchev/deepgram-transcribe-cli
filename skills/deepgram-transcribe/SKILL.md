---
name: deepgram-transcribe
description: Use when operating deepgram-transcribe to transcribe audio, inspect transcription costs, manage cached responses, or manage ffmpeg.
author: alexgorbatchev
metadata:
  created_on: 2026-10-01 21:03
  last_modified: 2026-11-08 14:46
  status: current
---

## Workflows

Run `AGENT=1 deepgram-transcribe skill` to print the Skills CLI command that installs
this skill and the URL where it is published.

```bash
export DEEPGRAM_API_KEY="your-deepgram-api-key"
deepgram-transcribe transcript create interview.opus > interview.md
deepgram-transcribe transcript create interview.m4a -t Envoy -t 'Alex,Harvey' -o notes.md
deepgram-transcribe transcript create session.opus --format json > session.json
deepgram-transcribe transcript create session.opus --trim-mode silence --silence-threshold=-35dB
deepgram-transcribe job list --limit 10
deepgram-transcribe job inspect interview.m4a
deepgram-transcribe cache status
deepgram-transcribe dependency list
```

Use `--force` to request a fresh, billable transcription with changed options.
Use `--no-cache` to bypass cached responses and leave response/history storage untouched.
Use `--no-preprocess` to upload the original audio. Use `--no-mono` to keep
every channel of trimmed audio and `--trim-mode none` to upload the whole recording.

## Output, environment and errors

- Transcription outputs Markdown with a metadata table and timestamped speaker turns, or with `--format json` the JSON document described below, identically in both modes. `--output` writes that result to a file, creates parent directories and overwrites an existing file.
- Every time in Markdown and JSON is in seconds on the original recording's timeline, even when trimming cut parts of the upload. A word that straddles a cut is placed in the kept range holding most of it and clipped to that range; an utterance keeps its own start and end and can span removed audio. The Markdown metadata shows the original `Duration` and, when trimming shortened the upload, the `Transcribed` duration.
- Trimming: `--trim-mode vad` (default) runs the Silero VAD speech model, built into the binary, over a 16 kHz mono decode; a 32 ms frame is speech at or above `--vad-threshold`, speech ends after `--vad-min-silence` seconds below the threshold minus 0.15, and speech shorter than `--vad-min-speech` is dropped. It runs about 50 times faster than real time on one core. `--trim-mode silence` keeps everything outside ffmpeg `silencedetect` silences of at least `--silence-duration` below `--silence-threshold`. Both widen each kept range by `--trim-padding` on each side, merge overlaps, decode to raw samples streamed through the CLI (never written to disk), join the kept sample ranges and encode them as 16-bit FLAC at the input's sample rate (one channel after the mono merge, otherwise the input's channels), which has no encoder delay, so `kept_ranges` is exact to the sample and the upload's duration equals their sum. The last kept range ends at the end of the decoded audio, not at ffprobe's duration estimate. Mono-only preparation also produces FLAC. Prepared audio is uploaded as `audio/flac`. `--vad-*` flags fail unless the mode is `vad`, `--silence-*` flags fail unless it is `silence`, and `--trim-padding` fails with `none`. `--no-preprocess` disables trimming whatever the mode. When nothing is worth keeping, a warning is printed and nothing is uploaded, billed, cached or recorded in history: the transcript is empty, and its JSON has `request_id` `""`, `transcribed_duration` and `cost_estimate_usd` `0`, `kept_ranges` `[]`, and empty `words` and `utterances`.
- Progress, confirmations, warnings and errors go to stderr. Explicit help and `skill` go to stdout. Syntax-error usage goes to stderr. Success exits 0; a command error exits 1.
- `AGENT=1`, `true` or `yes` selects compact output; matching ignores case and surrounding whitespace. Other values select human output. Listings use TSV, details use `key: value` lines, and diagnostics use `INFO:`, `WARN:`, `OK:`, `ERR:` or `HINT:`. Human output uses bordered tables, aligned fields and bracketed tags. Agent help begins with the skill-reading alert, retains full descriptions and lists every command. The human help tree omits the `skill` and `completion` commands, which still run.
- `COLUMNS` accepts a positive integer for human help/output width. Otherwise terminal width is detected. Human help wraps to that width and is never truncated. When neither is available, human help stays unwrapped and human command output uses 100 columns. Agent help is always complete.
- `DEEPGRAM_API_KEY` supplies credentials when `--api-key` is empty. A nonempty flag takes precedence. A cache hit works without credentials; a fresh request (cache miss, `--no-cache` or `--force`) needs credentials and, when they are missing, fails after preparing the audio and before uploading it; a recording in which trimming finds no speech needs no upload and succeeds without them. History commands query the management API for pending charges when credentials are available and save resolved charges locally.
- An empty `--cache-dir` resolves to `$XDG_CACHE_HOME/deepgram-transcribe`, then `~/.cache/deepgram-transcribe`; if the home directory is unavailable, it uses `.cache/deepgram-transcribe` relative to the working directory.
- `$XDG_DATA_HOME/deepgram-transcribe/bin` (or `~/.local/share/deepgram-transcribe/bin`) is prepended to this process's `PATH` for managed-program discovery. The `dependency install` and `dependency update` commands use the host package manager and can change packages outside that directory.
- Preprocessing uses ffmpeg 4.4 or newer and creates temporary audio in the operating system's temporary directory (`os.TempDir()`; `TMPDIR` on Unix, `GetTempPath` on Windows). ffprobe reads the first audio stream's sample rate, channels and duration (the container's when the stream states none). With trimming, both durations are counted from decoded samples, which stays exact where ffprobe only estimates (ADTS AAC, VBR MP3 without a Xing header) and works where it reports none (live WebM). Prepared audio exists only when trimming cut something, so it is always shorter; a recording kept whole is uploaded as it is and never re-encoded only to merge its channels, because Deepgram transcribes a request without multichannel as one channel. The mono merge applies to trimmed audio only. Decoded audio, including the 16 kHz mono copy `vad` mode reads, is piped and never written there; only the prepared FLAC is. Temporary audio is removed after success, command errors, or cancellation, including Ctrl-C and SIGTERM. Forced termination such as SIGKILL cannot run cleanup. Progress reports duration and byte comparisons; re-encoding can increase bytes despite shorter duration. Deepgram's response duration times the channel count it reports processing determines the estimated charge; a response without that count uses the uploaded audio's channels (read by ffprobe even with `--no-preprocess` or with nothing to prepare), then one.
- Missing or outdated ffmpeg or ffprobe, or an unreadable version banner, emits a warning with the program and reason (for example `could not parse ffprobe version output`) and uploads original audio. Versions are read from the first `-version` line: a leading release number (`7.1.1`, `7.1.1-1+b1`, `n9.0.2-47-g…`, `7.0-static`) is compared with 4.4, and master-branch builds (`N-118000-g…`, `git-…`, `2024-01-21-git-…`) count as current. An error while processing with usable ffmpeg fails the command. Duration-probe errors warn and select original audio; cancellation fails the command. File-read, terms-file, API, and transcript-write errors fail the command. Cache/history write failures warn while allowing transcript delivery. Management-cost lookup failures leave charges pending, with details in agent mode.
- Cached responses store the kept ranges they were transcribed with, so a cache hit maps times exactly as the original run and produces the same JSON apart from `cache_hit`. A preprocessed cached response with no kept ranges on record cannot be mapped: with `--format json` it is treated as a cache miss, transcribed again (billed) and saved with its ranges, with the reason as a `WARN:` line; Markdown serves it with a warning that its times may be on the trimmed audio.
- Cached results are matched by original audio contents, including requests with different options. Responses cached by version 3.0.0 or earlier match by audio contents only. An exact hit needs the same model, language, diarization, keyterms and preparation (`--trim-mode`, `--trim-padding`, `--vad-*`, `--silence-*`, mono merge); any other cached response for the audio is served as a content hit. A cached response can therefore retain an earlier model, vocabulary, speaker configuration or trimming. Markdown and JSON then report the cached request's model, language, diarization, keyterms and cost estimate, not the ones asked for. `--force` skips reads and updates storage; `--no-cache` skips both reads and writes. `cache clear` deletes responses and job history in the selected cache directory.
- Serving a cached transcript preserves its original history and billed cost, including when requested options differ. History-only entries contain no reusable response and are ignored as transcript hits; lookup can still find another saved response for the same audio.
- Uploads go to Deepgram's pre-recorded transcription API. Keyterms, model, language and formatting options are sent with the audio. A cancelled run cancels the in-flight request; a completed server-side transcription may already be billable.

## JSON transcript (`--format json`, schema 1)

One indented JSON object followed by a newline. Times are seconds on the original
recording. `schema` changes only when a field is removed, renamed or changes meaning.

| Field | Type | Meaning |
| :--- | :--- | :--- |
| `schema` | number | `1` |
| `source.filename` | string | Base name of the input file |
| `source.size_bytes` | number | Input file size |
| `request_id` | string | Deepgram request ID; empty when no speech was found and nothing was uploaded |
| `model` | string | Model of the request that produced the transcript (`nova-3` when empty); for a cache hit on audio content alone, the cached request's |
| `language` | string | Language of that request; empty for content hits on responses cached without a recorded language |
| `diarized` | bool | Whether that request labeled speakers |
| `duration` | number | Original recording length: decoded sample count when trimming ran, Deepgram's duration otherwise |
| `transcribed_duration` | number | Duration Deepgram processed and bills; 0 when nothing was uploaded |
| `cache_hit` | bool | Served from the local cache |
| `cost_estimate_usd` | number or null | List-price estimate for that request's model and keyterms; null for models without a published rate |
| `kept_ranges` | array or null | `{start, end}` ranges of the original that were uploaded, in order; null when nothing was cut, empty when no speech was found |
| `transcript` | string | Full text of the first channel's best alternative |
| `words[]` | array | `word`, `punctuated_word`, `start`, `end`, `confidence`, `speaker` (null when not diarized) |
| `utterances[]` | array | `start`, `end`, `confidence`, `speaker` (null when not diarized), `transcript` |

`words` and `utterances` are empty arrays, never null.

## Command and flag reference

Tables list parser defaults exactly. An empty string is shown as an empty pair
of backticks; effective defaults are described above or in the behavior column.
Every command accepts inherited `--api-key` and `--cache-dir`. `--help`/`-h` is
local to every command. `--version`/`-v` is available on the root only.
Booleans accept `--flag` and explicit true/false values. String flags take a value;
short aliases accept the same value. Domain verbs without a positional argument
reject extra arguments. Subject groups display their help when run alone.

### `deepgram-transcribe`

Invocation: `deepgram-transcribe`

Display root help, invoke a subject command, or print the bare version with `--version`.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--api-key` | `` | `string` | `` | Credentials; effective default is `DEEPGRAM_API_KEY` |
| `--cache-dir` | `` | `string` | `` | Cache directory; effective default follows XDG/home resolution above |
| `--help` | `-h` | `bool` | `false` | Print requested help |
| `--version` | `-v` | `bool` | `false` | Print the bare version and exit |

### `deepgram-transcribe skill`

Invocation: `deepgram-transcribe skill`

Print the Skills CLI install command and the published location of this skill,
byte-identical in both modes:

```text
Install the deepgram-transcribe skill using the Skills CLI:

  npx skills add alexgorbatchev/deepgram-transcribe-cli --skill deepgram-transcribe

Skill:

  https://github.com/alexgorbatchev/deepgram-transcribe-cli/tree/main/skills/deepgram-transcribe
```

Accepts zero positional arguments and has no command-specific flags. Runs offline
without credentials or repository files. Output-write failures fail the command.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print command help |

### `deepgram-transcribe transcript`

Invocation: `deepgram-transcribe transcript`

Display the transcript command subtree.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print subject help |

### `deepgram-transcribe transcript create`

Invocation: `deepgram-transcribe transcript create <audio-file>`

Supply exactly one `<audio-file>` path. The file is read locally; extension-based
MIME detection recognizes mp3, m4a, mp4, wav, flac, ogg and aac. Other extensions,
including opus, are uploaded as `application/octet-stream` for Deepgram to detect.
Deepgram determines whether the actual audio is supported.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print command help |
| `--term` | `-t` | `stringArray` | `[]` | Repeat or use comma-separated keyterms; whitespace and empty terms are removed |
| `--terms-file` | `` | `string` | `` | Additional keyterms, one per line; blank and comment lines are skipped |
| `--model` | `-m` | `string` | `nova-3` | Model name passed to Deepgram; accepted names are determined by the API |
| `--language` | `-l` | `string` | `en` | Language identifier passed to Deepgram; accepted identifiers are determined by the API |
| `--output` | `-o` | `string` | `` | Empty means stdout; a path creates/overwrites the transcript file |
| `--format` | `` | `string` | `markdown` | `markdown` or `json`; other values fail before any work |
| `--force` | `-f` | `bool` | `false` | Bypass cache reads and request a billable transcription |
| `--no-diarize` | `` | `bool` | `false` | Disable speaker labeling in the API request |
| `--no-tech-terms` | `` | `bool` | `false` | Exclude built-in engineering keyterms |
| `--no-cache` | `` | `bool` | `false` | Bypass cache reads and writes, including job history |
| `--no-preprocess` | `` | `bool` | `false` | Disable both processing steps |
| `--no-mono` | `` | `bool` | `false` | Keep every channel of trimmed audio instead of merging them into one |
| `--trim-mode` | `` | `string` | `vad` | `vad`, `silence` or `none` (case-insensitive); other values fail |
| `--trim-padding` | `` | `float64` | `0.3` | Seconds kept on each side of each kept range; negative fails; rejected with `--trim-mode none` |
| `--vad-threshold` | `` | `float64` | `0.5` | Speech probability in (0, 1); `vad` mode only |
| `--vad-min-speech` | `` | `float64` | `0.25` | Shortest kept speech in seconds; negative fails; `vad` mode only |
| `--vad-min-silence` | `` | `float64` | `1` | Shortest cut pause in seconds; negative fails; `vad` mode only |
| `--silence-threshold` | `` | `string` | `-30dB` | ffmpeg silencedetect noise level; accepts ffmpeg amplitude or decibel syntax; `silence` mode only |
| `--silence-duration` | `` | `string` | `2.0` | ffmpeg silencedetect duration in seconds; ffmpeg validates its value; `silence` mode only |

### `deepgram-transcribe job`

Invocation: `deepgram-transcribe job`

Display the history command subtree.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print subject help |

### `deepgram-transcribe job list`

Invocation: `deepgram-transcribe job list`

Accepts zero positional arguments. List recent jobs and totals for the displayed
jobs. Output columns are date, file, duration, channels, model, cost and request ID.
Unresolved charges show `not billed yet`; an empty history is reported on stderr.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print command help |
| `--limit` | `` | `int` | `0` | Positive values limit recent jobs; zero and negative values list all |

### `deepgram-transcribe job inspect`

Invocation: `deepgram-transcribe job inspect <audio-file|request-id>`

Supply exactly one `<audio-file|request-id>`. Match a readable audio file by its
contents or look up a request ID from history. Print original file/path, request
ID, timestamp, duration, channels, model, preprocessing, speaker configuration,
keyterms and estimated/actual cost. `Preprocessing requested` (agent key
`preprocessing_requested`) reflects the stored request's processing options;
it does not assert that preparation ran or reduced bytes. A missing match fails
with a history hint.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print command help |

### `deepgram-transcribe cache`

Invocation: `deepgram-transcribe cache`

Display the cache command subtree.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print subject help |

### `deepgram-transcribe cache status`

Invocation: `deepgram-transcribe cache status`

Accepts zero positional arguments. Print the selected cache directory and number
of recorded cached responses.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print command help |

### `deepgram-transcribe cache clear`

Invocation: `deepgram-transcribe cache clear`

Accepts zero positional arguments. Delete cached JSON responses and history
without prompting; report the number removed on stderr. Other files are retained.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print command help |

### `deepgram-transcribe dependency`

Invocation: `deepgram-transcribe dependency`

Display the external-program command subtree.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print subject help |

### `deepgram-transcribe dependency list`

Invocation: `deepgram-transcribe dependency list`

Accepts zero positional arguments. Execute ffmpeg and ffprobe version queries and list program,
minimum version (4.4), installed version and status (`ok`, `outdated`, `not installed` or `version unknown`). Missing/outdated tools are
shown as status information; it does not cause a command error.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print command help |

### `deepgram-transcribe dependency install`

Invocation: `deepgram-transcribe dependency install`

Accepts zero positional arguments. Install missing or outdated ffmpeg or ffprobe through their shared ffmpeg package using
Homebrew, apt-get, pacman, dnf, winget or Chocolatey, according to host support.
Already satisfied dependencies are skipped. Package-manager permissions apply.
Failure returns an error and suggests manual installation.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print command help |

### `deepgram-transcribe dependency update`

Invocation: `deepgram-transcribe dependency update`

Accepts zero positional arguments. Invoke the host package manager's installation
operation for every declared program, including installed ffmpeg and ffprobe. Whether that
operation upgrades an existing package depends on the host package manager.
Failure returns an error and suggests manual updating.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print command help |

### `deepgram-transcribe help`

Invocation: `deepgram-transcribe help [command]`

Supply an optional command path, such as `help transcript create`. Print help for
that path; an unknown target prints an unknown-topic diagnostic and root help.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print help-command help |

### `deepgram-transcribe completion`

Invocation: `deepgram-transcribe completion`

Display shell completion choices. Accepts zero positional arguments.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print completion help |

### `deepgram-transcribe completion bash`

Invocation: `deepgram-transcribe completion bash`

Accepts zero positional arguments. Print a Bash completion script to stdout;
loading it requires bash-completion. Save or source the script to activate it.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print shell help |
| `--no-descriptions` | `` | `bool` | `false` | Generate completions without descriptions |

### `deepgram-transcribe completion zsh`

Invocation: `deepgram-transcribe completion zsh`

Accepts zero positional arguments. Print a Zsh completion script to stdout. Load
completions with `compinit`, then source the script or save it on your `fpath`.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print shell help |
| `--no-descriptions` | `` | `bool` | `false` | Generate completions without descriptions |

### `deepgram-transcribe completion fish`

Invocation: `deepgram-transcribe completion fish`

Accepts zero positional arguments. Print a Fish completion script to stdout;
source it or save it in Fish's completions directory.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print shell help |
| `--no-descriptions` | `` | `bool` | `false` | Generate completions without descriptions |

### `deepgram-transcribe completion powershell`

Invocation: `deepgram-transcribe completion powershell`

Accepts zero positional arguments. Print a PowerShell completion script to stdout;
pipe it to `Out-String | Invoke-Expression` or add it to your PowerShell profile.

| Flag | Short | Type | Parser default | Behavior |
| :--- | :--- | :--- | :--- | :--- |
| `--help` | `-h` | `bool` | `false` | Print shell help |
| `--no-descriptions` | `` | `bool` | `false` | Generate completions without descriptions |
