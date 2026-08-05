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

// Client sends audio transcription requests to Deepgram API v1/listen using net/http.
type Client struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

// ClientOption configures Client.
type ClientOption func(*Client)

// WithEndpoint sets a custom endpoint URL (useful for testing).
func WithEndpoint(endpoint string) ClientOption {
	return func(c *Client) {
		c.endpoint = endpoint
	}
}

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.client = httpClient
	}
}

// NewClient returns a new Deepgram Client.
func NewClient(apiKey string, opts ...ClientOption) *Client {
	c := &Client{
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
func (c *Client) Transcribe(ctx context.Context, audioReader io.Reader, mimeType string, opts Options) (*PreRecordedResponse, error) {
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

// ProjectsResponse represents the response from GET /v1/projects.
type ProjectsResponse struct {
	Projects []Project `json:"projects"`
}

// Project represents metadata for a Deepgram project.
type Project struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
}

// ProjectRequestResponse represents the response from GET /v1/projects/{project_id}/requests/{request_id}.
type ProjectRequestResponse struct {
	RequestID   string `json:"request_id"`
	ProjectUUID string `json:"project_uuid"`
	Response    *struct {
		Details *struct {
			USD *float64 `json:"usd"`
		} `json:"details"`
	} `json:"response"`
}

func (c *Client) baseURL() string {
	u, err := url.Parse(c.endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "https://api.deepgram.com"
	}
	return u.Scheme + "://" + u.Host
}

// GetProjectID retrieves the primary project ID associated with the API key from GET /v1/projects.
func (c *Client) GetProjectID(ctx context.Context) (string, error) {
	if c.apiKey == "" {
		return "", errors.New("DEEPGRAM_API_KEY is not set or empty")
	}

	reqURL := c.baseURL() + "/v1/projects"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request for projects: %w", err)
	}
	req.Header.Set("Authorization", "Token "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("requesting projects from Deepgram: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return "", errors.New("API key lacks Member/Admin scope to list projects")
	}
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("deepgram API returned HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var projResp ProjectsResponse
	if err := json.NewDecoder(resp.Body).Decode(&projResp); err != nil {
		return "", fmt.Errorf("decoding projects response JSON: %w", err)
	}

	if len(projResp.Projects) == 0 {
		return "", errors.New("no projects found for API key")
	}

	return projResp.Projects[0].ProjectID, nil
}

// GetRequestCost queries Deepgram's Management API (GET /v1/projects/{project_id}/requests/{request_id}) to get the actual billed USD cost.
func (c *Client) GetRequestCost(ctx context.Context, requestID string) (float64, error) {
	if requestID == "" {
		return 0, errors.New("request ID is empty")
	}

	projectID, err := c.GetProjectID(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting project ID: %w", err)
	}

	reqURL := fmt.Sprintf("%s/v1/projects/%s/requests/%s", c.baseURL(), projectID, requestID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, fmt.Errorf("creating request for request details: %w", err)
	}
	req.Header.Set("Authorization", "Token "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("requesting request details from Deepgram: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return 0, errors.New("API key lacks Member/Admin scope to view request logs")
	}
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("deepgram API returned HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("reading request details body: %w", err)
	}

	if string(bodyBytes) == "null" || len(bodyBytes) == 0 {
		return 0, errors.New("request log details unavailable (request logging may be disabled under Project Settings in Deepgram Console or log processing is pending)")
	}

	var reqResp ProjectRequestResponse
	if err := json.Unmarshal(bodyBytes, &reqResp); err != nil {
		return 0, fmt.Errorf("decoding request details JSON: %w", err)
	}

	if reqResp.Response == nil || reqResp.Response.Details == nil || reqResp.Response.Details.USD == nil {
		return 0, errors.New("request details or cost USD field missing in Deepgram log response")
	}

	return *reqResp.Response.Details.USD, nil
}

// GetRequestCostFormatted queries Deepgram for actual billed cost and returns it formatted as "$X.XXX".
func (c *Client) GetRequestCostFormatted(ctx context.Context, requestID string) (string, error) {
	cost, err := c.GetRequestCost(ctx, requestID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("$%.3f", cost), nil
}
