package deepgram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEndpoint = "https://api.deepgram.com/v1/listen"
	defaultTimeout  = 5 * time.Minute
)

// Client is the interface for Deepgram speech-to-text operations.
type Client interface {
	Transcribe(ctx context.Context, audioReader io.Reader, mimeType string, opts Options) (*PreRecordedResponse, error)
}

// HTTPClient is the concrete implementation of Client using net/http.
type HTTPClient struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

// Option configures HTTPClient.
type ClientOption func(*HTTPClient)

// WithEndpoint sets a custom endpoint URL (useful for testing).
func WithEndpoint(endpoint string) ClientOption {
	return func(c *HTTPClient) {
		c.endpoint = endpoint
	}
}

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *HTTPClient) {
		c.client = httpClient
	}
}

// NewClient returns a new Deepgram Client.
func NewClient(apiKey string, opts ...ClientOption) *HTTPClient {
	c := &HTTPClient{
		apiKey:   apiKey,
		endpoint: defaultEndpoint,
		client: &http.Client{
			Timeout: defaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// BuildURL constructs the Deepgram API request URL with query parameters based on Options.
func BuildURL(baseEndpoint string, opts Options) string {
	u, err := url.Parse(baseEndpoint)
	if err != nil {
		return baseEndpoint
	}

	q := u.Query()

	model := opts.Model
	if model == "" {
		model = "nova-3"
	}
	q.Set("model", model)

	if opts.Language != "" {
		q.Set("language", opts.Language)
	}

	if opts.Diarize {
		q.Set("diarize", "true")
	}

	if opts.SmartFormatting {
		q.Set("smart_format", "true")
	}

	if opts.Utterances {
		q.Set("utterances", "true")
	}

	if opts.Punctuate {
		q.Set("punctuate", "true")
	}

	// Term/Keyword boosting:
	// Nova-3 and Flux use `keyterm` parameter.
	// Nova-2, Nova-1, and Base use `keywords` parameter.
	isNova3OrFlux := strings.HasPrefix(model, "nova-3") || strings.HasPrefix(model, "flux")

	for _, term := range opts.Terms {
		cleaned := strings.TrimSpace(term)
		if cleaned == "" {
			continue
		}
		if isNova3OrFlux {
			q.Add("keyterm", cleaned)
		} else {
			q.Add("keywords", cleaned)
		}
	}

	u.RawQuery = q.Encode()
	return u.String()
}

// Transcribe sends the audio stream to Deepgram and parses the response.
func (c *HTTPClient) Transcribe(ctx context.Context, audioReader io.Reader, mimeType string, opts Options) (*PreRecordedResponse, error) {
	if c.apiKey == "" {
		return nil, errors.New("DEEPGRAM_API_KEY is not set or empty")
	}

	reqURL := BuildURL(c.endpoint, opts)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, audioReader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+c.apiKey)
	if mimeType != "" {
		req.Header.Set("Content-Type", mimeType)
	} else {
		req.Header.Set("Content-Type", "application/octet-stream")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request to Deepgram: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("deepgram API returned HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result PreRecordedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response JSON: %w", err)
	}

	return &result, nil
}

// FormatSeconds formats float seconds into HH:MM:SS or MM:SS format.
func FormatSeconds(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// FloatToString formats float to string with given decimals.
func FloatToString(val float64, decimals int) string {
	return strconv.FormatFloat(val, 'f', decimals, 64)
}
