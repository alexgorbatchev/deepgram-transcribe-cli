package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/audio"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/markdown"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/terms"
)

var (
	// version is populated at compile time by GoReleaser via ldflags (-X main.version={{.Version}})
	version = "dev"
)

type transcribeOptions struct {
	extraTerms       []string
	termsFilePath    string
	model            string
	language         string
	outputFile       string
	apiKey           string
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

func NewRootCmd() *cobra.Command {
	return NewRootCmdWithDirAndEndpoint(deepgram.DefaultCacheDir(), "")
}

func NewRootCmdWithDirAndEndpoint(cacheDir, overrideEndpoint string) *cobra.Command {
	opts := &transcribeOptions{}

	rootCmd := &cobra.Command{
		Use:     "deepgram-transcribe [command|flags] <audio-file>",
		Short:   "Transcribe audio phone conversations using Deepgram and output Markdown",
		Version: version,
		Long: `deepgram-transcribe is a CLI tool that sends audio files (mp3, m4a, wav, etc.) to Deepgram API v1/listen
and formats the transcript into clean Markdown with speaker diarization and custom keyterm boosting.

Subcommands:
  deepgram-transcribe cost <file|request-id>    Display post-transcription cost & job metadata
  deepgram-transcribe history                   List recent transcription jobs and total spending
  deepgram-transcribe cache [status|clear]      Manage or clear local SHA-256 transcript cache

Default transcription output goes to stdout:
  deepgram-transcribe conversation.m4a > conversation.md
  deepgram-transcribe -t Envoy -t Alex conversation.mp3 -o output.md`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTranscribeWithEndpoint(cmd, args, overrideEndpoint, cacheDir, opts)
		},
	}

	flags := rootCmd.Flags()
	flags.StringArrayVarP(&opts.extraTerms, "term", "t", nil, "Additional terms to boost recognition (company names, people names, etc., can be used multiple times or comma-separated)")
	flags.StringVar(&opts.termsFilePath, "terms-file", "", "Path to a text file containing additional terms (one per line or comma-separated)")
	flags.StringVarP(&opts.model, "model", "m", "nova-3", "Deepgram transcription model (e.g. nova-3, nova-2, flux)")
	flags.StringVarP(&opts.language, "language", "l", "en", "Audio language code (e.g. en, en-US)")
	flags.StringVarP(&opts.outputFile, "output", "o", "", "Output file path (default stdout)")
	flags.StringVar(&opts.apiKey, "api-key", "", "Deepgram API key (defaults to DEEPGRAM_API_KEY environment variable)")
	flags.BoolVar(&opts.noDiarize, "no-diarize", false, "Disable speaker diarization")
	flags.BoolVar(&opts.noTechTerms, "no-tech-terms", false, "Disable preconfigured common tech interview terms")
	flags.BoolVar(&opts.noCache, "no-cache", false, "Bypass local SHA-256 transcript response cache")
	flags.BoolVarP(&opts.force, "force", "f", false, "Force fresh Deepgram re-transcription even if cached source audio exists with different terms")

	// Cost reduction preprocessing flags (ON by default)
	flags.BoolVar(&opts.noPreprocess, "no-preprocess", false, "Disable automatic audio preprocessing (mono conversion & silence trimming)")
	flags.BoolVar(&opts.noMono, "no-mono", false, "Disable stereo-to-mono downmixing")
	flags.BoolVar(&opts.noTrimSilence, "no-trim-silence", false, "Disable silence/dead-air trimming")
	flags.StringVar(&opts.silenceThreshold, "silence-threshold", audio.DefaultSilenceThreshold, "Noise threshold for silence removal (e.g. -30dB, -40dB)")
	flags.StringVar(&opts.silenceDuration, "silence-duration", audio.DefaultSilenceDuration, "Minimum silence duration in seconds to trim (e.g. 2.0)")

	// Subcommand alias for 'deepgram-transcribe transcribe <file>'
	transcribeSubCmd := &cobra.Command{
		Use:   "transcribe <audio-file>",
		Short: "Transcribe audio file (alias for root command)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTranscribeWithEndpoint(cmd, args, overrideEndpoint, cacheDir, opts)
		},
	}
	transcribeSubCmd.Flags().AddFlagSet(flags)

	// Add Subcommands
	rootCmd.AddCommand(
		transcribeSubCmd,
		newCostCmdWithDir(cacheDir),
		newHistoryCmdWithDir(cacheDir),
		newCacheCmdWithDir(cacheDir),
	)

	return rootCmd
}

