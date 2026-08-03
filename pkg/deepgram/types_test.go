package deepgram

import (
	"encoding/json"
	"testing"
)

func TestPreRecordedResponseUnmarshal(t *testing.T) {
	jsonResp := `{
		"metadata": {
			"transaction_key": "test-key",
			"request_id": "12345-abcde",
			"sha256": "abcdef1234567890",
			"created": "2026-03-31T12:00:00Z",
			"duration": 45.5,
			"channels": 1,
			"models": ["123-abc-nova-3"]
		},
		"results": {
			"channels": [
				{
					"alternatives": [
						{
							"transcript": "Hello, welcome to Envoy.",
							"confidence": 0.98,
							"words": [
								{
									"word": "Hello",
									"start": 0.5,
									"end": 0.9,
									"confidence": 0.99,
									"speaker": 0,
									"punctuated_word": "Hello,"
								},
								{
									"word": "welcome",
									"start": 1.0,
									"end": 1.4,
									"confidence": 0.98,
									"speaker": 0,
									"punctuated_word": "welcome"
								}
							]
						}
					]
				}
			],
			"utterances": [
				{
					"start": 0.5,
					"end": 2.1,
					"confidence": 0.98,
					"channel": 0,
					"transcript": "Hello, welcome to Envoy.",
					"speaker": 0,
					"words": [
						{
							"word": "Hello",
							"start": 0.5,
							"end": 0.9,
							"confidence": 0.99,
							"speaker": 0,
							"punctuated_word": "Hello,"
						}
					]
				},
				{
					"start": 2.5,
					"end": 5.2,
					"confidence": 0.96,
					"channel": 0,
					"transcript": "Thanks! I'm excited to speak with you today.",
					"speaker": 1,
					"words": []
				}
			]
		}
	}`

	var resp PreRecordedResponse
	err := json.Unmarshal([]byte(jsonResp), &resp)
	if err != nil {
		t.Fatalf("failed to unmarshal PreRecordedResponse: %v", err)
	}

	if resp.Metadata.RequestID != "12345-abcde" {
		t.Errorf("expected RequestID '12345-abcde', got %q", resp.Metadata.RequestID)
	}

	if resp.Metadata.Duration != 45.5 {
		t.Errorf("expected Duration 45.5, got %f", resp.Metadata.Duration)
	}

	if len(resp.Results.Utterances) != 2 {
		t.Fatalf("expected 2 utterances, got %d", len(resp.Results.Utterances))
	}

	if resp.Results.Utterances[0].Speaker != 0 {
		t.Errorf("utterance 0 speaker = %d, want 0", resp.Results.Utterances[0].Speaker)
	}

	if resp.Results.Utterances[1].Speaker != 1 {
		t.Errorf("utterance 1 speaker = %d, want 1", resp.Results.Utterances[1].Speaker)
	}
}
