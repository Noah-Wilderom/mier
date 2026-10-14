package githubclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Workflow struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
	URL   string `json:"url"`
}

func (v *Workflow) UnmarshalJSON(data []byte) error {
	type plain Workflow
	return decodeCamel(data, (*plain)(v))
}

type WorkflowRun struct {
	ID           int64  `json:"id"`
	WorkflowID   int64  `json:"workflowId"`
	Name         string `json:"name"`
	DisplayTitle string `json:"displayTitle"`
	RunNumber    int    `json:"runNumber"`
	RunAttempt   int    `json:"runAttempt"`
	HeadBranch   string `json:"headBranch"`
	HeadSHA      string `json:"headSha"`
	Event        string `json:"event"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	RunStartedAt string `json:"runStartedAt"`
	URL          string `json:"url"`
	Actor        User   `json:"actor"`
}

func (v *WorkflowRun) UnmarshalJSON(data []byte) error {
	type plain WorkflowRun
	return decodeCamel(data, (*plain)(v))
}

type WorkflowStep struct {
	Name        string `json:"name"`
	Number      int    `json:"number"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt"`
}
type WorkflowJob struct {
	ID          int64          `json:"id"`
	RunID       int64          `json:"runId"`
	RunAttempt  int            `json:"runAttempt"`
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	Conclusion  string         `json:"conclusion"`
	StartedAt   string         `json:"startedAt"`
	CompletedAt string         `json:"completedAt"`
	URL         string         `json:"url"`
	CheckRunURL string         `json:"checkRunUrl"`
	RunnerName  string         `json:"runnerName"`
	Steps       []WorkflowStep `json:"steps"`
}

func (v *WorkflowJob) UnmarshalJSON(data []byte) error {
	type plain WorkflowJob
	return decodeCamel(data, (*plain)(v))
}

type WorkflowRunsRequest struct {
	WorkflowID int64  `json:"workflowId"`
	Branch     string `json:"branch"`
	Status     string `json:"status"`
	Page       int    `json:"page"`
}
type WorkflowRunsPage struct {
	Items      []WorkflowRun `json:"items"`
	TotalCount int           `json:"totalCount"`
	HasMore    bool          `json:"hasMore"`
}
type WorkflowRunDetail struct {
	Run  WorkflowRun   `json:"run"`
	Jobs []WorkflowJob `json:"jobs"`
}
type WorkflowMutation struct {
	RunID           int64  `json:"runId"`
	ExpectedAttempt int    `json:"expectedAttempt"`
	JobID           int64  `json:"jobId"`
	Action          string `json:"action"`
}

func actionPath(owner, repo, resource string, id int64) (string, error) {
	base, err := repoPath(owner, repo)
	if err != nil {
		return "", err
	}
	if id < 1 {
		return "", errors.New("Choose a valid GitHub Actions identifier")
	}
	return base + "/actions/" + resource + "/" + strconv.FormatInt(id, 10), nil
}
func actionsError(err error) error {
	var api *APIError
	if errors.As(err, &api) {
		copy := *api
		// The common client supplies PR guidance. Actions has different permissions.
		if at := strings.Index(copy.Message, ". Check repository access,"); at >= 0 {
			copy.Message = copy.Message[:at]
		}
		if api.Status == 403 || api.Status == 404 {
			copy.Message += ". Check Actions access, repository access and SSO. Fine-grained tokens need Actions read to view runs/logs, Actions write to rerun, cancel and dispatch, and Contents read for workflow inputs; classic tokens need repo for private repositories"
		}
		if api.Status == 409 || api.Status == 422 {
			if at := strings.Index(copy.Message, ". The pull request head changed;"); at >= 0 {
				copy.Message = copy.Message[:at]
			}
			if at := strings.Index(copy.Message, ". Check branch names, diff line locations,"); at >= 0 {
				copy.Message = copy.Message[:at]
			}
			copy.Message += ". Refresh the workflow run and check its state, selected ref and declared inputs"
		}
		return &copy
	}
	return err
}

