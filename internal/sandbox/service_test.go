package sandbox_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/runtime"
	"github.com/getorcal/orcal/internal/runtime/fake"
	"github.com/getorcal/orcal/internal/sandbox"
	"github.com/getorcal/orcal/internal/store/sqlite"
)

func newService(t *testing.T) (*sandbox.Service, *fake.Fake) {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })
	f := fake.New()
	defaults := sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512}
	return sandbox.NewService(st.Sandboxes(), f, defaults, sandbox.Networks{Full: "orcal", Isolated: "orcal-isolated"}), f
}

func newServiceWithRuntime(t *testing.T, rt runtime.Runtime) *sandbox.Service {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "orcal.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { st.Close() })
	defaults := sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512}
	return sandbox.NewService(st.Sandboxes(), rt, defaults, sandbox.Networks{Full: "orcal", Isolated: "orcal-isolated"})
}

type erroringRuntime struct {
	*fake.Fake
	startErr error
	stopErr  error
}

func (r *erroringRuntime) Start(ctx context.Context, id string) error {
	if r.startErr != nil {
		return r.startErr
	}
	return r.Fake.Start(ctx, id)
}

func (r *erroringRuntime) Stop(ctx context.Context, id string, timeout time.Duration) error {
	if r.stopErr != nil {
		return r.stopErr
	}
	return r.Fake.Stop(ctx, id, timeout)
}

type spyRuntime struct {
	*fake.Fake
	mu     sync.Mutex
	starts []string
}

func (r *spyRuntime) Create(ctx context.Context, spec runtime.CreateSpec) (string, error) {
	id, err := r.Fake.Create(ctx, spec)
	time.Sleep(20 * time.Millisecond)
	return id, err
}

func (r *spyRuntime) Start(ctx context.Context, id string) error {
	r.mu.Lock()
	r.starts = append(r.starts, id)
	r.mu.Unlock()
	return r.Fake.Start(ctx, id)
}

func (r *spyRuntime) recordedStarts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.starts...)
}

