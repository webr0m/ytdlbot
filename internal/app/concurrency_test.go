package app

import (
	"sync"
	"testing"
)

func TestConcurrencyAdmissionIsAtomic(t *testing.T) {
	s := NewServer(Config{Workers: 2}, nil, nil)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.acquireSlot() }()
	}
	wg.Wait()
	if s.activeSlots.Load() != 2 {
		t.Fatal("se excedió la concurrencia")
	}
	s.concurrency.Store(1)
	if s.acquireSlot() {
		t.Fatal("inició tarea al reducir el límite")
	}
	s.activeSlots.Add(-2)
	if !s.acquireSlot() {
		t.Fatal("no reanudó al liberar capacidad")
	}
}
