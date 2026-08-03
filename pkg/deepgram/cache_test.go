package deepgram

import (
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
