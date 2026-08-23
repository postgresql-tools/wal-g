// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package neon

import (
	"bytes"
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

// DefaultEndpoint is the Neon control plane.
const DefaultEndpoint = "https://console.neon.tech/api/v2"

// Defaults for the knobs a caller usually does not want to think about.
const (
	DefaultTimeout          = 30 * time.Second
	DefaultOperationTimeout = 5 * time.Minute
	DefaultPollInterval     = 2 * time.Second
)

// maxErrorBodyBytes bounds how much of a failed response is read into an error
// message, so a control plane returning an HTML error page cannot flood a report.
const maxErrorBodyBytes = 4096

// Config configures a Client.
type Config struct {
	// APIKey authenticates every request. It is secret: it is sent in a header,
	// never placed in a URL, and never included in an error.
	APIKey string
	// ProjectID is the Neon project operated on.
	ProjectID string
	// Endpoint overrides the control plane URL. Empty means DefaultEndpoint.
	Endpoint string
	// Timeout bounds a single HTTP request. Zero means DefaultTimeout.
	Timeout time.Duration
	// OperationTimeout bounds waiting for asynchronous operations to finish.
	// Zero means DefaultOperationTimeout.
	OperationTimeout time.Duration
	// PollInterval is how often operations are polled. Zero means
	// DefaultPollInterval.
	PollInterval time.Duration
	// HTTPClient overrides the HTTP client, for tests.
	HTTPClient *http.Client
}

// Client is a Neon control plane client.
//
// The zero value is not usable; construct one with NewClient.
type Client struct {
	apiKey           string
	projectID        string
	endpoint         string
	operationTimeout time.Duration
	pollInterval     time.Duration
	httpClient       *http.Client
}

// APIError is a non-2xx response from the control plane.
//
// It carries the status and whatever message the body held. It never carries
// the API key: the key travels in a header that is not echoed back here.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("neon: %s %s returned %d %s",
			e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode))
	}

	return fmt.Sprintf("neon: %s %s returned %d: %s", e.Method, e.Path, e.StatusCode, e.Message)
}

// IsNotFound reports whether the error is a 404, which callers treat as "already
// gone" when deleting.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}

	return apiErr.StatusCode == http.StatusNotFound
}

// NewClient validates a config and builds a client.
func NewClient(config Config) (*Client, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("neon: no API key: set %s", "WALG_NEON_API_KEY")
	}

	if strings.TrimSpace(config.ProjectID) == "" {
		return nil, fmt.Errorf("neon: no project: set %s", "WALG_NEON_PROJECT_ID")
	}

	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	endpoint = strings.TrimRight(endpoint, "/")

	if _, err := url.Parse(endpoint); err != nil {
		return nil, fmt.Errorf("neon: invalid endpoint %q: %w", endpoint, err)
	}

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}

	operationTimeout := config.OperationTimeout
	if operationTimeout <= 0 {
		operationTimeout = DefaultOperationTimeout
	}

	pollInterval := config.PollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}

	return &Client{
		apiKey:           config.APIKey,
		projectID:        config.ProjectID,
		endpoint:         endpoint,
		operationTimeout: operationTimeout,
		pollInterval:     pollInterval,
		httpClient:       httpClient,
	}, nil
}

// ProjectID reports the project this client operates on.
func (c *Client) ProjectID() string {
	return c.projectID
}

// Ping checks that the credentials work and the project exists, so a drill can
// fail before it restores several hundred gigabytes.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.ListBranches(ctx)

	return err
}

// CreateBranch creates a branch with a read-write endpoint and waits for it to
// become usable.
//
// parentID may be empty, which forks the project's default branch.
func (c *Client) CreateBranch(ctx context.Context, name, parentID string) (Branch, error) {
	body := createBranchRequest{
		Branch:    createBranchSpec{Name: name, ParentID: parentID},
		Endpoints: []createEndpointSpec{{Type: EndpointTypeReadWrite}},
	}

	var response branchResponse

	err := c.do(ctx, http.MethodPost, c.projectPath("/branches"), body, &response)
	if err != nil {
		return Branch{}, err
	}

	// The branch exists but its endpoint may still be provisioning. Returning
	// here would hand back a branch that refuses connections.
	if err := c.WaitForOperations(ctx, response.Operations); err != nil {
		return response.Branch, err
	}

	return response.Branch, nil
}

// ListBranches returns every branch in the project.
func (c *Client) ListBranches(ctx context.Context) ([]Branch, error) {
	var response listBranchesResponse

	if err := c.do(ctx, http.MethodGet, c.projectPath("/branches"), nil, &response); err != nil {
		return nil, err
	}

	return response.Branches, nil
}

// ListDrillBranches returns only the branches wal-g created.
func (c *Client) ListDrillBranches(ctx context.Context) ([]Branch, error) {
	branches, err := c.ListBranches(ctx)
	if err != nil {
		return nil, err
	}

	drills := make([]Branch, 0, len(branches))

	for _, branch := range branches {
		if branch.IsDrillBranch() {
			drills = append(drills, branch)
		}
	}

	return drills, nil
}

