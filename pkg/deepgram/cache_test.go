package deepgram

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCacheKey(t *testing.T) {
	audio1 := []byte("audio data 1")
	audio2 := []byte("audio data 2")

	opts1 := Options{Model: "nova-3", Terms: []string{"Go", "Kubernetes"}}
	opts2 := Options{Model: "nova-3", Terms: []string{"Go", "Kubernetes"}}
	opts3 := Options{Model: "nova-2", Terms: []string{"Go", "Kubernetes"}}

	key1 := CacheKey(audio1, opts1)
	key2 := CacheKey(audio1, opts2)
	key3 := CacheKey(audio2, opts1)
	key4 := CacheKey(audio1, opts3)

	if key1 != key2 {
		t.Errorf("expected identical keys for same audio & options, got %s vs %s", key1, key2)
	}

	if key1 == key3 {
		t.Errorf("expected different keys for different audio content, got same %s", key1)
	}

	if key1 == key4 {
		t.Errorf("expected different keys for different model, got same %s", key1)
	}
}

func TestSourceAudioKey(t *testing.T) {
	audio1 := []byte("test audio content")
	key1 := SourceAudioKey(audio1)
	key2 := SourceAudioKey(audio1)

	if key1 != key2 {
		t.Errorf("expected SourceAudioKey to be deterministic, got %s vs %s", key1, key2)
	}
}

func TestFindCachedJobBySourceSHA(t *testing.T) {
	dir := t.TempDir()

	resp := &PreRecordedResponse{
		Metadata: Metadata{
			RequestID: "req-source-match",
			Duration:  20.0,
		},
		Results: Results{
			Utterances: []Utterance{
				{Speaker: 0, Transcript: "Matched source audio transcript"},
			},
		},
	}

	sourceSHA := "rawaudiosha12345"
	optsKey := "optskey67890"

	jobRec := JobRecord{
		RequestID:    "req-source-match",
		Filename:     "interview.m4a",
		SourceSHA256: sourceSHA,
		SHA256:       optsKey,
		Timestamp:    time.Now(),
		Terms:        []string{"Go", "Kubernetes"},
	}

	env := JobRecordEnvelope{
		Record:   jobRec,
		Response: resp,
	}

	// Save envelope under optsKey
	if err := SaveJobEnvelope(dir, optsKey, env); err != nil {
		t.Fatalf("SaveJobEnvelope failed: %v", err)
	}

	// Lookup by Source SHA
	foundEnv, err := FindCachedJobBySourceSHA(dir, sourceSHA)
	if err != nil {
		t.Fatalf("FindCachedJobBySourceSHA failed: %v", err)
	}

	if foundEnv.Record.RequestID != "req-source-match" {
		t.Errorf("expected RequestID 'req-source-match', got %q", foundEnv.Record.RequestID)
	}

	if foundEnv.Response.Results.Utterances[0].Transcript != "Matched source audio transcript" {
		t.Errorf("unexpected transcript text: %q", foundEnv.Response.Results.Utterances[0].Transcript)
	}
}

func TestDefaultCacheDir(t *testing.T) {
	dir := DefaultCacheDir()
	if dir == "" {
		t.Fatal("expected non-empty DefaultCacheDir")
	}
	if !strings.Contains(dir, ".cache/deepgram-transcribe") && !strings.Contains(dir, "deepgram-transcribe") {
		t.Errorf("expected DefaultCacheDir() to end in 'deepgram-transcribe', got %q", dir)
	}
}

func TestDefaultCacheDir_XDGOverride(t *testing.T) {
	customDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", customDir)

	got := DefaultCacheDir()
	expected := filepath.Join(customDir, "deepgram-transcribe")
	if got != expected {
		t.Errorf("expected DefaultCacheDir() to be %q, got %q", expected, got)
	}
}

func TestSaveAndGetCachedResponse(t *testing.T) {
	dir := t.TempDir()

	resp := &PreRecordedResponse{
		Metadata: Metadata{
			RequestID: "req-cache-123",
			Duration:  45.5,
		},
		Results: Results{
			Utterances: []Utterance{
				{Speaker: 0, Transcript: "Hello world"},
			},
		},
	}

	key := "test-cache-key-123"

	// Save
	if err := SaveCachedResponse(dir, key, resp); err != nil {
		t.Fatalf("SaveCachedResponse failed: %v", err)
	}

	// Get
	got, err := GetCachedResponse(dir, key)
	if err != nil {
		t.Fatalf("GetCachedResponse failed: %v", err)
	}

	if got.Metadata.RequestID != "req-cache-123" {
		t.Errorf("expected RequestID 'req-cache-123', got %q", got.Metadata.RequestID)
	}
	if len(got.Results.Utterances) != 1 || got.Results.Utterances[0].Transcript != "Hello world" {
		t.Errorf("unexpected transcript in cached response: %+v", got)
	}
}

func TestGetCachedResponseRawFormat(t *testing.T) {
	dir := t.TempDir()
	key := "raw-key"

	resp := PreRecordedResponse{
		Metadata: Metadata{
			RequestID: "req-raw-999",
			Duration:  10.0,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	cachePath := filepath.Join(dir, key+".json")
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	got, err := GetCachedResponse(dir, key)
	if err != nil {
		t.Fatalf("GetCachedResponse failed on raw format: %v", err)
	}

	if got.Metadata.RequestID != "req-raw-999" {
		t.Errorf("expected 'req-raw-999', got %q", got.Metadata.RequestID)
	}
}

func TestGetCachedResponseErrors(t *testing.T) {
	dir := t.TempDir()

	// Missing file
	_, err := GetCachedResponse(dir, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent cache file, got nil")
	}

	// Corrupt JSON
	corruptPath := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corruptPath, []byte("invalid json {{{"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	_, err = GetCachedResponse(dir, "corrupt")
	if err == nil {
		t.Error("expected error for corrupt cache file JSON, got nil")
	}
}

func TestFindCachedJobBySourceSHAErrors(t *testing.T) {
	dir := t.TempDir()

	// 1. Non-existent cache directory
	_, err := FindCachedJobBySourceSHA(filepath.Join(dir, "doesnotexist"), "1234567890123456")
	if err == nil {
		t.Error("expected error for non-existent cache directory")
	}

	// 2. Directory with invalid JSON files and short SHA
	if err := os.WriteFile(filepath.Join(dir, "invalid.json"), []byte("not-json"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	_, err = FindCachedJobBySourceSHA(dir, "shortsha")
	if err == nil {
		t.Error("expected error when no matching cache entry exists")
	}
}
