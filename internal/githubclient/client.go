package githubclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var hostnamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func NormalizeHost(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "github.com", nil
	}
	if !strings.Contains(input, "://") {
		input = "https://" + input
	}
	u, err := url.Parse(input)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Enter a GitHub hostname or HTTPS Enterprise URL without credentials, query, or fragment")
	}
	if u.Path != "" && u.Path != "/" && strings.TrimSuffix(u.Path, "/") != "/api/v3" && strings.TrimSuffix(u.Path, "/") != "/api/graphql" && strings.TrimSuffix(u.Path, "/") != "/graphql" {
		return "", errors.New("GitHub host URLs must not include a repository path")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if !hostnamePattern.MatchString(host) || strings.Contains(host, "..") {
		return "", errors.New("Invalid GitHub hostname")
	}
	if host == "api.github.com" {
		host = "github.com"
	}
	if strings.HasPrefix(host, "api.") && strings.HasSuffix(host, ".ghe.com") {
		host = strings.TrimPrefix(host, "api.")
	}
	if len(host) > 253 {
		return "", errors.New("GitHub hostname is too long")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", errors.New("Invalid GitHub hostname label")
		}
	}
	if port := u.Port(); port != "" && port != "443" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", errors.New("Invalid GitHub host port")
		}
		host += ":" + port
	}
	return host, nil
}

type cacheEntry struct {
	etag    string
	body    []byte
	headers http.Header
}
type Client struct {
	host, base, graphql, token string
	http                       *http.Client
	mu                         sync.Mutex
	cache                      map[string]cacheEntry
	blockedUntil               time.Time
}

func New(host, token string) (*Client, error) {
	host, err := NormalizeHost(host)
	if err != nil {
		return nil, err
	}
	base, graphql := "https://"+host+"/api/v3", "https://"+host+"/api/graphql"
	if host == "github.com" {
		base, graphql = "https://api.github.com", "https://api.github.com/graphql"
	} else if strings.HasSuffix(strings.Split(host, ":")[0], ".ghe.com") {
		base = "https://api." + host
		graphql = base + "/graphql"
	}
	return &Client{host: host, base: base, graphql: graphql, token: strings.TrimSpace(token), http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, cache: map[string]cacheEntry{}}, nil
}

// NewWithHTTPClient supports trusted transport injection without changing host validation.
func NewWithHTTPClient(host, token string, transport *http.Client) (*Client, error) {
	client, err := New(host, token)
	if err != nil {
		return nil, err
	}
	if transport != nil {
		client.http = transport
	}
	return client, nil
}

type APIError struct {
	Status  int
	Message string
	RetryAt string
}

func (e *APIError) Error() string {
	message := fmt.Sprintf("GitHub (%d): %s", e.Status, e.Message)
	if e.RetryAt != "" {
		message += "; retry after " + e.RetryAt
	}
	return message
}
func permissionError(err error) bool {
	var api *APIError
	return errors.As(err, &api) && (api.Status == 403 || api.Status == 404)
}

func (c *Client) request(ctx context.Context, method, path string, body, output any) (http.Header, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	blockedUntil := c.blockedUntil
	c.mu.Unlock()
	if time.Now().Before(blockedUntil) {
		return nil, &APIError{Status: 429, Message: "GitHub rate limit is cooling down", RetryAt: blockedUntil.UTC().Format(time.RFC3339)}
	}
	endpoint := c.base + path
	if path == "@graphql" {
		endpoint = c.graphql
	}
	var content io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		content = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, content)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "M.I.E.R./0.1")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.mu.Lock()
	cached := c.cache[endpoint]
	c.mu.Unlock()
	if method == http.MethodGet && cached.etag != "" {
		req.Header.Set("If-None-Match", cached.etag)
	}
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Unable to reach GitHub host %s: %w", c.host, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 32*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 32*1024*1024 {
		return response.Header, errors.New("GitHub response exceeded 32 MiB; narrow the request")
	}
	if response.StatusCode == http.StatusNotModified && cached.etag != "" {
		data = cached.body
		mergedHeaders := cached.headers.Clone()
		if mergedHeaders == nil {
			mergedHeaders = make(http.Header)
		}
		for name, values := range response.Header {
			mergedHeaders[name] = append([]string(nil), values...)
		}
		response.Header = mergedHeaders
	} else if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.Header, c.responseError(response, data)
	}
	if method == http.MethodGet && response.StatusCode == http.StatusOK {
		if etag := response.Header.Get("ETag"); etag != "" {
			c.mu.Lock()
			if len(c.cache) >= 512 {
				c.cache = map[string]cacheEntry{}
			}
			c.cache[endpoint] = cacheEntry{etag: etag, body: append([]byte(nil), data...), headers: response.Header.Clone()}
			c.mu.Unlock()
		}
	}
	if method != http.MethodGet && path != "@graphql" {
		c.invalidate()
	}
	if output != nil && len(data) > 0 {
		if err := json.Unmarshal(data, output); err != nil {
			return response.Header, fmt.Errorf("Invalid GitHub response: %w", err)
		}
	}
	return response.Header, nil
}

