package idempotency

import (
	"context"
	"maps"
	"slices"
	"sync"
)

// MemoryStore keeps entries in a map, for tests.
//
// It is **not** a deployment option. It has no expiry, and it is per-process — two
// instances behind a load balancer would each believe they were the first to see a key,
// which is the exact failure the mechanism exists to prevent. [RedisStore] is the only
// implementation the service runs with.
//
// What it is for is testing the behaviour around the store: that a handler runs once, that
// a key is released after a failure, that a replay carries the original response. Those
// are properties of the middleware, and asserting them against a real Redis would make
// every test that touches a state-changing endpoint depend on a running container.
type MemoryStore struct {
	mu      sync.Mutex
	entries map[string]Entry

	// FailWith, when set, makes every operation fail with it — the unreachable-Redis
	// case, which the middleware has to fail closed on.
	FailWith error
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: map[string]Entry{}}
}

// Begin claims key, or reports the entry already there.
func (m *MemoryStore) Begin(_ context.Context, key, fingerprint string) (*Entry, bool, error) {
	if err := m.failure(); err != nil {
		return nil, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.entries[key]; ok {
		return &existing, false, nil
	}
	m.entries[key] = Entry{Fingerprint: fingerprint}
	return nil, true, nil
}

// Complete records resp against key.
func (m *MemoryStore) Complete(_ context.Context, key, fingerprint string, resp Response) error {
	if err := m.failure(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries[key] = Entry{Fingerprint: fingerprint, Response: &resp}
	return nil
}

// Release gives up a claim.
func (m *MemoryStore) Release(_ context.Context, key string) error {
	if err := m.failure(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.entries, key)
	return nil
}

// Held reports whether key currently has an entry, claimed or completed.
func (m *MemoryStore) Held(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.entries[key]
	return ok
}

// Keys returns every key currently held, for assertions about namespacing.
func (m *MemoryStore) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Sorted(maps.Keys(m.entries))
}

func (m *MemoryStore) failure() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.FailWith
}
