package githubclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type CheckAnnotation struct {
	Path        string `json:"path"`
	StartLine   int    `json:"startLine"`
	EndLine     int    `json:"endLine"`
	StartColumn int    `json:"startColumn"`
	EndColumn   int    `json:"endColumn"`
	Level       string `json:"annotationLevel"`
	Message     string `json:"message"`
	Title       string `json:"title"`
	RawDetails  string `json:"rawDetails"`
}

func (v *CheckAnnotation) UnmarshalJSON(data []byte) error {
	type plain CheckAnnotation
	return decodeCamel(data, (*plain)(v))
}

type CheckAnnotations struct {
	Items   []CheckAnnotation `json:"items"`
	HasMore bool              `json:"hasMore"`
	HeadSHA string            `json:"headSha"`
}

func (c *Client) GetCheckAnnotations(ctx context.Context, owner, repo string, id int64, head string, page int) (CheckAnnotations, error) {
	result := CheckAnnotations{Items: []CheckAnnotation{}}
	base, err := repoPath(owner, repo)
	if err != nil {
		return result, err
	}
	if id < 1 || !immutableCommit.MatchString(strings.ToLower(head)) || page < 1 || page > 100 {
		return result, errors.New("Choose a check, immutable commit and annotation page")
	}
	path := base + "/check-runs/" + strconv.FormatInt(id, 10)
	var check struct {
		Head string `json:"head_sha"`
	}
	if _, err = c.request(ctx, http.MethodGet, path, nil, &check); err != nil {
		return result, actionsError(err)
	}
	if !strings.EqualFold(head, check.Head) {
		return result, errors.New("These annotations belong to a different commit. Refresh checks before opening their code")
	}
	result.HeadSHA = check.Head
	headers, err := c.request(ctx, http.MethodGet, fmt.Sprintf("%s/annotations?per_page=100&page=%d", path, page), nil, &result.Items)
	if err != nil {
		return result, actionsError(err)
	}
	result.HasMore = strings.Contains(headers.Get("Link"), `rel="next"`)
	valid := result.Items[:0]
	for _, item := range result.Items {
		if item.Path != "" && item.StartLine > 0 && item.EndLine >= item.StartLine {
			valid = append(valid, item)
		}
	}
	result.Items = valid
	return result, nil
}

type WorkflowArtifact struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"sizeInBytes"`
	Expired     bool   `json:"expired"`
	CreatedAt   string `json:"createdAt"`
	ExpiresAt   string `json:"expiresAt"`
	Digest      string `json:"digest"`
	WorkflowRun struct {
		ID      int64  `json:"id"`
		HeadSHA string `json:"headSha"`
	} `json:"workflowRun"`
}

func (v *WorkflowArtifact) UnmarshalJSON(data []byte) error {
	type plain WorkflowArtifact
	return decodeCamel(data, (*plain)(v))
}

type WorkflowArtifacts struct {
	Items   []WorkflowArtifact `json:"items"`
	HasMore bool               `json:"hasMore"`
}

func (c *Client) ListWorkflowArtifacts(ctx context.Context, owner, repo string, run int64, page int) (WorkflowArtifacts, error) {
	result := WorkflowArtifacts{Items: []WorkflowArtifact{}}
	path, err := actionPath(owner, repo, "runs", run)
	if err != nil {
		return result, err
	}
	if page < 1 || page > 100 {
		return result, errors.New("Choose a valid artifacts page")
	}
	var wire struct {
		Artifacts []WorkflowArtifact `json:"artifacts"`
	}
	headers, err := c.request(ctx, http.MethodGet, fmt.Sprintf("%s/artifacts?per_page=100&page=%d", path, page), nil, &wire)
	if err != nil {
		return result, actionsError(err)
	}
	result.Items = wire.Artifacts
	result.HasMore = strings.Contains(headers.Get("Link"), `rel="next"`)
	return result, nil
}

const artifactLimit int64 = 512 * 1024 * 1024

func (c *Client) WorkflowArtifact(ctx context.Context, owner, repo string, id, run int64, head string) (WorkflowArtifact, error) {
	var artifact WorkflowArtifact
	path, err := actionPath(owner, repo, "artifacts", id)
	if err != nil {
		return artifact, err
	}
	if _, err = c.request(ctx, http.MethodGet, path, nil, &artifact); err != nil {
		return artifact, actionsError(err)
	}
	if run <= 0 || artifact.ID != id || artifact.WorkflowRun.ID != run || !immutableCommit.MatchString(strings.ToLower(head)) || !strings.EqualFold(artifact.WorkflowRun.HeadSHA, head) {
		return artifact, errors.New("The artifact does not belong to the selected workflow commit")
	}
	if artifact.Expired {
		return artifact, errors.New("This artifact has expired under GitHub's retention policy")
	}
	if artifact.Size < 0 || artifact.Size > artifactLimit {
		return artifact, errors.New("Artifact downloads support archives of at most 512 MiB")
	}
	return artifact, nil
}

// Stream into a caller-owned temporary file. Only the GitHub API receives the
// token. Short-lived storage redirects never receive credentials or cookies.
func (c *Client) DownloadWorkflowArtifact(ctx context.Context, owner, repo string, id, run int64, head string, writer io.Writer) error {
	artifact, err := c.WorkflowArtifact(ctx, owner, repo, id, run, head)
	if err != nil {
		return err
	}
	path, _ := actionPath(owner, repo, "artifacts", id)
	response, err := c.downloadResponse(ctx, c.base+path+"/zip", true, false, 5*time.Minute)
	if err != nil {
		return err
	}
	if response.StatusCode == 302 || response.StatusCode == 307 {
		location := response.Header.Get("Location")
		response.Body.Close()
		target, err := safeLogLocation(location, c.base+path)
		if err != nil {
			return err
		}
		response, err = c.followDownload(ctx, target, false, 5*time.Minute)
		if err != nil {
			return err
		}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Artifact download returned HTTP %d. Refresh the run and retry", response.StatusCode)
	}
	if response.ContentLength > artifactLimit {
		return errors.New("Artifact archive exceeds the 512 MiB download limit")
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(writer, hash), io.LimitReader(response.Body, artifactLimit+1))
	if err != nil {
		return err
	}
	if written > artifactLimit {
		return errors.New("Artifact archive exceeds the 512 MiB download limit")
	}
	if response.ContentLength >= 0 && written != response.ContentLength {
		return errors.New("The artifact download was interrupted")
	}
	if strings.HasPrefix(artifact.Digest, "sha256:") && strings.TrimPrefix(artifact.Digest, "sha256:") != hex.EncodeToString(hash.Sum(nil)) {
		return errors.New("The downloaded artifact did not match GitHub's SHA-256 digest")
	}
	return nil
}
