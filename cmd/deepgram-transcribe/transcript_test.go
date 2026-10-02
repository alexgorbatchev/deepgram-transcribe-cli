package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bytes"
	"context"
	"errors"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/internal/cliout"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/terms"
)

func TestPrepareUploadReportsLargerAudio(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "-version" {
			return []byte(strings.Replace(ffmpegVersionOutput, "ffmpeg version", name+" version", 1)), nil
		}
		if name == "ffprobe" {
			if strings.Contains(args[len(args)-1], "transcribe-proc-") {
				return []byte(`{"streams":[{"duration":"5"}]}`), nil
			}
			return []byte(`{"streams":[{"duration":"10"}]}`), nil
		}
		return nil, os.WriteFile(args[len(args)-1], []byte("larger processed recording"), 0o600)
	}
	root, out, stderr := newTestCLIWithRunner(t, t.TempDir(), "", runner)
	root.SetContext(t.Context())
	t.Setenv("PATH", t.TempDir())
	audioPath := writeAudio(t, dir, "recording.opus", "source")
	run := &transcriptRun{
		cmd: root, out: cliout.New(out, stderr),
		global: &globalOptions{depsRunner: runner},
		opts:   &transcriptOptions{}, audioPath: audioPath,
	}
	if err := run.loadSource(); err != nil {
		t.Fatal(err)
	}
	cleanup, err := run.prepareUpload()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !strings.Contains(stderr.String(), "up from 6 B") || strings.Contains(stderr.String(), "Upload is 26 B, down from") || out.Len() != 0 {
		t.Fatalf("incorrect size reporting: %s", stderr)
	}
	if run.uploadPath == audioPath {
		t.Fatal("prepared upload was not selected")
	}
}

func TestPrepareUploadRequiresShorterDuration(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		for _, tc := range []struct {
			name, duration string
			shorter        bool
		}{
			{"shorter", "5", true}, {"equal", "10", false}, {"longer", "12", false},
			{"unknown", "N/A", false},
			{"original unknown", "5", false},
			{"probe failed", "5", false},
			{"mono only", "10", false},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				t.Chdir(t.TempDir())
				t.Setenv("AGENT", mode)
				var prepared string
				runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
					if len(args) == 1 {
						return []byte(strings.Replace(ffmpegVersionOutput, "ffmpeg version", name+" version", 1)), nil
					}
					if name == "ffprobe" {
						if tc.name == "probe failed" {
							return nil, errors.New("duration probe failed")
						}
						duration := "10"
						if tc.name == "original unknown" {
							duration = "N/A"
						}
						if args[len(args)-1] == prepared {
							duration = tc.duration
						}
						return []byte(`{"streams":[{"duration":"` + duration + `"}]}`), nil
					}
					prepared = args[len(args)-1]
					return nil, os.WriteFile(prepared, []byte("larger prepared audio"), 0o600)
				}
				root, out, stderr := newTestCLIWithRunner(t, t.TempDir(), "", runner)
				root.SetContext(t.Context())
				audioPath := writeAudio(t, t.TempDir(), "recording.opus", "source")
				run := &transcriptRun{cmd: root, out: cliout.New(out, stderr), global: &globalOptions{depsRunner: runner}, opts: &transcriptOptions{}, audioPath: audioPath}
				run.opts.noTrimSilence = tc.name == "mono only"
				if err := run.loadSource(); err != nil {
					t.Fatal(err)
				}
				cleanup, err := run.prepareUpload()
				if err != nil {
					t.Fatal(err)
				}
				defer cleanup()
				if (run.uploadPath != audioPath) != tc.shorter {
					t.Fatalf("wrong upload selection: %s; %s", run.uploadPath, stderr)
				}
				if out.Len() != 0 {
					t.Fatalf("diagnostic leaked to stdout: %s", out)
				}
				if !tc.shorter {
					if _, err := os.Stat(prepared); !os.IsNotExist(err) {
						t.Fatalf("discarded audio retained: %v", err)
					}
					if !strings.Contains(stderr.String(), "original audio") {
						t.Fatalf("missing fallback explanation: %s", stderr)
					}
				}
			})
		}
	}
}

