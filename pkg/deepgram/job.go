package deepgram

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// JobRecord represents persistent metadata for a completed transcription job.
type JobRecord struct {
	RequestID       string    `json:"request_id"`
	Filename        string    `json:"filename"`
	FilePath        string    `json:"filepath"`
	SourceSHA256    string    `json:"source_sha256"`
	SHA256          string    `json:"sha256"`
	Timestamp       time.Time `json:"timestamp"`
	DurationSeconds float64   `json:"duration_seconds"`
	Channels        int       `json:"channels"`
	Model           string    `json:"model"`
	Preprocessed    bool      `json:"preprocessed"`
	Terms           []string  `json:"terms,omitempty"`
	CostUSD         string    `json:"cost_usd"`
}

// JobRecordEnvelope wraps the job record metadata and raw Deepgram response inside cache files.
type JobRecordEnvelope struct {
	Record   JobRecord            `json:"record"`
	Response *PreRecordedResponse `json:"response"`
}

// SaveJobEnvelope saves a full JobRecordEnvelope (record + response) under key.
func SaveJobEnvelope(cacheDir, key string, env JobRecordEnvelope) error {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("creating cache directory %q: %w", cacheDir, err)
	}

	cachePath := filepath.Join(cacheDir, key+".json")

	if env.Record.SHA256 == "" {
		env.Record.SHA256 = key
	}

	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding job envelope JSON: %w", err)
	}

	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return fmt.Errorf("writing job envelope file %q: %w", cachePath, err)
	}

	return nil
}

// SaveJobRecord persists a JobRecord and associated response to cacheDir under the SHA256 key.
func SaveJobRecord(cacheDir string, record JobRecord) error {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("creating cache directory %q: %w", cacheDir, err)
	}

	cachePath := filepath.Join(cacheDir, record.SHA256+".json")

	var env JobRecordEnvelope
	if existingData, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(existingData, &env)
	}

	env.Record = record

	return SaveJobEnvelope(cacheDir, record.SHA256, env)
}

// GetJobRecordBySHA retrieves a JobRecord by its SHA256 content key.
func GetJobRecordBySHA(cacheDir, sha256 string) (*JobRecord, error) {
	cachePath := filepath.Join(cacheDir, sha256+".json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("reading cache file for sha256 %q: %w", sha256, err)
	}

	var env JobRecordEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("decoding job record JSON: %w", err)
	}

	if env.Record.SHA256 == "" {
		env.Record.SHA256 = sha256
	}

	return &env.Record, nil
}

// GetJobRecordByRequestID searches cacheDir for a JobRecord matching requestID.
func GetJobRecordByRequestID(cacheDir, requestID string) (*JobRecord, error) {
	records, err := ListJobRecords(cacheDir)
	if err != nil {
		return nil, err
	}

	target := strings.TrimSpace(strings.ToLower(requestID))
	for _, rec := range records {
		if strings.ToLower(rec.RequestID) == target {
			return &rec, nil
		}
	}

	return nil, fmt.Errorf("no transcription job found matching request ID %q", requestID)
}

// ListJobRecords reads all stored JobRecords in cacheDir sorted chronologically.
func ListJobRecords(cacheDir string) ([]JobRecord, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading cache directory %q: %w", cacheDir, err)
	}

	var records []JobRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(cacheDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var env JobRecordEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}

		if env.Record.SHA256 == "" {
			env.Record.SHA256 = strings.TrimSuffix(entry.Name(), ".json")
		}

		records = append(records, env.Record)
	}

	// Sort newest first
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.After(records[j].Timestamp)
	})

	return records, nil
}

// ClearCache removes all files in cacheDir.
func ClearCache(cacheDir string) error {
	if err := os.RemoveAll(cacheDir); err != nil {
		return fmt.Errorf("clearing cache directory %q: %w", cacheDir, err)
	}
	return nil
}
