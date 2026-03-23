package main

import (
	"fmt"
	"sync"
)

// AllocationTable is a thread-safe map from flow key to allocated bandwidth in kbps.
// DummySwitch writes to it when auction results arrive; DummyProducer reads from it
// to cap egress (tx) bytes, simulating real switch behaviour.
type AllocationTable struct {
	mu   sync.RWMutex
	data map[string]uint64
}

func NewAllocationTable() *AllocationTable {
	return &AllocationTable{data: make(map[string]uint64)}
}

func allocKey(ingressPort, egressPort uint64) string {
	return fmt.Sprintf("%d-%d", ingressPort, egressPort)
}

func (t *AllocationTable) Set(ingressPort, egressPort, kbps uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data[allocKey(ingressPort, egressPort)] = kbps
}

func (t *AllocationTable) Get(ingressPort, egressPort uint64) uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.data[allocKey(ingressPort, egressPort)]
}
