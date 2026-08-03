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

// DefaultCacheDir returns the local cache directory path inside the project root's .tmp/
func DefaultCacheDir() string {
	cwd, err := os.Getwd()
	if err == nil {
		dir := cwd
		for {
			tmpCandidate := filepath.Join(dir, ".tmp")
			gitCandidate := filepath.Join(dir, ".git")

			if _, err := os.Stat(tmpCandidate); err == nil {
				return filepath.Join(tmpCandidate, "transcribe_cache")
			}
			if _, err := os.Stat(gitCandidate); err == nil {
				return filepath.Join(dir, ".tmp", "transcribe_cache")
			}

			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return filepath.Join(".tmp", "transcribe_cache")
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
	cachePath := filepath.Join(cacheDir, key+".json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("reading cache file %q: %w", cachePath, err)
	}

	var env JobRecordEnvelope
	if err := json.Unmarshal(data, &env); err == nil && env.Response != nil {
		return env.Response, nil
	}

	var resp PreRecordedResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding cached response JSON: %w", err)
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

		filePath := filepath.Join(cacheDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var env JobRecordEnvelope
		if err := json.Unmarshal(data, &env); err != nil || env.Response == nil {
			continue
		}

		if env.Record.SourceSHA256 == sourceSHA || env.Record.SHA256 == sourceSHA || strings.HasPrefix(entry.Name(), sourceSHA[:16]) {
			if bestMatch == nil || env.Record.Timestamp.After(bestMatch.Record.Timestamp) {
				cp := env
				bestMatch = &cp
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
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("creating cache directory %q: %w", cacheDir, err)
	}

	cachePath := filepath.Join(cacheDir, key+".json")

	var env JobRecordEnvelope
	if existingData, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(existingData, &env)
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

	return SaveJobEnvelope(cacheDir, key, env)
}
