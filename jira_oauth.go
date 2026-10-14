package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type jiraAuthorization struct {
	ctx      context.Context
	cancel   context.CancelFunc
	state    string
	epoch    uint64
	used     bool
	clientID string
	secret   string
	callback string
}
type jiraTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}
type jiraHTTPError struct {
	status     int
	retryAfter string
}

func (e *jiraHTTPError) Error() string {
	switch e.status {
	case 401:
		return "Jira authorization expired. Authorize your account again in Settings."
	case 403:
		return "Your Jira account or OAuth app does not have permission to view this information."
	case 404:
		return "This Jira ticket was not found, or your account cannot view it."
	case 429:
		if e.retryAfter != "" {
			return "Jira is limiting requests. Retry after " + e.retryAfter + " seconds."
		}
		return "Jira is limiting requests. Wait a moment before retrying."
	default:
		return "Jira could not complete the request. Check your connection and retry."
	}
}
func jiraCallbackURL(port int) string {
	return fmt.Sprintf("http://localhost:%d/oauth/jira/callback", port)
}
func validJiraSite(site JiraSite) bool {
	u, err := url.Parse(site.URL)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && len(site.URL) <= 2048 && len(site.Name) <= 1024 && jiraSiteID.MatchString(site.ID)
}

func (s *IntegrationService) BeginJiraConnection(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", errors.New("Jira integration has shut down")
	}
	if err := s.loadSecretsLocked(); err != nil {
		return "", err
	}
	if s.config.ClientID == "" || s.secrets.ClientSecret == "" {
		return "", errors.New("Save your Jira OAuth app credentials first")
	}
	if s.pending != nil {
		return "", errors.New("Jira authorization is already in progress. Finish or cancel it first.")
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", s.config.CallbackPort))
	if err != nil {
		return "", errors.New("The Jira callback port is in use. Choose another port and update your OAuth app's callback URL.")
	}
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		listener.Close()
		return "", errors.New("Jira authorization could not be started")
	}
	workCtx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	p := &jiraAuthorization{ctx: workCtx, cancel: cancel, state: base64.RawURLEncoding.EncodeToString(nonce), epoch: s.epoch, clientID: s.config.ClientID, secret: s.secrets.ClientSecret, callback: jiraCallbackURL(s.config.CallbackPort)}
	s.pending = p
	s.state = "authorizing"
	s.connectionError = ""
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 65 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.handleJiraCallback(p, w, r) })
	go func() { _ = server.Serve(listener) }()
	go func() {
		<-workCtx.Done()
		_ = server.Close()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.pending == p {
			s.pending = nil
			s.state = "failed"
			s.connectionError = "Jira authorization timed out. Try authorizing again."
		}
	}()
	values := url.Values{"audience": {"api.atlassian.com"}, "client_id": {p.clientID}, "scope": {strings.Join(jiraScopes, " ")}, "redirect_uri": {p.callback}, "state": {p.state}, "response_type": {"code"}, "prompt": {"consent"}}
	return "https://auth.atlassian.com/authorize?" + values.Encode(), nil
}
func (s *IntegrationService) CancelJiraConnection() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending != nil {
		s.pending.cancel()
		s.pending = nil
	}
	s.state = "idle"
	s.connectionError = ""
}
func (s *IntegrationService) handleJiraCallback(p *jiraAuthorization, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet || r.URL.Path != "/oauth/jira/callback" || len(r.URL.RawQuery) > 8192 {
		http.Error(w, "Invalid callback", http.StatusBadRequest)
		return
	}
	callback, _ := url.Parse(p.callback)
	if r.Host != callback.Host && r.Host != strings.Replace(callback.Host, "localhost", "127.0.0.1", 1) {
		http.Error(w, "Invalid callback host", http.StatusBadRequest)
		return
	}
	query := r.URL.Query()
	if len(query["state"]) != 1 || subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(p.state)) != 1 {
		http.Error(w, "Invalid authorization state", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if s.pending != p || p.used || p.ctx.Err() != nil {
		s.mu.Unlock()
		http.Error(w, "Authorization is no longer active", http.StatusConflict)
		return
	}
	p.used = true
	s.state = "verifying"
	s.mu.Unlock()
	var err error
	if query.Get("error") != "" {
		err = errors.New("Jira authorization was declined. Try again when you are ready.")
	} else if len(query["code"]) != 1 || query.Get("code") == "" || len(query.Get("code")) > 4096 {
		err = errors.New("Jira did not return a valid authorization code")
	} else {
		err = s.completeJiraAuthorization(p, query.Get("code"))
	}
	s.mu.Lock()
	if s.pending == p {
		s.pending = nil
		if err != nil {
			s.state = "failed"
			s.connectionError = err.Error()
		} else {
			s.state = "idle"
			s.connectionError = ""
		}
	}
	s.mu.Unlock()
	// Finish the browser response before closing the loopback server.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "Jira could not be connected. Return to M.I.E.R. to retry.")
	} else {
		_, _ = io.WriteString(w, "Jira connected. You can close this tab and return to M.I.E.R.")
	}
	if flush, ok := w.(http.Flusher); ok {
		flush.Flush()
	}
	p.cancel()
}
func (s *IntegrationService) completeJiraAuthorization(p *jiraAuthorization, code string) error {
	var token jiraTokenResponse
	if err := s.jiraJSON(p.ctx, http.MethodPost, "https://auth.atlassian.com/oauth/token", "", map[string]string{"grant_type": "authorization_code", "client_id": p.clientID, "client_secret": p.secret, "code": code, "redirect_uri": p.callback}, &token); err != nil {
		return errors.New("Jira could not authorize this app. Check the client credentials, callback URL, and permissions, then retry.")
	}
	if token.AccessToken == "" || token.RefreshToken == "" || token.ExpiresIn <= 0 || token.ExpiresIn > 365*24*3600 || !containsJiraScope(token.Scope, "read:jira-work") {
		return errors.New("Jira did not grant the required read access and refresh token. Check the app permissions and authorize again.")
	}
	var resources []struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		URL    string   `json:"url"`
		Scopes []string `json:"scopes"`
	}
	if err := s.jiraJSON(p.ctx, http.MethodGet, "https://api.atlassian.com/oauth/token/accessible-resources", token.AccessToken, nil, &resources); err != nil {
		return errors.New("Authorized Jira sites could not be verified. Retry authorization.")
	}
	sites := []JiraSite{}
	for _, item := range resources {
		site := JiraSite{ID: item.ID, Name: item.Name, URL: strings.TrimRight(item.URL, "/")}
		if !validJiraSite(site) {
			continue
		}
		allowed := false
		for _, scope := range item.Scopes {
			if scope == "read:jira-work" {
				allowed = true
			}
		}
		if allowed {
			sites = append(sites, site)
		}
		if len(sites) >= 32 {
			break
		}
	}
	if len(sites) == 0 {
		return errors.New("No Jira site granted read access. Select your Jira site on the consent screen.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.pending != p || s.epoch != p.epoch || p.ctx.Err() != nil {
		return errors.New("Jira configuration changed during authorization. Try again.")
	}
	oldConfig, oldSecrets := s.config, s.secrets
	if err := s.saveSecretsLocked(jiraSecrets{ClientSecret: p.secret, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)}); err != nil {
		return err
	}
	next := s.config
	next.Sites = sites
	next.DefaultSiteID = sites[0].ID
	for _, site := range sites {
		if site.ID == oldConfig.DefaultSiteID {
			next.DefaultSiteID = site.ID
		}
	}
	if err := writeJiraConfiguration(s.configPath, next); err != nil {
		_ = s.saveSecretsLocked(oldSecrets)
		return err
	}
	s.config = next
	s.epoch++
	s.cache = map[string]jiraCachedIssue{}
	s.retryAt = time.Time{}
	return nil
}
func containsJiraScope(scopes, wanted string) bool {
	for _, value := range strings.Fields(scopes) {
		if value == wanted {
			return true
		}
	}
	return false
}

