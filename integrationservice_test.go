package main

import (
	"context"
	"errors"
	"testing"
)

func TestUnavailableIntegrationDoesNotReportSuccess(t *testing.T) {
	if err := NewIntegrationService().InstallJira(context.Background()); err == nil {
		t.Fatal("unimplemented integration reported installation success")
	}
}

func TestUnavailableIntegrationHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewIntegrationService().InstallJira(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost: %v", err)
	}
}
