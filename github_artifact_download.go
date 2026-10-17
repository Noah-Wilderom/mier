package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/wailsapp/wails/v3/pkg/application"
	"os"
	"path/filepath"
	"time"
)

type ArtifactDownloadRequest struct {
	RunID      int64  `json:"runId"`
	ArtifactID int64  `json:"artifactId"`
	HeadSHA    string `json:"headSha"`
}

func (s *GitHubService) DownloadWorkflowArtifact(ctx context.Context, host, owner, repo string, request ArtifactDownloadRequest) (string, error) {
	app := application.Get()
	if app == nil {
		return "", errors.New("The native save dialog is unavailable")
	}
	client, err := s.client(ctx, host)
	if err != nil {
		return "", err
	}
	if _, err = client.WorkflowArtifact(ctx, owner, repo, request.ArtifactID, request.RunID, request.HeadSHA); err != nil {
		return "", err
	}
	path, err := app.Dialog.SaveFile().SetMessage("Save workflow artifact").SetFilename(fmt.Sprintf("artifact-%d.zip", request.ArtifactID)).AddFilter("ZIP archive", "*.zip").PromptForSingleSelection()
	if err != nil || path == "" {
		return "", err
	}
	// The dialog can remain open; acquire fresh metadata and a download URL after
	// the user chooses a destination, then stream into a private temporary file.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	file, err := os.CreateTemp(filepath.Dir(path), ".mier-artifact-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = file.Chmod(0600); err != nil {
		return "", err
	}
	if err = client.DownloadWorkflowArtifact(ctx, owner, repo, request.ArtifactID, request.RunID, request.HeadSHA, file); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}
