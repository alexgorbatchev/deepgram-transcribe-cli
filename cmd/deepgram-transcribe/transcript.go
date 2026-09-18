package main

import (
	"bytes"
	"errors"
	"fmt"
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
)

const (
	defaultModel    = "nova-3"
	defaultLanguage = "en"

	// termsSummaryLimit caps how many words are listed when explaining that a
	// saved transcript was made with a different vocabulary.
	termsSummaryLimit = 10
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
	noTrimSilence    bool
	silenceThreshold string
	silenceDuration  string
}

// wantsMono reports whether stereo audio should be merged into one channel,
// which halves what Deepgram bills for the recording.
func (o *transcriptOptions) wantsMono() bool { return !o.noPreprocess && !o.noMono }

// wantsTrimSilence reports whether long silences should be cut before upload.
func (o *transcriptOptions) wantsTrimSilence() bool { return !o.noPreprocess && !o.noTrimSilence }

// newTranscriptCmd builds the transcript subject.
func newTranscriptCmd(g *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transcript",
		Short: "Create transcripts from recorded audio",
	}

	cmd.AddCommand(newTranscriptCreateCmd(g))

	return cmd
}

// newTranscriptCreateCmd builds the transcript create verb.
func newTranscriptCreateCmd(g *globalOptions) *cobra.Command {
	opts := &transcriptOptions{}

	cmd := &cobra.Command{
		Use:   "create <audio-file>",
		Short: "Turn a recording into a Markdown transcript",
		Long: `Turn a recording into a Markdown transcript, labelled with who is speaking.

The transcript is printed to the screen unless you name a file to write with
--output, so it can be redirected:

  deepgram-transcribe transcript create interview.m4a > interview.md
  deepgram-transcribe transcript create interview.m4a -t Envoy -t Alex -o interview.md`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTranscriptCreate(cmd, g, opts, args[0])
		},
	}

	flags := cmd.Flags()
	flags.StringArrayVarP(&opts.extraTerms, "term", "t", nil, "Extra word or name to listen for, such as a company or a person (repeat or separate with commas)")
	flags.StringVar(&opts.termsFile, "terms-file", "", "File of extra words to listen for, one per line")
	flags.StringVarP(&opts.model, "model", "m", defaultModel, "Transcription model to use")
	flags.StringVarP(&opts.language, "language", "l", defaultLanguage, "Language spoken in the recording")
	flags.StringVarP(&opts.outputFile, "output", "o", "", "Write the transcript to this file instead of the screen")
	flags.BoolVarP(&opts.force, "force", "f", false, "Transcribe the recording again even if a saved transcript exists")
	flags.BoolVar(&opts.noDiarize, "no-diarize", false, "Do not label who is speaking")
	flags.BoolVar(&opts.noTechTerms, "no-tech-terms", false, "Do not listen for the built-in engineering vocabulary")
	flags.BoolVar(&opts.noCache, "no-cache", false, "Do not reuse or keep a saved copy of the transcript")
	flags.BoolVar(&opts.noPreprocess, "no-preprocess", false, "Upload the recording as it is, without making it smaller first")
	flags.BoolVar(&opts.noMono, "no-mono", false, "Keep both stereo channels instead of merging them into one")
	flags.BoolVar(&opts.noTrimSilence, "no-trim-silence", false, "Keep long silences instead of cutting them out")
	flags.StringVar(&opts.silenceThreshold, "silence-threshold", audio.DefaultSilenceThreshold, "How quiet a passage has to be to count as silence")
	flags.StringVar(&opts.silenceDuration, "silence-duration", audio.DefaultSilenceDuration, "How many seconds a silence has to last before it is cut")

	return cmd
}

// transcriptRun carries one run of `transcript create` through its steps: read
// the recording, reuse a saved transcript if there is one, otherwise shrink the
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

	uploadPath string
	response   *deepgram.PreRecordedResponse
	cost       string
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

	run.reuseSavedTranscript()

	cleanup, err := run.prepareUpload()
	if err != nil {
		return err
	}
	defer cleanup()

	if err := run.fetchTranscript(); err != nil {
		return err
	}

	run.recordJob()

	return run.writeTranscript()
}

