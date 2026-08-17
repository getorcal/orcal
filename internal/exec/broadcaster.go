package exec

import "sync"

type Broadcaster struct {
	mu      sync.Mutex
	waiters map[string]chan struct{}
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{waiters: make(map[string]chan struct{})}
}

func (b *Broadcaster) Wait(execID string) <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch, ok := b.waiters[execID]
	if !ok {
		ch = make(chan struct{})
		b.waiters[execID] = ch
	}
	return ch
}

func (b *Broadcaster) Notify(execID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.waiters[execID]; ok {
		close(ch)
		delete(b.waiters, execID)
	}
}
