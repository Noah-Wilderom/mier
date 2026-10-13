package main

import (
	"context"
	"errors"
	"github.com/wailsapp/wails/v3/pkg/application"
	"mag-ik-een-review/internal/gitclient"
	"mag-ik-een-review/internal/lspclient"
)

type LSPServerStatus = lspclient.ServerStatus
type LSPServerConfiguration = lspclient.ServerConfiguration
type CodePositionRequest = lspclient.CodePositionRequest
type CodeHover = lspclient.Hover
type CodeDefinitionTarget = lspclient.DefinitionTarget
type ReadDefinitionRequest = lspclient.ReadDefinitionRequest
type DefinitionDocument = lspclient.DefinitionDocument
type LSPService struct{ manager *lspclient.Manager }

func NewLSPService(repositories *RepositoryService) *LSPService {
	return &LSPService{manager: lspclient.New(lspclient.Options{Archive: func(ctx context.Context, request lspclient.CodePositionRequest) (lspclient.ArchiveResult, error) {
		if repositories == nil {
			return lspclient.ArchiveResult{}, errors.New("Open a Git repository before using code intelligence")
		}
		if repositories.initErr != nil {
			return lspclient.ArchiveResult{}, repositories.initErr
		}
		var archive gitclient.CommitArchiveResult
		var err error
		if request.Revision == "working" || request.Revision == "index" {
			archive, err = repositories.client.ArchiveWorkspace(ctx, request.Root, request.Revision)
		} else {
			archive, err = repositories.client.ArchiveCommit(ctx, request.Root, gitclient.CommitArchiveRequest{Revision: request.Revision, ComparisonHead: request.ComparisonHead, Host: request.Host, Owner: request.Owner, Repo: request.Repo, PullRequest: request.PullRequest})
		}
		return lspclient.ArchiveResult{Revision: archive.Revision, Tar: archive.Data}, err
	}})}
}
func (s *LSPService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	context.AfterFunc(ctx, func() { _ = s.manager.Close() })
	return nil
}
func (s *LSPService) ServiceShutdown() error { return s.manager.Close() }
func (s *LSPService) Servers(ctx context.Context, root string) ([]LSPServerStatus, error) {
	return s.manager.Servers(ctx, root)
}
func (s *LSPService) ConfigureServer(ctx context.Context, language string, configuration LSPServerConfiguration) error {
	return s.manager.Configure(ctx, language, configuration)
}
func (s *LSPService) Hover(ctx context.Context, request CodePositionRequest) (CodeHover, error) {
	return s.manager.Hover(ctx, request)
}
func (s *LSPService) Definition(ctx context.Context, request CodePositionRequest) ([]CodeDefinitionTarget, error) {
	return s.manager.Definition(ctx, request)
}
func (s *LSPService) ReadDefinition(ctx context.Context, request ReadDefinitionRequest) (DefinitionDocument, error) {
	return s.manager.ReadDefinition(ctx, request)
}
