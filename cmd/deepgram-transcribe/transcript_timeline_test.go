package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
)

// trimmedResponseJSON is what Deepgram returns for audio that kept 0-5 s and
// 40-60 s of a 60 s recording: its times are on the 25 s upload, with the
// joint at 5 s. "again" straddles the joint, mostly after it.
const trimmedResponseJSON = `{
	"metadata": {"request_id": "trimmed-req", "duration": 25, "channels": 1},
	"results": {
		"channels": [{"alternatives": [{
			"transcript": "Hello again world",
			"words": [
				{"word": "hello", "punctuated_word": "Hello", "start": 1.0, "end": 1.5, "confidence": 0.99, "speaker": 0},
				{"word": "again", "punctuated_word": "again", "start": 4.8, "end": 5.4, "confidence": 0.9, "speaker": 0},
				{"word": "world", "punctuated_word": "world.", "start": 6.0, "end": 6.5, "confidence": 0.95, "speaker": 1}
			]
		}]}],
		"utterances": [
			{"start": 1.0, "end": 5.4, "confidence": 0.95, "speaker": 0, "transcript": "Hello again"},
			{"start": 6.0, "end": 14.0, "confidence": 0.95, "speaker": 1, "transcript": "world."}
		]
	}
}`

// trimmedMedia reports a 60 s recording with silence from 5 s to 40 s.
func trimmedMedia(t *testing.T) *fakeMedia {
	return &fakeMedia{t: t, sourceDuration: "60", processedDuration: "25", silences: "lavfi.silence_start=5\nlavfi.silence_end=40\n"}
}

// countingServer answers every transcription with body and counts requests.
func countingServer(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("writing stub response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func runTrimmedTranscript(t *testing.T, cacheDir, endpoint, audioPath string, extra ...string) string {
	t.Helper()
	root, out, errOut := newTestCLIWithRunner(t, cacheDir, endpoint, trimmedMedia(t).run)
	args := append([]string{"transcript", "create", audioPath, "--api-key", "test-key", "--trim-mode", "silence", "--trim-padding", "0", "--no-mono"}, extra...)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("transcript create failed: %v\n%s", err, errOut)
	}
	return out.String()
}

func TestJSONTranscriptIsOnTheOriginalTimelineAndStableAcrossCacheHits(t *testing.T) {
	server, requests := countingServer(t, trimmedResponseJSON)
	cacheDir := t.TempDir()
	audioPath := writeAudio(t, t.TempDir(), "session.wav", "sixty seconds of audio")

	miss := runTrimmedTranscript(t, cacheDir, server.URL, audioPath, "--format", "json")
	hit := runTrimmedTranscript(t, cacheDir, server.URL, audioPath, "--format", "json")

	if got := requests.Load(); got != 1 {
		t.Fatalf("Deepgram was asked %d times, want once", got)
	}

	var missDoc, hitDoc map[string]any
	if err := json.Unmarshal([]byte(miss), &missDoc); err != nil {
		t.Fatalf("cache miss output is not JSON: %v\n%s", err, miss)
	}
	if err := json.Unmarshal([]byte(hit), &hitDoc); err != nil {
		t.Fatalf("cache hit output is not JSON: %v\n%s", err, hit)
	}
	if missDoc["cache_hit"] != false || hitDoc["cache_hit"] != true {
		t.Fatalf("cache_hit miss=%v hit=%v", missDoc["cache_hit"], hitDoc["cache_hit"])
	}
	delete(missDoc, "cache_hit")
	delete(hitDoc, "cache_hit")
	if !reflect.DeepEqual(missDoc, hitDoc) {
		t.Fatalf("cache hit changed the document:\nmiss: %s\nhit:  %s", miss, hit)
	}
	if strings.Replace(miss, `"cache_hit": false`, `"cache_hit": true`, 1) != hit {
		t.Fatalf("cache hit output differs beyond cache_hit:\nmiss: %s\nhit:  %s", miss, hit)
	}

	var doc struct {
		Schema              int     `json:"schema"`
		RequestID           string  `json:"request_id"`
		Duration            float64 `json:"duration"`
		TranscribedDuration float64 `json:"transcribed_duration"`
		KeptRanges          []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		} `json:"kept_ranges"`
		Words []struct {
			Start   float64 `json:"start"`
			End     float64 `json:"end"`
			Speaker *int    `json:"speaker"`
		} `json:"words"`
		Utterances []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		} `json:"utterances"`
	}
	if err := json.Unmarshal([]byte(hit), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != 1 || doc.RequestID != "trimmed-req" || doc.Duration != 60 || doc.TranscribedDuration != 25 {
		t.Fatalf("unexpected header fields: %+v", doc)
	}
	if len(doc.KeptRanges) != 2 || doc.KeptRanges[0].End != 5 || doc.KeptRanges[1].Start != 40 || doc.KeptRanges[1].End != 60 {
		t.Fatalf("kept_ranges = %+v", doc.KeptRanges)
	}
	wantWords := [][2]float64{{1, 1.5}, {40, 40.4}, {41, 41.5}}
	for i, want := range wantWords {
		if got := doc.Words[i]; got.Start != want[0] || got.End != want[1] || got.Speaker == nil {
			t.Errorf("word %d = %+v, want %v", i, got, want)
		}
	}
	if u := doc.Utterances[1]; u.Start != 41 || u.End != 49 {
		t.Errorf("second utterance = %+v, want 41-49", u)
	}
}

