package reviewproviders

import (
	"context"
	"encoding/base64"
	"errors"
	"mag-ik-een-review/internal/githubclient"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProviderAuthenticationStaysOnItsAPIHost(t *testing.T) {
	for _, kind := range []string{"gitlab", "bitbucket", "azure"} {
		t.Run(kind, func(t *testing.T) {
			host := map[string]string{"gitlab": "gitlab.example.test", "bitbucket": "bitbucket.org", "azure": "dev.azure.com"}[kind]
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "gitlab" {
					if r.Header.Get("PRIVATE-TOKEN") != "synthetic-token" {
						t.Error("missing GitLab authentication")
					}
				} else {
					user, password, ok := r.BasicAuth()
					expected := "person@example.invalid"
					if kind == "azure" {
						expected = ""
					}
					if !ok || user != expected || password != "synthetic-token" {
						t.Error("incorrect scoped Basic authentication")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()
			client, err := newTransport(kind, host, "synthetic-token", "person@example.invalid", Options{BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			var out struct {
				OK bool `json:"ok"`
			}
			if _, err = client.request(context.Background(), http.MethodGet, "/resource", nil, &out); err != nil || !out.OK {
				t.Fatal(out, err)
			}
		})
	}
}
func TestProviderRedirectAndErrorNeverExposeCredentials(t *testing.T) {
	var reached atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Store(true) }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		w.Write([]byte(`{"message":"synthetic-token ` + base64.StdEncoding.EncodeToString([]byte("person@example.invalid:synthetic-token")) + `"}`))
	}))
	defer server.Close()
	client, err := newTransport("bitbucket", "bitbucket.org", "synthetic-token", "person@example.invalid", Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.request(context.Background(), "GET", "/redirect", nil, nil); err == nil || reached.Load() {
		t.Fatal("credential-bearing redirect followed", err)
	}
	_, err = client.request(context.Background(), "GET", "/error", nil, nil)
	var response *HTTPError
	if !errors.As(err, &response) || response.StatusCode != 401 || strings.Contains(err.Error(), "synthetic-token") || strings.Contains(err.Error(), base64.StdEncoding.EncodeToString([]byte("person@example.invalid:synthetic-token"))) {
		t.Fatal(err)
	}
}
func TestProviderResponseAndCursorBounds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(strings.Repeat("x", 64))) }))
	defer server.Close()
	client, err := newTransport("gitlab", "gitlab.example.test", "synthetic-token", "", Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = client.raw(context.Background(), "GET", "/large", "text/plain", nil, 32); err == nil {
		t.Fatal("unbounded response accepted")
	}
	cursor := makeCursor(2, "first-query")
	if value, err := readCursor(cursor, "first-query"); err != nil || value != 2 {
		t.Fatal(value, err)
	}
	if _, err := readCursor(cursor, "second-query"); err == nil {
		t.Fatal("cursor reused for another query")
	}
	if _, err := normalizeList(githubclient.PullRequestListRequest{PageSize: 101}); err == nil {
		t.Fatal("large page accepted")
	}
	for _, path := range []string{"../escape", ".git/config", "a/../b", "/absolute", "a\x00b"} {
		if validFile(path) {
			t.Fatal("unsafe source path", path)
		}
	}
	if _, err = textFile([]byte("binary\x00content")); err == nil {
		t.Fatal("binary source treated as text")
	}
}
