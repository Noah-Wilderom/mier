package main

import (
	"context"
	"errors"
	"mag-ik-een-review/internal/reviewproviders"
)

func (s *GitHubService) reviewClient(ctx context.Context, input string) (reviewproviders.API, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.credentials == nil {
		return nil, errors.New("Connect a review account first")
	}
	if s.credentials.providers != nil && s.credentials.providers.Kind(input) != "" {
		return s.credentials.providers.client(input)
	}
	return s.client(ctx, input)
}
