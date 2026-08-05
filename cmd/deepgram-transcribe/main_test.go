package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/audio"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/terms"
)

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
		got := audio.DetectMIMEType(tt.filename)
		if got != tt.want {
			t.Errorf("DetectMIMEType(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{500, "500 B"},
		{1024 * 500, "500.0 KB"},
		{1024 * 1024 * 12, "12.0 MB"},
		{1024 * 1024 * 1024 * 2, "2.0 GB"},
	}

	for _, tt := range tests {
		got := formatFileSize(tt.bytes)
		if got != tt.want {
			t.Errorf("formatFileSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestNewRootCmdSubcommands(t *testing.T) {
	cmd := NewRootCmd()

	if cmd.Use != "deepgram-transcribe [command|flags] <audio-file>" {
		t.Errorf("unexpected Use string: %q", cmd.Use)
	}

	subCmds := cmd.Commands()
	subCmdNames := make(map[string]bool)
	for _, sc := range subCmds {
		subCmdNames[sc.Name()] = true
	}

	if !subCmdNames["cost"] {
		t.Errorf("expected subcommand 'cost' to exist")
	}

	if !subCmdNames["history"] {
		t.Errorf("expected subcommand 'history' to exist")
	}

	if !subCmdNames["cache"] {
		t.Errorf("expected subcommand 'cache' to exist")
	}
}

func TestRunTranscribeVersion(t *testing.T) {
	var outBuf bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"-v"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error for -v flag, got: %v", err)
	}

	if !strings.Contains(outBuf.String(), "version") {
		t.Errorf("expected version output, got %q", outBuf.String())
	}
}

func TestCostAndHistoryCommands(t *testing.T) {
	dir := t.TempDir()

	recActual := deepgram.JobRecord{
		RequestID:       "req-actual-123",
		Filename:        "actual.m4a",
		FilePath:        filepath.Join(dir, "actual.m4a"),
		SHA256:          "hash1234567890",
		Timestamp:       time.Date(2026, 3, 31, 14, 0, 0, 0, time.UTC),
		DurationSeconds: 600.0,
		Channels:        1,
		Model:           "nova-3",
		Preprocessed:    true,
		CostUSD:         "$0.043",
		CostIsActual:    true,
	}

	recEstimated := deepgram.JobRecord{
		RequestID:       "req-estimate-456",
		Filename:        "estimate.m4a",
		FilePath:        filepath.Join(dir, "estimate.m4a"),
		SHA256:          "hash0987654321",
		Timestamp:       time.Date(2026, 3, 31, 15, 0, 0, 0, time.UTC),
		DurationSeconds: 300.0,
		Channels:        1,
		Model:           "nova-3",
		Preprocessed:    true,
		CostUSD:         "$0.021",
		CostIsActual:    false,
	}

	if err := deepgram.SaveJobRecord(dir, recActual); err != nil {
		t.Fatalf("SaveJobRecord actual failed: %v", err)
	}
	if err := deepgram.SaveJobRecord(dir, recEstimated); err != nil {
		t.Fatalf("SaveJobRecord estimated failed: %v", err)
	}

	// Test history command without API key -> recActual shows $0.043, recEstimated shows N/A yet
	var histBuf bytes.Buffer
	histCmd := newHistoryCmdWithDir(dir)
	histCmd.SetOut(&histBuf)

	if err := histCmd.Execute(); err != nil {
		t.Fatalf("history command failed: %v", err)
	}

	histStr := histBuf.String()
	if !strings.Contains(histStr, "actual.m4a") || !strings.Contains(histStr, "$0.043") {
		t.Errorf("expected history output to contain 'actual.m4a' and '$0.043', got:\n%s", histStr)
	}
	if !strings.Contains(histStr, "estimate.m4a") || !strings.Contains(histStr, "N/A yet") {
		t.Errorf("expected history output to contain 'estimate.m4a' and 'N/A yet', got:\n%s", histStr)
	}

	// Test cost command on cached actual job -> displays cached value directly without API call
	var costBuf bytes.Buffer
	costCmd := newCostCmdWithDir(dir)
	costCmd.SetOut(&costBuf)
	costCmd.SetArgs([]string{"req-actual-123"})

	if err := costCmd.Execute(); err != nil {
		t.Fatalf("cost command failed: %v", err)
	}

	if !strings.Contains(costBuf.String(), "req-actual-123") || !strings.Contains(costBuf.String(), "$0.043 USD") {
		t.Errorf("expected cost output to contain request id and cached cost, got:\n%s", costBuf.String())
	}

	// Test cache clear command
	var cacheBuf bytes.Buffer
	cacheCmd := newCacheCmdWithDir(dir)
	cacheCmd.SetOut(&cacheBuf)
	cacheCmd.SetArgs([]string{"clear"})

	if err := cacheCmd.Execute(); err != nil {
		t.Fatalf("cache clear command failed: %v", err)
	}

	if !strings.Contains(cacheBuf.String(), "Cleared") {
		t.Errorf("expected cache clear confirmation, got:\n%s", cacheBuf.String())
	}
}

func TestHistoryCmdConcurrentCostFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/projects":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"projects":[{"project_id":"proj-hist"}]}`))
		case "/v1/projects/proj-hist/requests/req-job-1":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"response":{"details":{"usd":0.09633}}}`))
		case "/v1/projects/proj-hist/requests/req-job-2":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`null`)) // Pending / N/A
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	dir := t.TempDir()

	rec1 := deepgram.JobRecord{
		RequestID:       "req-job-1",
		Filename:        "file1.m4a",
		SHA256:          "sha111",
		DurationSeconds: 1000.0,
		CostUSD:         "$0.073",
		CostIsActual:    false,
	}

	rec2 := deepgram.JobRecord{
		RequestID:       "req-job-2",
		Filename:        "file2.m4a",
		SHA256:          "sha222",
		DurationSeconds: 500.0,
		CostUSD:         "$0.036",
		CostIsActual:    false,
	}

	_ = deepgram.SaveJobRecord(dir, rec1)
	_ = deepgram.SaveJobRecord(dir, rec2)

	t.Setenv("DEEPGRAM_API_KEY", "test-key")

	var outBuf bytes.Buffer
	cmd := newHistoryCmdWithDirAndEndpoint(dir, server.URL)
	cmd.SetOut(&outBuf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("history execution failed: %v", err)
	}

	outStr := outBuf.String()
	if !strings.Contains(outStr, "$0.096") {
		t.Errorf("expected fetched actual cost $0.096 in history, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "N/A yet") {
		t.Errorf("expected N/A yet for pending job in history, got:\n%s", outStr)
	}

	// Verify that rec1 was updated in cache with CostIsActual: true
	updatedRec1, err := deepgram.GetJobRecordBySHA(dir, "sha111")
	if err != nil || updatedRec1 == nil {
		t.Fatalf("failed to retrieve updated rec1 from cache: %v", err)
	}
	if !updatedRec1.CostIsActual || updatedRec1.CostUSD != "$0.096" {
		t.Errorf("expected rec1 in cache to be updated to actual cost $0.096, got: %+v", updatedRec1)
	}
}

func TestRunTranscribeCacheFirstSkipsPreprocessing(t *testing.T) {
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "interview.m4a")
	audioContent := []byte("fake raw audio bytes for cache first test")

	if err := os.WriteFile(audioFile, audioContent, 0644); err != nil {
		t.Fatalf("failed to write test audio file: %v", err)
	}

	// Pre-seed cache entry with default tech terms
	defaultTerms := terms.CombineTerms(terms.DefaultTechTerms())
	opts := deepgram.Options{
		Model:           "nova-3",
		Language:        "en",
		Diarize:         true,
		SmartFormatting: true,
		Utterances:      true,
		Punctuate:       true,
		Terms:           defaultTerms,
	}

	optsKey := deepgram.CacheKey(audioContent, opts)
	mockResp := &deepgram.PreRecordedResponse{
		Metadata: deepgram.Metadata{
			RequestID: "cached-req-001",
			Duration:  15.0,
		},
		Results: deepgram.Results{
			Utterances: []deepgram.Utterance{
				{Speaker: 0, Transcript: "Cached transcript without preprocessing"},
			},
		},
	}

	if err := deepgram.SaveCachedResponse(dir, optsKey, mockResp); err != nil {
		t.Fatalf("failed to seed cached response: %v", err)
	}

	var errBuf bytes.Buffer
	var outBuf bytes.Buffer

	cmd := NewRootCmdWithDirAndEndpoint(dir, "")
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{audioFile, "--no-preprocess"})

	os.Unsetenv("DEEPGRAM_API_KEY")

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected cache hit success, got error: %v", err)
	}

	if !strings.Contains(errBuf.String(), "Using cached transcript") {
		t.Errorf("expected stderr log 'Using cached transcript', got: %s", errBuf.String())
	}

	if !strings.Contains(outBuf.String(), "Cached transcript without preprocessing") {
		t.Errorf("expected cached transcript output, got: %s", outBuf.String())
	}
}

func TestRunTranscribeSourceAudioMatchTermDiff(t *testing.T) {
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "interview.m4a")
	audioContent := []byte("fake raw audio bytes for term diff test")

	if err := os.WriteFile(audioFile, audioContent, 0644); err != nil {
		t.Fatalf("failed to write test audio file: %v", err)
	}

	sourceSHA := deepgram.SourceAudioKey(audioContent)
	mockResp := &deepgram.PreRecordedResponse{
		Metadata: deepgram.Metadata{
			RequestID: "req-source-diff",
			Duration:  25.0,
		},
		Results: deepgram.Results{
			Utterances: []deepgram.Utterance{
				{Speaker: 0, Transcript: "Transcript from original source audio"},
			},
		},
	}

	env := deepgram.JobRecordEnvelope{
		Record: deepgram.JobRecord{
			RequestID:    "req-source-diff",
			Filename:     "interview.m4a",
			SourceSHA256: sourceSHA,
			SHA256:       "oldoptskey123",
			Timestamp:    time.Now(),
			Terms:        []string{"Go", "Kubernetes"},
		},
		Response: mockResp,
	}

	if err := deepgram.SaveJobEnvelope(dir, "oldoptskey123", env); err != nil {
		t.Fatalf("failed to seed job envelope: %v", err)
	}

	var errBuf bytes.Buffer
	var outBuf bytes.Buffer

	cmd := NewRootCmdWithDirAndEndpoint(dir, "")
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{audioFile, "-t", "Harvey", "-t", "Brian", "--no-preprocess"})

	os.Unsetenv("DEEPGRAM_API_KEY")

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected source match success, got error: %v", err)
	}

	if !strings.Contains(errBuf.String(), "Provided terms/flags differ from cached job") {
		t.Errorf("expected term diff warning in stderr, got: %s", errBuf.String())
	}

	if !strings.Contains(outBuf.String(), "Transcript from original source audio") {
		t.Errorf("expected cached transcript output, got: %s", outBuf.String())
	}
}

func TestRunTranscribeForceFlagBypassesCache(t *testing.T) {
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "interview.m4a")
	audioContent := []byte("fake raw audio bytes for force flag test")

	if err := os.WriteFile(audioFile, audioContent, 0644); err != nil {
		t.Fatalf("failed to write test audio file: %v", err)
	}

	sourceSHA := deepgram.SourceAudioKey(audioContent)
	oldResp := &deepgram.PreRecordedResponse{
		Metadata: deepgram.Metadata{
			RequestID: "old-cached-req",
			Duration:  10.0,
		},
		Results: deepgram.Results{
			Utterances: []deepgram.Utterance{
				{Speaker: 0, Transcript: "Old cached transcript"},
			},
		},
	}

	_ = deepgram.SaveJobEnvelope(dir, "oldkey", deepgram.JobRecordEnvelope{
		Record: deepgram.JobRecord{
			RequestID:    "old-cached-req",
			SourceSHA256: sourceSHA,
		},
		Response: oldResp,
	})

	freshMockJSON := `{
		"metadata": {
			"request_id": "fresh-forced-req-002",
			"duration": 12.0
		},
		"results": {
			"utterances": [
				{
					"start": 0.0,
					"end": 12.0,
					"speaker": 0,
					"transcript": "Fresh forced transcript from Deepgram"
				}
			]
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(freshMockJSON))
	}))
	defer server.Close()

	var outBuf bytes.Buffer
	cmd := NewRootCmdWithDirAndEndpoint(dir, server.URL)
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{audioFile, "--force", "--api-key", "test-key", "--no-preprocess"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected force re-transcribe success, got error: %v", err)
	}

	if !strings.Contains(outBuf.String(), "Fresh forced transcript from Deepgram") {
		t.Errorf("expected fresh forced transcript output, got: %s", outBuf.String())
	}
}

