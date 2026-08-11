package deepgram

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultCacheDir returns the default transcript cache directory using XDG standards.
// Priority:
// 1. $XDG_CACHE_HOME/deepgram-transcribe (if XDG_CACHE_HOME is set)
// 2. ~/.cache/deepgram-transcribe
func DefaultCacheDir() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "deepgram-transcribe")
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "deepgram-transcribe")
	}

	return filepath.Join(".cache", "deepgram-transcribe")
}

// SourceAudioKey computes a SHA-256 hash strictly on raw source audio content bytes.
func SourceAudioKey(audioData []byte) string {
	h := sha256.Sum256(audioData)
	return hex.EncodeToString(h[:])
}

// CacheKey computes a deterministic SHA-256 hash based on audio content and request options.
func CacheKey(audioData []byte, opts Options) string {
	h := sha256.New()
	h.Write(audioData)
	h.Write([]byte(opts.Model))
	h.Write([]byte(opts.Language))

	if opts.Diarize {
		h.Write([]byte(":diarize"))
	}
	if opts.SmartFormatting {
		h.Write([]byte(":smart_format"))
	}

	// Sort terms for deterministic hashing regardless of slice order
	sortedTerms := make([]string, len(opts.Terms))
	copy(sortedTerms, opts.Terms)
	sort.Strings(sortedTerms)

	for _, term := range sortedTerms {
		h.Write([]byte(":" + strings.TrimSpace(term)))
	}

	return hex.EncodeToString(h.Sum(nil))
}

// GetCachedResponse attempts to load a previously saved Deepgram response from cacheDir.
func GetCachedResponse(cacheDir, key string) (*PreRecordedResponse, error) {
	env, err := GetJobEnvelope(cacheDir, key)
	if err == nil && env.Response != nil {
		return env.Response, nil
	}

	// Fallback for raw PreRecordedResponse JSON files (e.g. unwrapped response dumps or test fixtures)
	cachePath := filepath.Join(cacheDir, key+".json")
	data, readErr := os.ReadFile(cachePath)
	if readErr != nil {
		return nil, fmt.Errorf("reading cache file %q: %w", cachePath, readErr)
	}

	var resp PreRecordedResponse
	if jsonErr := json.Unmarshal(data, &resp); jsonErr != nil {
		return nil, fmt.Errorf("decoding cached response JSON: %w", jsonErr)
	}

	return &resp, nil
}

// FindCachedJobBySourceSHA scans cacheDir for any cached job envelope matching raw sourceSHA.
func FindCachedJobBySourceSHA(cacheDir, sourceSHA string) (*JobRecordEnvelope, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("cache directory does not exist")
		}
		return nil, fmt.Errorf("reading cache directory: %w", err)
	}

	var bestMatch *JobRecordEnvelope
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		key := strings.TrimSuffix(entry.Name(), ".json")
		env, err := GetJobEnvelope(cacheDir, key)
		if err != nil || env.Response == nil {
			continue
		}

		matchByPrefix := len(sourceSHA) >= 16 && strings.HasPrefix(entry.Name(), sourceSHA[:16])
		if env.Record.SourceSHA256 == sourceSHA || env.Record.SHA256 == sourceSHA || matchByPrefix {
			if bestMatch == nil || env.Record.Timestamp.After(bestMatch.Record.Timestamp) {
				bestMatch = env
			}
		}
	}

	if bestMatch != nil {
		return bestMatch, nil
	}

	return nil, fmt.Errorf("no cached job found matching source SHA %q", sourceSHA)
}

// SaveCachedResponse saves a Deepgram response JSON to cacheDir under key, wrapping in JobRecordEnvelope.
func SaveCachedResponse(cacheDir, key string, resp *PreRecordedResponse) error {
	env, _ := GetJobEnvelope(cacheDir, key)
	if env == nil {
		env = &JobRecordEnvelope{}
	}

	env.Response = resp
	if env.Record.SHA256 == "" {
		env.Record.SHA256 = key
	}
	if resp != nil && env.Record.RequestID == "" {
		env.Record.RequestID = resp.Metadata.RequestID
	}
	if resp != nil && resp.Metadata.Duration > 0 && env.Record.DurationSeconds == 0 {
		env.Record.DurationSeconds = resp.Metadata.Duration
	}

	return SaveJobEnvelope(cacheDir, key, *env)
}
