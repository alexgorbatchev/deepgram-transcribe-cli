package markdown

import (
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
)

func TestFormatMarkdownWithUtterances(t *testing.T) {
	resp := &deepgram.PreRecordedResponse{
		Metadata: deepgram.Metadata{
			RequestID: "req-123",
			Duration:  125.4,
		},
		Results: deepgram.Results{
			Utterances: []deepgram.Utterance{
				{
					Start:      0.0,
					End:        12.3,
					Speaker:    0,
					Transcript: "Hello Alex, thanks for joining the call today.",
				},
				{
					Start:      13.1,
					End:        25.0,
					Speaker:    1,
					Transcript: "Thanks for having me! Excited to talk about Envoy and Go.",
				},
				{
					Start:      25.5,
					End:        35.2,
					Speaker:    1,
					Transcript: "I've been building distributed microservices with Kubernetes.",
				},
				{
					Start:      36.0,
					End:        48.0,
					Speaker:    0,
					Transcript: "That sounds great. Tell me more about your architecture.",
				},
			},
		},
	}

	meta := MetaInfo{
		Filename:  "interview-call.m4a",
		FileSize:  "12.4 MB",
		Model:     "nova-3",
		Diarized:  true,
		KeyTerms:  []string{"Go", "Kubernetes", "Envoy", "Alex"},
		Timestamp: time.Date(2026, 3, 31, 14, 0, 0, 0, time.UTC),
	}

	md := Format(resp, meta)

	// Verify Header
	if !strings.Contains(md, "# Transcript: interview-call.m4a") {
		t.Errorf("missing or incorrect title in markdown:\n%s", md)
	}

	// Verify Metadata fields
	if !strings.Contains(md, "02:05") { // 125.4 seconds formatted
		t.Errorf("missing expected formatted duration 02:05 in markdown:\n%s", md)
	}

	if !strings.Contains(md, "nova-3") {
		t.Errorf("missing model info in markdown:\n%s", md)
	}

	if !strings.Contains(md, "Envoy") || !strings.Contains(md, "Kubernetes") {
		t.Errorf("missing key terms in markdown:\n%s", md)
	}

	// Verify Speakers
	if !strings.Contains(md, "### Speaker 0") || !strings.Contains(md, "### Speaker 1") {
		t.Errorf("missing speaker section headers in markdown:\n%s", md)
	}

	// Verify speaker consecutive merging
	// Speaker 1 had two consecutive utterances (13.1-25.0 and 25.5-35.2), should merge or format together cleanly
	if !strings.Contains(md, "Excited to talk about Envoy and Go.") {
		t.Errorf("missing transcript content in markdown:\n%s", md)
	}
}

func TestFormatMarkdownFallback(t *testing.T) {
	resp := &deepgram.PreRecordedResponse{
		Metadata: deepgram.Metadata{
			Duration: 10.0,
		},
		Results: deepgram.Results{
			Channels: []deepgram.Channel{
				{
					Alternatives: []deepgram.Alternative{
						{
							Transcript: "This is a fallback transcript when no utterances are present.",
						},
					},
				},
			},
		},
	}

	meta := MetaInfo{
		Filename: "single.mp3",
		Model:    "nova-3",
	}

	md := Format(resp, meta)

	if !strings.Contains(md, "This is a fallback transcript when no utterances are present.") {
		t.Errorf("fallback transcript missing in markdown:\n%s", md)
	}
}
