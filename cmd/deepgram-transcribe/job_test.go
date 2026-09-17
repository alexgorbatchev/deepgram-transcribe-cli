package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
)

// seedJob stores a job record under a realistic cache key and returns it.
func seedJob(t *testing.T, cacheDir string, record deepgram.JobRecord) deepgram.JobRecord {
	t.Helper()

	if record.SHA256 == "" {
		record.SHA256 = deepgram.SourceAudioKey([]byte(record.RequestID))
	}
	if err := deepgram.SaveJobRecord(cacheDir, record); err != nil {
		t.Fatalf("seeding job record %q: %v", record.RequestID, err)
	}

	return record
}

// newBillingServer answers Deepgram's management API with the given cost per
// request ID. A request ID that is absent answers "null", which is what Deepgram
// returns while a charge is still being processed.
func newBillingServer(t *testing.T, projectID string, costs map[string]string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/v1/projects" {
			if _, err := w.Write([]byte(`{"projects":[{"project_id":"` + projectID + `"}]}`)); err != nil {
				t.Errorf("writing projects response: %v", err)
			}
			return
		}

		prefix := "/v1/projects/" + projectID + "/requests/"
		requestID := strings.TrimPrefix(r.URL.Path, prefix)
		body, ok := costs[requestID]
		if !strings.HasPrefix(r.URL.Path, prefix) || !ok {
			body = "null"
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("writing request details response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func TestJobListShowsRecordedAndPendingCosts(t *testing.T) {
	cacheDir := t.TempDir()

	seedJob(t, cacheDir, deepgram.JobRecord{
		RequestID:       "req-actual-123",
		Filename:        "actual.m4a",
		Timestamp:       time.Date(2026, 3, 31, 14, 0, 0, 0, time.UTC),
		DurationSeconds: 600,
		Channels:        1,
		Model:           "nova-3",
		CostUSD:         "$0.043",
		CostIsActual:    true,
	})
	seedJob(t, cacheDir, deepgram.JobRecord{
		RequestID:       "req-estimate-456",
		Filename:        "estimate.m4a",
		Timestamp:       time.Date(2026, 3, 31, 15, 0, 0, 0, time.UTC),
		DurationSeconds: 300,
		Channels:        1,
		Model:           "nova-3",
		CostUSD:         "$0.021",
		CostIsActual:    false,
	})

	root, out, _ := newTestCLI(t, cacheDir, "")
	root.SetArgs([]string{"job", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("job list failed: %v", err)
	}

	listing := out.String()
	for _, want := range []string{"actual.m4a", "$0.043", "estimate.m4a", costPending, "Total billed"} {
		if !strings.Contains(listing, want) {
			t.Errorf("expected %q in the listing, got:\n%s", want, listing)
		}
	}
}

func TestJobListFetchesMissingCostsConcurrently(t *testing.T) {
	cacheDir := t.TempDir()
	server := newBillingServer(t, "proj-hist", map[string]string{
		"req-job-1": `{"response":{"details":{"usd":0.09633}}}`,
	})

	billed := seedJob(t, cacheDir, deepgram.JobRecord{
		RequestID:       "req-job-1",
		Filename:        "file1.m4a",
		Timestamp:       time.Date(2026, 3, 31, 15, 0, 0, 0, time.UTC),
		DurationSeconds: 1000,
		CostUSD:         "$0.073",
	})
	seedJob(t, cacheDir, deepgram.JobRecord{
		RequestID:       "req-job-2",
		Filename:        "file2.m4a",
		Timestamp:       time.Date(2026, 3, 31, 14, 0, 0, 0, time.UTC),
		DurationSeconds: 500,
		CostUSD:         "$0.036",
	})

	root, out, _ := newTestCLI(t, cacheDir, server.URL)
	root.SetArgs([]string{"job", "list", "--api-key", "test-key"})

	if err := root.Execute(); err != nil {
		t.Fatalf("job list failed: %v", err)
	}

	listing := out.String()
	if !strings.Contains(listing, "$0.096") {
		t.Errorf("expected the billed cost in the listing, got:\n%s", listing)
	}
	if !strings.Contains(listing, costPending) {
		t.Errorf("expected the unbilled job to be marked pending, got:\n%s", listing)
	}

	stored, err := deepgram.GetJobRecordBySHA(cacheDir, billed.SHA256)
	if err != nil {
		t.Fatalf("reading the updated record: %v", err)
	}
	if !stored.CostIsActual || stored.CostUSD != "$0.096" {
		t.Errorf("expected the billed cost to be saved, got %+v", stored)
	}
}

func TestJobListLimitsOutput(t *testing.T) {
	cacheDir := t.TempDir()

	seedJob(t, cacheDir, deepgram.JobRecord{
		RequestID: "req-new",
		Filename:  "newer.m4a",
		Timestamp: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
	})
	seedJob(t, cacheDir, deepgram.JobRecord{
		RequestID: "req-old",
		Filename:  "older.m4a",
		Timestamp: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	})

	root, out, _ := newTestCLI(t, cacheDir, "")
	root.SetArgs([]string{"job", "list", "--limit", "1"})

	if err := root.Execute(); err != nil {
		t.Fatalf("job list --limit failed: %v", err)
	}

	listing := out.String()
	if !strings.Contains(listing, "newer.m4a") {
		t.Errorf("expected the most recent job, got:\n%s", listing)
	}
	if strings.Contains(listing, "older.m4a") {
		t.Errorf("expected --limit to drop the older job, got:\n%s", listing)
	}
}

func TestJobListUsesTabSeparatedOutputForAgents(t *testing.T) {
	cacheDir := t.TempDir()
	seedJob(t, cacheDir, deepgram.JobRecord{
		RequestID:       "req-agent",
		Filename:        "agent.m4a",
		Timestamp:       time.Date(2026, 3, 31, 14, 0, 0, 0, time.UTC),
		DurationSeconds: 60,
		Channels:        1,
		Model:           "nova-3",
		CostUSD:         "$0.011",
		CostIsActual:    true,
	})

	root, out, _ := newTestCLI(t, cacheDir, "")
	t.Setenv("AGENT", "1")
	root.SetArgs([]string{"job", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("job list failed: %v", err)
	}

	listing := out.String()
	if !strings.Contains(listing, "date\tfile\tduration\tchannels\tmodel\tcost\trequest_id") {
		t.Errorf("expected a tab separated header, got:\n%s", listing)
	}
	if strings.ContainsAny(listing, "│┌└├") {
		t.Errorf("expected no table borders in agent mode, got:\n%s", listing)
	}
	if !strings.Contains(listing, "total_billed: ") {
		t.Errorf("expected flat key-value totals, got:\n%s", listing)
	}
}

func TestJobInspectReportsStoredCost(t *testing.T) {
	cacheDir := t.TempDir()
	seedJob(t, cacheDir, deepgram.JobRecord{
		RequestID:       "req-actual-123",
		Filename:        "actual.m4a",
		Timestamp:       time.Date(2026, 3, 31, 14, 0, 0, 0, time.UTC),
		DurationSeconds: 600,
		Channels:        1,
		Model:           "nova-3",
		CostUSD:         "$0.043",
		CostIsActual:    true,
	})

	root, out, _ := newTestCLI(t, cacheDir, "")
	root.SetArgs([]string{"job", "inspect", "req-actual-123"})

	if err := root.Execute(); err != nil {
		t.Fatalf("job inspect failed: %v", err)
	}

	details := out.String()
	if !strings.Contains(details, "req-actual-123") || !strings.Contains(details, "$0.043") {
		t.Errorf("expected the request ID and its cost, got:\n%s", details)
	}
}

func TestJobInspectFallsBackToTheEstimate(t *testing.T) {
	cacheDir := t.TempDir()
	server := newBillingServer(t, "proj-fallback", nil)

	seedJob(t, cacheDir, deepgram.JobRecord{
		RequestID:       "mock-req-fallback",
		Filename:        "interview.m4a",
		DurationSeconds: 60,
		CostUSD:         "$0.011",
	})

	root, out, _ := newTestCLI(t, cacheDir, server.URL)
	root.SetArgs([]string{"job", "inspect", "mock-req-fallback", "--api-key", "test-key"})

	if err := root.Execute(); err != nil {
		t.Fatalf("job inspect failed: %v", err)
	}

	details := out.String()
	if !strings.Contains(details, costPending) || !strings.Contains(details, "$0.011") {
		t.Errorf("expected the pending marker and the estimate, got:\n%s", details)
	}
}

func TestJobInspectSuggestsListingWhenNothingMatches(t *testing.T) {
	root, _, errOut := newTestCLI(t, t.TempDir(), "")
	root.SetArgs([]string{"job", "inspect", "unknown-request"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for an unknown job")
	}

	reportForTest(root, err)

	if !strings.Contains(errOut.String(), "job list") {
		t.Errorf("expected a hint pointing at job list, got: %s", errOut.String())
	}
}

func TestParseCostUSD(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  float64
	}{
		{"dollars", "$0.043", 0.043},
		{"bare number", "0.5", 0.5},
		{"padded", "  $1.250 ", 1.25},
		{"unreadable", "n/a", 0},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseCostUSD(tt.input); got != tt.want {
				t.Errorf("parseCostUSD(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
