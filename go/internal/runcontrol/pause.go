package runcontrol

import (
	"context"
	"sync"
)

type PauseGate struct {
	mu     sync.Mutex
	paused bool
	cond   *sync.Cond
}

func NewPauseGate() *PauseGate {
	gate := &PauseGate{}
	gate.cond = sync.NewCond(&gate.mu)
	return gate
}

func (p *PauseGate) Pause() {
	p.mu.Lock()
	p.paused = true
	p.mu.Unlock()
}

func (p *PauseGate) Resume() {
	p.mu.Lock()
	p.paused = false
	p.mu.Unlock()
	p.cond.Broadcast()
}

func (p *PauseGate) IsPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paused
}

func (p *PauseGate) Wait(ctx context.Context) bool {
	p.mu.Lock()
	for p.paused {
		if ctx.Err() != nil {
			p.mu.Unlock()
			return false
		}
		p.cond.Wait()
	}
	p.mu.Unlock()
	return ctx.Err() == nil
}
