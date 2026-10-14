package main

import (
	"context"
	"mag-ik-een-review/internal/githubclient"
	"time"
)

func (s *GitHubService) GetWorkflowDispatch(ctx context.Context, host, owner, repo string, id int64, reference string) (githubclient.WorkflowDispatch, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.WorkflowDispatch{}, err
	}
	return client.GetWorkflowDispatch(ctx, owner, repo, id, reference)
}
func (s *GitHubService) DispatchWorkflow(ctx context.Context, host, owner, repo string, input githubclient.WorkflowDispatchRequest) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := s.client(ctx, host)
	if err != nil {
		return err
	}
	return client.DispatchWorkflow(ctx, owner, repo, input)
}

func (s *GitHubService) ListWorkflows(ctx context.Context, host, owner, repo string) ([]githubclient.Workflow, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := s.client(ctx, host)
	if err != nil {
		return nil, err
	}
	return client.ListWorkflows(ctx, owner, repo)
}
func (s *GitHubService) ListWorkflowRuns(ctx context.Context, host, owner, repo string, input githubclient.WorkflowRunsRequest) (githubclient.WorkflowRunsPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.WorkflowRunsPage{}, err
	}
	return client.ListWorkflowRuns(ctx, owner, repo, input)
}
func (s *GitHubService) GetWorkflowRun(ctx context.Context, host, owner, repo string, id int64, attempt int) (githubclient.WorkflowRunDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.WorkflowRunDetail{}, err
	}
	return client.GetWorkflowRun(ctx, owner, repo, id, attempt)
}
func (s *GitHubService) GetCheckWorkflowJob(ctx context.Context, host, owner, repo string, id int64) (*githubclient.WorkflowJob, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := s.client(ctx, host)
	if err != nil {
		return nil, err
	}
	return client.GetCheckWorkflowJob(ctx, owner, repo, id)
}
func (s *GitHubService) GetWorkflowJobLog(ctx context.Context, host, owner, repo string, id int64) (githubclient.WorkflowJobLog, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client, err := s.client(ctx, host)
	if err != nil {
		return githubclient.WorkflowJobLog{}, err
	}
	return client.GetWorkflowJobLog(ctx, owner, repo, id)
}
func (s *GitHubService) MutateWorkflowRun(ctx context.Context, host, owner, repo string, input githubclient.WorkflowMutation) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := s.client(ctx, host)
	if err != nil {
		return err
	}
	return client.MutateWorkflowRun(ctx, owner, repo, input)
}
