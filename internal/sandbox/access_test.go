package sandbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/runtime/fake"
	"github.com/getorcal/orcal/internal/sandbox"
)

func TestRuntimeIDForResolvesNameAndID(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	byName, err := svc.RuntimeIDFor(ctx, "my-agent")
	if err != nil {
		t.Fatalf("RuntimeIDFor(name) error = %v", err)
	}
	byID, err := svc.RuntimeIDFor(ctx, created.ID)
	if err != nil {
		t.Fatalf("RuntimeIDFor(id) error = %v", err)
	}
	if byName != created.RuntimeID || byID != created.RuntimeID {
		t.Errorf("got %q/%q, want both %q", byName, byID, created.RuntimeID)
	}
}

func TestRuntimeIDForAllowsStoppedSandbox(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})
	if _, err := svc.Stop(ctx, "my-agent"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if _, err := svc.RuntimeIDFor(ctx, "my-agent"); err != nil {
		t.Errorf("RuntimeIDFor() on stopped sandbox = %v, want nil", err)
	}
}

func TestRuntimeIDForRejectsDestroyedSandbox(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})
	svc.Destroy(ctx, "my-agent")

	if _, err := svc.RuntimeIDFor(ctx, "my-agent"); !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("RuntimeIDFor() on destroyed = %v, want ErrNotFound", err)
	}
}

func TestRuntimeIDForRejectsErroredSandbox(t *testing.T) {
	svc := newServiceWithRuntime(t, &erroringRuntime{Fake: fake.New(), startErr: errors.New("start refused")})
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})

	if _, err := svc.RuntimeIDFor(ctx, "my-agent"); !errors.Is(err, sandbox.ErrInvalidState) {
		t.Errorf("RuntimeIDFor() on errored = %v, want ErrInvalidState", err)
	}
}

func TestWithLockedRuntimeIDPropagatesCallbackError(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})

	boom := errors.New("boom")
	err := svc.WithLockedRuntimeID(ctx, "my-agent", func(string) error { return boom })
	if !errors.Is(err, boom) {
		t.Errorf("WithLockedRuntimeID() = %v, want the callback error", err)
	}
}

func TestWithLockedRuntimeIDBlocksConcurrentDestroy(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine:3.20"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	inside := make(chan struct{})
	destroyed := make(chan struct{})
	go func() {
		<-inside
		svc.Destroy(ctx, created.ID)
		close(destroyed)
	}()

	err = svc.WithLockedRuntimeID(ctx, "my-agent", func(runtimeID string) error {
		if runtimeID != created.RuntimeID {
			t.Errorf("runtimeID = %q, want %q", runtimeID, created.RuntimeID)
		}
		close(inside)
		select {
		case <-destroyed:
			t.Error("Destroy completed while the file lock was held; an upload could interleave with a snapshot")
		case <-time.After(150 * time.Millisecond):
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithLockedRuntimeID() error = %v", err)
	}
	<-destroyed
}
