package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/terms"
)

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
}

func TestTranscriptCreateReusesSavedTranscriptBeforeTouchingAudio(t *testing.T) {
	cacheDir := t.TempDir()
	audioContent := "fake raw audio bytes for cache first test"
	audioPath := writeAudio(t, t.TempDir(), "interview.m4a", audioContent)

	request := deepgram.Options{
		Model:           defaultModel,
		Language:        defaultLanguage,
		Diarize:         true,
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
		t.Fatalf("seeding the saved transcript: %v", err)
	}

	root, out, errOut := newTestCLI(t, cacheDir, "")
	root.SetArgs([]string{"transcript", "create", audioPath, "--no-preprocess"})

	if err := root.Execute(); err != nil {
		t.Fatalf("expected the saved transcript to be reused, got: %v", err)
	}

	if !strings.Contains(errOut.String(), "Reusing the saved transcript") {
		t.Errorf("expected a reuse notice on stderr, got: %s", errOut.String())
	}
	if !strings.Contains(out.String(), "Cached transcript without preprocessing") {
		t.Errorf("expected the saved transcript on stdout, got: %s", out.String())
	}
}

func TestTranscriptCreateWarnsWhenSavedTermsDiffer(t *testing.T) {
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
	if !strings.Contains(stderr, "words to listen for differ") {
		t.Errorf("expected a vocabulary warning on stderr, got: %s", stderr)
	}
	if !strings.Contains(stderr, "Go, Kubernetes") {
		t.Errorf("expected the saved vocabulary to be listed, got: %s", stderr)
	}
	if !strings.Contains(out.String(), "Transcript from original source audio") {
		t.Errorf("expected the saved transcript on stdout, got: %s", out.String())
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
