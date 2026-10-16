package main

import (
	"context"
	"mag-ik-een-review/internal/gitclient"
)

type RepositorySnapshot = gitclient.RepositorySnapshot
type ChangedFile = gitclient.ChangedFile

// RepositoryService exposes local Git operations to the Wails application.
type RepositoryService struct {
	client  *gitclient.Client
	initErr error
}

func NewRepositoryService(credentials ...*CredentialService) *RepositoryService {
	options := gitclient.Options{}
	if len(credentials) > 0 && credentials[0] != nil {
		options.CredentialResolver = func(ctx context.Context, host string) (string, string, error) {
			service := credentials[0]
			if service.providers != nil && service.providers.Kind(host) != "" {
				return service.providers.gitCredential(ctx, host)
			}
			_, token, _, err := service.resolveToken(ctx, host)
			return "x-access-token", token, err
		}
		if credentials[0].providers != nil {
			options.ProviderResolver = credentials[0].providers.Kind
			options.ReviewSourceResolver = credentials[0].providers.reviewSource
		}
		options.CredentialProvider = func(ctx context.Context, host string) (string, error) {
			_, token, _, err := credentials[0].resolveToken(ctx, host)
			return token, err
		}
	}
	client, err := gitclient.New(options)
	return &RepositoryService{client: client, initErr: err}
}

func (s *RepositoryService) Open(ctx context.Context, path string) (RepositorySnapshot, error) {
	return s.Snapshot(ctx, path)
}

func (s *RepositoryService) Snapshot(ctx context.Context, path string) (RepositorySnapshot, error) {
	if s.initErr != nil {
		return RepositorySnapshot{}, s.initErr
	}
	return s.client.Snapshot(ctx, path)
}

func (s *RepositoryService) Diff(ctx context.Context, path, file string, staged bool) (ChangedFile, error) {
	if s.initErr != nil {
		return ChangedFile{}, s.initErr
	}
	return s.client.Diff(ctx, path, file, staged)
}

func (s *RepositoryService) Stage(ctx context.Context, path string, files []string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.Stage(ctx, path, files)
}

func (s *RepositoryService) Unstage(ctx context.Context, path string, files []string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.Unstage(ctx, path, files)
}

func (s *RepositoryService) Commit(ctx context.Context, path, message string) (string, error) {
	if s.initErr != nil {
		return "", s.initErr
	}
	return s.client.Commit(ctx, path, message)
}

func (s *RepositoryService) Checkout(ctx context.Context, path, branch string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.Checkout(ctx, path, branch)
}

func (s *RepositoryService) CreateBranch(ctx context.Context, path, branch string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.CreateBranch(ctx, path, branch)
}

func (s *RepositoryService) CreateAndCheckoutBranch(ctx context.Context, path, branch string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.CreateAndCheckoutBranch(ctx, path, branch)
}

func (s *RepositoryService) DeleteBranch(ctx context.Context, path, branch string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.DeleteBranch(ctx, path, branch)
}

func (s *RepositoryService) RenameBranch(ctx context.Context, path, branch, name string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.RenameBranch(ctx, path, branch, name)
}

func (s *RepositoryService) Fetch(ctx context.Context, path, remote string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.Fetch(ctx, path, remote)
}

func (s *RepositoryService) Pull(ctx context.Context, path, remote string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.Pull(ctx, path, remote)
}

func (s *RepositoryService) Push(ctx context.Context, path, remote string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.Push(ctx, path, remote)
}

func (s *RepositoryService) Clone(ctx context.Context, url, directory string) (RepositorySnapshot, error) {
	if s.initErr != nil {
		return RepositorySnapshot{}, s.initErr
	}
	return s.client.Clone(ctx, url, directory)
}

func (s *RepositoryService) CheckoutPullRequest(ctx context.Context, path string, number int, remote string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.CheckoutPullRequest(ctx, path, number, remote)
}
func (s *RepositoryService) CheckoutReviewedPullRequest(ctx context.Context, path string, number int, remote, expectedHead string) error {
	if s.initErr != nil {
		return s.initErr
	}
	return s.client.CheckoutPullRequest(ctx, path, number, remote, expectedHead)
}

func (s *RepositoryService) Compare(ctx context.Context, path, base, head string) ([]ChangedFile, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.Compare(ctx, path, base, head)
}

func (s *RepositoryService) CommitDiff(ctx context.Context, path, revision string) ([]ChangedFile, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.client.CommitDiff(ctx, path, revision)
}