func TestRunTranscribeSuccessWithMockServer(t *testing.T) {
	mockJSON := `{
		"metadata": {
			"request_id": "mock-req-001",
			"duration": 5.2
		},
		"results": {
			"utterances": [
				{
					"start": 0.0,
					"end": 5.2,
					"speaker": 0,
					"transcript": "Hello world from Deepgram"
				}
			]
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockJSON))
	}))
	defer server.Close()

	dir := t.TempDir()
	audioFile := filepath.Join(dir, "interview.m4a")
	termsFile := filepath.Join(dir, "terms.txt")
	outFile := filepath.Join(dir, "out", "transcript.md")

	if err := os.WriteFile(audioFile, []byte("fake audio content"), 0644); err != nil {
		t.Fatalf("failed to write audio file: %v", err)
	}

	if err := os.WriteFile(termsFile, []byte("Envoy\nAlex"), 0644); err != nil {
		t.Fatalf("failed to write terms file: %v", err)
	}

	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	cmd := NewRootCmdWithDirAndEndpoint(dir, server.URL)
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{
		audioFile,
		"--api-key", "test-key",
		"-t", "CustomTerm",
		"--terms-file", termsFile,
		"--no-preprocess",
		"-o", outFile,
	})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("runTranscribe failed: %v", err)
	}

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	if !strings.Contains(string(content), "Hello world from Deepgram") {
		t.Errorf("expected transcript in output file, got:\n%s", string(content))
	}

	if !strings.Contains(errBuf.String(), "(Calculated API Cost: $0.001)") {
		t.Errorf("expected stderr to contain Calculated API Cost, got: %s", errBuf.String())
	}
}

func TestCostCommandFallbackEstimatedCost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/projects":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"projects":[{"project_id":"proj-fallback"}]}`))
		case "/v1/projects/proj-fallback/requests/mock-req-fallback":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`null`)) // Pending / unpopulated
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	dir := t.TempDir()

	rec := deepgram.JobRecord{
		RequestID:       "mock-req-fallback",
		Filename:        "interview.m4a",
		SHA256:          "sha-fallback-123",
		DurationSeconds: 60.0,
		CostUSD:         "$0.011",
		CostIsActual:    false,
	}

	_ = deepgram.SaveJobRecord(dir, rec)

	t.Setenv("DEEPGRAM_API_KEY", "test-key")

	var outBuf bytes.Buffer
	cmd := newCostCmdWithDirAndEndpoint(dir, server.URL)
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"mock-req-fallback"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("cost command failed: %v", err)
	}

	outStr := outBuf.String()
	if !strings.Contains(outStr, "N/A yet (Estimated: $0.011 USD)") {
		t.Errorf("expected cost output to contain 'N/A yet (Estimated: $0.011 USD)', got:\n%s", outStr)
	}
}