func TestPrepareUploadWithoutFFprobe(t *testing.T) {
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if len(args) != 1 || args[0] != "-version" {
			t.Fatalf("preprocessing ran without ffprobe: %s %v", name, args)
		}
		if name == "ffprobe" {
			return nil, errors.New("ffprobe missing")
		}
		return []byte(ffmpegVersionOutput), nil
	}
	root, out, stderr := newTestCLIWithRunner(t, t.TempDir(), "", runner)
	root.SetContext(t.Context())
	audioPath := writeAudio(t, t.TempDir(), "recording.opus", "source")
	run := &transcriptRun{cmd: root, out: cliout.New(out, stderr), global: &globalOptions{depsRunner: runner}, opts: &transcriptOptions{}, audioPath: audioPath}
	if err := run.loadSource(); err != nil {
		t.Fatal(err)
	}
	cleanup, err := run.prepareUpload()
	defer cleanup()
	if err != nil || run.uploadPath != audioPath || !strings.Contains(stderr.String(), "ffprobe") || out.Len() != 0 {
		t.Fatalf("incorrect missing-probe handling: %v %s", err, stderr)
	}
}

func TestUploadSizeComparison(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		for _, tc := range []struct {
			name string
			size int64
			want string
		}{
			{"smaller", 500, "Upload is 500 B, down from 1000 B."},
			{"larger", 1500, "Upload is 1.5 KB, up from 1000 B."},
			{"unchanged", 1000, "Upload is 1000 B, unchanged from the original."},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				t.Setenv("AGENT", mode)
				var out, stderr bytes.Buffer
				run := &transcriptRun{out: cliout.New(&out, &stderr), audioSize: 1000}
				run.reportUploadSize(tc.size)
				if !strings.Contains(stderr.String(), tc.want) || out.Len() != 0 {
					t.Fatalf("got stdout %q stderr %q, want %q on stderr", out.String(), stderr.String(), tc.want)
				}
			})
		}
	}
}

const transcribeResponseJSON = `{
	"metadata": {"request_id": "mock-req-001", "duration": 5.2, "channels": 1},
	"results": {"utterances": [
		{"start": 0.0, "end": 5.2, "speaker": 0, "transcript": "Hello world from Deepgram"}
	]}
}`

