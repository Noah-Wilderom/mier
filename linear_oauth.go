package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type linearAuthorization struct {
	ctx      context.Context
	cancel   context.CancelFunc
	state    string
	verifier string
	clientID string
	callback string
	listener net.Listener
	epoch    uint64
	used     bool
}
type linearTokenResponse struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	TokenType    string          `json:"token_type"`
	ExpiresIn    int             `json:"expires_in"`
	Scope        json.RawMessage `json:"scope"`
}

func linearCallbackURL(port int) string {
	return fmt.Sprintf("http://localhost:%d/oauth/linear/callback", port)
}

func (s *LinearService) BeginLinearConnection(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", errors.New("Linear integration has shut down")
	}
	if s.config.ClientID == "" {
		return "", errors.New("Save your Linear OAuth client ID first")
	}
	if s.pending != nil {
		return "", errors.New("Linear authorization is already in progress. Finish or cancel it first.")
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", s.config.CallbackPort))
	if err != nil {
		return "", errors.New("The Linear callback port is in use. Choose another port and update your OAuth app's callback URL.")
	}
	var state, verifier [32]byte
	if _, err = rand.Read(state[:]); err == nil {
		_, err = rand.Read(verifier[:])
	}
	if err != nil {
		_ = listener.Close()
		return "", errors.New("Linear authorization could not be started")
	}
	workCtx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	p := &linearAuthorization{ctx: workCtx, cancel: cancel, state: base64.RawURLEncoding.EncodeToString(state[:]), verifier: base64.RawURLEncoding.EncodeToString(verifier[:]), clientID: s.config.ClientID, callback: linearCallbackURL(s.config.CallbackPort), listener: listener, epoch: s.epoch}
	s.pending, s.state, s.connectionError = p, "authorizing", ""
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 65 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.handleLinearCallback(p, w, r) })
	go func() { _ = server.Serve(listener) }()
	go func() {
		<-workCtx.Done()
		_ = server.Close()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.pending == p {
			s.pending, s.state = nil, "failed"
			s.connectionError = "Linear authorization timed out. Try authorizing again."
		}
	}()
	challenge := sha256.Sum256([]byte(p.verifier))
	values := url.Values{"client_id": {p.clientID}, "redirect_uri": {p.callback}, "response_type": {"code"}, "scope": {"read"}, "state": {p.state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}, "actor": {"user"}, "prompt": {"consent"}}
	return "https://linear.app/oauth/authorize?" + values.Encode(), nil
}

func (s *LinearService) CancelLinearConnection() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending != nil {
		_ = s.pending.listener.Close()
		s.pending.cancel()
		s.pending = nil
	}
	s.state, s.connectionError = "idle", ""
}

func (s *LinearService) handleLinearCallback(p *linearAuthorization, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	callback, _ := url.Parse(p.callback)
	if r.Method != http.MethodGet || r.URL.Path != "/oauth/linear/callback" || len(r.URL.RawQuery) > 8192 || (r.Host != callback.Host && r.Host != strings.Replace(callback.Host, "localhost", "127.0.0.1", 1)) {
		http.Error(w, "Invalid callback", http.StatusBadRequest)
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
	p.used, s.state = true, "verifying"
	s.mu.Unlock()
	var err error
	if query.Get("error") != "" {
		err = errors.New("Linear authorization was declined. Try again when you are ready.")
	} else if len(query["code"]) != 1 || query.Get("code") == "" || len(query.Get("code")) > 4096 {
		err = errors.New("Linear did not return a valid authorization code")
	} else {
		err = s.completeLinearAuthorization(p, query.Get("code"))
	}
	s.mu.Lock()
	if s.pending == p {
		// Closing only the listener keeps this browser response writable while
		// releasing the callback port before another authorization can begin.
		_ = p.listener.Close()
		s.pending, s.state, s.connectionError = nil, "idle", ""
		if err != nil {
			s.state, s.connectionError = "failed", err.Error()
		}
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "Linear could not be connected. Return to M.I.E.R. Settings for details and retry.")
	} else {
		_, _ = io.WriteString(w, "Linear connected. You can close this tab and return to M.I.E.R.")
	}
	if flush, ok := w.(http.Flusher); ok {
		flush.Flush()
	}
	p.cancel()
}

func validLinearToken(token linearTokenResponse) bool {
	return token.AccessToken != "" && token.RefreshToken != "" && len(token.AccessToken)+len(token.RefreshToken) < 60*1024 && !strings.ContainsAny(token.AccessToken+token.RefreshToken, "\r\n\x00") && strings.EqualFold(token.TokenType, "Bearer") && token.ExpiresIn > 0 && token.ExpiresIn <= 365*24*3600
}

func linearReadScope(raw json.RawMessage) bool {
	var text string
	var scopes []string
	if json.Unmarshal(raw, &text) == nil {
		scopes = strings.Fields(strings.ReplaceAll(text, ",", " "))
	} else if json.Unmarshal(raw, &scopes) != nil {
		return false
	}
	for _, scope := range scopes {
		if scope == "read" {
			return true
		}
	}
	return false
}

