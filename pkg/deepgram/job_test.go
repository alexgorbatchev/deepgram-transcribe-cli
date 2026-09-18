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

}

func TestClearCacheRemovesOnlyItsOwnFiles(t *testing.T) {
	dir := t.TempDir()

	key := SourceAudioKey([]byte("recording contents"))
	if err := SaveJobRecord(dir, JobRecord{RequestID: "req-clear", SHA256: key}); err != nil {
		t.Fatalf("SaveJobRecord failed: %v", err)
	}

	unrelated := filepath.Join(dir, "important.json")
	if err := os.WriteFile(unrelated, []byte(`{"keep":true}`), 0o644); err != nil {
		t.Fatalf("writing an unrelated file failed: %v", err)
	}

	removed, err := ClearCache(dir)
	if err != nil {
		t.Fatalf("ClearCache failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 removed cache file, got %d", removed)
	}

	if _, err := os.Stat(filepath.Join(dir, key+".json")); !os.IsNotExist(err) {
		t.Errorf("expected the cache file to be removed, got err %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("expected the unrelated file to survive, got %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("expected the cache directory itself to survive, got %v", err)
	}
}

func TestClearCacheRefusesDangerousDirectories(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("home directory is unavailable: %v", err)
	}

	tests := []struct {
		name string
		dir  string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"filesystem root", string(filepath.Separator)},
		{"home directory", home},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removed, err := ClearCache(tt.dir)
			if err == nil {
				t.Fatalf("expected ClearCache(%q) to be refused", tt.dir)
			}
			if removed != 0 {
				t.Errorf("expected nothing to be removed, got %d", removed)
			}
		})
	}
}

func TestClearCacheOnMissingDirectory(t *testing.T) {
	removed, err := ClearCache(filepath.Join(t.TempDir(), "never-created"))
	if err != nil {
		t.Fatalf("expected a missing cache directory to be harmless, got %v", err)
	}
	if removed != 0 {
		t.Errorf("expected nothing to be removed, got %d", removed)
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
			name:     "default nova-3 with no add-ons",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "nova-3"},
			want:     "$0.043",
		},
		{
			// Deepgram bills speaker diarization only on streaming audio; on
			// pre-recorded audio it is included in the model rate.
			name:     "diarization alone costs nothing extra",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "nova-3", DiarizeModel: DiarizeModelLatest},
			want:     "$0.043",
		},
		{
			name:     "keyterm prompting is the billed add-on",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "nova-3", Terms: []string{"Kubernetes"}},
			want:     "$0.056",
		},
		{
			name:     "blank terms are never sent, so they never bill",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "nova-3", Terms: []string{"", "   "}},
			want:     "$0.043",
		},
		{
			name:     "nova-3 with diarization and keyterms",
			duration: 1019.588, // ~17 min
			channels: 1,
			opts:     Options{Model: "nova-3", DiarizeModel: DiarizeModelLatest, Terms: []string{"Kubernetes"}},
			want:     "$0.095",
		},
		{
			name:     "nova-3 multilingual costs more per minute",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "nova-3", Language: "multi"},
			want:     "$0.052",
		},
		{
			name:     "multilingual is recognised whatever its case",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "nova-3-general", Language: " MULTI "},
			want:     "$0.052",
		},
		{
			name:     "a single spoken language is monolingual",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "nova-3", Language: "en"},
			want:     "$0.043",
		},
		{
			name:     "no model means the default one",
			duration: 600.0,
			channels: 1,
			opts:     Options{},
			want:     "$0.043",
		},
		{
			name:     "whisper large",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "whisper-large"},
			want:     "$0.048",
		},
		{
			// Deepgram's rate card no longer prices these models, so quoting a
			// figure for them would be inventing one.
			name:     "enhanced is no longer publicly priced",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "enhanced"},
			want:     CostUnknown,
		},
		{
			name:     "base is no longer publicly priced",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "base"},
			want:     CostUnknown,
		},
		{
			name:     "nova-2 is no longer publicly priced",
			duration: 600.0,
			channels: 1,
			opts:     Options{Model: "nova-2", Terms: []string{"Kubernetes"}},
			want:     CostUnknown,
		},
		{
			name:     "multichannel with diarization and keyterms",
			duration: 300.0,
			channels: 2,
			opts:     Options{Model: "nova-3", DiarizeModel: DiarizeModelLatest, Terms: []string{"Kubernetes"}},
			want:     "$0.056",
		},
		{
			name:     "zero duration",
			duration: 0.0,
			channels: 1,
			opts:     Options{DiarizeModel: DiarizeModelLatest},
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

func TestFindJobRecordByTargetIgnoresMatchingFilenames(t *testing.T) {
	dir := t.TempDir()

	recorded := []byte("the recording that was actually transcribed")
	if err := SaveJobRecord(dir, JobRecord{
		RequestID:    "req-recorded",
		Filename:     "interview.m4a",
		FilePath:     filepath.Join(dir, "first", "interview.m4a"),
		SourceSHA256: SourceAudioKey(recorded),
		SHA256:       CacheKey(recorded, Options{}),
	}); err != nil {
		t.Fatalf("SaveJobRecord failed: %v", err)
	}

	// A different recording that happens to carry the same file name must not
	// resolve to the record above, or it would report someone else's cost.
	otherDir := filepath.Join(dir, "second")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("creating the second directory failed: %v", err)
	}
	impostor := filepath.Join(otherDir, "interview.m4a")
	if err := os.WriteFile(impostor, []byte("an entirely different recording"), 0o644); err != nil {
		t.Fatalf("writing the second recording failed: %v", err)
	}

	if got, err := FindJobRecordByTarget(dir, impostor); err == nil {
		t.Errorf("expected no match for a different recording, got record %q", got.RequestID)
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
