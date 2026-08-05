package audio

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildFFmpegArgs(t *testing.T) {
	opts := PreprocessOptions{
		Mono:             true,
		TrimSilence:      true,
		SilenceThreshold: "-30dB",
		SilenceDuration:  "2.0",
	}

	args := BuildFFmpegArgs("input.m4a", "output.m4a", opts)

	foundInput := false
	foundOutput := false
	foundMono := false
	foundFilter := false

	for i, arg := range args {
		if arg == "input.m4a" && i > 0 && args[i-1] == "-i" {
			foundInput = true
		}
		if arg == "output.m4a" && i == len(args)-1 {
			foundOutput = true
		}
		if arg == "1" && i > 0 && args[i-1] == "-ac" {
			foundMono = true
		}
		if arg == "silenceremove=stop_periods=-1:stop_duration=2.0:stop_threshold=-30dB" && i > 0 && args[i-1] == "-af" {
			foundFilter = true
		}
	}

	if !foundInput || !foundOutput {
		t.Errorf("expected input and output files in ffmpeg args, got %v", args)
	}

	if !foundMono {
		t.Errorf("expected -ac 1 in ffmpeg args, got %v", args)
	}

	if !foundFilter {
		t.Errorf("expected -af silenceremove in ffmpeg args, got %v", args)
	}
}

func TestDetectMIMEType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"audio.mp3", "audio/mpeg"},
		{"call.m4a", "audio/m4a"},
		{"recording.mp4", "audio/mp4"},
		{"voice.wav", "audio/wav"},
		{"recording.ogg", "audio/ogg"},
		{"recording.flac", "audio/flac"},
		{"audio.aac", "audio/aac"},
		{"unknown.xyz", "application/octet-stream"},
	}

	for _, tt := range tests {
		got := DetectMIMEType(tt.filename)
		if got != tt.want {
			t.Errorf("DetectMIMEType(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}

func TestPreprocessAudioNoOp(t *testing.T) {
	opts := PreprocessOptions{
		Mono:        false,
		TrimSilence: false,
	}

	inputPath := "test.mp3"
	gotPath, cleanup, err := PreprocessAudio(context.Background(), inputPath, opts)
	if err != nil {
		t.Fatalf("unexpected error for no-op preprocess: %v", err)
	}
	defer cleanup()

	if gotPath != inputPath {
		t.Errorf("expected input path unchanged for no-op, got %q", gotPath)
	}
}

func TestPreprocessAudioWithFFmpeg(t *testing.T) {
	if !IsFFmpegAvailable() {
		t.Skip("ffmpeg not found on system PATH, skipping integration test")
	}

	dir := t.TempDir()

	inputFile := filepath.Join(dir, "test-input.wav")
	cmd := execCommand("ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=1000:duration=3", "-ac", "2", inputFile)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create test audio file via ffmpeg: %v", err)
	}

	opts := PreprocessOptions{
		Mono:        true,
		TrimSilence: false,
	}

	processedPath, cleanup, err := PreprocessAudio(context.Background(), inputFile, opts)
	if err != nil {
		t.Fatalf("PreprocessAudio failed: %v", err)
	}
	defer cleanup()

	if processedPath == inputFile {
		t.Errorf("expected new temp processed file path, got same %q", processedPath)
	}

	info, err := os.Stat(processedPath)
	if err != nil || info.Size() == 0 {
		t.Errorf("processed file does not exist or is empty: %v", err)
	}
}