// DeleteBranch removes a branch. A branch that is already gone is not an error:
// cleanup runs on paths where the branch may never have been created.
//
// It refuses to delete a branch wal-g did not create. Cleanup runs from defers
// and signal handlers, and a bug there must not be able to destroy a user's
// data.
func (c *Client) DeleteBranch(ctx context.Context, branch Branch) error {
	if !branch.IsDrillBranch() {
		return fmt.Errorf("neon: refusing to delete branch %q: not a %s branch",
			branch.Name, DrillBranchPrefix)
	}

	if branch.Default || branch.Protected {
		return fmt.Errorf("neon: refusing to delete branch %q: it is the default or protected branch",
			branch.Name)
	}

	path := c.projectPath("/branches/" + url.PathEscape(branch.ID))

	var response branchResponse

	err := c.do(ctx, http.MethodDelete, path, nil, &response)
	if err != nil {
		if IsNotFound(err) {
			return nil
		}

		return err
	}

	return nil
}

// ConnectionURI returns a libpq URI for a branch.
//
// The URI embeds a password. It must not be logged, written to a report, or
// passed as a command-line argument, since argv is readable by any process on
// the host. Put it in a subprocess environment instead.
func (c *Client) ConnectionURI(ctx context.Context, branchID, databaseName, roleName string) (string, error) {
	query := url.Values{}
	query.Set("branch_id", branchID)
	query.Set("database_name", databaseName)
	query.Set("role_name", roleName)

	path := c.projectPath("/connection_uri") + "?" + query.Encode()

	var response connectionURIResponse

	if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		return "", err
	}

	if response.URI == "" {
		return "", fmt.Errorf("neon: control plane returned an empty connection URI for branch %s", branchID)
	}

	return response.URI, nil
}

// WaitForOperations blocks until every operation finishes, fails, or the
// operation timeout expires.
func (c *Client) WaitForOperations(ctx context.Context, operations []Operation) error {
	if len(operations) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, c.operationTimeout)
	defer cancel()

	for _, operation := range operations {
		if err := c.waitForOperation(ctx, operation); err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) waitForOperation(ctx context.Context, operation Operation) error {
	if operation.Done() {
		return c.operationOutcome(operation)
	}

	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	path := c.projectPath("/operations/" + url.PathEscape(operation.ID))

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("neon: gave up waiting for operation %s (%s) after %s: %w",
				operation.ID, operation.Action, c.operationTimeout, ctx.Err())
		case <-ticker.C:
		}

		var response operationResponse

		if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
			return err
		}

		if response.Operation.Done() {
			return c.operationOutcome(response.Operation)
		}
	}
}

// operationOutcome turns a finished operation into an error or nil. The
// operation's own error text came from the control plane, so it is scrubbed for
// the same reason an API error message is.
func (c *Client) operationOutcome(operation Operation) error {
	if operation.Succeeded() {
		return nil
	}

	if operation.Error != "" {
		return fmt.Errorf("neon: operation %s (%s) %s: %s",
			operation.ID, operation.Action, operation.Status, c.scrub(operation.Error))
	}

	return fmt.Errorf("neon: operation %s (%s) %s", operation.ID, operation.Action, operation.Status)
}

func (c *Client) projectPath(suffix string) string {
	return "/projects/" + url.PathEscape(c.projectID) + suffix
}

// do performs one request and decodes the response.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload io.Reader

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("neon: could not encode the %s %s request: %w", method, path, err)
		}

		payload = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, payload)
	if err != nil {
		return fmt.Errorf("neon: could not build the %s %s request: %w", method, path, err)
	}

	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Accept", "application/json")

	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		// url.Error stringifies the request URL, which never holds the key.
		return fmt.Errorf("neon: %s %s failed: %w", method, path, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return c.responseError(method, path, response)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("neon: could not decode the %s %s response: %w", method, path, err)
	}

	return nil
}

// responseError builds an APIError, extracting the control plane's own message
// when it sent one.
func (c *Client) responseError(method, path string, response *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))

	apiErr := &APIError{
		StatusCode: response.StatusCode,
		Method:     method,
		Path:       path,
		// Scrubbed, not trusted: the body is written by the far end, and an
		// endpoint that echoes the Authorization header back would otherwise
		// put the key straight into a CI log.
		Message: c.scrub(extractMessage(raw)),
	}

	if response.StatusCode == http.StatusTooManyRequests {
		if retry := response.Header.Get("Retry-After"); retry != "" {
			if seconds, err := strconv.Atoi(retry); err == nil {
				apiErr.Message = strings.TrimSpace(fmt.Sprintf("%s (retry after %ds)", apiErr.Message, seconds))
			}
		}
	}

	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		apiErr.Message = strings.TrimSpace(apiErr.Message +
			"; check WALG_NEON_API_KEY and that it has access to WALG_NEON_PROJECT_ID")
	}

	return apiErr
}

// redactedAPIKey replaces the key wherever it would otherwise be rendered.
const redactedAPIKey = "--HIDDEN--"

// scrub removes the API key from text that came back from the control plane.
//
// Nothing should echo the Authorization header, but an error path is the wrong
// place to rely on that: the message ends up in CI logs and JSON drill reports,
// and a leaked key grants full control of the project.
func (c *Client) scrub(text string) string {
	if c.apiKey == "" {
		return text
	}

	return strings.ReplaceAll(text, c.apiKey, redactedAPIKey)
}

// extractMessage pulls Neon's error message out of a body, falling back to the
// raw text when it is not the JSON shape we expect.
func extractMessage(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}

	var envelope struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}

	if err := json.Unmarshal(raw, &envelope); err == nil {
		if envelope.Message != "" {
			return envelope.Message
		}

		if envelope.Error != "" {
			return envelope.Error
		}
	}

	return strings.TrimSpace(string(raw))
}