// newTranscribeServer returns a stub that answers a transcription request with
// body, so that no test ever spends a Deepgram credit.
func newTranscribeServer(t *testing.T, body string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("writing stub response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

// writeAudio creates a stand-in recording and returns its path.
func writeAudio(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test audio file: %v", err)
	}

	return path
}

func TestTranscriptCreateWritesTranscriptAndRecordsJob(t *testing.T) {
	server := newTranscribeServer(t, transcribeResponseJSON)
	cacheDir := t.TempDir()
	workDir := t.TempDir()

	audioPath := writeAudio(t, workDir, "interview.m4a", "fake audio content")
	termsPath := filepath.Join(workDir, "terms.txt")
	if err := os.WriteFile(termsPath, []byte("Envoy\nAlex"), 0o644); err != nil {
		t.Fatalf("writing terms file: %v", err)
	}
	outputPath := filepath.Join(workDir, "out", "transcript.md")

	root, _, errOut := newTestCLI(t, cacheDir, server.URL)
	root.SetArgs([]string{
		"transcript", "create", audioPath,
		"--api-key", "test-key",
		"-t", "CustomTerm",
		"--terms-file", termsPath,
		"--no-preprocess",
		"-o", outputPath,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("transcript create failed: %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading transcript: %v", err)
	}
	if !strings.Contains(string(content), "Hello world from Deepgram") {
		t.Errorf("expected the transcript in %s, got:\n%s", outputPath, content)
	}

	if !strings.Contains(errOut.String(), "Saved the transcript to") {
		t.Errorf("expected a confirmation on stderr, got: %s", errOut.String())
	}

	records, err := deepgram.ListJobRecords(cacheDir)
	if err != nil {
		t.Fatalf("listing job records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 job record, got %d", len(records))
	}
	if records[0].Channels != 1 {
		t.Errorf("expected the channel count Deepgram reported, got %d", records[0].Channels)
	}
	if records[0].DiarizeModel != deepgram.DiarizeModelLatest {
		t.Errorf("expected the diarizer that was requested, got %q", records[0].DiarizeModel)
	}
}

// TestTranscriptCreateSaysWhenItCannotEstimateTheCost covers a model Deepgram
// no longer publishes a rate for: the run must say the cost is not known rather
// than print a number nobody can source.
func TestTranscriptCreateSaysWhenItCannotEstimateTheCost(t *testing.T) {
	server := newTranscribeServer(t, transcribeResponseJSON)
	cacheDir := t.TempDir()
	workDir := t.TempDir()

	audioPath := writeAudio(t, workDir, "legacy.m4a", "fake audio content")
	outputPath := filepath.Join(workDir, "legacy.md")

	root, _, errOut := newTestCLI(t, cacheDir, server.URL)
	root.SetArgs([]string{
		"transcript", "create", audioPath,
		"--api-key", "test-key",
		"-m", "enhanced",
		"--no-preprocess",
		"-o", outputPath,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("transcript create failed: %v", err)
	}

	progress := errOut.String()
	if strings.Contains(progress, "at a cost of") {
		t.Errorf("expected no cost to be quoted for an unpriced model, got: %s", progress)
	}
	if !strings.Contains(progress, "does not publish a rate") {
		t.Errorf("expected an explanation of why the cost is unknown, got: %s", progress)
	}

	records, err := deepgram.ListJobRecords(cacheDir)
	if err != nil {
		t.Fatalf("listing job records: %v", err)
	}
	if len(records) != 1 || records[0].CostUSD != deepgram.CostUnknown {
		t.Fatalf("expected the stored cost to read as unknown, got %+v", records)
	}
}

func TestTranscriptCreateServesCachedResponseBeforeTouchingAudio(t *testing.T) {
	cacheDir := t.TempDir()
	audioContent := "fake raw audio bytes for cache first test"
	audioPath := writeAudio(t, t.TempDir(), "interview.m4a", audioContent)

	request := deepgram.Options{
		Model:           defaultModel,
		Language:        defaultLanguage,
		DiarizeModel:    deepgram.DiarizeModelLatest,
		SmartFormatting: true,
		Utterances:      true,
		Punctuate:       true,
		Terms:           terms.CombineTerms(terms.DefaultTechTerms()),
	}
	optionsKey := deepgram.CacheKey([]byte(audioContent), request)

	saved := &deepgram.PreRecordedResponse{
		Metadata: deepgram.Metadata{RequestID: "cached-req-001", Duration: 15.0},
		Results: deepgram.Results{Utterances: []deepgram.Utterance{
			{Speaker: 0, Transcript: "Cached transcript without preprocessing"},
		}},
	}
	if err := deepgram.SaveCachedResponse(cacheDir, optionsKey, saved); err != nil {
		t.Fatalf("seeding the cached response: %v", err)
	}

	root, out, errOut := newTestCLI(t, cacheDir, "")
	root.SetArgs([]string{"transcript", "create", audioPath, "--no-preprocess"})

	if err := root.Execute(); err != nil {
		t.Fatalf("expected the cached response to be served, got: %v", err)
	}

	if !strings.Contains(errOut.String(), "Cache hit for") {
		t.Errorf("expected a cache hit notice on stderr, got: %s", errOut.String())
	}
	if !strings.Contains(out.String(), "Cached transcript without preprocessing") {
		t.Errorf("expected the cached response on stdout, got: %s", out.String())
	}
}

func TestTranscriptCreateWarnsWhenCachedKeytermsDiffer(t *testing.T) {
	cacheDir := t.TempDir()
	audioContent := "fake raw audio bytes for term diff test"
	audioPath := writeAudio(t, t.TempDir(), "interview.m4a", audioContent)

	envelope := deepgram.JobRecordEnvelope{
		Record: deepgram.JobRecord{
			RequestID:    "req-source-diff",
			Filename:     "interview.m4a",
			SourceSHA256: deepgram.SourceAudioKey([]byte(audioContent)),
			SHA256:       deepgram.SourceAudioKey([]byte("different options key")),
			Timestamp:    time.Now(),
			Terms:        []string{"Go", "Kubernetes"},
		},
		Response: &deepgram.PreRecordedResponse{
			Metadata: deepgram.Metadata{RequestID: "req-source-diff", Duration: 25.0},
			Results: deepgram.Results{Utterances: []deepgram.Utterance{
				{Speaker: 0, Transcript: "Transcript from original source audio"},
			}},
		},
	}
	if err := deepgram.SaveJobEnvelope(cacheDir, envelope.Record.SHA256, envelope); err != nil {
		t.Fatalf("seeding the saved job: %v", err)
	}

	root, out, errOut := newTestCLI(t, cacheDir, "")
	root.SetArgs([]string{"transcript", "create", audioPath, "-t", "Harvey", "-t", "Brian", "--no-preprocess"})

	if err := root.Execute(); err != nil {
		t.Fatalf("expected the earlier transcript to be returned, got: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "keyterms differ from the cached request") {
		t.Errorf("expected a keyterm mismatch warning on stderr, got: %s", stderr)
	}
	if !strings.Contains(stderr, "Go, Kubernetes") {
		t.Errorf("expected the cached keyterms to be listed, got: %s", stderr)
	}
	if !strings.Contains(out.String(), "Transcript from original source audio") {
		t.Errorf("expected the cached response on stdout, got: %s", out.String())
	}
}

func TestRepeatedSourceCacheHitsPreserveTranscriptAndHistory(t *testing.T) {
	cacheDir := t.TempDir()
	audioBytes := "original recording"
	audioPath := writeAudio(t, t.TempDir(), "interview.m4a", audioBytes)
	envelope := deepgram.JobRecordEnvelope{
		Record: deepgram.JobRecord{
			RequestID: "cached-original", SHA256: "original-key",
			SourceSHA256: deepgram.SourceAudioKey([]byte(audioBytes)),
			Model:        "nova-3", Terms: []string{"Original"}, CostUSD: "$0.123", CostIsActual: true,
		},
		Response: &deepgram.PreRecordedResponse{
			Metadata: deepgram.Metadata{RequestID: "cached-original", Duration: 5.2, Channels: 1},
			Results:  deepgram.Results{Utterances: []deepgram.Utterance{{Transcript: "Original cached transcript"}}},
		},
	}
	if err := deepgram.SaveJobEnvelope(cacheDir, "original-key", envelope); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(cacheDir, "original-key.json"))
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		root, out, _ := newTestCLI(t, cacheDir, "")
		root.SetArgs([]string{"transcript", "create", audioPath, "--no-preprocess", "-t", "Changed"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Original cached transcript") {
			t.Fatalf("cached response lost: %s", out)
		}
	}
	files, err := os.ReadDir(cacheDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("cache hits created additional job records: count=%d err=%v", len(files), err)
	}
	stored, err := os.ReadFile(filepath.Join(cacheDir, "original-key.json"))
	if err != nil || !bytes.Equal(stored, original) {
		t.Fatalf("cache hits changed original history: %v", err)
	}
}

func TestTranscriptCreateForceIgnoresSavedTranscript(t *testing.T) {
	fresh := `{
		"metadata": {"request_id": "fresh-forced-req-002", "duration": 12.0, "channels": 1},
		"results": {"utterances": [
			{"start": 0.0, "end": 12.0, "speaker": 0, "transcript": "Fresh forced transcript from Deepgram"}
		]}
	}`
	server := newTranscribeServer(t, fresh)

	cacheDir := t.TempDir()
	audioContent := "fake raw audio bytes for force flag test"
	audioPath := writeAudio(t, t.TempDir(), "interview.m4a", audioContent)

	savedKey := deepgram.SourceAudioKey([]byte("stale options key"))
	if err := deepgram.SaveJobEnvelope(cacheDir, savedKey, deepgram.JobRecordEnvelope{
		Record: deepgram.JobRecord{
			RequestID:    "old-cached-req",
			SourceSHA256: deepgram.SourceAudioKey([]byte(audioContent)),
			SHA256:       savedKey,
		},
		Response: &deepgram.PreRecordedResponse{
			Metadata: deepgram.Metadata{RequestID: "old-cached-req", Duration: 10.0},
			Results: deepgram.Results{Utterances: []deepgram.Utterance{
				{Speaker: 0, Transcript: "Old cached transcript"},
			}},
		},
	}); err != nil {
		t.Fatalf("seeding the saved job: %v", err)
	}

	root, out, _ := newTestCLI(t, cacheDir, server.URL)
	root.SetArgs([]string{"transcript", "create", audioPath, "--force", "--api-key", "test-key", "--no-preprocess"})

	if err := root.Execute(); err != nil {
		t.Fatalf("expected a forced transcription, got: %v", err)
	}

	if !strings.Contains(out.String(), "Fresh forced transcript from Deepgram") {
		t.Errorf("expected the freshly fetched transcript, got: %s", out.String())
	}
}

// TestTranscriptCreateContinuesWithoutFFmpeg proves that a missing ffmpeg costs
// the user money rather than the transcript: the recording is uploaded at full
// size, and the run still succeeds.
func TestTranscriptCreateContinuesWithoutFFmpeg(t *testing.T) {
	server := newTranscribeServer(t, transcribeResponseJSON)
	audioPath := writeAudio(t, t.TempDir(), "interview.m4a", "fake audio content")

	root, out, errOut := newTestCLIWithRunner(t, t.TempDir(), server.URL, stubRunner("", errFFmpegMissing))
	// Preprocessing is left on, so the run has to decide what to do about ffmpeg.
	root.SetArgs([]string{"transcript", "create", audioPath, "--api-key", "test-key"})

	if err := root.Execute(); err != nil {
		t.Fatalf("expected the transcript to be produced without ffmpeg, got: %v", err)
	}

	if !strings.Contains(out.String(), "Hello world from Deepgram") {
		t.Errorf("expected the transcript on stdout, got: %s", out.String())
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "ffmpeg") {
		t.Errorf("expected ffmpeg to be named in the warning, got: %s", stderr)
	}
	if !strings.Contains(stderr, "Uploading original audio without preprocessing") {
		t.Errorf("expected the consequence to be explained, got: %s", stderr)
	}
	if !strings.Contains(stderr, "dependency install") {
		t.Errorf("expected a hint pointing at dependency install, got: %s", stderr)
	}
}

func TestTranscriptCreateExplainsMissingAPIKey(t *testing.T) {
	audioPath := writeAudio(t, t.TempDir(), "interview.m4a", "audio without a key")

	root, _, errOut := newTestCLI(t, t.TempDir(), "")
	root.SetArgs([]string{"transcript", "create", audioPath, "--no-preprocess"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error when no API key is available")
	}

	// main is what reports the failure, so exercise the same path here.
	reportForTest(root, err)

	stderr := errOut.String()
	if !strings.Contains(stderr, "no Deepgram API key") {
		t.Errorf("expected the missing key to be named, got: %s", stderr)
	}
	if !strings.Contains(stderr, apiKeyEnvVar) {
		t.Errorf("expected a hint naming %s, got: %s", apiKeyEnvVar, stderr)
	}
}

func TestTranscriptCreateCacheDirFlagOverridesDefault(t *testing.T) {
	server := newTranscribeServer(t, transcribeResponseJSON)
	customDir := t.TempDir()
	audioPath := writeAudio(t, t.TempDir(), "flag_test.mp3", "dummy audio content for flag test")

	root, _, _ := newTestCLI(t, t.TempDir(), server.URL)
	root.SetArgs([]string{
		"transcript", "create", audioPath,
		"--api-key", "test-key",
		"--cache-dir", customDir,
		"--no-preprocess",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("transcript create with --cache-dir failed: %v", err)
	}

	records, err := deepgram.ListJobRecords(customDir)
	if err != nil {
		t.Fatalf("listing records from the custom folder: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 job record in the custom folder, got %d", len(records))
	}
}

func TestFormatTermsSummary(t *testing.T) {
	long := make([]string, 0, termsSummaryLimit+3)
	long = append(long, []string{"k", "j", "i", "h", "g", "f", "e", "d", "c", "b", "a", "l", "m"}...)

	tests := []struct {
		name  string
		terms []string
		want  string
	}{
		{"empty", nil, "(none)"},
		{"sorted", []string{"Kubernetes", "Go"}, "Go, Kubernetes"},
		{"truncated", long, "a, b, c, d, e, f, g, h, i, j and 3 more"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTermsSummary(tt.terms); got != tt.want {
				t.Errorf("formatTermsSummary(%v) = %q, want %q", tt.terms, got, tt.want)
			}
		})
	}
}
