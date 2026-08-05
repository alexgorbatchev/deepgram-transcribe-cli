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

func TestGetProjectIDAndRequestCost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/v1/projects":
			if r.Header.Get("Authorization") != "Token test-api-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"projects":[{"project_id":"proj-456","name":"Test Project"}]}`))

		case "/v1/projects/proj-456/requests/req-789":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"request_id": "req-789",
				"project_uuid": "proj-456",
				"response": {
					"details": {
						"usd": 0.09633
					}
				}
			}`))

		case "/v1/projects/proj-456/requests/req-null":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`null`))

		case "/v1/projects/proj-456/requests/req-forbidden":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"err_code":"FORBIDDEN"}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient("test-api-key", WithEndpoint(server.URL))

	// Test GetProjectID
	projID, err := client.GetProjectID(context.Background())
	if err != nil {
		t.Fatalf("GetProjectID failed: %v", err)
	}
	if projID != "proj-456" {
		t.Errorf("expected project ID 'proj-456', got %q", projID)
	}

	// Test GetRequestCost (success)
	costFloat, err := client.GetRequestCost(context.Background(), "req-789")
	if err != nil {
		t.Fatalf("GetRequestCost failed: %v", err)
	}
	if costFloat != 0.09633 {
		t.Errorf("expected cost 0.09633, got %f", costFloat)
	}

	// Test GetRequestCostFormatted
	costFormatted, err := client.GetRequestCostFormatted(context.Background(), "req-789")
	if err != nil {
		t.Fatalf("GetRequestCostFormatted failed: %v", err)
	}
	if costFormatted != "$0.096" {
		t.Errorf("expected cost formatted '$0.096', got %q", costFormatted)
	}

	// Test null response (log details unavailable)
	_, err = client.GetRequestCost(context.Background(), "req-null")
	if err == nil {
		t.Fatal("expected error on null request details, got nil")
	}
	if !strings.Contains(err.Error(), "request log details unavailable") {
		t.Errorf("expected error message to mention log details unavailable, got: %v", err)
	}

	// Test 403 Forbidden
	_, err = client.GetRequestCost(context.Background(), "req-forbidden")
	if err == nil {
		t.Fatal("expected error on 403 forbidden, got nil")
	}
	if !strings.Contains(err.Error(), "Member/Admin scope") {
		t.Errorf("expected error message to mention Member/Admin scope, got: %v", err)
	}
}

func TestGetRequestCostEmptyIDOrKey(t *testing.T) {
	clientNoKey := NewClient("")
	_, err := clientNoKey.GetRequestCost(context.Background(), "req-123")
	if err == nil {
		t.Fatal("expected error when API key is missing, got nil")
	}

	clientWithKey := NewClient("some-key")
	_, err = clientWithKey.GetRequestCost(context.Background(), "")
	if err == nil {
		t.Fatal("expected error when request ID is empty, got nil")
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
