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
	version = "0.1.0"

	// Flag variables
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
	limitHistory     int
	showVersion      bool
)

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "deepgram-transcribe [command|flags] <audio-file>",
		Short: "Transcribe audio phone conversations using Deepgram and output Markdown",
		Long: `deepgram-transcribe is a CLI tool that sends audio files (mp3, m4a, wav, etc.) to Deepgram API v1/listen
and formats the transcript into clean Markdown with speaker diarization and custom keyterm boosting.

Subcommands:
  deepgram-transcribe cost <file|request-id>    Display post-transcription cost & job metadata
  deepgram-transcribe history                   List recent transcription jobs and total spending
  deepgram-transcribe cache [status|clear]      Manage or clear local SHA-256 transcript cache

Default transcription output goes to stdout:
  deepgram-transcribe conversation.m4a > conversation.md
  deepgram-transcribe -t Envoy -t Alex conversation.mp3 -o output.md`,
		Args: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTranscribeWithEndpoint(cmd, args, "", deepgram.DefaultCacheDir())
		},
	}

	flags := rootCmd.Flags()
	flags.StringArrayVarP(&extraTerms, "term", "t", nil, "Additional terms to boost recognition (company names, people names, etc., can be used multiple times or comma-separated)")
	flags.StringVar(&termsFilePath, "terms-file", "", "Path to a text file containing additional terms (one per line or comma-separated)")
	flags.StringVarP(&model, "model", "m", "nova-3", "Deepgram transcription model (e.g. nova-3, nova-2, flux)")
	flags.StringVarP(&language, "language", "l", "en", "Audio language code (e.g. en, en-US)")
	flags.StringVarP(&outputFile, "output", "o", "", "Output file path (default stdout)")
	flags.StringVar(&apiKey, "api-key", "", "Deepgram API key (defaults to DEEPGRAM_API_KEY environment variable)")
	flags.BoolVar(&noDiarize, "no-diarize", false, "Disable speaker diarization")
	flags.BoolVar(&noTechTerms, "no-tech-terms", false, "Disable preconfigured common tech interview terms")
	flags.BoolVar(&noCache, "no-cache", false, "Bypass local SHA-256 transcript response cache")
	flags.BoolVarP(&force, "force", "f", false, "Force fresh Deepgram re-transcription even if cached source audio exists with different terms")

	// Cost reduction preprocessing flags (ON by default)
	flags.BoolVar(&noPreprocess, "no-preprocess", false, "Disable automatic audio preprocessing (mono conversion & silence trimming)")
	flags.BoolVar(&noMono, "no-mono", false, "Disable stereo-to-mono downmixing")
	flags.BoolVar(&noTrimSilence, "no-trim-silence", false, "Disable silence/dead-air trimming")
	flags.StringVar(&silenceThreshold, "silence-threshold", "-30dB", "Noise threshold for silence removal (e.g. -30dB, -40dB)")
	flags.StringVar(&silenceDuration, "silence-duration", "2.0", "Minimum silence duration in seconds to trim (e.g. 2.0)")

	flags.BoolVarP(&showVersion, "version", "v", false, "Print version information and exit")

	// Subcommand alias for 'deepgram-transcribe transcribe <file>'
	transcribeSubCmd := &cobra.Command{
		Use:   "transcribe <audio-file>",
		Short: "Transcribe audio file (alias for root command)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTranscribeWithEndpoint(cmd, args, "", deepgram.DefaultCacheDir())
		},
	}
	transcribeSubCmd.Flags().AddFlagSet(flags)

	// Add Subcommands
	rootCmd.AddCommand(
		transcribeSubCmd,
		newCostCmdWithDir(deepgram.DefaultCacheDir()),
		newHistoryCmdWithDir(deepgram.DefaultCacheDir()),
		newCacheCmdWithDir(deepgram.DefaultCacheDir()),
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

			var record *deepgram.JobRecord
			var err error

			// 1. Try matching as an existing local audio file (SHA-256 lookup)
			if _, statErr := os.Stat(target); statErr == nil {
				audioBytes, readErr := os.ReadFile(target)
				if readErr == nil {
					records, listErr := deepgram.ListJobRecords(cacheDir)
					if listErr == nil {
						rawSHA := deepgram.SourceAudioKey(audioBytes)
						for _, rec := range records {
							if rec.SourceSHA256 == rawSHA || rec.SHA256 == rawSHA || strings.HasPrefix(rec.SHA256, rawSHA[:16]) || filepath.Base(rec.FilePath) == filepath.Base(target) {
								r := rec
								record = &r
								break
							}
						}
					}
				}
			}

			// 2. If not found by file SHA/name, try matching by Request ID
			if record == nil {
				record, err = deepgram.GetJobRecordByRequestID(cacheDir, target)
			}

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
			fmt.Fprintf(out, "Calculated Cost:  %s USD\n", record.CostUSD)

			return nil
		},
	}
}