func (s *IntegrationService) jiraJSON(ctx context.Context, method, address, token string, body any, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.HasPrefix(address, "https://api.atlassian.com/") {
		s.mu.Lock()
		retryAt := s.retryAt
		s.mu.Unlock()
		if retryAt.After(time.Now()) {
			return &jiraHTTPError{status: 429, retryAfter: strconv.Itoa(int(time.Until(retryAt).Seconds()) + 1)}
		}
	}
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, input)
	if err != nil {
		return errors.New("Jira request could not be created")
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Jira could not be reached. Check your connection and retry.")
	}
	defer response.Body.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retry := ""
		if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 && seconds <= 3600 {
			retry = strconv.Itoa(seconds)
			if response.StatusCode == http.StatusTooManyRequests {
				s.mu.Lock()
				s.retryAt = time.Now().Add(time.Duration(seconds) * time.Second)
				s.mu.Unlock()
			}
		}
		return &jiraHTTPError{status: response.StatusCode, retryAfter: retry}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if err != nil {
		return errors.New("Jira response could not be read")
	}
	if len(data) > 2*1024*1024 {
		return errors.New("Jira returned too much data to display. Open the ticket in Jira.")
	}
	if json.Unmarshal(data, result) != nil {
		return errors.New("Jira returned an invalid response")
	}
	return nil
}
func (s *IntegrationService) jiraAccessToken(ctx context.Context, rejected string) (string, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", errors.New("Jira integration has shut down")
	}
	if err := s.loadSecretsLocked(); err != nil {
		s.mu.Unlock()
		return "", err
	}
	// Keep a newly rotated token in memory if the keychain was temporarily locked.
	// Retry its durable write before issuing another API call or token refresh.
	if s.secretsDirty {
		if err := s.saveSecretsLocked(s.secrets); err != nil {
			s.mu.Unlock()
			return "", err
		}
		s.connectionError = ""
	}
	secret, clientID, epoch := s.secrets, s.config.ClientID, s.epoch
	s.mu.Unlock()
	if secret.RefreshToken == "" {
		return "", errors.New("Authorize your Jira account in Settings first")
	}
	if secret.AccessToken != "" && secret.ExpiresAt.After(time.Now().Add(30*time.Second)) && (rejected == "" || rejected != secret.AccessToken) {
		return secret.AccessToken, nil
	}
	var token jiraTokenResponse
	if err := s.jiraJSON(ctx, http.MethodPost, "https://auth.atlassian.com/oauth/token", "", map[string]string{"grant_type": "refresh_token", "client_id": clientID, "client_secret": secret.ClientSecret, "refresh_token": secret.RefreshToken}, &token); err != nil {
		var status *jiraHTTPError
		if errors.As(err, &status) && (status.status == 400 || status.status == 401 || status.status == 403) {
			s.mu.Lock()
			if s.epoch == epoch {
				secret.AccessToken = ""
				secret.RefreshToken = ""
				if s.saveSecretsLocked(secret) != nil {
					s.secrets = secret
					s.secretsDirty = true
				}
				s.epoch++
				s.cache = map[string]jiraCachedIssue{}
				s.connectionError = "Jira authorization expired. Authorize again in Settings."
			}
			s.mu.Unlock()
			return "", errors.New("Jira authorization expired. Authorize again in Settings.")
		}
		return "", err
	}
	if token.AccessToken == "" || token.RefreshToken == "" || token.ExpiresIn <= 0 || token.ExpiresIn > 365*24*3600 {
		return "", errors.New("Jira returned an invalid refresh response. Authorize again in Settings.")
	}
	secret.AccessToken = token.AccessToken
	secret.RefreshToken = token.RefreshToken
	secret.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch != epoch || s.closed {
		return "", errors.New("Jira configuration changed. Retry the request.")
	}
	if err := s.saveSecretsLocked(secret); err != nil {
		s.secrets = secret
		s.secretsDirty = true
		s.connectionError = err.Error()
		return "", err
	}
	return secret.AccessToken, nil
}
func (s *IntegrationService) jiraAPI(ctx context.Context, address string, body any, result any) error {
	token, err := s.jiraAccessToken(ctx, "")
	if err != nil {
		return err
	}
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	err = s.jiraJSON(ctx, method, address, token, body, result)
	var status *jiraHTTPError
	if errors.As(err, &status) && status.status == 401 {
		token, err = s.jiraAccessToken(ctx, token)
		if err != nil {
			return err
		}
		return s.jiraJSON(ctx, method, address, token, body, result)
	}
	return err
}
