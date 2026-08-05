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

const (
	// Default base rates per minute
	BilledRatePerMinute   = 0.0043 // Nova-3, Nova-2, Nova-1, Flux default list rate
	EnhancedRatePerMinute = 0.0145 // Enhanced model rate
	BaseRatePerMinute     = 0.0125 // Base model rate

	// Add-on feature rates per minute
	DiarizeRatePerMinute = 0.00137 // Diarization add-on
)

// CalculateCostUSD calculates estimated Deepgram transcription cost formatted as "$X.XXX".
func CalculateCostUSD(durationSeconds float64, channels int) string {
	return CalculateCostWithOptions(durationSeconds, channels, Options{})
}

// CalculateCostWithOptions calculates estimated Deepgram transcription cost factoring in model rates and feature add-ons.
func CalculateCostWithOptions(durationSeconds float64, channels int, opts Options) string {
	if channels < 1 {
		channels = 1
	}

	ratePerMinute := BilledRatePerMinute

	modelLower := strings.ToLower(opts.Model)
	if strings.Contains(modelLower, "enhanced") {
		ratePerMinute = EnhancedRatePerMinute
	} else if strings.HasPrefix(modelLower, "base") {
		ratePerMinute = BaseRatePerMinute
	}

	if opts.Diarize {
		ratePerMinute += DiarizeRatePerMinute
	}

	cost := (durationSeconds / 60.0) * float64(channels) * ratePerMinute
	return fmt.Sprintf("$%.3f", cost)
}

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

// GetJobEnvelope reads and unmarshals a JobRecordEnvelope from cacheDir under key.
func GetJobEnvelope(cacheDir, key string) (*JobRecordEnvelope, error) {
	cachePath := filepath.Join(cacheDir, key+".json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("reading cache file %q: %w", cachePath, err)
	}

	var env JobRecordEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("decoding job envelope JSON from %q: %w", cachePath, err)
	}

	if env.Record.SHA256 == "" {
		env.Record.SHA256 = key
	}

	return &env, nil
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
	env, _ := GetJobEnvelope(cacheDir, record.SHA256)
	if env == nil {
		env = &JobRecordEnvelope{}
	}

	env.Record = record

	return SaveJobEnvelope(cacheDir, record.SHA256, *env)
}

// GetJobRecordBySHA retrieves a JobRecord by its SHA256 content key.
func GetJobRecordBySHA(cacheDir, sha256 string) (*JobRecord, error) {
	env, err := GetJobEnvelope(cacheDir, sha256)
	if err != nil {
		return nil, err
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

		key := strings.TrimSuffix(entry.Name(), ".json")
		env, err := GetJobEnvelope(cacheDir, key)
		if err != nil {
			continue
		}

		if env.Record.SHA256 == "" && env.Record.RequestID == "" && env.Record.Filename == "" && env.Record.Timestamp.IsZero() {
			continue
		}

		records = append(records, env.Record)
	}

	// Sort newest first
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.After(records[j].Timestamp)
	})

	return records, nil
}

// FindJobRecordByTarget searches for a JobRecord by audio file path / content SHA or Request ID.
func FindJobRecordByTarget(cacheDir, target string) (*JobRecord, error) {
	// 1. Try matching as an existing local audio file (SHA-256 lookup)
	if _, statErr := os.Stat(target); statErr == nil {
		audioBytes, readErr := os.ReadFile(target)
		if readErr == nil {
			records, listErr := ListJobRecords(cacheDir)
			if listErr == nil {
				rawSHA := SourceAudioKey(audioBytes)
				for _, rec := range records {
					matchPrefix := len(rawSHA) >= 16 && strings.HasPrefix(rec.SHA256, rawSHA[:16])
					if rec.SourceSHA256 == rawSHA || rec.SHA256 == rawSHA || matchPrefix || filepath.Base(rec.FilePath) == filepath.Base(target) {
						r := rec
						return &r, nil
					}
				}
			}
		}
	}

	// 2. If not found by file SHA/name, try matching by Request ID
	return GetJobRecordByRequestID(cacheDir, target)
}

// ClearCache removes all files in cacheDir.
func ClearCache(cacheDir string) error {
	if err := os.RemoveAll(cacheDir); err != nil {
		return fmt.Errorf("clearing cache directory %q: %w", cacheDir, err)
	}
	return nil
}