func (s *LinearService) linearToken(ctx context.Context, values url.Values) (linearTokenResponse, error) {
	data, err := s.linearRequest(ctx, "/oauth/token", "", values)
	if err != nil {
		return linearTokenResponse{}, err
	}
	var token linearTokenResponse
	if json.Unmarshal(data, &token) != nil || !validLinearToken(token) {
		return linearTokenResponse{}, errors.New("Linear returned an invalid token response. Authorize again in Settings.")
	}
	return token, nil
}

func (s *LinearService) completeLinearAuthorization(p *linearAuthorization, code string) error {
	token, err := s.linearToken(p.ctx, url.Values{"grant_type": {"authorization_code"}, "client_id": {p.clientID}, "redirect_uri": {p.callback}, "code": {code}, "code_verifier": {p.verifier}})
	if err != nil {
		return errors.New("Linear could not authorize this app. Check the client ID and registered callback URL, then retry.")
	}
	if !linearReadScope(token.Scope) {
		return errors.New("Linear did not grant read access. Authorize again with the read permission.")
	}
	var identity struct {
		Viewer       struct{ ID, Name string }
		Organization struct{ ID, Name, URLKey string }
	}
	if err := s.linearGraphQLRaw(p.ctx, token.AccessToken, "query MierLinearIdentity { viewer { id name } organization { id name urlKey } }", nil, &identity); err != nil {
		return errors.New("Your Linear account and workspace could not be verified. Retry authorization.")
	}
	workspace := LinearWorkspace{ID: identity.Organization.ID, Name: identity.Organization.Name, URL: "https://linear.app/" + identity.Organization.URLKey}
	if identity.Viewer.ID == "" || len(identity.Viewer.Name) > 1024 || !validLinearWorkspace(workspace) {
		return errors.New("Linear returned invalid workspace information. Retry authorization.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.pending != p || s.epoch != p.epoch || p.ctx.Err() != nil {
		return errors.New("Linear configuration changed during authorization. Try again.")
	}
	next := s.config
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return errors.New("Linear credentials could not be prepared")
	}
	next.CredentialID = hex.EncodeToString(nonce[:])
	next.Workspace, next.Account = &workspace, identity.Viewer.Name
	credentials := linearSecrets{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)}
	data, _ := json.Marshal(credentials)
	if err := s.store.Set(linearKeyringService, next.CredentialID, string(data)); err != nil {
		return errors.New("Linear credentials could not be saved in the OS keychain. Check keychain access and retry.")
	}
	if p.ctx.Err() != nil {
		_ = s.store.Delete(linearKeyringService, next.CredentialID)
		return errors.New("Linear authorization is no longer active. Try again.")
	}
	if err := writeLinearConfiguration(s.configPath, next); err != nil {
		_ = s.store.Delete(linearKeyringService, next.CredentialID)
		return err
	}
	_ = s.store.Delete(linearKeyringService, s.config.CredentialID)
	s.config = next
	s.secrets, s.secretsLoaded, s.secretsDirty = credentials, true, false
	s.epoch++
	s.cache = map[string]linearCachedIssue{}
	s.retryAt = time.Time{}
	return nil
}

func (s *LinearService) linearAccessToken(ctx context.Context, rejected string) (string, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", errors.New("Linear integration has shut down")
	}
	if err := s.loadSecretsLocked(); err != nil {
		s.mu.Unlock()
		return "", err
	}
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
		return "", errors.New("Authorize your Linear account in Settings first")
	}
	if secret.AccessToken != "" && secret.ExpiresAt.After(time.Now().Add(30*time.Second)) && (rejected == "" || rejected != secret.AccessToken) {
		return secret.AccessToken, nil
	}
	token, err := s.linearToken(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {secret.RefreshToken}})
	if err != nil {
		var status *linearAPIError
		if errors.As(err, &status) && (status.status == 400 || status.status == 401 || status.status == 403) {
			s.mu.Lock()
			if s.epoch == epoch {
				if err := s.saveSecretsLocked(linearSecrets{}); err != nil {
					s.secrets, s.secretsDirty = linearSecrets{}, true
				}
				s.epoch++
				s.cache = map[string]linearCachedIssue{}
				s.connectionError = "Linear authorization expired. Authorize again in Settings."
			}
			s.mu.Unlock()
			return "", errors.New("Linear authorization expired. Authorize again in Settings.")
		}
		return "", err
	}
	if len(token.Scope) > 0 && !linearReadScope(token.Scope) {
		return "", errors.New("Linear no longer grants read access. Authorize again in Settings.")
	}
	secret = linearSecrets{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch != epoch || s.closed {
		return "", errors.New("Linear configuration changed. Retry the request.")
	}
	if err := s.saveSecretsLocked(secret); err != nil {
		// Linear rotates refresh tokens. Keep the replacement in memory and
		// retry its durable write before issuing any further provider request.
		s.secrets, s.secretsDirty, s.connectionError = secret, true, err.Error()
		return "", err
	}
	return secret.AccessToken, nil
}