// loadSource reads the recording up front, because its contents alone decide
// whether a saved transcript can be reused.
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
// the two keys the saved-transcript lookup uses.
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
		Diarize:         !r.opts.noDiarize,
		SmartFormatting: true,
		Utterances:      true,
		Punctuate:       true,
		Terms:           r.keyTerms,
	}

	r.sourceKey = deepgram.SourceAudioKey(r.sourceBytes)
	r.optionsKey = deepgram.CacheKey(r.sourceBytes, r.request)

	return nil
}

// reuseSavedTranscript looks for a transcript of this recording that was already
// paid for. It runs before any audio is processed, so a repeat request costs
// neither money nor time.
func (r *transcriptRun) reuseSavedTranscript() {
	if r.opts.noCache || r.opts.force {
		return
	}

	cacheDir := r.global.resolvedCacheDir()
	name := filepath.Base(r.audioPath)

	// An exact match: same recording, same settings, same vocabulary.
	if saved, err := deepgram.GetCachedResponse(cacheDir, r.optionsKey); err == nil && saved != nil {
		r.out.Status("Reusing the saved transcript for %s.", name)
		r.out.Detail("cache_key: %s", r.optionsKey)
		r.response = saved
		return
	}

	// The same recording transcribed earlier with different settings. Returning
	// it avoids a charge the user probably did not intend.
	envelope, err := deepgram.FindCachedJobBySourceSHA(cacheDir, r.sourceKey)
	if err != nil || envelope == nil || envelope.Response == nil {
		return
	}

	r.out.Status("Found a saved transcript for %s from an earlier run.", name)
	if len(envelope.Record.Terms) > 0 || len(r.keyTerms) > 0 {
		r.out.Warn("The words to listen for differ from the saved transcript.")
		r.out.Bullets([]string{
			fmt.Sprintf("saved: %s", formatTermsSummary(envelope.Record.Terms)),
			fmt.Sprintf("requested: %s", formatTermsSummary(r.keyTerms)),
		})
	}
	r.out.Status("Returning the saved transcript so you are not charged again. Use --force to transcribe it again.")
	r.out.Detail("cache_key: %s", envelope.Record.SHA256)

	r.response = envelope.Response
}

// ffmpegReady reports whether ffmpeg can be used to shrink the recording, and
// explains on stderr when it cannot.
//
// A missing or outdated ffmpeg is not fatal. The recording is uploaded as it is,
// which costs more but still produces a transcript, so this reports the problem
// and lets the run continue.
func (r *transcriptRun) ffmpegReady() bool {
	// Verify returns an error when anything is unsatisfied. Only ffmpeg matters
	// here, so the report for it is what decides, not the overall result.
	reports, _ := r.global.newDependencyManager().Verify(r.cmd.Context())

	report, found := deps.Find(reports, deps.FFmpeg)
	if found && report.Satisfied {
		return true
	}

	if found && report.Error != "" {
		r.out.Warn("%s", report.Error)
	} else {
		r.out.Warn("ffmpeg is not usable.")
	}

	r.out.Status("The recording will be uploaded as it is, which costs more.")
	r.out.Hint("Run `deepgram-transcribe dependency install` to set ffmpeg up.")

	return false
}

