package main

import (
	"context"
	"errors"
)

// IntegrationService reserves the integration boundary until a real provider
// is implemented. Unsupported requests must never report installation success.
type IntegrationService struct{}

func NewIntegrationService() *IntegrationService { return &IntegrationService{} }

func (s *IntegrationService) InstallJira(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("Jira integration is not available yet")
}