func (c *Client) responseError(response *http.Response, data []byte) error {
	var wire struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &wire)
	message := wire.Message
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	if c.token != "" {
		message = strings.ReplaceAll(message, c.token, "[redacted]")
	}
	switch response.StatusCode {
	case 401:
		message += ". Sign in again with gh auth login or replace the saved token"
	case 403:
		message += ". Check repository access, SSO authorization, and token permissions (Pull requests, Issues and Checks; Contents write for merging)"
	case 404:
		message += ". Verify the repository and that this account can access it"
	case 409:
		message += ". The pull request head changed; refresh before merging"
	case 422:
		message += ". Check branch names, diff line locations, and review state"
	}
	apiError := &APIError{Status: response.StatusCode, Message: message}
	if seconds, _ := strconv.Atoi(response.Header.Get("Retry-After")); seconds > 0 {
		apiError.RetryAt = time.Now().Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339)
	} else if response.Header.Get("X-RateLimit-Remaining") == "0" {
		if reset, err := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			apiError.RetryAt = time.Unix(reset, 0).UTC().Format(time.RFC3339)
		}
	}
	if apiError.RetryAt != "" {
		if until, err := time.Parse(time.RFC3339, apiError.RetryAt); err == nil {
			c.mu.Lock()
			if until.After(c.blockedUntil) {
				c.blockedUntil = until
			}
			c.mu.Unlock()
		}
	}
	return apiError
}

func (c *Client) invalidate() { c.mu.Lock(); c.cache = map[string]cacheEntry{}; c.mu.Unlock() }
func repoPath(owner, repo string) (string, error) {
	if !repositoryPattern.MatchString(owner) || !repositoryPattern.MatchString(repo) || owner == "." || owner == ".." || repo == "." || repo == ".." {
		return "", errors.New("Choose a valid GitHub repository owner and name")
	}
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo), nil
}
func prPath(owner, repo string, number int) (string, error) {
	path, err := repoPath(owner, repo)
	if err != nil {
		return "", err
	}
	if number < 1 {
		return "", errors.New("Pull request number must be positive")
	}
	return path + "/pulls/" + strconv.Itoa(number), nil
}
func list[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	items := []T{}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	for page := 1; page <= 1000; page++ {
		var batch []T
		headers, err := c.request(ctx, http.MethodGet, path+separator+"per_page=100&page="+strconv.Itoa(page), nil, &batch)
		if err != nil {
			return nil, err
		}
		items = append(items, batch...)
		if !strings.Contains(headers.Get("Link"), `rel="next"`) {
			return items, nil
		}
	}
	return nil, errors.New("GitHub pagination exceeded 1000 pages; narrow the request")
}

func (c *Client) CurrentUser(ctx context.Context) (User, error) {
	var user User
	_, err := c.request(ctx, http.MethodGet, "/user", nil, &user)
	return user, err
}
func (c *Client) ListPullRequests(ctx context.Context, owner, repo, state string) ([]PullRequestSummary, error) {
	path, err := repoPath(owner, repo)
	if err != nil {
		return nil, err
	}
	if state == "" {
		state = "open"
	}
	if state != "open" && state != "closed" && state != "all" {
		return nil, errors.New("Pull request state must be open, closed, or all")
	}
	items, err := list[PullRequestSummary](ctx, c, path+"/pulls?state="+state+"&sort=updated&direction=desc")
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		if err := c.loadListCounts(ctx, owner, repo, state, items); err != nil {
			for i := range items {
				items[i].CountsKnown = false
				items[i].Warnings = append(items[i].Warnings, "Pull request totals unavailable: "+err.Error())
			}
		}
	}
	return items, nil
}
