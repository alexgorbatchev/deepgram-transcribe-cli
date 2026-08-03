package deepgram

import (
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
