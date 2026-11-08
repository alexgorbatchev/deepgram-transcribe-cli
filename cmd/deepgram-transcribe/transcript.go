package main

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/internal/cliout"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/internal/deps"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/audio"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/markdown"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/terms"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/timeline"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/transcriptjson"
)

const (
	defaultModel    = "nova-3"
	defaultLanguage = "en"

	// termsSummaryLimit caps how many words are listed when explaining that a
	// cached response was requested with different keyterms.
	termsSummaryLimit = 10

	formatMarkdown = "markdown"
	formatJSON     = "json"
)

// Flags that tune one trim mode only. Naming them with another mode is an
// error rather than a silent no-op.
var (
	vadOnlyFlags     = []string{"vad-threshold", "vad-min-speech", "vad-min-silence"}
	silenceOnlyFlags = []string{"silence-threshold", "silence-duration"}
)

// transcriptOptions holds the flags of `transcript create`.
type transcriptOptions struct {
	extraTerms       []string
	termsFile        string
	model            string
	language         string
	outputFile       string
	noDiarize        bool
	noTechTerms      bool
	noCache          bool
	force            bool
	noPreprocess     bool
	noMono           bool
	format           string
	trimModeFlag     string
	trimMode         audio.TrimMode
	trimPadding      float64
	silenceThreshold string
	silenceDuration  string
	vad              audio.VADOptions
}

// diarizeModel returns the Deepgram diarizer to run, or an empty string when the
// caller asked for a transcript without speaker labels.
func (o *transcriptOptions) diarizeModel() string {
	if o.noDiarize {
		return ""
	}
	return deepgram.DiarizeModelLatest
}

// wantsMono reports whether audio that is cut should also be merged into one
// channel. Deepgram transcribes and bills a request without multichannel as
// one channel either way, so merging only makes the upload smaller and never
// justifies re-encoding a recording that is not cut.
func (o *transcriptOptions) wantsMono() bool { return !o.noPreprocess && !o.noMono }

// effectiveTrimMode returns how audio is trimmed before upload, which is not
// at all when preprocessing is off.
func (o *transcriptOptions) effectiveTrimMode() audio.TrimMode {
	if o.noPreprocess {
		return audio.TrimNone
	}
	return o.trimMode
}

// wantsTrim reports whether anything may be cut before upload.
func (o *transcriptOptions) wantsTrim() bool { return o.effectiveTrimMode().Cuts() }

// preprocessOptions returns how audio is prepared before upload, without the
// programs that run it.
func (o *transcriptOptions) preprocessOptions() audio.PreprocessOptions {
	return audio.PreprocessOptions{
		Mono:             o.wantsMono(),
		Trim:             o.effectiveTrimMode(),
		SilenceThreshold: o.silenceThreshold,
		SilenceDuration:  o.silenceDuration,
		VAD:              o.vad,
		Padding:          o.trimPadding,
	}
}

// validate checks the flags before any audio is read or any money is spent.
func (o *transcriptOptions) validate(cmd *cobra.Command) error {
	if o.format != formatMarkdown && o.format != formatJSON {
		return fmt.Errorf("unknown format %q; use %s or %s", o.format, formatMarkdown, formatJSON)
	}

	mode, err := audio.ParseTrimMode(o.trimModeFlag)
	if err != nil {
		return err
	}
	o.trimMode = mode

	flags := cmd.Flags()
	for _, rule := range []struct {
		names []string
		mode  audio.TrimMode
	}{
		{vadOnlyFlags, audio.TrimVAD},
		{silenceOnlyFlags, audio.TrimSilence},
	} {
		for _, name := range rule.names {
			if flags.Changed(name) && mode != rule.mode {
				return fmt.Errorf("--%s applies only with --trim-mode %s", name, rule.mode)
			}
		}
	}
	if flags.Changed("trim-padding") && mode == audio.TrimNone {
		return errors.New("--trim-padding does not apply with --trim-mode none")
	}

	if o.trimPadding < 0 || math.IsNaN(o.trimPadding) {
		return fmt.Errorf("trim padding %v is negative", o.trimPadding)
	}
	if mode == audio.TrimVAD {
		return o.vad.Validate()
	}
	return nil
}

