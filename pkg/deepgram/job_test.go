package deepgram

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndGetJobRecord(t *testing.T) {
	dir := t.TempDir()

	rec := JobRecord{
		RequestID:       "req-999-abc",
		Filename:        "interview.m4a",
		FilePath:        "/tmp/interview.m4a",
		SHA256:          "sha256hash123456",
		Timestamp:       time.Date(2026, 3, 31, 14, 0, 0, 0, time.UTC),
		DurationSeconds: 930.4,
		Channels:        1,
		Model:           "nova-3",
		Preprocessed:    true,
		CostUSD:         "$0.067",
	}

	// Save record
	if err := SaveJobRecord(dir, rec); err != nil {
		t.Fatalf("SaveJobRecord failed: %v", err)
	}

	// Retrieve by SHA256
	gotBySHA, err := GetJobRecordBySHA(dir, "sha256hash123456")
	if err != nil {
		t.Fatalf("GetJobRecordBySHA failed: %v", err)
	}
	if gotBySHA.RequestID != "req-999-abc" {
		t.Errorf("expected RequestID 'req-999-abc', got %q", gotBySHA.RequestID)
	}
	if gotBySHA.CostUSD != "$0.067" {
		t.Errorf("expected CostUSD '$0.067', got %q", gotBySHA.CostUSD)
	}

	// Retrieve by RequestID
	gotByReqID, err := GetJobRecordByRequestID(dir, "req-999-abc")
	if err != nil {
		t.Fatalf("GetJobRecordByRequestID failed: %v", err)
	}
	if gotByReqID.SHA256 != "sha256hash123456" {
		t.Errorf("expected SHA256 'sha256hash123456', got %q", gotByReqID.SHA256)
	}

	// List records
	recs, err := ListJobRecords(dir)
	if err != nil {
		t.Fatalf("ListJobRecords failed: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 job record, got %d", len(recs))
	}

	// Clear cache
	if err := ClearCache(dir); err != nil {
		t.Fatalf("ClearCache failed: %v", err)
	}

	recsAfterClear, err := ListJobRecords(dir)
	if err != nil {
		t.Fatalf("ListJobRecords after clear failed: %v", err)
	}
	if len(recsAfterClear) != 0 {
		t.Errorf("expected 0 job records after clear, got %d", len(recsAfterClear))
	}
}

func TestCalculateCostUSD(t *testing.T) {
	tests := []struct {
		duration float64
		channels int
		want     string
	}{
		{60.0, 1, "$0.004"},
		{600.0, 1, "$0.043"},
		{600.0, 2, "$0.086"},
		{0.0, 0, "$0.000"},
	}

	for _, tt := range tests {
		got := CalculateCostUSD(tt.duration, tt.channels)
		if got != tt.want {
			t.Errorf("CalculateCostUSD(%f, %d) = %q, want %q", tt.duration, tt.channels, got, tt.want)
		}
	}
}

func TestCalculateCostWithOptions(t *testing.T) {
	tests := []struct {
		name     string
		duration float64
		channels int
		opts     Options
		want     string
	}{
		{
			name:     "default nova-3 no diarization",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "nova-3"},
			want:     "$0.043",
		},
		{
			name:     "nova-3 with diarization",
			duration: 1019.588, // ~17 min
			channels: 1,
			opts:     Options{Model: "nova-3", Diarize: true},
			want:     "$0.096", // Matches actual Deepgram billing ($0.09633)
		},
		{
			name:     "enhanced model",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "enhanced"},
			want:     "$0.145",
		},
		{
			name:     "base model",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "base"},
			want:     "$0.125",
		},
		{
			name:     "multichannel with diarization",
			duration: 300.0,
			channels: 2,
			opts:     Options{Model: "nova-3", Diarize: true},
			want:     "$0.057",
		},
		{
			name:     "zero duration",
			duration: 0.0,
			channels: 1,
			opts:     Options{Diarize: true},
			want:     "$0.000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateCostWithOptions(tt.duration, tt.channels, tt.opts)
			if got != tt.want {
				t.Errorf("CalculateCostWithOptions(%f, %d, %+v) = %q, want %q", tt.duration, tt.channels, tt.opts, got, tt.want)
			}
		})
	}
}

func TestFindJobRecordByTarget(t *testing.T) {
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "test.wav")
	audioData := []byte("audio data content for target test")

	if err := os.WriteFile(audioPath, audioData, 0644); err != nil {
		t.Fatalf("failed to write test audio file: %v", err)
	}

	rawSHA := SourceAudioKey(audioData)
	rec := JobRecord{
		RequestID:       "req-target-123",
		Filename:        "test.wav",
		FilePath:        audioPath,
		SourceSHA256:    rawSHA,
		SHA256:          "optskey1234567890",
		Timestamp:       time.Now(),
		DurationSeconds: 120.0,
		Channels:        1,
		CostUSD:         "$0.009",
	}

	if err := SaveJobRecord(dir, rec); err != nil {
		t.Fatalf("SaveJobRecord failed: %v", err)
	}

	// 1. Match by existing file path
	gotFile, err := FindJobRecordByTarget(dir, audioPath)
	if err != nil {
		t.Fatalf("FindJobRecordByTarget by file failed: %v", err)
	}
	if gotFile.RequestID != "req-target-123" {
		t.Errorf("expected RequestID 'req-target-123', got %q", gotFile.RequestID)
	}

	// 2. Match by Request ID
	gotReq, err := FindJobRecordByTarget(dir, "req-target-123")
	if err != nil {
		t.Fatalf("FindJobRecordByTarget by request ID failed: %v", err)
	}
	if gotReq.Filename != "test.wav" {
		t.Errorf("expected Filename 'test.wav', got %q", gotReq.Filename)
	}

	// 3. Non-existent target
	_, err = FindJobRecordByTarget(dir, "nonexistent-target")
	if err == nil {
		t.Error("expected error for nonexistent target, got nil")
	}
}

func TestJobRecordErrors(t *testing.T) {
	dir := t.TempDir()

	// Missing SHA record
	_, err := GetJobRecordBySHA(dir, "nonexistent-sha")
	if err == nil {
		t.Error("expected error for nonexistent SHA record")
	}

	// Corrupt JSON file in GetJobRecordBySHA
	corruptPath := filepath.Join(dir, "corrupt-sha.json")
	if err := os.WriteFile(corruptPath, []byte("invalid json"), 0644); err != nil {
		t.Fatalf("failed writing corrupt file: %v", err)
	}
	_, err = GetJobRecordBySHA(dir, "corrupt-sha")
	if err == nil {
		t.Error("expected error decoding corrupt job record JSON")
	}

	// GetJobRecordByRequestID missing target
	_, err = GetJobRecordByRequestID(dir, "missing-request-id")
	if err == nil {
		t.Error("expected error when Request ID is not found")
	}

	// ListJobRecords with non-existent directory returns nil error and empty list
	recs, err := ListJobRecords(filepath.Join(dir, "nonexistent-dir"))
	if err != nil {
		t.Errorf("expected nil error for non-existent cache dir in ListJobRecords, got %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("expected 0 records for non-existent cache dir, got %d", len(recs))
	}
}
