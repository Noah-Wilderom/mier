package reviewproviders

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mag-ik-een-review/internal/githubclient"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Options struct {
	HTTPClient   *http.Client
	BaseURL      string
	Organization string
	Diff         func(context.Context, string, string) (string, error)
	Publisher    *Publisher
	ReadOnly     bool
}
type ProgressStore interface {
	Load(string) (PublicationProgress, error)
	Save(string, PublicationProgress) error
}
type PublicationProgress struct {
	StartedAt string                      `json:"startedAt"`
	Comments  map[string]PublishedComment `json:"comments"`
	Votes     map[string]bool             `json:"votes"`
	LastVote  string                      `json:"lastVote,omitempty"`
}
type PublishedComment struct {
	ID   int64  `json:"id"`
	Hash string `json:"hash"`
}
type transport struct {
	kind, host, base, token, username string
	http                              *http.Client
	readOnly                          bool
}
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string { return e.Message }

func newTransport(kind, host, token, username string, options Options) (*transport, error) {
	normalized, err := githubclient.NormalizeHost(host)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\x00\r\n") {
		return nil, errors.New("Enter a valid access token")
	}
	t := &transport{kind: kind, host: normalized, token: strings.TrimSpace(token), username: username, readOnly: options.ReadOnly}
	switch kind {
	case "gitlab":
		t.base = "https://" + normalized + "/api/v4"
	case "bitbucket":
		if normalized != "bitbucket.org" && normalized != "api.bitbucket.org" {
			return nil, errors.New("Bitbucket Cloud connections use bitbucket.org")
		}
		if strings.TrimSpace(username) == "" {
			return nil, errors.New("Enter your Atlassian account email for API token authentication")
		}
		t.host = "bitbucket.org"
		t.base = "https://api.bitbucket.org/2.0"
	case "azure":
		if normalized != "dev.azure.com" {
			return nil, errors.New("Azure DevOps Services connections use dev.azure.com")
		}
		t.base = "https://dev.azure.com"
	default:
		return nil, errors.New("Choose a supported review provider")
	}
	if options.BaseURL != "" {
		parsed, err := url.Parse(options.BaseURL)
		if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, errors.New("Invalid provider test endpoint")
		}
		t.base = strings.TrimRight(options.BaseURL, "/")
	}
	client := &http.Client{Timeout: 45 * time.Second}
	if options.HTTPClient != nil {
		*client = *options.HTTPClient
	}
	base, _ := url.Parse(t.base)
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Host != base.Host || req.URL.Scheme != base.Scheme {
			return errors.New("The provider redirected outside its configured API host")
		}
		if len(via) > 5 {
			return errors.New("Too many provider redirects")
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		return nil
	}
	t.http = client
	return t, nil
}
func (t *transport) redact(message string) string {
	for _, secret := range []string{t.token, base64.StdEncoding.EncodeToString([]byte(t.username + ":" + t.token)), base64.StdEncoding.EncodeToString([]byte(":" + t.token))} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	if len(message) > 1500 {
		message = message[:1500] + "…"
	}
	return strings.ToValidUTF8(message, "")
}
func (t *transport) raw(ctx context.Context, method, path, accept string, input any, limit int64) ([]byte, http.Header, error) {
	if t.readOnly && method != "GET" && method != "HEAD" {
		return nil, nil, errors.New("This account is connected for read-only reviews")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, nil, errors.New("Invalid provider API path")
	}
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return nil, nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.base+path, body)
	if err != nil {
		return nil, nil, errors.New("Invalid provider request")
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "MIER desktop")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch t.kind {
	case "gitlab":
		req.Header.Set("PRIVATE-TOKEN", t.token)
	case "bitbucket":
		req.SetBasicAuth(t.username, t.token)
	case "azure":
		req.SetBasicAuth("", t.token)
	}
	response, err := t.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errors.New(t.redact("The provider request failed: " + err.Error()))
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, nil, errors.New(t.redact(err.Error()))
	}
	if int64(len(data)) > limit {
		return nil, nil, errors.New("The provider response exceeds its size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := http.StatusText(response.StatusCode)
		var wire struct {
			Message     any    `json:"message"`
			Error       any    `json:"error"`
			Description string `json:"error_description"`
		}
		if json.Unmarshal(data, &wire) == nil {
			value := wire.Message
			if value == nil {
				value = wire.Error
			}
			if value != nil {
				encoded, _ := json.Marshal(value)
				message = string(encoded)
			}
			if wire.Description != "" {
				message = wire.Description
			}
		}
		return nil, response.Header, &HTTPError{StatusCode: response.StatusCode, Message: t.redact(fmt.Sprintf("%s API (HTTP %d): %s", t.kind, response.StatusCode, message))}
	}
	return data, response.Header, nil
}
func (t *transport) request(ctx context.Context, method, path string, input, output any) (http.Header, error) {
	data, headers, err := t.raw(ctx, method, path, "application/json", input, 16*1024*1024)
	if err != nil {
		return headers, err
	}
	if output != nil && len(bytes.TrimSpace(data)) > 0 {
		if err = json.Unmarshal(data, output); err != nil {
			return headers, errors.New("The provider returned an invalid JSON response")
		}
	}
	return headers, nil
}
func component(value string) (string, error) {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, "\x00\r\n/\\") {
		return "", errors.New("Choose a valid repository identity")
	}
	return url.PathEscape(value), nil
}
func namespace(value string) (string, error) {
	parts := strings.Split(value, "/")
	encoded := []string{}
	for _, part := range parts {
		part, err := component(part)
		if err != nil {
			return "", err
		}
		encoded = append(encoded, part)
	}
	return strings.Join(encoded, "/"), nil
}
func immutable(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'f' || ch >= '0' && ch <= '9') {
			return false
		}
	}
	return true
}
func validFile(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\x00\\") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." || part == ".git" {
			return false
		}
	}
	return true
}
func textFile(data []byte) (string, error) {
	if len(data) > 2*1024*1024 {
		return "", errors.New("This source file exceeds 2 MiB")
	}
	if bytes.ContainsRune(data, 0) || !utf8.Valid(data) {
		return "", errors.New("This source file is binary")
	}
	return string(data), nil
}
func hash(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(h, "%d:", len(part))
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}
func normalizeList(request githubclient.PullRequestListRequest) (githubclient.PullRequestListRequest, error) {
	if request.State == "" {
		request.State = "open"
	}
	if request.View == "" {
		request.View = "inbox"
	}
	if request.PageSize == 0 {
		request.PageSize = 25
	}
	switch request.State {
	case "open", "all", "closed", "merged":
	default:
		return request, errors.New("Choose open, closed, merged, or all pull requests")
	}
	switch request.View {
	case "inbox", "mine", "assigned", "review-requested", "drafts":
	default:
		return request, errors.New("Choose a supported review view")
	}
	if request.PageSize < 1 || request.PageSize > 100 || len(request.Query) > 4096 || len(request.Cursor) > 2048 {
		return request, errors.New("Invalid review page size, query, or cursor")
	}
	return request, nil
}

