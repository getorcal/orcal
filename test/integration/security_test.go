//go:build docker

package integration

import (
	"context"
	"testing"

	"github.com/getorcal/orcal/pkg/orcalclient"
)

func TestNoneSandboxHasNoEgress(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	full := e.sandbox(t, "egress-full")

	none, err := e.client.CreateSandbox(ctx, orcalclient.CreateSandboxParams{
		Name: "egress-none", Image: testImage, Network: "none",
	})
	if err != nil {
		t.Fatalf("create isolated sandbox: %v", err)
	}
	t.Cleanup(func() { _, _ = e.client.DestroySandbox(context.Background(), none.Id) })

	command := []string{"wget", "-q", "-T", "5", "-O", "/dev/null", "http://example.com/"}

	_, fullCode := e.runToCompletion(t, full, command...)
	if fullCode != 0 {
		t.Fatalf("control leg failed: a full-network sandbox could not reach the network (exit %d). "+
			"This test is a paired control, not a single assertion: leg one proves the environment has "+
			"egress at all, and only then does leg two's failure prove the none network is isolating it. "+
			"An environment with no outbound access makes leg two pass for entirely the wrong reason, "+
			"which is how this project once shipped a dead setuid-stripping code path under a green suite. "+
			"The fix is to give this Docker daemon real internet access, never to skip or weaken this test.",
			fullCode)
	}

	_, noneCode := e.runToCompletion(t, none.Id, command...)
	if noneCode == 0 {
		t.Fatal("a none-network sandbox reached the internet; the isolated network is not isolating")
	}
}

func TestNoneSurvivesSnapshotAndFork(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	none, err := e.client.CreateSandbox(ctx, orcalclient.CreateSandboxParams{
		Name: "lineage-none", Image: testImage, Network: "none",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = e.client.DestroySandbox(context.Background(), none.Id) })

	snap, err := e.client.CreateSnapshot(ctx, none.Id, orcalclient.CreateSnapshotParams{})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	t.Cleanup(func() { _ = e.client.DeleteSnapshot(context.Background(), snap.Id) })

	forked, err := e.client.CreateSandbox(ctx, orcalclient.CreateSandboxParams{Name: "lineage-fork", Snapshot: snap.Id})
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	t.Cleanup(func() { _, _ = e.client.DestroySandbox(context.Background(), forked.Id) })

	if string(forked.Network) != "none" {
		t.Fatalf("forking an isolated sandbox must not hand the fork the internet, got %q", forked.Network)
	}

	_, code := e.runToCompletion(t, forked.Id, "wget", "-q", "-T", "5", "-O", "/dev/null", "http://example.com/")
	if code == 0 {
		t.Fatal("the fork of an isolated sandbox reached the internet")
	}
}

func TestAuditRoundTrip(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	sb := e.sandbox(t, "audited")
	if _, err := e.client.CreateExec(ctx, sb, orcalclient.CreateExecParams{Command: []string{"true"}}); err != nil {
		t.Fatalf("exec: %v", err)
	}

	list, err := e.client.ListEvents(ctx, orcalclient.ListEventsParams{Action: "exec.create", Limit: 50})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var found bool
	for _, item := range list.Items {
		if item.ResourceType != nil && *item.ResourceType == "exec" {
			found = true
			if item.ActorName == nil || *item.ActorName == "" {
				t.Fatal("the event must name the acting token")
			}
		}
	}
	if !found {
		t.Fatal("creating an exec left no audit event")
	}
}
