package hls

import (
	"sync"
	"time"

	"github.com/bariiss/hls-proxy/model"
	log "github.com/sirupsen/logrus"
)

type manifestSegment struct {
	Sequence      int
	Tags          []string
	Line          string
	ClipURL       string
	HasKey        bool
	DecryptionKey string
	IV            int
}

type manifestHistory struct {
	mu                sync.Mutex
	playlistID        string
	segments          map[string]*manifestSegment
	order             []string
	lastAccess        time.Time
	nextSeq           int
	segmentsRequested bool
}

var histories = newConcurrentMap[string, *manifestHistory]()
var manifestJanitorOnce sync.Once

func getManifestHistory(key string) *manifestHistory {
	history, exists := histories.Get(key)
	if exists {
		history.touch()
		return history
	}
	return createManifestHistory(key)
}

func createManifestHistory(key string) *manifestHistory {
	history := &manifestHistory{
		segments:   make(map[string]*manifestSegment),
		order:      make([]string, 0),
		lastAccess: time.Now(),
	}
	histories.Set(key, history)
	return history
}

func (h *manifestHistory) ensurePlaylistID(id string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.playlistID == "" {
		h.playlistID = id
	}
	return h.playlistID
}

func (h *manifestHistory) currentPlaylistID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.playlistID
}

func (h *manifestHistory) merge(entries []*manifestSegment, limit int) []*manifestSegment {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastAccess = time.Now()

	for _, entry := range entries {
		if entry == nil || entry.ClipURL == "" {
			continue
		}
		existing, ok := h.segments[entry.ClipURL]
		if ok {
			existing.Tags = append([]string(nil), entry.Tags...)
			existing.Line = entry.Line
			existing.ClipURL = entry.ClipURL
			existing.HasKey = entry.HasKey
			existing.DecryptionKey = entry.DecryptionKey
			existing.IV = entry.IV
			continue
		}

		entry.Sequence = h.nextSeq
		h.nextSeq++
		h.segments[entry.ClipURL] = entry
		h.order = append(h.order, entry.ClipURL)
	}

	if limit > 0 && len(h.order) > limit {
		drop := len(h.order) - limit
		for i := range drop {
			clip := h.order[i]
			delete(h.segments, clip)
		}
		h.order = append([]string(nil), h.order[drop:]...)
	}

	combined := make([]*manifestSegment, 0, len(h.order))
	for _, clip := range h.order {
		segment, ok := h.segments[clip]
		if !ok {
			continue
		}
		combined = append(combined, segment)
	}

	return combined
}

func (h *manifestHistory) touch() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastAccess = time.Now()
}

func (h *manifestHistory) inactiveSince(cutoff time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lastAccess.IsZero() {
		return false
	}
	return h.lastAccess.Before(cutoff)
}

func (h *manifestHistory) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.segments = make(map[string]*manifestSegment)
	h.order = nil
	h.playlistID = ""
	h.lastAccess = time.Time{}
	h.nextSeq = 0
	h.segmentsRequested = false
}

func (h *manifestHistory) markSegmentRequested() {
	h.mu.Lock()
	h.lastAccess = time.Now()
	h.segmentsRequested = true
	h.mu.Unlock()
}

func (h *manifestHistory) hasServedSegments() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.segmentsRequested
}

// TouchManifest refreshes the last-access time for a manifest history if it exists.
func TouchManifest(key string) {
	if key == "" {
		return
	}
	if history, ok := histories.Get(key); ok {
		history.touch()
	}
}

func RecordSegmentRequest(key string) {
	if key == "" {
		return
	}
	history := getManifestHistory(key)
	history.markSegmentRequested()
}

// StartManifestInactivityJanitor purges inactive manifests and their persisted segments.
func StartManifestInactivityJanitor(prefetcher *Prefetcher, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	manifestJanitorOnce.Do(func() {
		interval := max(ttl/2, 5*time.Second)
		ticker := time.NewTicker(interval)
		go func() {
			for range ticker.C {
				purgeInactiveManifests(prefetcher, ttl)
			}
		}()
	})
}

func purgeInactiveManifests(prefetcher *Prefetcher, ttl time.Duration) {
	cutoff := time.Now().Add(-ttl)
	for key, history := range histories.Items() {
		if history == nil || !history.inactiveSince(cutoff) {
			continue
		}

		if model.Configuration.SegmentIdleRequireSegments && !history.hasServedSegments() {
			continue
		}

		playlistID := history.currentPlaylistID()
		history.reset()
		histories.Remove(key)

		if playlistID == "" {
			continue
		}

		log.Infof("Purging inactive manifest %s", playlistID)
		if prefetcher != nil {
			prefetcher.RemovePlaylist(playlistID)
		}

		if err := RemoveManifestSegments(playlistID); err != nil {
			log.Warnf("Failed to remove persisted segments for %s: %v", playlistID, err)
		}

		ClearSegmentCache(playlistID)
	}
}