func newHistoryCmdWithDir(cacheDir string) *cobra.Command {
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

func runTranscribeWithEndpoint(cmd *cobra.Command, args []string, overrideEndpoint, cacheDir string) error {
	if showVersion {
		cmd.Printf("transcribe version %s\n", version)
		return nil
	}

	audioPath := args[0]

	fileInfo, err := os.Stat(audioPath)
	if err != nil {
		return fmt.Errorf("checking audio file %q: %w", audioPath, err)
	}

	// Read original audio bytes FIRST for instant cache checking (0ms ffmpeg overhead on cache hit)
	originalAudioBytes, err := os.ReadFile(audioPath)
	if err != nil {
		return fmt.Errorf("reading audio file %q: %w", audioPath, err)
	}

	// Build term list & options
	var termLists [][]string

	if !noTechTerms {
		termLists = append(termLists, terms.DefaultTechTerms())
	}

	if len(extraTerms) > 0 {
		parsedExtra := terms.ParseCustomTerms(extraTerms)
		termLists = append(termLists, parsedExtra)
	}

	if termsFilePath != "" {
		fileTerms, err := terms.LoadTermsFromFile(termsFilePath)
		if err != nil {
			return fmt.Errorf("loading terms file: %w", err)
		}
		termLists = append(termLists, fileTerms)
	}

	combinedTerms := terms.CombineTerms(termLists...)

	mimeType := detectMIMEType(audioPath)

	opts := deepgram.Options{
		Model:           model,
		Language:        language,
		Diarize:         !noDiarize,
		SmartFormatting: true,
		Utterances:      true,
		Punctuate:       true,
		Terms:           combinedTerms,
	}

	sourceSHA := deepgram.SourceAudioKey(originalAudioBytes)
	optsKey := deepgram.CacheKey(originalAudioBytes, opts)

	var resp *deepgram.PreRecordedResponse

	// CHECK CACHE FIRST BEFORE RUNNING FFMPEG OR MAKING API CALLS
	if !noCache && !force {
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
	doMono := !noPreprocess && !noMono
	doTrimSilence := !noPreprocess && !noTrimSilence

	// Run audio preprocessing ONLY on cache MISS (or if --force is set)
	targetAudioPath := audioPath
	if resp == nil && (doMono || doTrimSilence) {
		if audio.IsFFmpegAvailable() {
			cmd.PrintErrf("Preprocessing audio (%s, mono: %t, trim-silence: %t)...\n",
				filepath.Base(audioPath), doMono, doTrimSilence)

			prepOpts := audio.PreprocessOptions{
				Mono:             doMono,
				TrimSilence:      doTrimSilence,
				SilenceThreshold: silenceThreshold,
				SilenceDuration:  silenceDuration,
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

	// If not cached or --force was passed, call Deepgram API
	if resp == nil {
		// Resolve API Key
		resolvedAPIKey := apiKey
		if resolvedAPIKey == "" {
			resolvedAPIKey = os.Getenv("DEEPGRAM_API_KEY")
		}
		if resolvedAPIKey == "" {
			return fmt.Errorf("DEEPGRAM_API_KEY environment variable is not set and --api-key was not provided")
		}

		transmitBytes, err := os.ReadFile(targetAudioPath)
		if err != nil {
			return fmt.Errorf("reading processed audio file %q: %w", targetAudioPath, err)
		}

		cmd.PrintErrf("Transcribing %s (%s, model: %s, terms: %d)...\n",
			filepath.Base(audioPath), formatFileSize(fileInfo.Size()), model, len(combinedTerms))

		var clientOpts []deepgram.ClientOption
		if overrideEndpoint != "" {
			clientOpts = append(clientOpts, deepgram.WithEndpoint(overrideEndpoint))
		}

		client := deepgram.NewClient(resolvedAPIKey, clientOpts...)

		apiResp, err := client.Transcribe(ctx, bytes.NewReader(transmitBytes), mimeType, opts)
		if err != nil {
			return fmt.Errorf("transcription failed: %w", err)
		}
		resp = apiResp

		// Save to cache
		if !noCache {
			_ = deepgram.SaveCachedResponse(cacheDir, optsKey, resp)
		}
	}

	// Calculate actual cost
	channels := 1
	if !doMono {
		channels = 2
	}
	durationSecs := 0.0
	if resp != nil && resp.Metadata.Duration > 0 {
		durationSecs = resp.Metadata.Duration
	}
	costUSD := fmt.Sprintf("$%.3f", (durationSecs/60.0)*float64(channels)*0.0043)

	// Persist JobRecord metadata
	if !noCache {
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
			Model:           model,
			Preprocessed:    doMono || doTrimSilence,
			Terms:           combinedTerms,
			CostUSD:         costUSD,
		}
		_ = deepgram.SaveJobRecord(cacheDir, jobRec)
	}

	meta := markdown.MetaInfo{
		Filename:  filepath.Base(audioPath),
		FileSize:  formatFileSize(fileInfo.Size()),
		Model:     model,
		Diarized:  !noDiarize,
		KeyTerms:  combinedTerms,
		Timestamp: time.Now(),
	}

	mdContent := markdown.Format(resp, meta)

	var outWriter io.Writer = cmd.OutOrStdout()
	if outputFile != "" {
		outDir := filepath.Dir(outputFile)
		if outDir != "." && outDir != "" {
			if err := os.MkdirAll(outDir, 0755); err != nil {
				return fmt.Errorf("creating output directory %q: %w", outDir, err)
			}
		}
		outFile, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("creating output file %q: %w", outputFile, err)
		}
		defer outFile.Close()
		outWriter = outFile
	}

	_, err = fmt.Fprint(outWriter, mdContent)
	if err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	if outputFile != "" {
		cmd.PrintErrf("Successfully saved transcript to %s (Calculated API Cost: %s)\n", outputFile, costUSD)
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

func detectMIMEType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/m4a"
	case ".mp4":
		return "audio/mp4"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	case ".aac":
		return "audio/aac"
	default:
		return "application/octet-stream"
	}
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