type pageCursor struct {
	Scope string `json:"scope"`
	Page  int    `json:"page"`
}

func readCursor(value, scope string) (int, error) {
	if value == "" {
		return 1, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, errors.New("Invalid review pagination cursor")
	}
	var cursor pageCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.Scope != scope || cursor.Page < 1 || cursor.Page > 100000 {
		return 0, errors.New("This cursor belongs to another review query")
	}
	return cursor.Page, nil
}
func makeCursor(page int, scope string) string {
	data, _ := json.Marshal(pageCursor{Scope: scope, Page: page})
	return base64.RawURLEncoding.EncodeToString(data)
}
func integerHeader(header http.Header, key string, fallback int) int {
	value, err := strconv.Atoi(header.Get(key))
	if err != nil || value < 0 {
		return fallback
	}
	return value
}
func encodedThread(kind, owner, repo string, number int, id string) string {
	data, _ := json.Marshal([]string{kind, owner, repo, strconv.Itoa(number), id})
	return "mier-thread:" + base64.RawURLEncoding.EncodeToString(data)
}
func decodeThread(value, kind string) (string, string, int, string, error) {
	if len(value) > 4096 || !strings.HasPrefix(value, "mier-thread:") {
		return "", "", 0, "", errors.New("Invalid provider thread identity")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "mier-thread:"))
	var parts []string
	if err != nil || json.Unmarshal(data, &parts) != nil || len(parts) != 5 || parts[0] != kind {
		return "", "", 0, "", errors.New("This thread belongs to another provider")
	}
	number, err := strconv.Atoi(parts[3])
	if err != nil || number < 1 || parts[4] == "" || strings.ContainsAny(parts[4], "\x00\r\n") {
		return "", "", 0, "", errors.New("Invalid provider thread")
	}
	if _, err = component(parts[4]); err != nil {
		return "", "", 0, "", err
	}
	return parts[1], parts[2], number, parts[4], nil
}
