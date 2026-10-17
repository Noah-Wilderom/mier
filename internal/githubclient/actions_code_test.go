package githubclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCheckAnnotationsRequireExactHeadAndKeepPagination(t *testing.T) {
	head := strings.Repeat("a", 40)
	annotationRequests := 0
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/check-runs/5":
			respond(w, map[string]any{"head_sha": head})
		case "/repos/owner/repo/check-runs/5/annotations":
			annotationRequests++
			if r.URL.Query().Get("page") != "2" {
				t.Error("wrong page")
			}
			w.Header().Set("Link", `<ignored>; rel="next"`)
			respond(w, []any{map[string]any{"path": "src/app.go", "start_line": 4, "end_line": 5, "annotation_level": "failure", "message": "Actual compiler error"}})
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(404)
		}
	})
	page, err := client.GetCheckAnnotations(context.Background(), "owner", "repo", 5, head, 2)
	if err != nil || !page.HasMore || len(page.Items) != 1 || page.Items[0].StartLine != 4 || page.Items[0].Level != "failure" {
		t.Fatalf("annotations: %#v %v", page, err)
	}
	if _, err = client.GetCheckAnnotations(context.Background(), "owner", "repo", 5, strings.Repeat("b", 40), 2); err == nil || annotationRequests != 1 {
		t.Fatal("stale annotations were fetched")
	}
}
func TestArtifactDownloadValidatesRunExpiryDigestAndDropsTokenOnStorage(t *testing.T) {
	head := strings.Repeat("a", 40)
	archive := []byte("PK\x03\x04test artifact archive")
	sum := sha256.Sum256(archive)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	expired := false
	var target string
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("API authentication missing")
		}
		switch r.URL.Path {
		case "/repos/owner/repo/actions/artifacts/7":
			respond(w, map[string]any{"id": 7, "name": "build", "size_in_bytes": len(archive), "expired": expired, "digest": digest, "workflow_run": map[string]any{"id": 12, "head_sha": head}})
		case "/repos/owner/repo/actions/runs/12/artifacts":
			w.Header().Set("Link", `<ignored>; rel="next"`)
			respond(w, map[string]any{"artifacts": []any{map[string]any{"id": 7, "size_in_bytes": len(archive), "name": "build"}}})
		case "/repos/owner/repo/actions/artifacts/7/zip":
			w.Header().Set("Location", target)
			w.WriteHeader(302)
		default:
			t.Errorf("unexpected artifact API %s", r.URL)
			w.WriteHeader(404)
		}
	})
	transport := client.http.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "storage.example.test" {
			if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
				t.Error("credentials leaked to artifact storage")
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(archive)), ContentLength: int64(len(archive)), Request: request}, nil
		}
		return transport.RoundTrip(request)
	})
	target = "https://storage.example.test/archive?signature=temporary"
	artifacts, err := client.ListWorkflowArtifacts(context.Background(), "owner", "repo", 12, 1)
	if err != nil || !artifacts.HasMore || artifacts.Items[0].Size != int64(len(archive)) {
		t.Fatalf("artifact page: %#v %v", artifacts, err)
	}
	var out bytes.Buffer
	if err = client.DownloadWorkflowArtifact(context.Background(), "owner", "repo", 7, 12, head, &out); err != nil || !bytes.Equal(out.Bytes(), archive) {
		t.Fatalf("download: %q %v", out.String(), err)
	}
	if _, err = client.WorkflowArtifact(context.Background(), "owner", "repo", 7, 99, head); err == nil {
		t.Fatal("wrong run accepted")
	}
	expired = true
	if _, err = client.WorkflowArtifact(context.Background(), "owner", "repo", 7, 12, head); err == nil {
		t.Fatal("expired artifact accepted")
	}
	expired = false
	digest = fmt.Sprintf("sha256:%064d", 0)
	out.Reset()
	if err = client.DownloadWorkflowArtifact(context.Background(), "owner", "repo", 7, 12, head, &out); err == nil {
		t.Fatal("corrupt artifact accepted")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