func (c *Client) ListWorkflows(ctx context.Context, owner, repo string) ([]Workflow, error) {
	base, err := repoPath(owner, repo)
	if err != nil {
		return nil, err
	}
	items := []Workflow{}
	for page := 1; page <= 100; page++ {
		var result struct {
			Workflows []Workflow `json:"workflows"`
		}
		headers, err := c.request(ctx, http.MethodGet, fmt.Sprintf("%s/actions/workflows?per_page=100&page=%d", base, page), nil, &result)
		if err != nil {
			return nil, actionsError(err)
		}
		items = append(items, result.Workflows...)
		if !strings.Contains(headers.Get("Link"), `rel="next"`) {
			return items, nil
		}
	}
	return nil, errors.New("Too many workflows to load; open this repository on GitHub")
}
func (c *Client) ListWorkflowRuns(ctx context.Context, owner, repo string, input WorkflowRunsRequest) (WorkflowRunsPage, error) {
	result := WorkflowRunsPage{Items: []WorkflowRun{}}
	base, err := repoPath(owner, repo)
	if err != nil {
		return result, err
	}
	path := base + "/actions/runs"
	if input.WorkflowID != 0 {
		path, err = actionPath(owner, repo, "workflows", input.WorkflowID)
		if err != nil {
			return result, err
		}
		path += "/runs"
	}
	if input.Page < 1 {
		input.Page = 1
	}
	if input.Page > 1000 {
		return result, errors.New("Narrow the workflow or branch filter to load more runs")
	}
	query := url.Values{"per_page": {"30"}, "page": {strconv.Itoa(input.Page)}}
	if len(input.Branch) > 1024 {
		return result, errors.New("Branch name is too long")
	}
	if branch := strings.TrimSpace(input.Branch); branch != "" {
		query.Set("branch", branch)
	}
	if input.Status != "" {
		switch input.Status {
		case "queued", "in_progress", "completed", "success", "failure", "cancelled", "waiting", "action_required", "timed_out", "skipped", "neutral", "pending", "requested":
		default:
			return result, errors.New("Choose a valid Actions status filter")
		}
		query.Set("status", input.Status)
	}
	var wire struct {
		TotalCount int           `json:"total_count"`
		Runs       []WorkflowRun `json:"workflow_runs"`
	}
	headers, err := c.request(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &wire)
	if err != nil {
		return result, actionsError(err)
	}
	if wire.Runs != nil {
		result.Items = wire.Runs
	}
	result.TotalCount = wire.TotalCount
	result.HasMore = strings.Contains(headers.Get("Link"), `rel="next"`)
	return result, nil
}
func (c *Client) getWorkflowRun(ctx context.Context, owner, repo string, id int64, attempt int) (WorkflowRun, error) {
	var run WorkflowRun
	path, err := actionPath(owner, repo, "runs", id)
	if err != nil {
		return run, err
	}
	if attempt < 0 {
		return run, errors.New("Choose a valid run attempt")
	}
	if attempt > 0 {
		path += "/attempts/" + strconv.Itoa(attempt)
	}
	_, err = c.request(ctx, http.MethodGet, path, nil, &run)
	return run, actionsError(err)
}
func (c *Client) GetWorkflowRun(ctx context.Context, owner, repo string, id int64, attempt int) (WorkflowRunDetail, error) {
	result := WorkflowRunDetail{Jobs: []WorkflowJob{}}
	run, err := c.getWorkflowRun(ctx, owner, repo, id, attempt)
	if err != nil {
		return result, err
	}
	result.Run = run
	if attempt == 0 {
		attempt = run.RunAttempt
	}
	if attempt < 1 {
		attempt = 1
	}
	base, _ := actionPath(owner, repo, "runs", id)
	for page := 1; page <= 100; page++ {
		var wire struct {
			Jobs []WorkflowJob `json:"jobs"`
		}
		headers, err := c.request(ctx, http.MethodGet, fmt.Sprintf("%s/attempts/%d/jobs?per_page=100&page=%d", base, attempt, page), nil, &wire)
		if err != nil {
			return result, actionsError(err)
		}
		result.Jobs = append(result.Jobs, wire.Jobs...)
		if !strings.Contains(headers.Get("Link"), `rel="next"`) {
			return result, nil
		}
	}
	return result, errors.New("Too many jobs to load; open this run on GitHub")
}
func (c *Client) getWorkflowJob(ctx context.Context, owner, repo string, id int64) (WorkflowJob, error) {
	var job WorkflowJob
	path, err := actionPath(owner, repo, "jobs", id)
	if err != nil {
		return job, err
	}
	_, err = c.request(ctx, http.MethodGet, path, nil, &job)
	return job, actionsError(err)
}