func TestCreateStartsSandboxAndReturnsRunning(t *testing.T) {
	svc, _ := newService(t)

	s, err := svc.Create(context.Background(), sandbox.CreateOptions{Name: "my-agent", Image: "python:3.13"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if s.State != sandbox.StateRunning {
		t.Errorf("State = %s, want running", s.State)
	}
	if s.RuntimeID == "" {
		t.Error("RuntimeID is empty, want the runtime handle recorded")
	}
	if s.Runtime != "docker" {
		t.Errorf("Runtime = %q, want docker", s.Runtime)
	}
	if s.ID == "" {
		t.Error("ID is empty")
	}
}

func TestCreateAppliesResourceDefaults(t *testing.T) {
	svc, _ := newService(t)

	s, _ := svc.Create(context.Background(), sandbox.CreateOptions{Image: "alpine"})
	if s.Resources.CPUMillis != 1000 {
		t.Errorf("CPUMillis = %d, want 1000", s.Resources.CPUMillis)
	}
	if s.Resources.MemoryBytes != 1<<30 {
		t.Errorf("MemoryBytes = %d, want %d", s.Resources.MemoryBytes, int64(1)<<30)
	}
	if s.Resources.PidsLimit != 512 {
		t.Errorf("PidsLimit = %d, want 512", s.Resources.PidsLimit)
	}
}

func TestCreateKeepsExplicitResources(t *testing.T) {
	svc, _ := newService(t)

	s, _ := svc.Create(context.Background(), sandbox.CreateOptions{
		Image:     "alpine",
		Resources: sandbox.Resources{CPUMillis: 4000, MemoryBytes: 8 << 30, PidsLimit: 64},
	})
	if s.Resources.CPUMillis != 4000 || s.Resources.MemoryBytes != 8<<30 || s.Resources.PidsLimit != 64 {
		t.Errorf("Resources = %+v, want explicit values preserved", s.Resources)
	}
}

func TestCreatePassesHardeningRelevantSpecToRuntime(t *testing.T) {
	svc, f := newService(t)

	created, _ := svc.Create(context.Background(), sandbox.CreateOptions{
		Image:     "alpine",
		Env:       map[string]string{"A": "1"},
		Resources: sandbox.Resources{CPUMillis: 2000, MemoryBytes: 2 << 30, PidsLimit: 128},
	})

	spec := f.LastCreateSpec()
	if spec.SandboxID != created.ID {
		t.Errorf("spec.SandboxID = %q, want %q", spec.SandboxID, created.ID)
	}
	if spec.Image != "alpine" {
		t.Errorf("spec.Image = %q, want alpine", spec.Image)
	}
	if spec.CPUMillis != 2000 || spec.MemoryBytes != 2<<30 || spec.PidsLimit != 128 {
		t.Errorf("spec resources = %d/%d/%d, want 2000/%d/128", spec.CPUMillis, spec.MemoryBytes, spec.PidsLimit, int64(2)<<30)
	}
	if spec.NetworkName != "orcal" {
		t.Errorf("spec.NetworkName = %q, want orcal", spec.NetworkName)
	}
	if spec.Env["A"] != "1" {
		t.Errorf("spec.Env = %v, want A=1", spec.Env)
	}
}

func TestCreateRejectsMissingImage(t *testing.T) {
	svc, _ := newService(t)

	_, err := svc.Create(context.Background(), sandbox.CreateOptions{Name: "my-agent"})
	if !errors.Is(err, sandbox.ErrInvalidImage) {
		t.Errorf("Create() error = %v, want ErrInvalidImage", err)
	}
	if errors.Is(err, sandbox.ErrInvalidName) {
		t.Errorf("Create() error = %v, must not also satisfy ErrInvalidName", err)
	}
}

func TestCreateRejectsInvalidName(t *testing.T) {
	svc, _ := newService(t)

	_, err := svc.Create(context.Background(), sandbox.CreateOptions{Name: "Bad_Name", Image: "alpine"})
	if !errors.Is(err, sandbox.ErrInvalidName) {
		t.Errorf("Create() error = %v, want ErrInvalidName", err)
	}
}

func TestCreateRejectsDuplicateName(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine"})

	_, err := svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine"})
	if !errors.Is(err, sandbox.ErrNameTaken) {
		t.Errorf("Create() error = %v, want ErrNameTaken", err)
	}
}

func TestCreateMarksSandboxErroredWhenRuntimeFails(t *testing.T) {
	svc, f := newService(t)
	f.SetCreateError(runtime.ErrUnavailable)

	_, err := svc.Create(context.Background(), sandbox.CreateOptions{Name: "doomed", Image: "alpine"})
	if err == nil {
		t.Fatal("Create() error = nil, want runtime failure")
	}

	s, getErr := svc.Get(context.Background(), "doomed")
	if getErr != nil {
		t.Fatalf("Get() error = %v, want the errored record to be readable", getErr)
	}
	if s.State != sandbox.StateError {
		t.Errorf("State = %s, want error", s.State)
	}
}

func TestCreateMarksSandboxErroredWhenStartFails(t *testing.T) {
	rt := &erroringRuntime{Fake: fake.New(), startErr: runtime.ErrUnavailable}
	svc := newServiceWithRuntime(t, rt)

	_, err := svc.Create(context.Background(), sandbox.CreateOptions{Name: "doomed", Image: "alpine"})
	if err == nil {
		t.Fatal("Create() error = nil, want start failure")
	}

	s, getErr := svc.Get(context.Background(), "doomed")
	if getErr != nil {
		t.Fatalf("Get() error = %v, want the errored record to be readable", getErr)
	}
	if s.State != sandbox.StateError {
		t.Errorf("State = %s, want error", s.State)
	}
}

func TestStopMarksSandboxErroredOnRuntimeFailure(t *testing.T) {
	rt := &erroringRuntime{Fake: fake.New()}
	svc := newServiceWithRuntime(t, rt)
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine"})

	rt.stopErr = runtime.ErrUnavailable
	_, err := svc.Stop(ctx, "my-agent")
	if err == nil {
		t.Fatal("Stop() error = nil, want runtime failure")
	}

	s, getErr := svc.Get(ctx, "my-agent")
	if getErr != nil {
		t.Fatalf("Get() error = %v, want the errored record to be readable", getErr)
	}
	if s.State != sandbox.StateError {
		t.Errorf("State = %s, want error", s.State)
	}
}

func TestGetResolvesByIDAndByName(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	created, _ := svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine"})

	byID, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get(id) error = %v", err)
	}
	byName, err := svc.Get(ctx, "my-agent")
	if err != nil {
		t.Fatalf("Get(name) error = %v", err)
	}
	if byID.ID != byName.ID {
		t.Errorf("Get(id).ID = %s, Get(name).ID = %s, want equal", byID.ID, byName.ID)
	}
}

func TestGetUnknownRefReturnsErrNotFound(t *testing.T) {
	svc, _ := newService(t)
	if _, err := svc.Get(context.Background(), "nope"); !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestStopThenStartRoundTrip(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine"})

	stopped, err := svc.Stop(ctx, "my-agent")
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if stopped.State != sandbox.StateStopped {
		t.Errorf("State after Stop = %s, want stopped", stopped.State)
	}

	started, err := svc.Start(ctx, "my-agent")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if started.State != sandbox.StateRunning {
		t.Errorf("State after Start = %s, want running", started.State)
	}
}

func TestStartOnRunningSandboxReturnsErrInvalidState(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine"})

	if _, err := svc.Start(ctx, "my-agent"); !errors.Is(err, sandbox.ErrInvalidState) {
		t.Errorf("Start() on running error = %v, want ErrInvalidState", err)
	}
}

func TestDestroyMarksDestroyedAndFreesName(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine"})

	destroyed, err := svc.Destroy(ctx, "my-agent")
	if err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if destroyed.State != sandbox.StateDestroyed {
		t.Errorf("State = %s, want destroyed", destroyed.State)
	}

	if _, err := svc.Get(ctx, "my-agent"); !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("Get(name) after destroy error = %v, want ErrNotFound", err)
	}
	if _, err := svc.Get(ctx, destroyed.ID); err != nil {
		t.Errorf("Get(id) after destroy error = %v, want the record to remain", err)
	}
	if _, err := svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine"}); err != nil {
		t.Errorf("Create() reusing freed name error = %v, want nil", err)
	}
}