func TestMarkdownTimestampsAreOnTheOriginalTimeline(t *testing.T) {
	server, _ := countingServer(t, trimmedResponseJSON)
	audioPath := writeAudio(t, t.TempDir(), "session.wav", "sixty seconds of audio")

	got := runTrimmedTranscript(t, t.TempDir(), server.URL, audioPath)

	for _, want := range []string{
		"### Speaker 0 (00:01 - 00:40)",
		"### Speaker 1 (00:41 - 00:49)",
		"| **Duration** | 01:00 |",
		"| **Transcribed** | 00:25 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "(00:06 - 00:14)") {
		t.Errorf("timestamps are on the trimmed audio:\n%s", got)
	}
}

func TestTranscriptCreateRejectsInvalidTrimAndFormatFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown format", []string{"--format", "xml"}, `unknown format "xml"`},
		{"unknown trim mode", []string{"--trim-mode", "loud"}, `unknown trim mode "loud"`},
		{"vad flag with silence mode", []string{"--trim-mode", "silence", "--vad-threshold", "0.7"}, "--vad-threshold applies only with --trim-mode vad"},
		{"silence flag with vad mode", []string{"--silence-threshold", "-50dB"}, "--silence-threshold applies only with --trim-mode silence"},
		{"padding with no trimming", []string{"--trim-mode", "none", "--trim-padding", "1"}, "--trim-padding does not apply"},
		{"negative padding", []string{"--trim-padding", "-1"}, "trim padding -1 is negative"},
		{"threshold out of range", []string{"--vad-threshold", "1.5"}, "speech threshold 1.5 is outside (0, 1)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			audioPath := writeAudio(t, t.TempDir(), "session.wav", "audio")
			root, out, _ := newTestCLI(t, t.TempDir(), "")
			root.SetArgs(append([]string{"transcript", "create", audioPath}, tc.args...))
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
			if out.Len() != 0 {
				t.Fatalf("output written despite invalid flags: %s", out)
			}
		})
	}
}

func TestCachedResponseWithoutTimelineWarnsWhenItWasPreprocessed(t *testing.T) {
	for _, preprocessed := range []bool{true, false} {
		cacheDir := t.TempDir()
		audioBytes := "recording cached without a timeline"
		audioPath := writeAudio(t, t.TempDir(), "session.wav", audioBytes)
		envelope := deepgram.JobRecordEnvelope{
			Record: deepgram.JobRecord{
				SHA256: "untimed-key", SourceSHA256: deepgram.SourceAudioKey([]byte(audioBytes)), Preprocessed: preprocessed,
			},
			Response: &deepgram.PreRecordedResponse{Results: deepgram.Results{Utterances: []deepgram.Utterance{{Transcript: "cached words"}}}},
		}
		if err := deepgram.SaveJobEnvelope(cacheDir, "untimed-key", envelope); err != nil {
			t.Fatal(err)
		}

		root, out, errOut := newTestCLI(t, cacheDir, "")
		root.SetArgs([]string{"transcript", "create", audioPath})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		warned := strings.Contains(errOut.String(), "may be on the trimmed audio")
		if warned != preprocessed || !strings.Contains(out.String(), "cached words") {
			t.Fatalf("preprocessed %t: warned %t\n%s", preprocessed, warned, errOut)
		}
	}
}
