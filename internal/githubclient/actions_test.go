package githubclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func actionRunFixture(attempt int, status string) map[string]any {
	return map[string]any{"id": 12, "workflow_id": 4, "run_attempt": attempt, "run_number": 7, "status": status, "name": "Tests", "display_title": "Fix review", "head_sha": "abc", "head_branch": "feature", "html_url": "https://github.com/owner/repo/actions/runs/12", "actor": map[string]any{"login": "reviewer"}}
}
func actionJobFixture(id int64, attempt int, status string) map[string]any {
	return map[string]any{"id": id, "run_id": 12, "run_attempt": attempt, "status": status, "conclusion": "failure", "name": "Tests", "html_url": "https://github.com/owner/repo/actions/runs/12/job/99", "check_run_url": "https://api.github.com/repos/owner/repo/check-runs/321", "steps": []any{map[string]any{"number": 1, "name": "Test", "status": "completed", "conclusion": "failure", "started_at": "2026-10-14T10:00:00Z"}}}
}
func TestActionsPaginationFiltersAndAttempts(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/actions/workflows":
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", `<ignored>; rel="next"`)
			}
			respond(w, map[string]any{"workflows": []any{map[string]any{"id": 4, "name": "Tests", "html_url": "https://github.com/owner/repo/actions/workflows/test.yml"}}})
		case r.URL.Path == "/repos/owner/repo/actions/workflows/4/runs":
			if r.URL.Query().Get("branch") != "feature/a b" || r.URL.Query().Get("status") != "failure" || r.URL.Query().Get("page") != "2" {
				t.Errorf("wrong filters: %s", r.URL)
			}
			w.Header().Set("Link", `<ignored>; rel="next"`)
			respond(w, map[string]any{"total_count": 41, "workflow_runs": []any{actionRunFixture(3, "completed")}})
		case r.URL.Path == "/repos/owner/repo/actions/runs/12/attempts/2":
			respond(w, actionRunFixture(2, "completed"))
		case r.URL.Path == "/repos/owner/repo/actions/runs/12/attempts/2/jobs":
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", `<ignored>; rel="next"`)
			}
			respond(w, map[string]any{"jobs": []any{actionJobFixture(99, 2, "completed")}})
		default:
			t.Errorf("unexpected %s", r.URL)
			w.WriteHeader(404)
		}
	})
	workflows, err := client.ListWorkflows(context.Background(), "owner", "repo")
	if err != nil || len(workflows) != 2 || workflows[0].URL == "" {
		t.Fatalf("%+v %v", workflows, err)
	}
	runs, err := client.ListWorkflowRuns(context.Background(), "owner", "repo", WorkflowRunsRequest{WorkflowID: 4, Branch: "feature/a b", Status: "failure", Page: 2})
	if err != nil || !runs.HasMore || runs.TotalCount != 41 || runs.Items[0].RunAttempt != 3 || runs.Items[0].HeadSHA != "abc" || runs.Items[0].Actor.Login != "reviewer" {
		t.Fatalf("%+v %v", runs, err)
	}
	detail, err := client.GetWorkflowRun(context.Background(), "owner", "repo", 12, 2)
	if err != nil || len(detail.Jobs) != 2 || detail.Run.RunAttempt != 2 || detail.Jobs[0].Steps[0].StartedAt == "" {
		t.Fatalf("%+v %v", detail, err)
	}
}
func TestCheckResolvesVerifiedOriginalJobAndSuiteFallback(t *testing.T) {
	for _, direct := range []bool{true, false} {
		t.Run(fmt.Sprint(direct), func(t *testing.T) {
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/repo/check-runs/321":
					link := "https://other.example/owner/repo/actions/runs/12/job/99"
					if direct {
						link = "https://github.com/owner/repo/actions/runs/12/job/99"
					}
					respond(w, map[string]any{"id": 321, "details_url": link, "app": map[string]any{"slug": "github-actions"}, "check_suite": map[string]any{"id": 8}})
				case "/repos/owner/repo/actions/jobs/99":
					respond(w, actionJobFixture(99, 1, "completed"))
				case "/repos/owner/repo/actions/runs":
					if direct || r.URL.Query().Get("check_suite_id") != "8" {
						t.Error("wrong suite resolution")
					}
					respond(w, map[string]any{"workflow_runs": []any{actionRunFixture(2, "completed")}})
				case "/repos/owner/repo/actions/runs/12/jobs":
					if r.URL.Query().Get("filter") != "all" {
						t.Error("lost older attempts")
					}
					respond(w, map[string]any{"jobs": []any{actionJobFixture(99, 1, "completed")}})
				default:
					t.Errorf("unexpected %s", r.URL)
					w.WriteHeader(404)
				}
			})
			job, err := client.GetCheckWorkflowJob(context.Background(), "owner", "repo", 321)
			if err != nil || job.ID != 99 || job.RunAttempt != 1 {
				t.Fatalf("%+v %v", job, err)
			}
		})
	}
}
func TestJobLogsRefreshExpiredURLsAndStripAuthAtStorage(t *testing.T) {
	var downloads, redirects atomic.Int32
	storage := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Range") == "" {
			t.Error("unsafe log download headers")
		}
		if r.URL.RawQuery == "signature=old" {
			w.WriteHeader(403)
			return
		}
		fmt.Fprint(w, "start\n##[error] failed\n")
	}))
	defer storage.Close()
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing API authorization")
		}
		if strings.HasSuffix(r.URL.Path, "/logs") {
			if r.Header.Get("If-None-Match") != "" {
				t.Error("cached signed URL")
			}
			suffix := "new"
			if redirects.Add(1) == 1 {
				suffix = "old"
			}
			w.Header().Set("ETag", "log-etag")
			w.Header().Set("Location", storage.URL+"?signature="+suffix)
			w.WriteHeader(302)
		} else {
			respond(w, actionJobFixture(99, 1, "completed"))
		}
	})
	client.http = storage.Client()
	for i := 0; i < 2; i++ {
		log, err := client.GetWorkflowJobLog(context.Background(), "owner", "repo", 99)
		if err != nil || log.State != "available" || !strings.Contains(log.Text, "failed") {
			t.Fatalf("%+v %v", log, err)
		}
	}
	if redirects.Load() != 3 || downloads.Load() != 3 {
		t.Fatalf("redirects %d downloads %d", redirects.Load(), downloads.Load())
	}
}
func TestJobLogsPendingUnavailablePermissionsAndCooldown(t *testing.T) {
	for _, test := range []struct {
		name, status string
		code         int
		state        string
	}{
		{"running", "in_progress", 200, "pending"}, {"retention", "completed", 410, "unavailable"}, {"not_uploaded", "completed", 404, "unavailable"}, {"permissions", "completed", 403, ""}, {"rate_limit", "completed", 429, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logCalls atomic.Int32
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/logs") {
					logCalls.Add(1)
					if test.code == 429 {
						w.Header().Set("Retry-After", "60")
					}
					w.WriteHeader(test.code)
					respond(w, map[string]string{"message": "denied test-secret"})
				} else {
					respond(w, actionJobFixture(99, 1, test.status))
				}
			})
			log, err := client.GetWorkflowJobLog(context.Background(), "owner", "repo", 99)
			if test.state != "" {
				if err != nil || log.State != test.state {
					t.Fatalf("%+v %v", log, err)
				}
			} else {
				if err == nil || strings.Contains(err.Error(), "test-secret") {
					t.Fatalf("%v", err)
				}
				if test.code == 403 && !strings.Contains(err.Error(), "Actions read") {
					t.Fatal(err)
				}
				if test.code == 429 {
					_, err = client.GetWorkflowJobLog(context.Background(), "owner", "repo", 99)
					var api *APIError
					if !errors.As(err, &api) || api.Status != 429 || logCalls.Load() != 1 {
						t.Fatalf("no cooldown: %v %d", err, logCalls.Load())
					}
				}
			}
			if test.status != "completed" && logCalls.Load() != 0 {
				t.Error("downloaded active job")
			}
		})
	}
}
func TestJobLogCancellationUnsafeRedirectsAndTransportErrors(t *testing.T) {
	for _, target := range []string{"http://evil.example/log?secret=signed", "https://user:pass@evil.example/log", "file:///log", ""} {
		client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/logs") {
				w.Header().Set("Location", target)
				w.WriteHeader(302)
			} else {
				respond(w, actionJobFixture(99, 1, "completed"))
			}
		})
		_, err := client.GetWorkflowJobLog(context.Background(), "owner", "repo", 99)
		if err == nil || strings.Contains(err.Error(), "signed") || strings.Contains(err.Error(), "pass@") {
			t.Fatalf("unsafe redirect %q: %v", target, err)
		}
	}
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := client.GetWorkflowJobLog(ctx, "owner", "repo", 99)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestJobLogsWaitBrieflyForNewArchiveButNotDeletedLogs(t *testing.T) {
	for _, status := range []int{404, 410} {
		client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/logs") {
				w.WriteHeader(status)
				return
			}
			job := actionJobFixture(99, 1, "completed")
			job["completed_at"] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
			respond(w, job)
		})
		log, err := client.GetWorkflowJobLog(context.Background(), "owner", "repo", 99)
		if err != nil || (log.State == "pending") != (status == 404) {
			t.Fatalf("status %d: %+v %v", status, log, err)
		}
	}
}
func TestReadJobLogKeepsBoundedFailureTailAndRangeMetadata(t *testing.T) {
	data := strings.Repeat("line\n", jobLogLimit/5+100) + "FINAL FAILURE\n"
	response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(data))}
	log, truncated, tail, err := readJobLog(response)
	if err != nil || !truncated || !tail || len(log) > jobLogLimit || !strings.HasSuffix(log, "FINAL FAILURE\n") {
		t.Fatalf("length %d %t %t %v", len(log), truncated, tail, err)
	}
	response = &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": {"bytes 100-112/113"}}, Body: io.NopCloser(strings.NewReader("partial\nend\n"))}
	log, truncated, tail, err = readJobLog(response)
	if err != nil || !truncated || !tail || log != "end\n" {
		t.Fatalf("%q %t %t %v", log, truncated, tail, err)
	}
	response = &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": {"bytes 0-4/100"}}, Body: io.NopCloser(strings.NewReader("start"))}
	_, truncated, tail, err = readJobLog(response)
	if err != nil || tail || !truncated {
		t.Fatal("claimed incomplete range was the final output")
	}
}
func TestWorkflowMutationsValidateStateAttemptAndJobOwnership(t *testing.T) {
	for _, test := range []struct {
		action, status      string
		attempt, jobAttempt int
		jobRun              int64
		ok                  bool
	}{
		{"cancel", "in_progress", 2, 2, 12, true}, {"rerun", "completed", 2, 2, 12, true}, {"rerun-failed", "completed", 2, 2, 12, true}, {"rerun-job", "completed", 2, 2, 12, true},
		{"rerun", "completed", 3, 2, 12, false}, {"rerun", "in_progress", 2, 2, 12, false}, {"cancel", "completed", 2, 2, 12, false}, {"rerun-job", "completed", 2, 1, 12, false}, {"rerun-job", "completed", 2, 2, 13, false}, {"invalid", "completed", 2, 2, 12, false},
	} {
		t.Run(fmt.Sprintf("%s-%s-%d-%d-%d", test.action, test.status, test.attempt, test.jobAttempt, test.jobRun), func(t *testing.T) {
			var writes atomic.Int32
			client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					writes.Add(1)
					expected := map[string]string{"cancel": "runs/12/cancel", "rerun": "runs/12/rerun", "rerun-failed": "runs/12/rerun-failed-jobs", "rerun-job": "jobs/99/rerun"}[test.action]
					if !strings.HasSuffix(r.URL.Path, expected) {
						t.Error(r.URL.Path)
					}
					w.WriteHeader(204)
					return
				}
				if strings.Contains(r.URL.Path, "/jobs/") {
					job := actionJobFixture(99, test.jobAttempt, "completed")
					job["run_id"] = test.jobRun
					respond(w, job)
				} else {
					respond(w, actionRunFixture(test.attempt, test.status))
				}
			})
			err := client.MutateWorkflowRun(context.Background(), "owner", "repo", WorkflowMutation{RunID: 12, ExpectedAttempt: 2, JobID: 99, Action: test.action})
			if (err == nil) != test.ok || (writes.Load() == 1) != test.ok {
				t.Fatalf("ok=%t writes=%d err=%v", test.ok, writes.Load(), err)
			}
		})
	}
}
func TestDispatchReadsAndValidatesTypedInputsBeforePosting(t *testing.T) {
	definition := `name: Tests
on:
  workflow_dispatch:
    inputs:
      target:
        type: choice
        required: true
        options: [linux, macos]
      debug:
        type: boolean
        default: false
      count:
        type: number
        default: 2
`
	var writes atomic.Int32
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/actions/workflows/4":
			respond(w, map[string]any{"id": 4, "name": "Tests", "state": "active", "path": ".github/workflows/test.yml"})
		case "/repos/owner/repo":
			respond(w, map[string]string{"default_branch": "main"})
		case "/repos/owner/repo/contents/.github/workflows/test.yml":
			if ref := r.URL.Query().Get("ref"); ref != "main" && ref != "feature" {
				t.Error(ref)
			}
			respond(w, map[string]any{"content": base64.StdEncoding.EncodeToString([]byte(definition)), "encoding": "base64", "size": len(definition)})
		case "/repos/owner/repo/actions/workflows/4/dispatches":
			writes.Add(1)
			var body struct {
				Ref    string
				Inputs map[string]string
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Ref != "feature" || body.Inputs["debug"] != "false" || body.Inputs["count"] != "2" {
				t.Errorf("%+v %v", body, err)
			}
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected %s", r.URL)
			w.WriteHeader(404)
		}
	})
	config, err := client.GetWorkflowDispatch(context.Background(), "owner", "repo", 4, "")
	if err != nil || !config.Enabled || config.Ref != "main" || len(config.Inputs) != 3 {
		t.Fatalf("%+v %v", config, err)
	}
	for _, values := range []map[string]string{{"target": "windows"}, {"target": "linux", "debug": "yes"}, {"target": "linux", "count": "NaN"}, {"target": "linux", "unknown": "1"}, {}} {
		err := client.DispatchWorkflow(context.Background(), "owner", "repo", WorkflowDispatchRequest{WorkflowID: 4, Ref: "feature", Inputs: values})
		if err == nil || writes.Load() != 0 {
			t.Fatalf("invalid dispatch %v: %v", values, err)
		}
	}
	err = client.DispatchWorkflow(context.Background(), "owner", "repo", WorkflowDispatchRequest{WorkflowID: 4, Ref: "feature", Inputs: map[string]string{"target": "linux"}})
	if err != nil || writes.Load() != 1 {
		t.Fatal(err, writes.Load())
	}
	for _, definition := range []string{"on: workflow_dispatch", "on: [push, workflow_dispatch]", "on:\n  push:\n  workflow_dispatch:"} {
		enabled, _, err := parseDispatchInputs([]byte(definition))
		if err != nil || !enabled {
			t.Fatal(definition, err)
		}
	}
	enabled, _, err := parseDispatchInputs([]byte("on: push"))
	if err != nil || enabled {
		t.Fatal(err)
	}
}
