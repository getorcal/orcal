package sandbox

type State string

const (
	StateCreating   State = "creating"
	StateRunning    State = "running"
	StateStopped    State = "stopped"
	StateDestroying State = "destroying"
	StateDestroyed  State = "destroyed"
	StateError      State = "error"
)

var transitions = map[State][]State{
	StateCreating:   {StateRunning, StateError},
	StateRunning:    {StateStopped, StateDestroying, StateError},
	StateStopped:    {StateRunning, StateDestroying, StateError},
	StateDestroying: {StateDestroyed, StateError},
	StateError:      {StateDestroying},
	StateDestroyed:  {},
}

func CanTransition(from, to State) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}
