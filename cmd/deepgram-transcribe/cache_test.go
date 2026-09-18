package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
)

func TestCacheStatusReportsLocationAndCount(t *testing.T) {
	cacheDir := t.TempDir()
	seedJob(t, cacheDir, deepgram.JobRecord{RequestID: "req-status", Filename: "call.m4a"})

	root, out, _ := newTestCLI(t, cacheDir, "")
	root.SetArgs([]string{"cache", "status"})

	if err := root.Execute(); err != nil {
		t.Fatalf("cache status failed: %v", err)
	}

	status := out.String()
	if !strings.Contains(status, cacheDir) {
		t.Errorf("expected the cache folder to be named, got:\n%s", status)
	}
	if !strings.Contains(status, "Cached responses:") || !strings.Contains(status, "1") {
		t.Errorf("expected a count of cached responses, got:\n%s", status)
	}
}

func TestCacheClearRemovesCachedResponsesOnly(t *testing.T) {
	cacheDir := t.TempDir()
	seedJob(t, cacheDir, deepgram.JobRecord{RequestID: "req-clear", Filename: "call.m4a"})

	unrelated := filepath.Join(cacheDir, "notes.txt")
	if err := os.WriteFile(unrelated, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("writing an unrelated file: %v", err)
	}

	root, _, errOut := newTestCLI(t, cacheDir, "")
	root.SetArgs([]string{"cache", "clear"})

	if err := root.Execute(); err != nil {
		t.Fatalf("cache clear failed: %v", err)
	}

	if !strings.Contains(errOut.String(), "Deleted 1 cached response") {
		t.Errorf("expected a confirmation naming what was deleted, got: %s", errOut.String())
	}

	records, err := deepgram.ListJobRecords(cacheDir)
	if err != nil {
		t.Fatalf("listing records after clear: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected no cached responses after clear, got %d", len(records))
	}

	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("expected the unrelated file to survive, got: %v", err)
	}
	if _, err := os.Stat(cacheDir); err != nil {
		t.Errorf("expected the cache folder itself to survive, got: %v", err)
	}
}