func TestRuntimeIDReturnsHandleForRunningSandbox(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	created, _ := svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine"})

	got, err := svc.RuntimeID(ctx, created.ID)
	if err != nil {
		t.Fatalf("RuntimeID() error = %v", err)
	}
	if got != created.RuntimeID {
		t.Errorf("RuntimeID() = %q, want %q", got, created.RuntimeID)
	}
}

func TestRuntimeIDRejectsStoppedSandbox(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	created, _ := svc.Create(ctx, sandbox.CreateOptions{Name: "my-agent", Image: "alpine"})
	svc.Stop(ctx, "my-agent")

	if _, err := svc.RuntimeID(ctx, created.ID); !errors.Is(err, sandbox.ErrInvalidState) {
		t.Errorf("RuntimeID() on stopped error = %v, want ErrInvalidState", err)
	}
}

func TestCreateSerializesAgainstConcurrentStart(t *testing.T) {
	for i := range 50 {
		st, err := sqlite.Open(filepath.Join(t.TempDir(), "orcal.db"))
		if err != nil {
			t.Fatalf("sqlite.Open() error = %v", err)
		}
		rt := &spyRuntime{Fake: fake.New()}
		defaults := sandbox.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, PidsLimit: 512}
		svc := sandbox.NewService(st.Sandboxes(), rt, defaults, sandbox.Networks{Full: "orcal", Isolated: "orcal-isolated"})
		ctx := context.Background()
		name := fmt.Sprintf("race-%d", i)

		done := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			svc.Create(ctx, sandbox.CreateOptions{Name: name, Image: "alpine"})
			close(done)
		}()
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				list, err := svc.List(ctx, sandbox.Filter{})
				if err != nil || len(list) == 0 {
					continue
				}
				svc.Start(ctx, list[0].ID)
			}
		}()
		wg.Wait()

		for _, id := range rt.recordedStarts() {
			if id == "" {
				t.Fatalf("iteration %d: rt.Start invoked with empty runtime id", i)
			}
		}
		st.Close()
	}
}

func TestCreateMarksSandboxErroredWhenTheContainerExitsImmediately(t *testing.T) {
	svc, f := newService(t)
	f.SetExitAfterStart(true)

	_, err := svc.Create(context.Background(), sandbox.CreateOptions{Name: "doomed", Image: "scratch"})
	if err == nil {
		t.Fatal("Create() error = nil, want a failure for an image whose command exits immediately")
	}
	if !strings.Contains(err.Error(), "exited immediately") {
		t.Errorf("Create() error = %v, want it to say the image's command exited immediately", err)
	}

	s, getErr := svc.Get(context.Background(), "doomed")
	if getErr != nil {
		t.Fatalf("Get() error = %v, want the errored record to be readable", getErr)
	}
	if s.State != sandbox.StateError {
		t.Errorf("State = %s, want error - a dead container must never be recorded running", s.State)
	}
}

func TestCreateRejectsNegativeResourceLimits(t *testing.T) {
	cases := []struct {
		name string
		res  sandbox.Resources
	}{
		{"pids limit", sandbox.Resources{PidsLimit: -1}},
		{"cpu millis", sandbox.Resources{CPUMillis: -1}},
		{"memory bytes", sandbox.Resources{MemoryBytes: -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, _ := newService(t)

			_, err := svc.Create(context.Background(), sandbox.CreateOptions{Image: "alpine", Resources: c.res})
			if !errors.Is(err, sandbox.ErrInvalidResources) {
				t.Errorf("Create() error = %v, want ErrInvalidResources", err)
			}
		})
	}
}