// newTranscriptCmd builds the transcript subject.
func newTranscriptCmd(g *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transcript",
		Short: "Create transcripts from audio",
	}

	cmd.AddCommand(newTranscriptCreateCmd(g))

	return cmd
}

// newTranscriptCreateCmd builds the transcript create verb.
func newTranscriptCreateCmd(g *globalOptions) *cobra.Command {
	opts := &transcriptOptions{}

	cmd := &cobra.Command{
		Use:   "create <audio-file>",
		Short: "Transcribe an audio file to Markdown or JSON",
		Long: `Transcribe the speech in an audio file to Markdown or JSON, labelled by speaker.

The transcript is written to stdout unless --output names a file, so it can be
redirected:

  deepgram-transcribe transcript create interview.m4a > interview.md
  deepgram-transcribe transcript create interview.m4a -t Envoy -o notes.md
  deepgram-transcribe transcript create interview.m4a --format json > interview.json

Before upload, the parts of the recording without speech are cut out, which
lowers the cost. Every time in the transcript is still on the original
recording's timeline. A recording in which no speech is found is not uploaded
at all: the transcript is empty and nothing is billed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.validate(cmd); err != nil {
				return err
			}
			return runTranscriptCreate(cmd, g, opts, args[0])
		},
	}

	flags := cmd.Flags()
	flags.StringArrayVarP(&opts.extraTerms, "term", "t", nil, "Additional keyterm to boost recognition, such as a company or person name (repeatable or comma-separated)")
	flags.StringVar(&opts.termsFile, "terms-file", "", "File of additional keyterms, one per line")
	flags.StringVarP(&opts.model, "model", "m", defaultModel, "Transcription model to use")
	flags.StringVarP(&opts.language, "language", "l", defaultLanguage, "Language spoken in the audio")
	flags.StringVarP(&opts.outputFile, "output", "o", "", "Write the transcript to this file instead of stdout")
	flags.BoolVarP(&opts.force, "force", "f", false, "Re-transcribe and be billed again, even on a cache hit")
	flags.BoolVar(&opts.noDiarize, "no-diarize", false, "Disable speaker diarization")
	flags.BoolVar(&opts.noTechTerms, "no-tech-terms", false, "Disable the built-in engineering keyterms")
	flags.BoolVar(&opts.noCache, "no-cache", false, "Bypass the response cache, for both reads and writes")
	flags.BoolVar(&opts.noPreprocess, "no-preprocess", false, "Disable ffmpeg preprocessing before upload")
	flags.BoolVar(&opts.noMono, "no-mono", false, "Keep every channel of trimmed audio instead of merging them into one")
	flags.StringVar(&opts.format, "format", formatMarkdown, "Transcript format: markdown or json")
	flags.StringVar(&opts.trimModeFlag, "trim-mode", string(audio.TrimVAD), "How to find the audio worth uploading: vad (speech model), silence (volume threshold) or none")
	flags.Float64Var(&opts.trimPadding, "trim-padding", audio.DefaultTrimPadding, "Seconds of audio kept on each side of what trimming keeps")
	flags.Float64Var(&opts.vad.Threshold, "vad-threshold", audio.DefaultVADThreshold, "Speech probability, between 0 and 1, at which the vad mode hears speech")
	flags.Float64Var(&opts.vad.MinSpeech, "vad-min-speech", audio.DefaultVADMinSpeech, "Shortest speech, in seconds, that the vad mode keeps")
	flags.Float64Var(&opts.vad.MinSilence, "vad-min-silence", audio.DefaultVADMinSilence, "Shortest pause, in seconds, that the vad mode cuts")
	flags.StringVar(&opts.silenceThreshold, "silence-threshold", audio.DefaultSilenceThreshold, "Volume below which the silence mode counts audio as silent")
	flags.StringVar(&opts.silenceDuration, "silence-duration", audio.DefaultSilenceDuration, "Shortest silence, in seconds, that the silence mode cuts")

	return cmd
}

// transcriptRun carries one run of `transcript create` through its steps: read
// the audio, serve a cached response if there is one, otherwise shrink the
// audio and ask Deepgram, then record what it cost and write the result.
type transcriptRun struct {
	cmd    *cobra.Command
	out    *cliout.Printer
	global *globalOptions
	opts   *transcriptOptions

	audioPath   string
	audioSize   int64
	sourceBytes []byte

	keyTerms   []string
	request    deepgram.Options
	sourceKey  string
	optionsKey string

	// answered is the request that produced the response: the one asked for,
	// or for a cache hit on audio content alone, the one recorded with the
	// cached response. Everything describing the transcript comes from it.
	answered deepgram.Options

	client     *deepgram.Client
	uploadPath string
	// noSpeech is set when trimming found no speech, so nothing is uploaded
	// and the transcript is empty.
	noSpeech        bool
	response        *deepgram.PreRecordedResponse
	timeline        timeline.Timeline
	cost            string
	costUSD         *float64
	servedFromCache bool
	// uploadChannels is the channel count of the audio uploaded, or for a
	// cache hit the count recorded with the cached response; zero when it is
	// not known.
	uploadChannels int
}

func runTranscriptCreate(cmd *cobra.Command, g *globalOptions, opts *transcriptOptions, audioPath string) error {
	run := &transcriptRun{
		cmd:       cmd,
		out:       cliout.New(cmd.OutOrStdout(), cmd.ErrOrStderr()),
		global:    g,
		opts:      opts,
		audioPath: audioPath,
	}

	if err := run.loadSource(); err != nil {
		return err
	}
	if err := run.buildRequest(); err != nil {
		return err
	}

	run.serveCachedResponse()

	cleanup, err := run.prepareUpload()
	if err != nil {
		return err
	}
	defer cleanup()

	if run.noSpeech {
		run.answerWithoutSpeech()
	} else {
		// The key is needed only for an upload, so a recording without
		// speech is answered without one.
		if err := run.connect(); err != nil {
			return err
		}
		if err := run.fetchTranscript(); err != nil {
			return err
		}
		run.recordJob()
	}

	return run.writeTranscript()
}

// loadSource reads the recording up front, because its contents alone decide
// whether a cached response can be served.
func (r *transcriptRun) loadSource() error {
	info, err := os.Stat(r.audioPath)
	if err != nil {
		return fmt.Errorf("checking audio file %q: %w", r.audioPath, err)
	}

	data, err := os.ReadFile(r.audioPath)
	if err != nil {
		return fmt.Errorf("reading audio file %q: %w", r.audioPath, err)
	}

	r.audioSize = info.Size()
	r.sourceBytes = data
	r.uploadPath = r.audioPath

	return nil
}

// buildRequest assembles the vocabulary and transcription settings, then derives
// the two keys the cache lookup uses.
func (r *transcriptRun) buildRequest() error {
	var termLists [][]string

	if !r.opts.noTechTerms {
		termLists = append(termLists, terms.DefaultTechTerms())
	}
	if len(r.opts.extraTerms) > 0 {
		termLists = append(termLists, terms.ParseCustomTerms(r.opts.extraTerms))
	}
	if r.opts.termsFile != "" {
		fileTerms, err := terms.LoadTermsFromFile(r.opts.termsFile)
		if err != nil {
			return fmt.Errorf("loading terms file: %w", err)
		}
		termLists = append(termLists, fileTerms)
	}

	r.keyTerms = terms.CombineTerms(termLists...)
	r.request = deepgram.Options{
		Model:           r.opts.model,
		Language:        r.opts.language,
		DiarizeModel:    r.opts.diarizeModel(),
		SmartFormatting: true,
		Utterances:      true,
		Punctuate:       true,
		Terms:           r.keyTerms,
	}

	r.sourceKey = deepgram.SourceAudioKey(r.sourceBytes)
	r.optionsKey = deepgram.CacheKey(r.sourceBytes, r.request, r.opts.preprocessOptions().Fingerprint())
	r.answered = r.request

	return nil
}

// serveCachedResponse looks for a cached Deepgram response for this audio. It
// runs before any audio is processed, so a cache hit costs neither money nor
// time.
func (r *transcriptRun) serveCachedResponse() {
	if r.opts.noCache || r.opts.force {
		return
	}

	cacheDir := r.global.resolvedCacheDir()
	name := filepath.Base(r.audioPath)

	// An exact hit: same audio, same options, same keyterms.
	if cached, err := deepgram.GetCachedResponse(cacheDir, r.optionsKey); err == nil && cached != nil {
		if r.rejectUntimed(cached) {
			return
		}
		r.out.Status("Cache hit for %s, serving the cached response.", name)
		r.out.Detail("cache_key: %s", r.optionsKey)
		r.serveEnvelope(cached)
		return
	}

	// The same audio transcribed earlier under different options. Serving that
	// response avoids a charge the user probably did not intend.
	envelope, err := deepgram.FindCachedJobBySourceSHA(cacheDir, r.sourceKey)
	if err != nil || envelope == nil || envelope.Response == nil || r.rejectUntimed(envelope) {
		return
	}

	r.out.Status("Cache hit for %s on audio content, from a request with different options.", name)
	if len(envelope.Record.Terms) > 0 || len(r.keyTerms) > 0 {
		r.out.Warn("The keyterms differ from the cached request.")
		r.out.Bullets([]string{
			fmt.Sprintf("cached: %s", formatTermsSummary(envelope.Record.Terms)),
			fmt.Sprintf("requested: %s", formatTermsSummary(r.keyTerms)),
		})
	}
	r.out.Status("Serving the cached response. Use --force to re-transcribe and be billed again.")
	r.out.Detail("cache_key: %s", envelope.Record.SHA256)

	r.serveEnvelope(envelope)
	r.answered = recordedRequest(r.request, envelope.Record)
}

// recordedRequest returns the request a job record describes. Formatting
// options are not recorded and are the same for every request this command
// makes, so they are taken from current.
func recordedRequest(current deepgram.Options, record deepgram.JobRecord) deepgram.Options {
	recorded := current
	recorded.Model = record.Model
	recorded.Language = record.Language
	recorded.DiarizeModel = record.DiarizeModel
	recorded.Terms = record.Terms
	return recorded
}

// rejectUntimed reports whether a cached response must not be served because
// its times cannot be placed on the original recording: it was preprocessed,
// but saved without the ranges trimming kept. JSON promises original-timeline
// times, so such a response is transcribed again and saved with its ranges.
// Markdown is for reading and serves it with a warning instead.
func (r *transcriptRun) rejectUntimed(envelope *deepgram.JobRecordEnvelope) bool {
	if envelope.Timeline != nil || !envelope.Record.Preprocessed || r.opts.format != formatJSON {
		return false
	}
	r.out.Warn("The cached transcript for %s has no record of what trimming cut, so its times cannot be mapped to the original recording; transcribing it again.", filepath.Base(r.audioPath))
	r.out.Detail("cache_key: %s", envelope.Record.SHA256)
	return true
}

// connect creates the Deepgram client when the run has to ask Deepgram, and
// fails when no API key was given. A cached answer needs no key.
func (r *transcriptRun) connect() error {
	if r.response != nil {
		return nil
	}

	r.client = r.global.newClient()
	if r.client == nil {
		return cliout.WithHint(
			errors.New("no Deepgram API key was given"),
			fmt.Sprintf("Set %s in your environment, or pass --api-key.", apiKeyEnvVar),
		)
	}
	return nil
}

// serveEnvelope answers the run from a cached response and the timeline its
// audio was uploaded on.
func (r *transcriptRun) serveEnvelope(envelope *deepgram.JobRecordEnvelope) {
	r.response = envelope.Response
	r.servedFromCache = true
	r.uploadChannels = envelope.Record.Channels

	if envelope.Timeline != nil {
		r.timeline = *envelope.Timeline
		return
	}
	if envelope.Record.Preprocessed {
		r.out.Warn("The cached transcript was saved without a record of what trimming cut, so its times may be on the trimmed audio rather than the original recording.")
		r.out.Hint("Use --force to transcribe again and be billed again.")
	}
}

// preprocessingReady reports whether ffmpeg and ffprobe can prepare and measure audio, and
// explains on stderr when it cannot.
//
// A missing or outdated tool is not fatal. The audio is uploaded unprocessed,
// which may retain billable silence but still produces a transcript, so this
// reports the problem and lets the run continue.
func (r *transcriptRun) preprocessingReady() bool {
	// Inspect both reports so diagnostics identify the unavailable tool and
	// why, including a version banner that could not be read.
	reports, _ := r.global.newDependencyManager().Verify(r.cmd.Context())

	for _, name := range []string{deps.FFmpeg, deps.FFprobe} {
		report, found := deps.Find(reports, name)
		if found && report.Satisfied {
			continue
		}
		if found && report.Error != "" {
			r.out.Warn("%s", report.Error)
		} else {
			r.out.Warn("%s is not usable.", name)
		}
		r.out.Status("Uploading original audio without preprocessing.")
		r.out.Hint("Run `deepgram-transcribe dependency install` to set %s up.", name)
		return false
	}

	return true
}

// prepareUpload trims the recording, and uploads the prepared audio when
// trimming cut something, which makes it shorter. It is skipped when a cached response already answered the request, because
// nothing will be uploaded.
//
// The returned function removes the temporary audio and is safe to call even
// when no file was written.
func (r *transcriptRun) prepareUpload() (func(), error) {
	noCleanup := func() {}

	if r.response != nil {
		return noCleanup, nil
	}
	if !r.opts.wantsTrim() {
		return noCleanup, r.countUploadChannels()
	}

	if !r.preprocessingReady() {
		return noCleanup, nil
	}

	ctx := r.cmd.Context()
	r.out.Status("Preprocessing %s before upload.", filepath.Base(r.audioPath))
	r.out.Detail("mono: %t trim_mode: %s", r.opts.wantsMono(), r.opts.effectiveTrimMode())

	source, err := audio.Probe(ctx, r.audioPath, r.global.depsRunner)
	if err != nil {
		if err := ctx.Err(); err != nil {
			return noCleanup, err
		}
		r.out.Warn("Could not measure the recording; uploading original audio.")
		r.out.Detail("%v", err)
		return noCleanup, nil
	}
	r.timeline.SourceDuration = source.Duration
	r.uploadChannels = source.Channels

	options := r.opts.preprocessOptions()
	options.Runner = r.global.depsRunner
	options.Stream = r.global.mediaStream
	prepared, cleanup, err := audio.PreprocessAudio(ctx, r.audioPath, source, options)
	// Decoded audio was counted sample by sample, which beats ffprobe's
	// estimate for containers without a duration index.
	if prepared.SourceDuration > 0 {
		r.timeline.SourceDuration = prepared.SourceDuration
	}
	if errors.Is(err, audio.ErrNoSpeech) {
		r.out.Warn("No speech was detected; nothing is uploaded and the transcript is empty.")
		r.noSpeech = true
		// Every part of the recording was cut.
		r.timeline.Kept = []timeline.Range{}
		return noCleanup, nil
	}
	if err != nil {
		return noCleanup, fmt.Errorf("preparing audio for upload: %w", err)
	}
	if prepared.Path == r.audioPath {
		r.out.Status("Nothing worth cutting was found; uploading original audio.")
		return noCleanup, nil
	}

	// Prepared audio is always shorter: a recording kept whole is uploaded
	// as it is. Both durations are counted from decoded samples.
	r.out.Status("Audio duration is %.3f s, down from %.3f s.", prepared.Duration, prepared.SourceDuration)
	r.out.Detail("kept_ranges: %d", len(prepared.Kept))
	r.uploadPath = prepared.Path
	r.uploadChannels = prepared.Channels
	r.timeline.Kept = prepared.Kept

	if info, err := os.Stat(prepared.Path); err == nil {
		r.reportUploadSize(info.Size())
	}

	return cleanup, nil
}

// countUploadChannels reads the original recording's channel count when
// nothing is prepared, for the cost estimate of a response that does not
// report how many channels Deepgram processed. Without a usable ffprobe the
// count stays unknown.
func (r *transcriptRun) countUploadChannels() error {
	ctx := r.cmd.Context()
	source, err := audio.Probe(ctx, r.audioPath, r.global.depsRunner)
	if err != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.out.Detail("channels: unknown (%v)", err)
		return nil
	}
	r.uploadChannels = source.Channels
	return nil
}

// Preprocessing changes duration and channels, but re-encoding can increase the
// byte count. Report the measured comparison rather than promising a reduction.
func (r *transcriptRun) reportUploadSize(size int64) {
	switch {
	case size < r.audioSize:
		r.out.Status("Upload is %s, down from %s.", formatFileSize(size), formatFileSize(r.audioSize))
	case size > r.audioSize:
		r.out.Status("Upload is %s, up from %s.", formatFileSize(size), formatFileSize(r.audioSize))
	default:
		r.out.Status("Upload is %s, unchanged from the original.", formatFileSize(size))
	}
}

// answerWithoutSpeech answers a recording in which trimming found no speech
// with an empty transcript. Nothing was asked of Deepgram, so nothing was
// billed, cached or recorded in history.
func (r *transcriptRun) answerWithoutSpeech() {
	r.response = &deepgram.PreRecordedResponse{}
	free := 0.0
	r.costUSD = &free
	r.cost = deepgram.FormatCostUSD(free)
}

// fetchTranscript asks Deepgram to transcribe the recording, unless a saved
// transcript already answered the request.
func (r *transcriptRun) fetchTranscript() error {
	if r.response != nil {
		return nil
	}

	payload, err := r.uploadBytes()
	if err != nil {
		return err
	}

	r.out.Status("Transcribing %s with the %s model.", filepath.Base(r.audioPath), r.opts.model)
	r.out.Detail("upload_bytes: %d terms: %d", len(payload), len(r.keyTerms))

	response, err := r.client.Transcribe(
		r.cmd.Context(),
		bytes.NewReader(payload),
		audio.DetectMIMEType(r.uploadPath),
		r.request,
	)
	if err != nil {
		return fmt.Errorf("transcribing %q: %w", filepath.Base(r.audioPath), err)
	}
	r.response = response

	if !r.opts.noCache {
		if err := deepgram.SaveCachedResponse(r.global.resolvedCacheDir(), r.optionsKey, response, r.timeline); err != nil {
			r.out.Warn("Could not write the response to cache: %v", err)
		}
	}

	return nil
}

// uploadBytes returns the audio to send. The original recording is already in
// memory, so it is only re-read when preprocessing produced a different file.
func (r *transcriptRun) uploadBytes() ([]byte, error) {
	if r.uploadPath == r.audioPath {
		return r.sourceBytes, nil
	}

	payload, err := os.ReadFile(r.uploadPath)
	if err != nil {
		return nil, fmt.Errorf("reading prepared audio file %q: %w", r.uploadPath, err)
	}

	return payload, nil
}

// recordJob stores what was transcribed and what it is expected to cost, so that
// `job list` and `job inspect` can report on it later.
func (r *transcriptRun) recordJob() {
	duration := r.response.Metadata.Duration

	// Deepgram bills the duration of the channels it reports processing. A
	// response without that count falls back to the upload's channels, then
	// to one.
	channels := r.response.Metadata.Channels
	if channels < 1 {
		channels = max(r.uploadChannels, 1)
	}

	r.cost = deepgram.CalculateCostWithOptions(duration, channels, r.answered)
	if cost, priced := deepgram.EstimateCostUSD(duration, channels, r.answered); priced {
		r.costUSD = &cost
	}

	if r.opts.noCache || r.servedFromCache {
		return
	}

	absolutePath, err := filepath.Abs(r.audioPath)
	if err != nil {
		absolutePath = r.audioPath
	}

	record := deepgram.JobRecord{
		RequestID:       r.response.Metadata.RequestID,
		Filename:        filepath.Base(r.audioPath),
		FilePath:        absolutePath,
		SourceSHA256:    r.sourceKey,
		SHA256:          r.optionsKey,
		Timestamp:       time.Now(),
		DurationSeconds: duration,
		Channels:        channels,
		Model:           r.opts.model,
		Language:        r.request.Language,
		DiarizeModel:    r.request.DiarizeModel,
		Preprocessed:    r.uploadPath != r.audioPath,
		Terms:           r.keyTerms,
		CostUSD:         r.cost,
		CostIsActual:    false,
	}

	if err := deepgram.SaveJobRecord(r.global.resolvedCacheDir(), record); err != nil {
		r.out.Warn("Could not write the job record to history: %v", err)
	}
}

// writeTranscript renders the transcript, with every time mapped to the
// original recording, to the screen or to a file.
func (r *transcriptRun) writeTranscript() error {
	content, err := r.renderTranscript()
	if err != nil {
		return err
	}

	if r.opts.outputFile == "" {
		if _, err := r.out.Out().Write(content); err != nil {
			return fmt.Errorf("writing transcript: %w", err)
		}
		return nil
	}

	if dir := filepath.Dir(r.opts.outputFile); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating output directory for %q: %w", r.opts.outputFile, err)
		}
	}

	if err := os.WriteFile(r.opts.outputFile, content, 0o644); err != nil {
		return fmt.Errorf("writing transcript to %q: %w", r.opts.outputFile, err)
	}

	if r.cost == deepgram.CostUnknown {
		r.out.Success(
			"Saved the transcript to %s. Deepgram does not publish a rate for the %s model, so what it cost will only be known once `job inspect` can read it from your bill.",
			r.opts.outputFile, r.answered.EffectiveModel(),
		)
		return nil
	}

	r.out.Success("Saved the transcript to %s, at a cost of %s.", r.opts.outputFile, r.cost)

	return nil
}

// renderTranscript formats the response in the requested format.
func (r *transcriptRun) renderTranscript() ([]byte, error) {
	mapped := r.response.OnTimeline(r.timeline)

	if r.opts.format == formatJSON {
		return transcriptjson.Format(mapped, transcriptjson.Meta{
			Filename: filepath.Base(r.audioPath),
			FileSize: r.audioSize,
			Model:    r.answered.EffectiveModel(),
			Language: r.answered.Language,
			Diarized: r.answered.Diarized(),
			CacheHit: r.servedFromCache,
			CostUSD:  r.costUSD,
			Timeline: r.timeline,
		})
	}

	return []byte(markdown.Format(&mapped, markdown.MetaInfo{
		Filename:       filepath.Base(r.audioPath),
		FileSize:       formatFileSize(r.audioSize),
		Model:          r.answered.EffectiveModel(),
		Diarized:       r.answered.Diarized(),
		KeyTerms:       r.answered.Keyterms(),
		Timestamp:      time.Now(),
		SourceDuration: r.timeline.SourceDuration,
	})), nil
}

// formatTermsSummary lists the words a transcript listened for, shortened to stay
// readable when the vocabulary is long.
func formatTermsSummary(termList []string) string {
	if len(termList) == 0 {
		return "(none)"
	}

	sorted := make([]string, len(termList))
	copy(sorted, termList)
	sort.Strings(sorted)

	if len(sorted) > termsSummaryLimit {
		return fmt.Sprintf("%s and %d more", strings.Join(sorted[:termsSummaryLimit], ", "), len(sorted)-termsSummaryLimit)
	}

	return strings.Join(sorted, ", ")
}
