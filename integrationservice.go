package main

import (
	"context"
	"time"
)

const jiraInstallDelay = 3 * time.Second

// IntegrationService provides the requested Jira installation preview.
// No account connection or external installation is performed yet.
type IntegrationService struct {
	installDelay time.Duration
}

func NewIntegrationService() *IntegrationService {
	return &IntegrationService{installDelay: jiraInstallDelay}
}

func (s *IntegrationService) InstallJira(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delay := s.installDelay
	if delay <= 0 {
		delay = jiraInstallDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
