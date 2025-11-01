package hls

import "sync"

type cacheEntry struct {
	data []byte
}

type manifestCache struct {
	order   []string
	entries map[string][]byte
}

type segmentCacheStore interface {
	Save(manifestID, key string, data []byte)
	Load(manifestID, key string) ([]byte, bool)
	Remove(manifestID string)
	Reset()
}

type noopSegmentCache struct{}

type memorySegmentCache struct {
	mu        sync.RWMutex
	limit     int
	manifests map[string]*manifestCache
}

func (noopSegmentCache) Save(string, string, []byte) {}
func (noopSegmentCache) Load(string, string) ([]byte, bool) {
	return nil, false
}
func (noopSegmentCache) Remove(string) {}
func (noopSegmentCache) Reset()        {}

func newMemorySegmentCache(limit int) *memorySegmentCache {
	return &memorySegmentCache{
		limit:     limit,
		manifests: make(map[string]*manifestCache),
	}
}

func (c *memorySegmentCache) Save(manifestID, key string, data []byte) {
	if len(data) == 0 || manifestID == "" || key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	manifest := c.ensureManifest(manifestID)
	if _, exists := manifest.entries[key]; exists {
		manifest.remove(key)
	}
	manifest.entries[key] = append([]byte(nil), data...)
	manifest.order = append(manifest.order, key)
	c.enforceLimit(manifest)
}

func (c *memorySegmentCache) Load(manifestID, key string) ([]byte, bool) {
	if manifestID == "" || key == "" {
		return nil, false
	}

	c.mu.RLock()
	manifest, ok := c.manifests[manifestID]
	if !ok {
		c.mu.RUnlock()
		return nil, false
	}
	data, ok := manifest.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	copy := append([]byte(nil), data...)
	return copy, true
}

func (c *memorySegmentCache) Remove(manifestID string) {
	if manifestID == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.manifests, manifestID)
}

func (c *memorySegmentCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.manifests = make(map[string]*manifestCache)
}

func (c *memorySegmentCache) ensureManifest(manifestID string) *manifestCache {
	if manifest, ok := c.manifests[manifestID]; ok {
		return manifest
	}
	manifest := &manifestCache{
		order:   make([]string, 0),
		entries: make(map[string][]byte),
	}
	c.manifests[manifestID] = manifest
	return manifest
}

func (c *memorySegmentCache) enforceLimit(manifest *manifestCache) {
	if c.limit <= 0 {
		return
	}

	for len(manifest.order) > c.limit {
		oldest := manifest.order[0]
		manifest.order = manifest.order[1:]
		delete(manifest.entries, oldest)
	}
}

func (m *manifestCache) remove(key string) {
	delete(m.entries, key)
	for i, existing := range m.order {
		if existing == key {
			m.order = append(m.order[:i], m.order[i+1:]...)
			return
		}
	}
}

var (
	cacheMu             sync.RWMutex
	activeSegmentCache  segmentCacheStore = noopSegmentCache{}
	segmentCacheEnabled bool
)

// ConfigureSegmentCache switches the in-memory cache implementation on or off.
func ConfigureSegmentCache(enabled bool, limit int) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if !enabled {
		activeSegmentCache = noopSegmentCache{}
		segmentCacheEnabled = false
		return
	}

	if limit < 0 {
		limit = 0
	}
	activeSegmentCache = newMemorySegmentCache(limit)
	segmentCacheEnabled = true
}

// SaveSegmentCache stores the provided bytes in the active in-memory cache, if enabled.
func SaveSegmentCache(manifestID, key string, data []byte) {
	cacheMu.RLock()
	cache := activeSegmentCache
	enabled := segmentCacheEnabled
	cacheMu.RUnlock()

	if !enabled {
		return
	}
	cache.Save(manifestID, key, data)
}

// LoadSegmentCache retrieves cached bytes for the given manifest and key.
func LoadSegmentCache(manifestID, key string) ([]byte, bool) {
	cacheMu.RLock()
	cache := activeSegmentCache
	enabled := segmentCacheEnabled
	cacheMu.RUnlock()

	if !enabled {
		return nil, false
	}
	return cache.Load(manifestID, key)
}

// ClearSegmentCache removes all cached entries associated with the manifest.
func ClearSegmentCache(manifestID string) {
	cacheMu.RLock()
	cache := activeSegmentCache
	enabled := segmentCacheEnabled
	cacheMu.RUnlock()

	if !enabled {
		return
	}
	cache.Remove(manifestID)
}

// ResetSegmentCache discards all in-memory cached segments.
func ResetSegmentCache() {
	cacheMu.RLock()
	cache := activeSegmentCache
	enabled := segmentCacheEnabled
	cacheMu.RUnlock()

	if !enabled {
		return
	}
	cache.Reset()
}
