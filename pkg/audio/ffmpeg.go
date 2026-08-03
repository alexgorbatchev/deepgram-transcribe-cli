package audio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var execCommand = exec.Command

// PreprocessOptions configures audio optimization parameters before API transmission.
type PreprocessOptions struct {
	Mono             bool   // Convert stereo audio to single channel (mono)
	TrimSilence      bool   // Remove long silent periods/dead air
	SilenceThreshold string // e.g. "-30dB"
	SilenceDuration  string // e.g. "2.0" (seconds)
}

// IsFFmpegAvailable checks if ffmpeg binary exists on the system PATH.
func IsFFmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
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
			threshold = "-30dB"
		}
		duration := opts.SilenceDuration
		if duration == "" {
			duration = "2.0"
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

	if !IsFFmpegAvailable() {
		return "", noCleanup, fmt.Errorf("ffmpeg is required for audio preprocessing (--mono / --trim-silence) but was not found on system PATH")
	}

	ext := filepath.Ext(inputPath)
	if ext == "" {
		ext = ".m4a"
	}

	tmpDir := filepath.Join(".tmp", "audio_preprocess")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", noCleanup, fmt.Errorf("creating temp audio directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(tmpDir, "transcribe-proc-*"+ext)
	if err != nil {
		return "", noCleanup, fmt.Errorf("creating temp audio file: %w", err)
	}
	outputPath := tmpFile.Name()
	tmpFile.Close()

	cleanup := func() {
		_ = os.Remove(outputPath)
	}

	args := BuildFFmpegArgs(inputPath, outputPath, opts)
	cmd := execCommandWithContext(ctx, "ffmpeg", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		cleanup()
		return "", noCleanup, fmt.Errorf("ffmpeg audio preprocessing failed: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}

	return outputPath, cleanup, nil
}

func execCommandWithContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, arg...)
}