func newCostCmdWithDir(cacheDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "cost <audio-file|request-id>",
		Short: "Display post-transcription cost & metadata for an audio file or Request ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			record, err := deepgram.FindJobRecordByTarget(cacheDir, target)
			if err != nil || record == nil {
				return fmt.Errorf("no transcription job found for %q", target)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "=== Job Cost & Details ===\n")
			fmt.Fprintf(out, "File Name:        %s\n", record.Filename)
			if record.FilePath != "" {
				fmt.Fprintf(out, "Original Path:    %s\n", record.FilePath)
			}
			fmt.Fprintf(out, "Request ID:       %s\n", record.RequestID)
			fmt.Fprintf(out, "SHA-256 Hash:     %s\n", record.SHA256)
			if !record.Timestamp.IsZero() {
				fmt.Fprintf(out, "Transcribed At:   %s\n", record.Timestamp.Format("2006-01-02 15:04:05 MST"))
			}
			fmt.Fprintf(out, "Processed Audio:  %s (%.1fs) | %d Channel(s) | Preprocessed: %t\n",
				deepgram.FormatSeconds(record.DurationSeconds), record.DurationSeconds, record.Channels, record.Preprocessed)
			fmt.Fprintf(out, "Model:            %s\n", record.Model)
			fmt.Fprintf(out, "Cost:             %s USD\n", record.CostUSD)

			return nil
		},
	}
}

func newHistoryCmdWithDir(cacheDir string) *cobra.Command {
	var limitHistory int

	cmd := &cobra.Command{
		Use:   "history",
		Short: "List recent transcription jobs and total cumulative spending",
		RunE: func(cmd *cobra.Command, args []string) error {
			records, err := deepgram.ListJobRecords(cacheDir)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if len(records) == 0 {
				fmt.Fprintln(out, "No transcription history found.")
				return nil
			}

			if limitHistory > 0 && limitHistory < len(records) {
				records = records[:limitHistory]
			}

			var totalDurationSecs float64
			var totalCostFloat float64

			fmt.Fprintf(out, "%-12s  %-20s  %-10s  %-8s  %-8s  %-10s  %s\n",
				"DATE", "FILE", "DURATION", "CHANNELS", "MODEL", "COST", "REQUEST ID")
			fmt.Fprintf(out, "%s\n", strings.Repeat("-", 85))

			for _, rec := range records {
				dateStr := rec.Timestamp.Format("2006-01-02")
				if rec.Timestamp.IsZero() {
					dateStr = "N/A"
				}

				fn := rec.Filename
				if len(fn) > 20 {
					fn = fn[:17] + "..."
				}

				durStr := deepgram.FormatSeconds(rec.DurationSeconds)

				fmt.Fprintf(out, "%-12s  %-20s  %-10s  %-8d  %-8s  %-10s  %s\n",
					dateStr, fn, durStr, rec.Channels, rec.Model, rec.CostUSD, rec.RequestID)

				totalDurationSecs += rec.DurationSeconds

				costClean := strings.TrimPrefix(rec.CostUSD, "$")
				if val, err := strconv.ParseFloat(costClean, 64); err == nil {
					totalCostFloat += val
				}
			}

			fmt.Fprintf(out, "%s\n", strings.Repeat("-", 85))
			fmt.Fprintf(out, "TOTAL USAGE: %d call(s) | Total Duration: %s | Total Billed: $%.3f USD\n",
				len(records), deepgram.FormatSeconds(totalDurationSecs), totalCostFloat)

			return nil
		},
	}

	cmd.Flags().IntVar(&limitHistory, "limit", 0, "Limit number of recent history entries to display")
	return cmd
}

func newCacheCmdWithDir(cacheDir string) *cobra.Command {
	cacheCmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage local SHA-256 transcript response cache",
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Display transcript cache statistics and entry count",
		RunE: func(cmd *cobra.Command, args []string) error {
			records, err := deepgram.ListJobRecords(cacheDir)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Cache Directory: %s\n", cacheDir)
			fmt.Fprintf(out, "Cached Jobs:     %d entry/entries\n", len(records))

			return nil
		},
	}

	clearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear local transcript response cache and job history",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := deepgram.ClearCache(cacheDir); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Cleared local transcript cache.")
			return nil
		},
	}

	cacheCmd.AddCommand(statusCmd, clearCmd)
	return cacheCmd
}