// prepareUpload makes the recording cheaper to transcribe by merging stereo into
// one channel and cutting long silences. It is skipped when a saved transcript
// already answered the request, because nothing will be uploaded.
//
// The returned function removes the temporary audio and is safe to call even
// when no file was written.
func (r *transcriptRun) prepareUpload() (func(), error) {
	noCleanup := func() {}

	if r.response != nil {
		return noCleanup, nil
	}
	if !r.opts.wantsMono() && !r.opts.wantsTrimSilence() {
		return noCleanup, nil
	}

	if !r.ffmpegReady() {
		return noCleanup, nil
	}

	r.out.Status("Preparing %s for upload.", filepath.Base(r.audioPath))
	r.out.Detail("mono: %t trim_silence: %t", r.opts.wantsMono(), r.opts.wantsTrimSilence())

	processed, cleanup, err := audio.PreprocessAudio(r.cmd.Context(), r.audioPath, audio.PreprocessOptions{
		Mono:             r.opts.wantsMono(),
		TrimSilence:      r.opts.wantsTrimSilence(),
		SilenceThreshold: r.opts.silenceThreshold,
		SilenceDuration:  r.opts.silenceDuration,
	})
	if err != nil {
		return noCleanup, fmt.Errorf("preparing audio for upload: %w", err)
	}

	r.uploadPath = processed

	if info, err := os.Stat(processed); err == nil {
		r.out.Status("Upload is %s, down from %s.", formatFileSize(info.Size()), formatFileSize(r.audioSize))
	}

	return cleanup, nil
}

// fetchTranscript asks Deepgram to transcribe the recording, unless a saved
// transcript already answered the request.
func (r *transcriptRun) fetchTranscript() error {
	if r.response != nil {
		return nil
	}

	client := r.global.newClient()
	if client == nil {
		return cliout.WithHint(
			errors.New("no Deepgram API key was given"),
			fmt.Sprintf("Set %s in your environment, or pass --api-key.", apiKeyEnvVar),
		)
	}

	payload, err := r.uploadBytes()
	if err != nil {
		return err
	}

	r.out.Status("Transcribing %s with the %s model.", filepath.Base(r.audioPath), r.opts.model)
	r.out.Detail("upload_bytes: %d terms: %d", len(payload), len(r.keyTerms))

	response, err := client.Transcribe(
		r.cmd.Context(),
		bytes.NewReader(payload),
		audio.DetectMIMEType(r.audioPath),
		r.request,
	)
	if err != nil {
		return fmt.Errorf("transcribing %q: %w", filepath.Base(r.audioPath), err)
	}
	r.response = response

	if !r.opts.noCache {
		if err := deepgram.SaveCachedResponse(r.global.resolvedCacheDir(), r.optionsKey, response); err != nil {
			r.out.Warn("The transcript could not be saved for reuse: %v", err)
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

	// Deepgram reports how many channels it actually processed. Older saved
	// responses predate that field, so fall back to what was asked for.
	channels := r.response.Metadata.Channels
	if channels < 1 {
		channels = 2
		if r.opts.wantsMono() {
			channels = 1
		}
	}

	r.cost = deepgram.CalculateCostWithOptions(duration, channels, r.request)

	if r.opts.noCache {
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
		Preprocessed:    r.opts.wantsMono() || r.opts.wantsTrimSilence(),
		Terms:           r.keyTerms,
		CostUSD:         r.cost,
		CostIsActual:    false,
	}

	if err := deepgram.SaveJobRecord(r.global.resolvedCacheDir(), record); err != nil {
		r.out.Warn("This job could not be added to your history: %v", err)
	}
}

// writeTranscript renders the Markdown transcript to the screen or to a file.
func (r *transcriptRun) writeTranscript() error {
	content := markdown.Format(r.response, markdown.MetaInfo{
		Filename:  filepath.Base(r.audioPath),
		FileSize:  formatFileSize(r.audioSize),
		Model:     r.opts.model,
		Diarized:  !r.opts.noDiarize,
		KeyTerms:  r.keyTerms,
		Timestamp: time.Now(),
	})

	if r.opts.outputFile == "" {
		if _, err := fmt.Fprint(r.out.Out(), content); err != nil {
			return fmt.Errorf("writing transcript: %w", err)
		}
		return nil
	}

	if dir := filepath.Dir(r.opts.outputFile); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating output directory for %q: %w", r.opts.outputFile, err)
		}
	}

	if err := os.WriteFile(r.opts.outputFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing transcript to %q: %w", r.opts.outputFile, err)
	}

	r.out.Success("Saved the transcript to %s, at a cost of %s.", r.opts.outputFile, r.cost)

	return nil
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
