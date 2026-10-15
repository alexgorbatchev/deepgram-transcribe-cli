package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptTemporaryAudioLifecycle(t *testing.T) {
	for _, stage := range []string{"success", "missing key", "API error", "write error", "cancel encoding", "cancel probe", "cancel upload"} {
		t.Run(stage, func(t *testing.T) {
			t.Chdir(t.TempDir())
			tmpDir := t.TempDir()
			t.Setenv("TMPDIR", tmpDir)
			t.Setenv("TMP", tmpDir)
			t.Setenv("TEMP", tmpDir)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var prepared string
			runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if len(args) == 1 && args[0] == "-version" {
					return []byte(strings.Replace(ffmpegVersionOutput, "ffmpeg version", name+" version", 1)), nil
				}
				if name == "ffprobe" {
					if stage == "cancel probe" {
						cancel()
						return nil, ctx.Err()
					}
					duration := "10"
					if args[len(args)-1] == prepared {
						duration = "5"
					}
					return []byte(`{"streams":[{"duration":"` + duration + `"}]}`), nil
				}
				prepared = args[len(args)-1]
				if err := os.WriteFile(prepared, []byte("prepared"), 0o600); err != nil {
					return nil, err
				}
				if stage == "cancel encoding" {
					cancel()
					return nil, ctx.Err()
				}
				return nil, nil
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := os.Stat(prepared); err != nil {
					t.Errorf("prepared audio removed before upload finished: %v", err)
				}
				if stage == "cancel upload" {
					cancel()
					return
				}
				if stage == "API error" {
					http.Error(w, "transcription failed", http.StatusBadRequest)
					return
				}
				if _, err := fmt.Fprint(w, transcribeResponseJSON); err != nil {
					t.Errorf("writing response: %v", err)
				}
			}))
			defer server.Close()
			root, _, _ := newTestCLIWithRunner(t, t.TempDir(), server.URL, runner)
			source := writeAudio(t, t.TempDir(), "recording.wav", "source")
			args := []string{"transcript", "create", source, "--no-cache"}
			if stage != "missing key" {
				args = append(args, "--api-key", "test-key")
			}
			if stage == "write error" {
				args = append(args, "--output", filepath.Join(source, "transcript.md"))
			}
			root.SetArgs(args)
			err := root.ExecuteContext(ctx)
			if (err == nil) != (stage == "success") {
				t.Fatalf("unexpected command result: %v", err)
			}
			if strings.HasPrefix(stage, "cancel ") && !errors.Is(err, context.Canceled) {
				t.Errorf("expected cancellation, got %v", err)
			}
			if prepared == "" || filepath.Dir(prepared) != os.TempDir() {
				t.Errorf("prepared audio %q is outside OS temp directory %q", prepared, os.TempDir())
			}
			if _, err := os.Stat(prepared); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("temporary audio retained after command exit: %v", err)
			}
			if data, err := os.ReadFile(source); err != nil || string(data) != "source" {
				t.Errorf("source audio changed: %q, %v", data, err)
			}
		})
	}
}
