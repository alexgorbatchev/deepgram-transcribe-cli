package audio

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreprocessExtensionlessAudio(t *testing.T) {
	t.Chdir(t.TempDir())
	path, cleanup, err := PreprocessAudio(t.Context(), "recording", PreprocessOptions{
		TrimSilence: true,
		Runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "ffmpeg" || !strings.HasSuffix(args[len(args)-1], ".m4a") ||
				!strings.Contains(strings.Join(args, " "), "stop_duration=2.0:stop_threshold=-30dB") {
				t.Fatalf("unexpected encoding arguments: %s %v", name, args)
			}
			return nil, os.WriteFile(args[len(args)-1], []byte("encoded"), 0o600)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output remains after cleanup: %v", err)
	}
}

func TestPreprocessRejectsBlockedTempDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("TMP", os.Getenv("TMPDIR"))
	t.Setenv("TEMP", os.Getenv("TMPDIR"))
	path, cleanup, err := PreprocessAudio(t.Context(), "recording.wav", PreprocessOptions{
		Mono: true,
		Runner: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("encoder ran despite unavailable temporary directory")
			return nil, nil
		},
	})
	cleanup()
	if path != "" || err == nil || !strings.Contains(err.Error(), "creating temp audio file") {
		t.Fatalf("got path %q, error %v", path, err)
	}
}

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
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("PATH", t.TempDir())
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	t.Setenv("TMP", tmpDir)
	t.Setenv("TEMP", tmpDir)

	inputFile := filepath.Join(dir, "test-input.wav")
	if err := os.WriteFile(inputFile, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}

	opts := PreprocessOptions{
		Mono:        true,
		TrimSilence: false,
		Runner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name != "ffmpeg" || args[2] != inputFile {
				t.Fatalf("unexpected preprocessing call: %s %v", name, args)
			}
			return nil, os.WriteFile(args[len(args)-1], []byte("prepared audio"), 0o600)
		},
	}

	processedPath, cleanup, err := PreprocessAudio(context.Background(), inputFile, opts)
	if err != nil {
		t.Fatalf("PreprocessAudio failed: %v", err)
	}
	defer cleanup()
	if filepath.Dir(processedPath) != os.TempDir() {
		t.Errorf("prepared file %q is outside OS temp directory %q", processedPath, os.TempDir())
	}
	if _, err := os.Stat(".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("preprocessing touched working-directory .tmp: %v", err)
	}

	if processedPath == inputFile {
		t.Errorf("expected new temp processed file path, got same %q", processedPath)
	}

	info, err := os.Stat(processedPath)
	if err != nil || info.Size() == 0 {
		t.Errorf("processed file does not exist or is empty: %v", err)
	}
	cleanup()
	if _, err := os.Stat(processedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prepared audio was not cleaned up: %v", err)
	}
}

func TestPreprocessAudioCleansUpAfterFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", t.TempDir())
	want := errors.New("encoding failed")
	var outputPath string
	path, cleanup, err := PreprocessAudio(context.Background(), "input.opus", PreprocessOptions{
		Mono: true,
		Runner: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			outputPath = args[len(args)-1]
			return []byte("encoder diagnostic"), want
		},
	})
	cleanup()
	if path != "" || !errors.Is(err, want) {
		t.Fatalf("got path %q err %v", path, err)
	}
	if _, err := os.Stat(outputPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed output retained: %v", err)
	}
}
