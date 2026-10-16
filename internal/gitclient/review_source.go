package gitclient

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

type PullRequestFetchSource struct {
	HeadURL string
	HeadRef string
	HeadSHA string
	BaseURL string
	BaseRef string
}

func (c *Client) pullRequestSource(ctx context.Context, root string, remote selectedRemote, number int) (PullRequestFetchSource, error) {
	source := PullRequestFetchSource{HeadURL: remote.fetchSource, HeadRef: "refs/pull/" + strconv.Itoa(number) + "/head", BaseURL: remote.fetchSource}
	if c.options.ReviewSourceResolver != nil && remote.Provider != "github" && remote.Provider != "unknown" {
		resolved, err := c.options.ReviewSourceResolver(ctx, remote.Host, remote.Owner, remote.Repository, number)
		if err != nil {
			return source, err
		}
		if resolved.HeadURL != "" {
			source.HeadURL = resolved.HeadURL
		}
		if resolved.BaseURL != "" {
			source.BaseURL = resolved.BaseURL
		}
		source.HeadRef = resolved.HeadRef
		source.BaseRef = resolved.BaseRef
		source.HeadSHA = resolved.HeadSHA
	} else if remote.Provider == "gitlab" {
		source.HeadRef = "refs/merge-requests/" + strconv.Itoa(number) + "/head"
	}
	for _, value := range []string{source.HeadURL, source.BaseURL} {
		if err := validateRemoteURL(value); err != nil {
			return source, err
		}
	}
	for _, ref := range []string{source.HeadRef, source.BaseRef} {
		if ref == "" {
			continue
		}
		if !strings.HasPrefix(ref, "refs/") {
			return source, errors.New("The provider returned an unsupported Git ref")
		}
		if _, err := c.run(ctx, root, c.options.LocalTimeout, "check-ref-format", ref); err != nil {
			return source, errors.New("The provider returned an invalid branch ref")
		}
	}
	if source.HeadSHA != "" && !objectID.MatchString(source.HeadSHA) {
		return source, errors.New("The provider returned an invalid head commit")
	}
	return source, nil
}
