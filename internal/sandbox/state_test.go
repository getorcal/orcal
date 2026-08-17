package sandbox

import "testing"

func TestCanTransitionAllowsSpecifiedEdges(t *testing.T) {
	allowed := []struct{ from, to State }{
		{StateCreating, StateRunning},
		{StateCreating, StateError},
		{StateRunning, StateStopped},
		{StateRunning, StateDestroying},
		{StateRunning, StateError},
		{StateStopped, StateRunning},
		{StateStopped, StateDestroying},
		{StateStopped, StateError},
		{StateDestroying, StateDestroyed},
		{StateDestroying, StateError},
		{StateError, StateDestroying},
	}
	for _, e := range allowed {
		if !CanTransition(e.from, e.to) {
			t.Errorf("CanTransition(%s, %s) = false, want true", e.from, e.to)
		}
	}
}

func TestCanTransitionRejectsUnspecifiedEdges(t *testing.T) {
	rejected := []struct{ from, to State }{
		{StateCreating, StateStopped},
		{StateCreating, StateDestroyed},
		{StateRunning, StateCreating},
		{StateRunning, StateDestroyed},
		{StateDestroyed, StateRunning},
		{StateDestroyed, StateDestroying},
		{StateStopped, StateDestroyed},
		{StateError, StateRunning},
	}
	for _, e := range rejected {
		if CanTransition(e.from, e.to) {
			t.Errorf("CanTransition(%s, %s) = true, want false", e.from, e.to)
		}
	}
}

func TestCanTransitionRejectsSelfLoop(t *testing.T) {
	if CanTransition(StateRunning, StateRunning) {
		t.Error("CanTransition(running, running) = true, want false")
	}
}