// Check IDs and Actions job IDs are different. Resolve and verify the real job,
// preserving the original attempt when a check belongs to an earlier rerun.
func (c *Client) GetCheckWorkflowJob(ctx context.Context, owner, repo string, checkID int64) (*WorkflowJob, error) {
	base, err := repoPath(owner, repo)
	if err != nil {
		return nil, err
	}
	if checkID < 1 {
		return nil, errors.New("Choose a valid check identifier")
	}
	var check CheckRun
	_, err = c.request(ctx, http.MethodGet, base+"/check-runs/"+strconv.FormatInt(checkID, 10), nil, &check)
	if err != nil {
		return nil, actionsError(err)
	}
	if check.App.Slug != "github-actions" {
		return nil, nil
	}
	if u, err := url.Parse(check.DetailsURL); err == nil && u.Scheme == "https" && u.User == nil && strings.EqualFold(u.Host, c.host) {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 7 && strings.EqualFold(parts[0], owner) && strings.EqualFold(parts[1], repo) && parts[2] == "actions" && parts[3] == "runs" && parts[5] == "job" {
			runID, _ := strconv.ParseInt(parts[4], 10, 64)
			jobID, _ := strconv.ParseInt(parts[6], 10, 64)
			if runID > 0 && jobID > 0 {
				job, err := c.getWorkflowJob(ctx, owner, repo, jobID)
				if err == nil && job.RunID == runID && checkURLID(job.CheckRunURL) == checkID {
					return &job, nil
				}
				if err != nil && !permissionError(err) {
					return nil, err
				}
			}
		}
	}
	if check.CheckSuite.ID < 1 {
		return nil, errors.New("GitHub has not attached an Actions job to this check yet. Retry or open the check on GitHub")
	}
	// The check suite selects the workflow even when third-party checks share its SHA.
	var runs struct {
		Runs []WorkflowRun `json:"workflow_runs"`
	}
	_, err = c.request(ctx, http.MethodGet, fmt.Sprintf("%s/actions/runs?check_suite_id=%d&per_page=100", base, check.CheckSuite.ID), nil, &runs)
	if err != nil {
		return nil, actionsError(err)
	}
	for _, run := range runs.Runs {
		for page := 1; page <= 100; page++ {
			var wire struct {
				Jobs []WorkflowJob `json:"jobs"`
			}
			headers, err := c.request(ctx, http.MethodGet, fmt.Sprintf("%s/actions/runs/%d/jobs?filter=all&per_page=100&page=%d", base, run.ID, page), nil, &wire)
			if err != nil {
				return nil, actionsError(err)
			}
			for _, job := range wire.Jobs {
				if checkURLID(job.CheckRunURL) == checkID {
					return &job, nil
				}
			}
			if !strings.Contains(headers.Get("Link"), `rel="next"`) {
				break
			}
		}
	}
	return nil, errors.New("The Actions job is not available yet. Retry or open the check on GitHub")
}
func checkURLID(value string) int64 {
	u, err := url.Parse(value)
	if err != nil {
		return 0
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[len(parts)-2] != "check-runs" {
		return 0
	}
	id, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	return id
}

func (c *Client) MutateWorkflowRun(ctx context.Context, owner, repo string, input WorkflowMutation) error {
	switch input.Action {
	case "rerun", "rerun-failed", "rerun-job", "cancel":
	default:
		return errors.New("Choose a valid workflow action")
	}
	if input.ExpectedAttempt < 1 {
		return errors.New("Refresh the run before changing it")
	}
	run, err := c.getWorkflowRun(ctx, owner, repo, input.RunID, 0)
	if err != nil {
		return err
	}
	if run.RunAttempt != input.ExpectedAttempt {
		return errors.New("This run has a newer attempt. Refresh before changing it")
	}
	path, _ := actionPath(owner, repo, "runs", input.RunID)
	if input.Action == "cancel" {
		if run.Status == "completed" {
			return errors.New("This run has already completed. Refresh its results")
		}
		path += "/cancel"
	} else {
		if run.Status != "completed" {
			return errors.New("Wait for the run to complete before rerunning jobs")
		}
		switch input.Action {
		case "rerun":
			path += "/rerun"
		case "rerun-failed":
			path += "/rerun-failed-jobs"
		case "rerun-job":
			job, err := c.getWorkflowJob(ctx, owner, repo, input.JobID)
			if err != nil {
				return err
			}
			if job.RunID != run.ID || job.RunAttempt != run.RunAttempt {
				return errors.New("This job belongs to another run or attempt. Refresh before rerunning it")
			}
			path, _ = actionPath(owner, repo, "jobs", job.ID)
			path += "/rerun"
		}
	}
	_, err = c.request(ctx, http.MethodPost, path, nil, nil)
	return actionsError(err)
}
