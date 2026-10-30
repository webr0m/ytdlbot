package app

import (
	"context"
	"fmt"
)

const maxConcurrentJobs = 4

func (s *Server) acquireSlot() bool {
	for {
		active := s.activeSlots.Load()
		if active >= s.concurrency.Load() {
			return false
		}
		if s.activeSlots.CompareAndSwap(active, active+1) {
			return true
		}
	}
}
func (s *Server) setConcurrency(ctx context.Context, n int) error {
	if n < 1 || n > maxConcurrentJobs {
		return fmt.Errorf("elige entre 1 y %d tareas simultáneas", maxConcurrentJobs)
	}
	if _, e := s.Store.Pool.Exec(ctx, "INSERT INTO bot_state(key,value) VALUES('concurrency',$1) ON CONFLICT(key) DO UPDATE SET value=excluded.value", n); e != nil {
		return e
	}
	s.concurrency.Store(int32(n))
	s.signalQueue()
	return nil
}
