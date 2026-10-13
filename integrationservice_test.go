package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIntegrationJiraWaitsThreeSeconds(t *testing.T) {
	service := NewIntegrationService()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	if err := service.InstallJira(ctx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 2900*time.Millisecond {
		t.Fatalf("Jira request completed before its three-second preview: %s", elapsed)
	}
}

func TestIntegrationJiraCancellationStopsTheWait(t *testing.T) {
	service := &IntegrationService{installDelay: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(20*time.Millisecond, cancel)
	defer timer.Stop()
	started := time.Now()
	if err := service.InstallJira(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wanted cancellation, got %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("Cancellation did not interrupt the installation timer")
	}
}

func TestIntegrationJiraHonorsConfiguredDurationAndDeadline(t *testing.T) {
	service := &IntegrationService{installDelay: 30 * time.Millisecond}
	started := time.Now()
	if err := service.InstallJira(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < service.installDelay {
		t.Fatal("Configured preview returned before its timer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := service.InstallJira(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wanted deadline exceeded, got %v", err)
	}
}
