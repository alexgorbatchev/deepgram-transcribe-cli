package deepgram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildURL(t *testing.T) {
	opts := Options{
		Model:           "nova-3",
		Language:        "en",
		Diarize:         true,
		SmartFormatting: true,
		Utterances:      true,
		Punctuate:       true,
		Terms:           []string{"Go", "Envoy", "Alex"},
	}

	urlStr := BuildURL("https://api.deepgram.com/v1/listen", opts)

	if !strings.Contains(urlStr, "model=nova-3") {
		t.Errorf("expected URL to contain model=nova-3, got %s", urlStr)
	}

	if !strings.Contains(urlStr, "diarize=true") {
		t.Errorf("expected URL to contain diarize=true, got %s", urlStr)
	}

	if !strings.Contains(urlStr, "smart_format=true") {
		t.Errorf("expected URL to contain smart_format=true, got %s", urlStr)
	}

	if !strings.Contains(urlStr, "keyterm=Go") || !strings.Contains(urlStr, "keyterm=Envoy") || !strings.Contains(urlStr, "keyterm=Alex") {
		t.Errorf("expected URL to contain keyterm parameters, got %s", urlStr)
	}
}

func TestBuildURLNova2Keywords(t *testing.T) {
	opts := Options{
		Model: "nova-2",
		Terms: []string{"Go", "Envoy"},
	}

	urlStr := BuildURL("https://api.deepgram.com/v1/listen", opts)

	if !strings.Contains(urlStr, "keywords=Go") || !strings.Contains(urlStr, "keywords=Envoy") {
		t.Errorf("expected URL for nova-2 to contain keywords parameters, got %s", urlStr)
	}
}

func TestTranscribeRequest(t *testing.T) {
	mockResponse := `{
		"metadata": {
			"request_id": "test-req-123",
			"duration": 10.0
		},
		"results": {
			"channels": [
				{
					"alternatives": [
						{"transcript": "Test transcript"}
					]
				}
			],
			"utterances": [
				{
					"start": 0.0,
					"end": 10.0,
					"speaker": 0,
					"transcript": "Test transcript"
				}
			]
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Token test-api-key" {
			t.Errorf("expected Authorization header 'Token test-api-key', got %q", authHeader)
		}

		contentType := r.Header.Get("Content-Type")
		if contentType != "audio/mpeg" {
			t.Errorf("expected Content-Type 'audio/mpeg', got %q", contentType)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		if string(body) != "dummy audio data" {
			t.Errorf("expected body 'dummy audio data', got %q", string(body))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	customHTTP := &http.Client{Timeout: 10 * time.Second}
	client := NewClient("test-api-key", WithEndpoint(server.URL), WithHTTPClient(customHTTP))

	audioData := strings.NewReader("dummy audio data")
	opts := Options{
		Model: "nova-3",
	}

	resp, err := client.Transcribe(context.Background(), audioData, "audio/mpeg", opts)
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}

	if resp.Metadata.RequestID != "test-req-123" {
		t.Errorf("expected request_id 'test-req-123', got %q", resp.Metadata.RequestID)
	}

	if len(resp.Results.Utterances) != 1 {
		t.Fatalf("expected 1 utterance, got %d", len(resp.Results.Utterances))
	}

	if resp.Results.Utterances[0].Transcript != "Test transcript" {
		t.Errorf("expected transcript 'Test transcript', got %q", resp.Results.Utterances[0].Transcript)
	}
}

func TestTranscribeHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"err_code":"INVALID_MODEL"}`))
	}))
	defer server.Close()

	client := NewClient("test-api-key", WithEndpoint(server.URL))
	_, err := client.Transcribe(context.Background(), strings.NewReader("data"), "audio/mpeg", Options{})
	if err == nil {
		t.Fatal("expected error on HTTP 400 response, got nil")
	}

	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("expected error to mention HTTP 400, got: %v", err)
	}
}

func TestTranscribeMissingAPIKey(t *testing.T) {
	client := NewClient("")
	audioData := strings.NewReader("data")

	_, err := client.Transcribe(context.Background(), audioData, "audio/mpeg", Options{})
	if err == nil {
		t.Fatal("expected error when API key is empty, got nil")
	}
}

func TestFormatSeconds(t *testing.T) {
	tests := []struct {
		secs float64
		want string
	}{
		{5.0, "00:05"},
		{65.0, "01:05"},
		{3665.0, "01:01:05"},
	}

	for _, tt := range tests {
		got := FormatSeconds(tt.secs)
		if got != tt.want {
			t.Errorf("FormatSeconds(%f) = %q, want %q", tt.secs, got, tt.want)
		}
	}
}

func TestFloatToString(t *testing.T) {
	got := FloatToString(12.3456, 2)
	if got != "12.35" {
		t.Errorf("FloatToString = %q, want 12.35", got)
	}
}
