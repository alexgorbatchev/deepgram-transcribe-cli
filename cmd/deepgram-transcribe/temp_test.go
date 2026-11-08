package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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
					// Trimmed audio is measured from its decoded samples, so the
					// only probe is of the source.
					if stage == "cancel probe" {
						cancel()
						return nil, ctx.Err()
					}
					duration := "10"
					return []byte(`{"streams":[{"duration":"` + duration + `","sample_rate":"1000","channels":1}]}`), nil
				}
				joined := strings.Join(args, " ")
				if strings.Contains(joined, "silencedetect=") {
					return []byte("lavfi.silence_start=2\nlavfi.silence_end=7\n"), nil
				}
				t.Errorf("unexpected %s %v", name, args)
				return nil, errors.New("unexpected program")
			}
			stream := func(ctx context.Context, stdin io.Reader, stdout io.Writer, _ string, args ...string) error {
				target := args[len(args)-1]
				if target == "pipe:1" {
					_, err := stdout.Write(make([]byte, 20000))
					return err
				}
				prepared = target
				data, err := io.ReadAll(stdin)
				if err != nil {
					return err
				}
				if err := os.WriteFile(prepared, data, 0o600); err != nil {
					return err
				}
				if stage == "cancel encoding" {
					cancel()
					return ctx.Err()
				}
				return nil
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := os.Stat(prepared); err != nil {
					t.Errorf("prepared audio removed before upload finished: %v", err)
				}
				if stage == "cancel upload" {
					// Hold the response until the client abandons the request, as a
					// slow transcription would. Returning at once races an empty 200
					// against the client noticing the cancellation. The server only
					// notices a dropped connection once the body has been read.
					if _, err := io.Copy(io.Discard, r.Body); err != nil {
						t.Errorf("reading upload: %v", err)
					}
					cancel()
					<-r.Context().Done()
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
			root, _, _ := newTestCLIWithPrograms(t, t.TempDir(), server.URL, runner, stream)
			source := writeAudio(t, t.TempDir(), "recording.wav", "source")
			args := []string{"transcript", "create", source, "--no-cache", "--trim-mode", "silence"}
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
			if stage == "missing key" || stage == "cancel probe" {
				// The key is checked, and the source probed, before any audio
				// is prepared.
				if prepared != "" {
					t.Errorf("audio was prepared for a run that cannot upload it: %s", prepared)
				}
			} else if prepared == "" || filepath.Dir(filepath.Dir(prepared)) != os.TempDir() {
				t.Errorf("prepared audio %q is outside OS temp directory %q", prepared, os.TempDir())
			}
			if left, err := os.ReadDir(tmpDir); err != nil || len(left) != 0 {
				t.Errorf("temporary audio retained after command exit: %v %v", left, err)
			}
			if data, err := os.ReadFile(source); err != nil || string(data) != "source" {
				t.Errorf("source audio changed: %q, %v", data, err)
			}
		})
	}
}
