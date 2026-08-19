package sandbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/getorcal/orcal/internal/sandbox"
)

func TestCreateDefaultsToFullNetwork(t *testing.T) {
	svc, f := newService(t)
	created, err := svc.Create(context.Background(), sandbox.CreateOptions{Image: "alpine"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Network != sandbox.NetworkFull {
		t.Fatalf("expected full, got %q", created.Network)
	}
	if got := f.LastCreateSpec().NetworkName; got != "orcal" {
		t.Fatalf("a full sandbox must join the egress network, got %q", got)
	}
}

func TestCreateNoneJoinsTheIsolatedNetwork(t *testing.T) {
	svc, f := newService(t)
	created, err := svc.Create(context.Background(), sandbox.CreateOptions{Image: "alpine", Network: sandbox.NetworkNone})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Network != sandbox.NetworkNone {
		t.Fatalf("expected none, got %q", created.Network)
	}
	if got := f.LastCreateSpec().NetworkName; got != "orcal-isolated" {
		t.Fatalf("a none sandbox must join the isolated network, got %q", got)
	}
}

func TestCreateRejectsAnUnknownNetwork(t *testing.T) {
	svc, _ := newService(t)
	_, err := svc.Create(context.Background(), sandbox.CreateOptions{Image: "alpine", Network: "host"})
	if !errors.Is(err, sandbox.ErrInvalidNetwork) {
		t.Fatalf("expected ErrInvalidNetwork, got %v", err)
	}
}

func TestNetworkIsPersisted(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, sandbox.CreateOptions{Image: "alpine", Network: sandbox.NetworkNone})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Network != sandbox.NetworkNone {
		t.Fatalf("network did not survive a round trip, got %q", got.Network)
	}
}

func TestStartAndStopDoNotChangeTheNetwork(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, sandbox.CreateOptions{Image: "alpine", Network: sandbox.NetworkNone})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Stop(ctx, created.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	started, err := svc.Start(ctx, created.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.Network != sandbox.NetworkNone {
		t.Fatalf("a stop/start cycle must not widen isolation, got %q", started.Network)
	}
}
