package node

import (
	"fmt"
	"sync"
)

// PortAllocator hands out unique TCP/UDP ports from a base upward, skipping any
// that are already reserved. It is safe for concurrent use. It does not probe
// the OS; callers that need a truly free port should bind and, on failure,
// Release and Next again.
type PortAllocator struct {
	mu    sync.Mutex
	base  int
	max   int
	taken map[int]bool
}

// NewPortAllocator returns an allocator starting at base. max is the exclusive
// upper bound (base+count); 0 means no explicit bound (up to 65535).
func NewPortAllocator(base, count int) *PortAllocator {
	max := 65536
	if count > 0 && base+count < max {
		max = base + count
	}
	return &PortAllocator{base: base, max: max, taken: make(map[int]bool)}
}

// Next returns the lowest available port and marks it taken. Released ports are
// reused, so it always scans from base.
func (p *PortAllocator) Next() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for port := p.base; port < p.max; port++ {
		if !p.taken[port] {
			p.taken[port] = true
			return port, nil
		}
	}
	return 0, fmt.Errorf("port allocator exhausted in [%d,%d)", p.base, p.max)
}

// Reserve marks a specific port taken. It returns an error if already taken.
func (p *PortAllocator) Reserve(port int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.taken[port] {
		return fmt.Errorf("port %d already reserved", port)
	}
	p.taken[port] = true
	return nil
}

// Release frees a previously allocated port.
func (p *PortAllocator) Release(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.taken, port)
}
