package hls

import "sync"

type concurrentMap[K comparable, V any] struct {
	mu   sync.RWMutex
	data map[K]V
}

func newConcurrentMap[K comparable, V any]() *concurrentMap[K, V] {
	return &concurrentMap[K, V]{
		data: make(map[K]V),
	}
}

func (m *concurrentMap[K, V]) Set(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
}

func (m *concurrentMap[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.data[key]
	return value, ok
}

func (m *concurrentMap[K, V]) Remove(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
}

func (m *concurrentMap[K, V]) Has(key K) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok
}

func (m *concurrentMap[K, V]) Items() map[K]V {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copy := make(map[K]V, len(m.data))
	for key, value := range m.data {
		copy[key] = value
	}
	return copy
}

func (m *concurrentMap[K, V]) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}
