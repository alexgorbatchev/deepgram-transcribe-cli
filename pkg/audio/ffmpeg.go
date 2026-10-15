package audio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexgorbatchev/godeps"
)

const (
	DefaultSilenceThreshold = "-30dB"
	DefaultSilenceDuration  = "2.0"
)

// PreprocessOptions configures audio optimization parameters before API transmission.
type PreprocessOptions struct {
	Mono             bool                 // Convert stereo audio to single channel (mono)
	TrimSilence      bool                 // Remove long silent periods/dead air
	SilenceThreshold string               // e.g. "-30dB"
	SilenceDuration  string               // e.g. "2.0" (seconds)
	Runner           godeps.CommandRunner // nil uses the real process runner
}

// BuildFFmpegArgs constructs command line arguments for ffmpeg audio processing.
func BuildFFmpegArgs(inputPath, outputPath string, opts PreprocessOptions) []string {
	args := []string{"-y", "-i", inputPath}

	if opts.Mono {
		args = append(args, "-ac", "1")
	}

	if opts.TrimSilence {
		threshold := opts.SilenceThreshold
		if threshold == "" {
			threshold = DefaultSilenceThreshold
		}
		duration := opts.SilenceDuration
		if duration == "" {
			duration = DefaultSilenceDuration
		}

		filterStr := fmt.Sprintf("silenceremove=stop_periods=-1:stop_duration=%s:stop_threshold=%s", duration, threshold)
		args = append(args, "-af", filterStr)
	}

	args = append(args, outputPath)
	return args
}

// PreprocessAudio runs ffmpeg to optimize audio if Mono or TrimSilence options are requested.
func PreprocessAudio(ctx context.Context, inputPath string, opts PreprocessOptions) (string, func(), error) {
	noCleanup := func() {}

	if !opts.Mono && !opts.TrimSilence {
		return inputPath, noCleanup, nil
	}

	ext := filepath.Ext(inputPath)
	if ext == "" {
		ext = ".m4a"
	}

	tmpFile, err := os.CreateTemp("", "transcribe-proc-*"+ext)
	if err != nil {
		return "", noCleanup, fmt.Errorf("creating temp audio file: %w", err)
	}
	// The file is only needed as a reserved name: ffmpeg writes the audio itself,
	// so it has to be closed before ffmpeg is started.
	outputPath := tmpFile.Name()
	cleanup := func() {
		_ = os.Remove(outputPath) // Best-effort cleanup, including repeated calls.
	}
	if err := tmpFile.Close(); err != nil {
		cleanup()
		return "", noCleanup, fmt.Errorf("closing temp audio file %q: %w", outputPath, err)
	}

	args := BuildFFmpegArgs(inputPath, outputPath, opts)
	runner := opts.Runner
	if runner == nil {
		runner = godeps.DefaultRunner
	}
	output, err := runner(ctx, "ffmpeg", args...)
	if err != nil {
		cleanup()
		return "", noCleanup, fmt.Errorf("ffmpeg audio preprocessing failed: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}

	return outputPath, cleanup, nil
}

// DetectMIMEType returns the MIME type corresponding to an audio file's extension.
func DetectMIMEType(path string) string {
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
