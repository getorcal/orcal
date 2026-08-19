package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/getorcal/orcal/internal/exec"
	"github.com/getorcal/orcal/internal/sandbox"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "orcal.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sample() *sandbox.Sandbox {
	return &sandbox.Sandbox{
		ID:        "0192f3a4-5b6c-7d8e-9f01-23456789abcd",
		Name:      "my-agent",
		Image:     "python:3.13",
		State:     sandbox.StateRunning,
		Runtime:   "docker",
		RuntimeID: "container123",
		Resources: sandbox.Resources{CPUMillis: 2000, MemoryBytes: 4 << 30, PidsLimit: 512},
		Env:       map[string]string{"API_MODE": "test"},
		Labels:    map[string]string{"team": "core"},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
		UpdatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestSandboxRoundTripPreservesAllFields(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()
	want := sample()

	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	got, err := repo.Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name != want.Name || got.Image != want.Image || got.State != want.State {
		t.Errorf("identity fields = %+v, want %+v", got, want)
	}
	if got.RuntimeID != want.RuntimeID || got.Runtime != want.Runtime {
		t.Errorf("runtime fields = %q/%q, want %q/%q", got.Runtime, got.RuntimeID, want.Runtime, want.RuntimeID)
	}
	if got.Resources != want.Resources {
		t.Errorf("Resources = %+v, want %+v", got.Resources, want.Resources)
	}
	if got.Env["API_MODE"] != "test" {
		t.Errorf("Env = %v, want API_MODE=test", got.Env)
	}
	if got.Labels["team"] != "core" {
		t.Errorf("Labels = %v, want team=core", got.Labels)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

func TestGetMissingSandboxReturnsErrNotFound(t *testing.T) {
	_, err := newStore(t).Sandboxes().Get(context.Background(), "missing")
	if !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("Get() error = %v, want sandbox.ErrNotFound", err)
	}
}

func TestCreateDuplicateLiveNameReturnsErrNameTaken(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()
	first := sample()
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	second := sample()
	second.ID = "0192f3a4-5b6c-7d8e-9f01-23456789abce"
	err := repo.Create(ctx, second)
	if !errors.Is(err, sandbox.ErrNameTaken) {
		t.Errorf("Create() error = %v, want sandbox.ErrNameTaken", err)
	}
}

func TestDestroyedSandboxFreesItsName(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()
	first := sample()
	repo.Create(ctx, first)
	first.State = sandbox.StateDestroyed
	if err := repo.Update(ctx, first); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	second := sample()
	second.ID = "0192f3a4-5b6c-7d8e-9f01-23456789abce"
	if err := repo.Create(ctx, second); err != nil {
		t.Errorf("Create() after destroy error = %v, want nil", err)
	}
}

func TestGetByNameIgnoresDestroyedSandboxes(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()
	s := sample()
	repo.Create(ctx, s)
	s.State = sandbox.StateDestroyed
	repo.Update(ctx, s)

	_, err := repo.GetByName(ctx, "my-agent")
	if !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("GetByName() error = %v, want sandbox.ErrNotFound", err)
	}
}

func TestListExcludesDestroyedUnlessRequested(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()
	live := sample()
	repo.Create(ctx, live)

	dead := sample()
	dead.ID = "0192f3a4-5b6c-7d8e-9f01-23456789abce"
	dead.Name = "gone-agent"
	repo.Create(ctx, dead)
	dead.State = sandbox.StateDestroyed
	repo.Update(ctx, dead)

	all, err := repo.List(ctx, sandbox.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("len(List()) = %d, want 1", len(all))
	}

	withDead, _ := repo.List(ctx, sandbox.Filter{States: []sandbox.State{sandbox.StateDestroyed}, Limit: 10})
	if len(withDead) != 1 || withDead[0].ID != dead.ID {
		t.Errorf("List(destroyed) = %+v, want the destroyed sandbox", withDead)
	}
}

func TestListFiltersByLabel(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()
	repo.Create(ctx, sample())

	other := sample()
	other.ID = "0192f3a4-5b6c-7d8e-9f01-23456789abce"
	other.Name = "other-agent"
	other.Labels = map[string]string{"team": "growth"}
	repo.Create(ctx, other)

	got, err := repo.List(ctx, sandbox.Filter{Labels: map[string]string{"team": "core"}, Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "my-agent" {
		t.Errorf("List(team=core) = %+v, want my-agent only", got)
	}
}

func TestListFiltersByDottedLabelKey(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()
	s := sample()
	s.Labels = map[string]string{"app.kubernetes.io/name": "core"}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.List(ctx, sandbox.Filter{Labels: map[string]string{"app.kubernetes.io/name": "core"}, Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != s.ID {
		t.Errorf("List(app.kubernetes.io/name=core) = %+v, want %s", got, s.ID)
	}
}

func TestListFiltersByQuotedLabelKey(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()
	s := sample()
	s.Labels = map[string]string{`weird"key`: "v"}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.List(ctx, sandbox.Filter{Labels: map[string]string{`weird"key`: "v"}, Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != s.ID {
		t.Errorf(`List(weird"key=v) = %+v, want %s`, got, s.ID)
	}
}

func TestListLabelFilterDoesNotOverMatch(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()

	wrongValue := sample()
	wrongValue.Labels = map[string]string{"team": "growth"}
	if err := repo.Create(ctx, wrongValue); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	missingKey := sample()
	missingKey.ID = "0192f3a4-5b6c-7d8e-9f01-23456789abce"
	missingKey.Name = "other-agent"
	missingKey.Labels = map[string]string{"other": "value"}
	if err := repo.Create(ctx, missingKey); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.List(ctx, sandbox.Filter{Labels: map[string]string{"team": "core"}, Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List(team=core) = %+v, want no matches", got)
	}
}

func TestListPaginatesByCursor(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()
	ids := []string{
		"0192f3a4-5b6c-7d8e-9f01-000000000001",
		"0192f3a4-5b6c-7d8e-9f01-000000000002",
		"0192f3a4-5b6c-7d8e-9f01-000000000003",
	}
	for i, id := range ids {
		s := sample()
		s.ID = id
		s.Name = "agent" + string(rune('a'+i))
		repo.Create(ctx, s)
	}

	page, _ := repo.List(ctx, sandbox.Filter{Limit: 2})
	if len(page) != 2 || page[0].ID != ids[0] {
		t.Fatalf("first page = %d items starting %s, want 2 starting %s", len(page), page[0].ID, ids[0])
	}
	next, _ := repo.List(ctx, sandbox.Filter{Limit: 2, Cursor: page[1].ID})
	if len(next) != 1 || next[0].ID != ids[2] {
		t.Errorf("second page = %+v, want only %s", next, ids[2])
	}
}

func TestExecRoundTripAndListRunning(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	st.Sandboxes().Create(ctx, sample())
	repo := st.Execs()

	code := 7
	finished := time.Now().UTC().Truncate(time.Millisecond)
	e := &exec.Exec{
		ID:            "0192f3a4-5b6c-7d8e-9f01-11111111aaaa",
		SandboxID:     sample().ID,
		RuntimeExecID: "dockerexec1",
		Command:       []string{"sh", "-c", "echo hi"},
		Env:           map[string]string{"X": "1"},
		WorkingDir:    "/work",
		User:          "root",
		State:         exec.StateRunning,
		StartedAt:     time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, e); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	running, err := repo.ListRunning(ctx)
	if err != nil {
		t.Fatalf("ListRunning() error = %v", err)
	}
	if len(running) != 1 || running[0].RuntimeExecID != "dockerexec1" {
		t.Fatalf("ListRunning() = %+v, want one exec with dockerexec1", running)
	}

	e.State = exec.StateExited
	e.ExitCode = &code
	e.FinishedAt = &finished
	e.OutputBytes = 42
	e.Truncated = true
	if err := repo.Update(ctx, e); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, _ := repo.Get(ctx, e.ID)
	if got.State != exec.StateExited {
		t.Errorf("State = %s, want exited", got.State)
	}
	if got.ExitCode == nil || *got.ExitCode != 7 {
		t.Errorf("ExitCode = %v, want 7", got.ExitCode)
	}
	if got.OutputBytes != 42 || !got.Truncated {
		t.Errorf("OutputBytes/Truncated = %d/%v, want 42/true", got.OutputBytes, got.Truncated)
	}
	if len(got.Command) != 3 || got.Command[2] != "echo hi" {
		t.Errorf("Command = %v, want [sh -c echo hi]", got.Command)
	}

	afterExit, _ := repo.ListRunning(ctx)
	if len(afterExit) != 0 {
		t.Errorf("ListRunning() after exit = %d, want 0", len(afterExit))
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t).Settings()

	if _, found, err := s.Get(ctx, "missing"); err != nil || found {
		t.Errorf("Get(missing) = (found=%v, err=%v), want (false, nil)", found, err)
	}
	if err := s.Set(ctx, "token_hash", "abc"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	got, found, err := s.Get(ctx, "token_hash")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found {
		t.Error("Get() found = false, want true")
	}
	if got != "abc" {
		t.Errorf("Get() = %q, want abc", got)
	}

	s.Set(ctx, "token_hash", "def")
	got, _, _ = s.Get(ctx, "token_hash")
	if got != "def" {
		t.Errorf("Get() after overwrite = %q, want def", got)
	}
}

func TestSettingsGetDistinguishesAbsentFromPresent(t *testing.T) {
	ctx := context.Background()
	s := newStore(t).Settings()

	value, found, err := s.Get(ctx, "absent_key")
	if err != nil {
		t.Fatalf("Get(absent) error = %v, want nil", err)
	}
	if found {
		t.Error("Get(absent) found = true, want false")
	}
	if value != "" {
		t.Errorf("Get(absent) value = %q, want empty", value)
	}

	if err := s.Set(ctx, "present_key", "some-value"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	value, found, err = s.Get(ctx, "present_key")
	if err != nil {
		t.Fatalf("Get(present) error = %v, want nil", err)
	}
	if !found {
		t.Error("Get(present) found = false, want true")
	}
	if value != "some-value" {
		t.Errorf("Get(present) value = %q, want some-value", value)
	}
}

func TestListLabelFilterAppliesBeforeLimit(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()

	unmatched := []string{
		"0192f3a4-5b6c-7d8e-9f01-000000000001",
		"0192f3a4-5b6c-7d8e-9f01-000000000002",
		"0192f3a4-5b6c-7d8e-9f01-000000000003",
	}
	for i, id := range unmatched {
		s := sample()
		s.ID = id
		s.Name = "unmatched" + string(rune('a'+i))
		s.Labels = map[string]string{"team": "growth"}
		repo.Create(ctx, s)
	}

	matched := []string{
		"0192f3a4-5b6c-7d8e-9f01-000000000004",
		"0192f3a4-5b6c-7d8e-9f01-000000000005",
	}
	for i, id := range matched {
		s := sample()
		s.ID = id
		s.Name = "matched" + string(rune('a'+i))
		s.Labels = map[string]string{"team": "core"}
		repo.Create(ctx, s)
	}

	got, err := repo.List(ctx, sandbox.Filter{Labels: map[string]string{"team": "core"}, Limit: 2})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(List()) = %d, want 2 matches, not an empty page", len(got))
	}
	for _, s := range got {
		if s.Labels["team"] != "core" {
			t.Errorf("List() returned %+v, want only team=core", s)
		}
	}
}

func TestUpdateRenameOntoTakenLiveNameReturnsErrNameTaken(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()

	taken := sample()
	taken.Name = "taken-name"
	if err := repo.Create(ctx, taken); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	renamed := sample()
	renamed.ID = "0192f3a4-5b6c-7d8e-9f01-23456789abce"
	renamed.Name = "original-name"
	if err := repo.Create(ctx, renamed); err != nil {
		t.Fatalf("second Create() error = %v", err)
	}

	renamed.Name = "taken-name"
	err := repo.Update(ctx, renamed)
	if !errors.Is(err, sandbox.ErrNameTaken) {
		t.Errorf("Update() error = %v, want sandbox.ErrNameTaken", err)
	}
}

func TestCreateMultipleUnnamedSandboxesCoexist(t *testing.T) {
	ctx := context.Background()
	repo := newStore(t).Sandboxes()

	first := sample()
	first.Name = ""
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	second := sample()
	second.ID = "0192f3a4-5b6c-7d8e-9f01-23456789abce"
	second.Name = ""
	if err := repo.Create(ctx, second); err != nil {
		t.Errorf("second Create() error = %v, want nil", err)
	}
}

func TestTimestampTextFormOrdersChronologically(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	repo := st.Sandboxes()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	earlier := sample()
	earlier.ID = "0192f3a4-5b6c-7d8e-9f01-000000000010"
	earlier.Name = "earlier-agent"
	earlier.CreatedAt = base
	earlier.UpdatedAt = base
	if err := repo.Create(ctx, earlier); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	later := sample()
	later.ID = "0192f3a4-5b6c-7d8e-9f01-000000000011"
	later.Name = "later-agent"
	later.CreatedAt = base.Add(500 * time.Millisecond)
	later.UpdatedAt = later.CreatedAt
	if err := repo.Create(ctx, later); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	gotEarlier, err := repo.Get(ctx, earlier.ID)
	if err != nil {
		t.Fatalf("Get(earlier) error = %v", err)
	}
	gotLater, err := repo.Get(ctx, later.ID)
	if err != nil {
		t.Fatalf("Get(later) error = %v", err)
	}
	if !gotEarlier.CreatedAt.Before(gotLater.CreatedAt) {
		t.Errorf("parsed CreatedAt ordering = %v, %v, want earlier before later", gotEarlier.CreatedAt, gotLater.CreatedAt)
	}

	var earlierText, laterText string
	if err := st.db.QueryRowContext(ctx, `SELECT created_at FROM sandboxes WHERE id = ?`, earlier.ID).Scan(&earlierText); err != nil {
		t.Fatalf("query earlier created_at: %v", err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT created_at FROM sandboxes WHERE id = ?`, later.ID).Scan(&laterText); err != nil {
		t.Fatalf("query later created_at: %v", err)
	}
	if earlierText >= laterText {
		t.Errorf("stored TEXT ordering = %q, %q, want earlier < later lexicographically", earlierText, laterText)
	}
}

func TestOpenIsIdempotentAcrossRestarts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "orcal.db")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	first.Sandboxes().Create(ctx, sample())
	first.Close()

	second, err := Open(path)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer second.Close()

	got, err := second.Sandboxes().Get(ctx, sample().ID)
	if err != nil {
		t.Fatalf("Get() after reopen error = %v", err)
	}
	if got.Name != "my-agent" {
		t.Errorf("Name after reopen = %q, want my-agent", got.Name)
	}
}
