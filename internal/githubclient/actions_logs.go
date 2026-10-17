package githubclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const jobLogLimit = 2 * 1024 * 1024
const jobLogTransferLimit = 64 * 1024 * 1024

type WorkflowJobLog struct {
	Job       WorkflowJob `json:"job"`
	State     string      `json:"state"` // available, pending, unavailable
	Message   string      `json:"message"`
	Text      string      `json:"text"`
	Truncated bool        `json:"truncated"`
	Tail      bool        `json:"tail"`
}

func (c *Client) GetWorkflowJobLog(ctx context.Context, owner, repo string, jobID int64) (WorkflowJobLog, error) {
	result := WorkflowJobLog{}
	job, err := c.getWorkflowJob(ctx, owner, repo, jobID)
	if err != nil {
		return result, err
	}
	result.Job = job
	if job.Status != "completed" {
		result.State = "pending"
		result.Message = "GitHub publishes downloadable job logs when this job completes. Steps update automatically while it runs."
		return result, nil
	}
	if job.Conclusion == "skipped" {
		result.State = "unavailable"
		result.Message = "This job was skipped and has no logs."
		return result, nil
	}
	path, _ := actionPath(owner, repo, "jobs", jobID)
	path += "/logs"
	// Log download URLs expire after one minute. Never cache a redirect; acquire
	// a fresh URL, and retry once if storage says the signed URL has expired.
	for attempt := 0; attempt < 2; attempt++ {
		response, err := c.jobLogResponse(ctx, c.base+path, true, false)
		if err != nil {
			return result, err
		}
		if response.StatusCode == http.StatusFound || response.StatusCode == http.StatusTemporaryRedirect {
			location := response.Header.Get("Location")
			response.Body.Close()
			target, err := safeLogLocation(location, c.base+path)
			if err != nil {
				return result, err
			}
			response, err = c.followJobLog(ctx, target, true)
			if err != nil {
				return result, err
			}
			if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
				response.Body.Close()
				if attempt == 0 {
					continue
				}
				return result, errors.New("The GitHub log download expired or was denied. Retry to request a new download")
			}
			if response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
				response.Body.Close()
				response, err = c.followJobLog(ctx, target, false)
				if err != nil {
					return result, err
				}
			}
		} else if response.StatusCode < 200 || response.StatusCode >= 300 {
			data, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
			response.Body.Close()
			if response.StatusCode == 404 || response.StatusCode == 410 {
				result.State = "unavailable"
				result.Message = "GitHub has no archived logs for this job. They may still be uploading, have expired under repository retention, or have been deleted. Retry or open the job on GitHub."
				if completed, err := time.Parse(time.RFC3339, job.CompletedAt); response.StatusCode == 404 && err == nil && time.Since(completed) >= 0 && time.Since(completed) < 2*time.Minute {
					result.State = "pending"
					result.Message = "This job just completed. GitHub is still publishing its log archive; retrying automatically."
				}
				return result, nil
			}
			return result, actionsError(c.responseError(response, data))
		}
		if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
			response.Body.Close()
			return result, fmt.Errorf("GitHub log storage returned HTTP %d. Retry or open this job on GitHub", response.StatusCode)
		}
		text, truncated, tail, err := readJobLog(response)
		response.Body.Close()
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, errors.New("The job log download was interrupted. Retry to load it again")
		}
		result.State = "available"
		result.Text = text
		result.Truncated = truncated
		result.Tail = tail
		return result, nil
	}
	return result, errors.New("Unable to download GitHub job logs")
}

func (c *Client) jobLogResponse(ctx context.Context, endpoint string, authenticated, ranged bool) (*http.Response, error) {
	return c.downloadResponse(ctx, endpoint, authenticated, ranged, 0)
}
func (c *Client) downloadResponse(ctx context.Context, endpoint string, authenticated, ranged bool, timeout time.Duration) (*http.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if authenticated {
		c.mu.Lock()
		until := c.blockedUntil
		c.mu.Unlock()
		if time.Now().Before(until) {
			return nil, &APIError{Status: 429, Message: "GitHub rate limit is cooling down", RetryAt: until.UTC().Format(time.RFC3339)}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("GitHub returned an invalid log download address")
	}
	req.Header.Set("User-Agent", "M.I.E.R./0.1")
	if authenticated {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
	}
	if ranged {
		req.Header.Set("Range", fmt.Sprintf("bytes=-%d", jobLogLimit))
	}
	// Isolate redirects and cookies, including when a caller injects a transport.
	client := *c.http
	if timeout > 0 {
		client.Timeout = timeout
	}
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Unable to reach GitHub log storage. Check your connection and retry")
	}
	return response, nil
}
func safeLogLocation(location, previous string) (string, error) {
	base, _ := url.Parse(previous)
	u, err := url.Parse(location)
	if err != nil || base == nil || location == "" {
		return "", errors.New("GitHub returned an invalid log download address")
	}
	u = base.ResolveReference(u)
	if u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return "", errors.New("GitHub returned an unsafe log download address")
	}
	return u.String(), nil
}
func (c *Client) followJobLog(ctx context.Context, target string, ranged bool) (*http.Response, error) {
	return c.followDownload(ctx, target, ranged, 0)
}
func (c *Client) followDownload(ctx context.Context, target string, ranged bool, timeout time.Duration) (*http.Response, error) {
	for redirects := 0; redirects < 4; redirects++ {
		response, err := c.downloadResponse(ctx, target, false, ranged, timeout)
		if err != nil {
			return nil, err
		}
		switch response.StatusCode {
		case 301, 302, 303, 307, 308:
			location := response.Header.Get("Location")
			response.Body.Close()
			target, err = safeLogLocation(location, target)
			if err != nil {
				return nil, err
			}
		default:
			return response, nil
		}
	}
	return nil, errors.New("GitHub log storage redirected too many times. Retry or open this job on GitHub")
}
func readJobLog(response *http.Response) (string, bool, bool, error) {
	// Retain a bounded tail so failures at the end of long logs stay visible.
	ring := make([]byte, jobLogLimit)
	count, pos := 0, 0
	reader := io.LimitReader(response.Body, jobLogTransferLimit+1)
	chunk := make([]byte, 32*1024)
	for {
		n, err := reader.Read(chunk)
		if n > 0 {
			first := min(n, len(ring)-pos)
			copy(ring[pos:], chunk[:first])
			copy(ring, chunk[first:n])
			pos = (pos + n) % len(ring)
			count += n
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false, false, err
		}
	}
	partial := count > jobLogTransferLimit
	truncated := count > len(ring)
	data := ring[:min(count, len(ring))]
	if truncated {
		data = append(append([]byte{}, ring[pos:]...), ring[:pos]...)
	}
	if response.StatusCode == http.StatusPartialContent {
		var start, end, total int64
		if _, err := fmt.Sscanf(response.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); err == nil {
			truncated = truncated || start > 0
			partial = partial || end+1 < total || start < 0 || end < start || end >= total
		} else {
			partial = true
		}
	}
	truncated = truncated || partial
	if truncated {
		if newline := strings.IndexByte(string(data), '\n'); newline >= 0 {
			data = data[newline+1:]
		}
	}
	return strings.ToValidUTF8(string(data), "�"), truncated, !partial, nil
}
