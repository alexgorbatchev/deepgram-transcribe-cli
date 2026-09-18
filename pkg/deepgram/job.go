package deepgram

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Deepgram's published pay-as-you-go list rates for pre-recorded audio, in US
// dollars per minute of audio per channel.
const (
	Nova3RatePerMinute             = 0.0043 // Nova-3 Monolingual
	Nova3MultilingualRatePerMinute = 0.0052 // Nova-3 Multilingual, which `language=multi` selects
	WhisperRatePerMinute           = 0.0048 // Whisper Large

	// KeytermRatePerMinute is the list rate for Deepgram's keyterm prompting
	// add-on on pre-recorded audio. It is the only add-on this tool turns on
	// that Deepgram charges for: smart formatting is included, and speaker
	// diarization is billed on streaming audio only, not on pre-recorded.
	KeytermRatePerMinute = 0.0013
)

// CostUnknown stands in for an estimate that cannot be made honestly. Deepgram's
// rate card prices only the models above; Nova-2, Nova-1, Enhanced and Base are
// still accepted by the API but no longer appear on it, and a stale number
// presented as a cost is worse than admitting there is none. What Deepgram
// actually charged still arrives later from the billing API.
const CostUnknown = "unknown"

// listRatePerMinute returns Deepgram's published pre-recorded rate for the model
// a request asks for, and false when Deepgram publishes no rate for it.
func listRatePerMinute(opts Options) (float64, bool) {
	model := strings.ToLower(opts.EffectiveModel())

	switch {
	case strings.HasPrefix(model, "nova-3"):
		// The multilingual model is a separate, dearer line on the rate card,
		// and `language=multi` is what selects it.
		if strings.EqualFold(strings.TrimSpace(opts.Language), "multi") {
			return Nova3MultilingualRatePerMinute, true
		}
		return Nova3RatePerMinute, true
	case strings.HasPrefix(model, "whisper"):
		return WhisperRatePerMinute, true
	default:
		return 0, false
	}
}

// CalculateCostWithOptions calculates estimated Deepgram transcription cost factoring in model rates and feature add-ons.
//
// The result is an estimate against Deepgram's published pay-as-you-go list
// prices, so it ignores negotiated or volume rates. `job list` and `job inspect`
// replace it with what Deepgram actually charged as soon as that is available.
func CalculateCostWithOptions(durationSeconds float64, channels int, opts Options) string {
	if channels < 1 {
		channels = 1
	}

	ratePerMinute, priced := listRatePerMinute(opts)
	if !priced {
		return CostUnknown
	}

	if len(opts.Keyterms()) > 0 {
		ratePerMinute += KeytermRatePerMinute
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
	DiarizeModel    string    `json:"diarize_model,omitempty"`
	Preprocessed    bool      `json:"preprocessed"`
	Terms           []string  `json:"terms,omitempty"`
	CostUSD         string    `json:"cost_usd"`
	CostIsActual    bool      `json:"cost_is_actual,omitempty"`
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

// FindJobRecordByTarget looks up a job by audio file or by Deepgram request ID.
//
// A file matches only when its contents hash to a value that was recorded, never
// when it merely shares a name: two different recordings called interview.m4a
// must not resolve to each other's cost.
func FindJobRecordByTarget(cacheDir, target string) (*JobRecord, error) {
	info, statErr := os.Stat(target)
	if statErr != nil || info.IsDir() {
		return GetJobRecordByRequestID(cacheDir, target)
	}

	audioBytes, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("reading audio file %q: %w", target, err)
	}

	records, err := ListJobRecords(cacheDir)
	if err != nil {
		return nil, err
	}

	// Records arrive newest first, so the first match is the most recent one.
	sourceSHA := SourceAudioKey(audioBytes)
	for _, rec := range records {
		if rec.SourceSHA256 == sourceSHA || rec.SHA256 == sourceSHA {
			return &rec, nil
		}
	}

	return nil, fmt.Errorf("no transcription job found for audio file %q", target)
}

// cacheFileName matches the file names this package writes: a SHA-256 key in
// lowercase hex, plus the .json suffix.
var cacheFileName = regexp.MustCompile(`^[0-9a-f]{64}\.json$`)

// ClearCache deletes the cache files this package created in cacheDir and reports
// how many were removed.
//
// It removes individual files rather than the directory, and only files whose
// names match the keys it writes, so pointing --cache-dir at the wrong folder can
// never destroy unrelated data.
func ClearCache(cacheDir string) (int, error) {
	if err := checkClearableCacheDir(cacheDir); err != nil {
		return 0, err
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading cache directory %q: %w", cacheDir, err)
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || !cacheFileName.MatchString(entry.Name()) {
			continue
		}

		path := filepath.Join(cacheDir, entry.Name())
		if err := os.Remove(path); err != nil {
			return removed, fmt.Errorf("removing cache file %q: %w", path, err)
		}
		removed++
	}

	return removed, nil
}

// checkClearableCacheDir rejects locations that cannot be a transcript cache,
// so that a mistyped --cache-dir is refused instead of acted on.
func checkClearableCacheDir(cacheDir string) error {
	if strings.TrimSpace(cacheDir) == "" {
		return errors.New("no cache directory was given")
	}

	abs, err := filepath.Abs(cacheDir)
	if err != nil {
		return fmt.Errorf("resolving cache directory %q: %w", cacheDir, err)
	}

	if abs == filepath.Dir(abs) {
		return fmt.Errorf("refusing to clear the filesystem root %q", abs)
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if homeAbs, err := filepath.Abs(home); err == nil && homeAbs == abs {
			return fmt.Errorf("refusing to clear the home directory %q", abs)
		}
	}

	return nil
}