func runTranscribeWithEndpoint(cmd *cobra.Command, args []string, overrideEndpoint, cacheDir string, opts *transcribeOptions) error {
	if opts == nil {
		opts = &transcribeOptions{}
	}

	audioPath := args[0]

	fileInfo, err := os.Stat(audioPath)
	if err != nil {
		return fmt.Errorf("checking audio file %q: %w", audioPath, err)
	}

	// Read original audio bytes FIRST for instant cache checking
	originalAudioBytes, err := os.ReadFile(audioPath)
	if err != nil {
		return fmt.Errorf("reading audio file %q: %w", audioPath, err)
	}

	// Build term list & options
	var termLists [][]string

	if !opts.noTechTerms {
		termLists = append(termLists, terms.DefaultTechTerms())
	}

	if len(opts.extraTerms) > 0 {
		parsedExtra := terms.ParseCustomTerms(opts.extraTerms)
		termLists = append(termLists, parsedExtra)
	}

	if opts.termsFilePath != "" {
		fileTerms, err := terms.LoadTermsFromFile(opts.termsFilePath)
		if err != nil {
			return fmt.Errorf("loading terms file: %w", err)
		}
		termLists = append(termLists, fileTerms)
	}

	combinedTerms := terms.CombineTerms(termLists...)

	mimeType := audio.DetectMIMEType(audioPath)

	dgOpts := deepgram.Options{
		Model:           opts.model,
		Language:        opts.language,
		Diarize:         !opts.noDiarize,
		SmartFormatting: true,
		Utterances:      true,
		Punctuate:       true,
		Terms:           combinedTerms,
	}

	sourceSHA := deepgram.SourceAudioKey(originalAudioBytes)
	optsKey := deepgram.CacheKey(originalAudioBytes, dgOpts)

	var resp *deepgram.PreRecordedResponse

	// CHECK CACHE FIRST BEFORE RUNNING FFMPEG OR MAKING API CALLS
	if !opts.noCache && !opts.force {
		// 1. Exact options match
		cachedResp, err := deepgram.GetCachedResponse(cacheDir, optsKey)
		if err == nil && cachedResp != nil {
			cmd.PrintErrf("Using cached transcript for %s (%s, key: %s...)\n",
				filepath.Base(audioPath), formatFileSize(fileInfo.Size()), optsKey[:8])
			resp = cachedResp
		} else {
			// 2. Source audio match
			jobEnv, err := deepgram.FindCachedJobBySourceSHA(cacheDir, sourceSHA)
			if err == nil && jobEnv != nil && jobEnv.Response != nil {
				cmd.PrintErrf("[transcribe] Found cached transcript for %s (source audio matched).\n", filepath.Base(audioPath))
				if len(jobEnv.Record.Terms) > 0 || len(combinedTerms) > 0 {
					cmd.PrintErrf("[transcribe] Note: Provided terms/flags differ from cached job:\n")
					cmd.PrintErrf("  • Cached terms (%d):   %s\n", len(jobEnv.Record.Terms), formatTermsSummary(jobEnv.Record.Terms))
					cmd.PrintErrf("  • Provided terms (%d): %s\n", len(combinedTerms), formatTermsSummary(combinedTerms))
				}
				cmd.PrintErrf("[transcribe] Returning cached transcript to avoid API charges. Use --force to re-transcribe with new terms.\n")
				resp = jobEnv.Response
			}
		}
	}

	ctx := context.Background()
	doMono := !opts.noPreprocess && !opts.noMono
	doTrimSilence := !opts.noPreprocess && !opts.noTrimSilence

	// Run audio preprocessing ONLY on cache MISS (or if --force is set)
	targetAudioPath := audioPath
	if resp == nil && (doMono || doTrimSilence) {
		if audio.IsFFmpegAvailable() {
			cmd.PrintErrf("Preprocessing audio (%s, mono: %t, trim-silence: %t)...\n",
				filepath.Base(audioPath), doMono, doTrimSilence)

			prepOpts := audio.PreprocessOptions{
				Mono:             doMono,
				TrimSilence:      doTrimSilence,
				SilenceThreshold: opts.silenceThreshold,
				SilenceDuration:  opts.silenceDuration,
			}

			procPath, cleanup, err := audio.PreprocessAudio(ctx, audioPath, prepOpts)
			if err != nil {
				return fmt.Errorf("audio preprocessing failed: %w", err)
			}
			defer cleanup()
			targetAudioPath = procPath

			if procInfo, err := os.Stat(targetAudioPath); err == nil {
				cmd.PrintErrf("Preprocessed audio size: %s (original: %s)\n",
					formatFileSize(procInfo.Size()), formatFileSize(fileInfo.Size()))
			}
		} else {
			cmd.PrintErrf("Notice: ffmpeg not found on PATH; skipping auto-preprocessing.\n")
		}
	}

	// Resolve API Key
	resolvedAPIKey := opts.apiKey
	if resolvedAPIKey == "" {
		resolvedAPIKey = os.Getenv("DEEPGRAM_API_KEY")
	}

	var client *deepgram.Client
	if resolvedAPIKey != "" {
		var clientOpts []deepgram.ClientOption
		if overrideEndpoint != "" {
			clientOpts = append(clientOpts, deepgram.WithEndpoint(overrideEndpoint))
		}
		client = deepgram.NewClient(resolvedAPIKey, clientOpts...)
	}

	// If not cached or --force was passed, call Deepgram API
	if resp == nil {
		if resolvedAPIKey == "" {
			return fmt.Errorf("DEEPGRAM_API_KEY environment variable is not set and --api-key was not provided")
		}

		transmitBytes, err := os.ReadFile(targetAudioPath)
		if err != nil {
			return fmt.Errorf("reading processed audio file %q: %w", targetAudioPath, err)
		}

		cmd.PrintErrf("Transcribing %s (%s, model: %s, terms: %d)...\n",
			filepath.Base(audioPath), formatFileSize(fileInfo.Size()), opts.model, len(combinedTerms))

		apiResp, err := client.Transcribe(ctx, bytes.NewReader(transmitBytes), mimeType, dgOpts)
		if err != nil {
			return fmt.Errorf("transcription failed: %w", err)
		}
		resp = apiResp

		// Save to cache
		if !opts.noCache {
			_ = deepgram.SaveCachedResponse(cacheDir, optsKey, resp)
		}
	}

	// Calculate cost estimate
	channels := 1
	if !doMono {
		channels = 2
	}
	durationSecs := 0.0
	if resp != nil && resp.Metadata.Duration > 0 {
		durationSecs = resp.Metadata.Duration
	}
	costUSD := deepgram.CalculateCostWithOptions(durationSecs, channels, dgOpts)

	// Attempt to query Deepgram for actual cost from request logs
	finalCostUSD := costUSD
	isActualCost := false
	var trueCostErr error

	if resp != nil && resp.Metadata.RequestID != "" && client != nil {
		var actualCost string
		actualCost, trueCostErr = client.GetRequestCostFormatted(cmd.Context(), resp.Metadata.RequestID)
		if trueCostErr == nil && actualCost != "" {
			finalCostUSD = actualCost
			isActualCost = true
		}
	}

	// Persist JobRecord metadata
	if !opts.noCache {
		absPath, _ := filepath.Abs(audioPath)
		jobRec := deepgram.JobRecord{
			RequestID:       resp.Metadata.RequestID,
			Filename:        filepath.Base(audioPath),
			FilePath:        absPath,
			SourceSHA256:    sourceSHA,
			SHA256:          optsKey,
			Timestamp:       time.Now(),
			DurationSeconds: durationSecs,
			Channels:        channels,
			Model:           opts.model,
			Preprocessed:    doMono || doTrimSilence,
			Terms:           combinedTerms,
			CostUSD:         finalCostUSD,
		}
		_ = deepgram.SaveJobRecord(cacheDir, jobRec)
	}

	meta := markdown.MetaInfo{
		Filename:  filepath.Base(audioPath),
		FileSize:  formatFileSize(fileInfo.Size()),
		Model:     opts.model,
		Diarized:  !opts.noDiarize,
		KeyTerms:  combinedTerms,
		Timestamp: time.Now(),
	}

	mdContent := markdown.Format(resp, meta)

	var outWriter io.Writer = cmd.OutOrStdout()
	if opts.outputFile != "" {
		if err := os.MkdirAll(filepath.Dir(opts.outputFile), 0755); err != nil {
			return fmt.Errorf("creating output directory for %q: %w", opts.outputFile, err)
		}
		outFile, err := os.Create(opts.outputFile)
		if err != nil {
			return fmt.Errorf("creating output file %q: %w", opts.outputFile, err)
		}
		defer outFile.Close()
		outWriter = outFile
	}

	_, err = fmt.Fprint(outWriter, mdContent)
	if err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	if opts.outputFile != "" {
		if isActualCost {
			cmd.PrintErrf("Successfully saved transcript to %s (Actual API Cost: %s)\n", opts.outputFile, finalCostUSD)
		} else {
			cmd.PrintErrf("Successfully saved transcript to %s (Calculated API Cost: %s)\n", opts.outputFile, finalCostUSD)
			if trueCostErr != nil {
				cmd.PrintErrf("Note: Could not fetch true cost from Deepgram (%v). To view true costs, ensure request logging is enabled under Project Settings in the Deepgram Console (https://console.deepgram.com) and your API key has Member/Admin scope.\n", trueCostErr)
			}
		}
	}

	return nil
}

func formatTermsSummary(termList []string) string {
	if len(termList) == 0 {
		return "(none)"
	}
	s := make([]string, len(termList))
	copy(s, termList)
	sort.Strings(s)
	if len(s) > 10 {
		return fmt.Sprintf("%s ... (+%d more)", strings.Join(s[:10], ", "), len(s)-10)
	}
	return strings.Join(s, ", ")
}

func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func main() {
	cmd := NewRootCmd()
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
