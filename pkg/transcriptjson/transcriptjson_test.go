package transcriptjson

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/timeline"
)

var sampleResponse = deepgram.PreRecordedResponse{
	Metadata: deepgram.Metadata{RequestID: "req-1", Duration: 4.5},
	Results: deepgram.Results{
		Channels: []deepgram.Channel{{Alternatives: []deepgram.Alternative{{
			Transcript: "hello there",
			Words: []deepgram.Word{
				{Word: "hello", PunctuatedWord: "Hello", Start: 10.5, End: 11, Confidence: 0.9, Speaker: 1},
				{Word: "there", PunctuatedWord: "there.", Start: 20, End: 20.4, Confidence: 0.8, Speaker: 1},
			},
		}}}},
		Utterances: []deepgram.Utterance{{Start: 10.5, End: 20.4, Confidence: 0.85, Speaker: 1, Transcript: "Hello there."}},
	},
}

func TestFormatWritesTheDocumentedFields(t *testing.T) {
	cost := 0.0123
	tl := timeline.Timeline{SourceDuration: 30, Kept: []timeline.Range{{Start: 10, End: 12}, {Start: 20, End: 22.5}}}
	data, err := Format(sampleResponse, Meta{
		Filename: "talk.m4a", FileSize: 2048, Model: "nova-3", Language: "en",
		Diarized: true, CacheHit: true, CostUSD: &cost, Timeline: tl,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "}\n") {
		t.Fatalf("document does not end with a newline: %q", data)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"schema": 1.0, "request_id": "req-1", "model": "nova-3", "language": "en",
		"diarized": true, "duration": 30.0, "transcribed_duration": 4.5,
		"cache_hit": true, "cost_estimate_usd": 0.0123, "transcript": "hello there",
	}
	for key, value := range want {
		if doc[key] != value {
			t.Errorf("%s = %v, want %v", key, doc[key], value)
		}
	}
	source := doc["source"].(map[string]any)
	if source["filename"] != "talk.m4a" || source["size_bytes"] != 2048.0 {
		t.Errorf("source = %v", source)
	}
	kept := doc["kept_ranges"].([]any)
	if len(kept) != 2 || kept[1].(map[string]any)["end"] != 22.5 {
		t.Errorf("kept_ranges = %v", kept)
	}
	word := doc["words"].([]any)[0].(map[string]any)
	wantWord := map[string]any{"word": "hello", "punctuated_word": "Hello", "start": 10.5, "end": 11.0, "confidence": 0.9, "speaker": 1.0}
	for key, value := range wantWord {
		if word[key] != value {
			t.Errorf("word %s = %v, want %v", key, word[key], value)
		}
	}
	utterance := doc["utterances"].([]any)[0].(map[string]any)
	if utterance["start"] != 10.5 || utterance["end"] != 20.4 || utterance["speaker"] != 1.0 || utterance["transcript"] != "Hello there." {
		t.Errorf("utterance = %v", utterance)
	}
}

func TestFormatWithoutTrimmingDiarizationOrCost(t *testing.T) {
	data, err := Format(sampleResponse, Meta{Filename: "talk.m4a"})
	if err != nil {
		t.Fatal(err)
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Duration != 4.5 || doc.KeptRanges != nil || doc.CostEstimateUSD != nil || doc.Words[0].Speaker != nil || doc.Utterances[0].Speaker != nil {
		t.Fatalf("unexpected document %+v", doc)
	}
	for _, null := range []string{`"kept_ranges": null`, `"cost_estimate_usd": null`, `"speaker": null`} {
		if !strings.Contains(string(data), null) {
			t.Errorf("missing %s in %s", null, data)
		}
	}
}

func TestFormatEmptyResponseHasEmptyLists(t *testing.T) {
	data, err := Format(deepgram.PreRecordedResponse{}, Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"words": []`) || !strings.Contains(string(data), `"utterances": []`) {
		t.Fatalf("empty lists not written as arrays: %s", data)
	}
}

func TestFormatRejectsUnencodableValues(t *testing.T) {
	_, err := Format(deepgram.PreRecordedResponse{Metadata: deepgram.Metadata{Duration: math.NaN()}}, Meta{})
	if err == nil || !strings.Contains(err.Error(), "encoding the JSON transcript") {
		t.Fatalf("got %v", err)
	}
}
