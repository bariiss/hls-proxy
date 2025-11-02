package hls

import (
	"errors"
	"math"
	"net/http"
	"runtime"
	"time"

	"github.com/bariiss/hls-proxy/http_retry"
	"github.com/bariiss/hls-proxy/model"
	mapset "github.com/deckarep/golang-set/v2"
	log "github.com/sirupsen/logrus"
)

// Interface for structures that can be cleaned by a janitor
type Cleanable interface {
	setJanitor(j *Janitor)
	getJanitor() *Janitor
	Clean()
}

// CacheItem is a struct that holds the data and expiration time of a cached item
// This makes it possible for the janitor to clean the cache
type CacheItem[T any] struct {
	Data       T
	Expiration time.Time
}

// Janitor is a struct that holds the interval and stop channel for a janitor
type Janitor struct {
	Interval time.Duration
	stop     chan bool
}

// Run is a function that runs the janitor, and cleans the cache at the specified interval
// It stops when the stop recives a value
func (j *Janitor) Run(c Cleanable) {
	ticker := time.NewTicker(j.Interval)
	for {
		select {
		case <-ticker.C:
			c.Clean()
		case <-j.stop:
			ticker.Stop()
			return
		}
	}
}

func runJanitor(c Cleanable, ci time.Duration) {
	j := &Janitor{
		Interval: ci,
		stop:     make(chan bool),
	}
	c.setJanitor(j)
	go j.Run(c)
}

// Structure that holds the playlist clips and the cached clips
type PrefetchPlaylist struct {
	clipRetention time.Duration
	playlistId    string
	playlistClips []string
	clipToIndex   *concurrentMap[string, int]
	fetchedClips  *concurrentMap[string, CacheItem[[]byte]]
}

func newPrefetchPlaylist(playlistId string, playlistClips []string, clipRetention time.Duration) *PrefetchPlaylist {
	clipToIndex := newConcurrentMap[string, int]()
	fetchedClips := newConcurrentMap[string, CacheItem[[]byte]]()

	for index, clip := range playlistClips {
		clipToIndex.Set(clip, index)
	}
	return &PrefetchPlaylist{
		playlistId:    playlistId,
		playlistClips: playlistClips,
		clipToIndex:   clipToIndex,
		fetchedClips:  fetchedClips,
		clipRetention: clipRetention,
	}
}

func initJanitor(cache Cleanable, ci time.Duration) {
	if ci <= 0 {
		return
	}
	runtime.SetFinalizer(cache, func(cache Cleanable) {
		stopJanitor(cache.getJanitor())
	})
	runJanitor(cache, ci)
}

func stopJanitor(j *Janitor) {
	if j == nil {
		return
	}
	select {
	case j.stop <- true:
	default:
	}
}

func (m PrefetchPlaylist) Clean() {
	log.Debug("Cleaning playlist ", m.playlistId)
	currentTime := time.Now()
	for clipUrl, clipItem := range m.fetchedClips.Items() {
		if clipItem.Expiration.Before(currentTime) {
			log.Debug("Removed clip from ", m.playlistId, " with url", clipUrl)
			m.fetchedClips.Remove(clipUrl)
		}
	}

}

func (m PrefetchPlaylist) getNextPrefetchClips(clipUrl string, count int) []string {
	clipIndex, ok := m.clipToIndex.Get(clipUrl)
	if !ok {
		return []string{}
	}

	start := clipIndex + 1
	end := int(math.Min(float64(start+count), float64(len(m.playlistClips))))
	if start >= end {
		return []string{}
	}
	return m.playlistClips[start:end]
}

func (m PrefetchPlaylist) addClip(clipUrl string, data []byte) error {
	now := time.Now()
	expires := now.Add(m.clipRetention)
	m.fetchedClips.Set(clipUrl, CacheItem[[]byte]{
		Data:       data,
		Expiration: expires,
	})

	if err := SaveSegment(m.playlistId, clipUrl, data); err != nil {
		log.Warn("Failed to persist segment ", clipUrl, ": ", err)
		return err
	}

	SaveSegmentCache(m.playlistId, clipUrl, data)
	return nil
}

/*
Structure that handles prefetching of clips, and caching of playlists
Supports automatic cleanup of cached playlists and clips if using the NewPrefetcherWithJanitor constructor
*/
type Prefetcher struct {
	janitor              *Janitor
	clipPrefetchCount    int
	currentlyPrefetching mapset.Set[string]
	playlistInfo         *concurrentMap[string, CacheItem[*PrefetchPlaylist]]
	playlistRetention    time.Duration
	clipRetention        time.Duration
}

func (p Prefetcher) GetFetchedClip(playlistId string, clipUrl string) ([]byte, bool) {
	playlistItem, ok := p.playlistInfo.Get(playlistId)

	if !ok {
		return nil, false
	}

	playlist := playlistItem.Data
	data, foundClip := playlist.fetchedClips.Get(clipUrl)
	clipIndex, foundIndex := playlist.clipToIndex.Get(clipUrl)

	if foundIndex {
		firstClip := math.Min(float64(clipIndex+1), float64(len(playlist.playlistClips)-1))
		go p.prefetchClips(playlist.playlistClips[int(firstClip)], playlistId)
	}

	if !foundClip {
		return nil, false
	}

	return data.Data, ok
}

func (p Prefetcher) AddPlaylistToCache(playlistId string, clipUrls []string) {
	log.Debug("Adding playlist to cache ", playlistId)
	expires := time.Now().Add(p.playlistRetention)
	newPlaylist := newPrefetchPlaylist(playlistId, clipUrls, p.clipRetention)

	existingItem, ok := p.playlistInfo.Get(playlistId)
	if ok {
		mergeFetchedClips(existingItem.Data, newPlaylist, clipUrls)
	}

	p.playlistInfo.Set(playlistId, CacheItem[*PrefetchPlaylist]{
		Data:       newPlaylist,
		Expiration: expires,
	})
}

// We might want to introduce a delay between requests to the same host
func (p Prefetcher) prefetchClips(clipUrl string, playlistId string) error {
	playlistItem, ok := p.playlistInfo.Get(playlistId)
	if !ok {
		return nil
	}

	playlist := playlistItem.Data
	nextClips := playlist.getNextPrefetchClips(clipUrl, p.clipPrefetchCount)
	p.queueClipsForPrefetch(playlist, nextClips)
	return nil
}

func (p *Prefetcher) WarmPlaylist(playlistId string) {
	if p == nil || playlistId == "" {
		return
	}

	playlistItem, ok := p.playlistInfo.Get(playlistId)
	if !ok {
		return
	}

	playlist := playlistItem.Data
	if playlist == nil {
		return
	}

	limit := p.clipPrefetchCount
	if limit <= 0 || limit > len(playlist.playlistClips) {
		limit = len(playlist.playlistClips)
	}
	if limit == 0 {
		return
	}

	clips := append([]string(nil), playlist.playlistClips[:limit]...)
	go p.queueClipsForPrefetch(playlist, clips)
}

func (p *Prefetcher) queueClipsForPrefetch(playlist *PrefetchPlaylist, clips []string) {
	if p == nil || playlist == nil || len(clips) == 0 {
		return
	}

	throttleRate := model.Configuration.Throttle
	if throttleRate <= 0 {
		throttleRate = 1
	}

	interval := time.Second / time.Duration(throttleRate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	first := true
	for _, clip := range clips {
		if clip == "" {
			continue
		}
		if p.currentlyPrefetching.Contains(clip) || playlist.fetchedClips.Has(clip) {
			continue
		}

		if !first {
			<-ticker.C
		} else {
			first = false
		}

		p.currentlyPrefetching.Add(clip)
		go func(clip string) {
			defer p.currentlyPrefetching.Remove(clip)

			data, err := fetchClip(clip)
			if err != nil {
				log.Debug("Error fetching clip ", clip, err)
				return
			}
			log.Debug("Fetched clip ", clip)

			if err := playlist.addClip(clip, data); err != nil {
				log.Debug("Failed to cache clip ", clip, err)
				return
			}

			RecordSegmentRequest(playlist.playlistId)
			log.Debug("Number of cached clips", playlist.fetchedClips.Count())
		}(clip)
	}
}

func fetchClip(clipUrl string) ([]byte, error) {
	if clipUrl == "" {
		return nil, errors.New("clip URL is empty")
	}

	request, err := http.NewRequest("GET", clipUrl, nil)
	if err != nil {
		log.Error("Error creating request ", clipUrl, err)
		return nil, err
	}

	resp, err := http_retry.ExecuteRetryClipRequest(request, model.Configuration.Attempts)
	if err != nil {
		log.Error("Error fetching clip ", clipUrl, err)
		return nil, err
	}

	return resp, nil
}

func NewPrefetcher(clipPrefetchCount int, playlistRetention time.Duration, clipRetention time.Duration) *Prefetcher {
	return &Prefetcher{
		clipPrefetchCount:    clipPrefetchCount,
		currentlyPrefetching: mapset.NewSet[string](),
		playlistInfo:         newConcurrentMap[string, CacheItem[*PrefetchPlaylist]](),
		playlistRetention:    playlistRetention,
		clipRetention:        clipRetention,
	}
}

func (p *Prefetcher) setJanitor(j *Janitor) {
	p.janitor = j
}

func (p *Prefetcher) getJanitor() *Janitor {
	return p.janitor
}

func (p *Prefetcher) Clean() {
	log.Debug("Cleaning playlist cache")
	currentTime := time.Now()
	for playlistId, playlistItem := range p.playlistInfo.Items() {
		if playlistItem.Expiration.Before(currentTime) {
			log.Debug("Removed playlist ", playlistId)
			p.playlistInfo.Remove(playlistId)
		} else {
			playlist := playlistItem.Data
			playlist.Clean()
		}
	}
}

// RemovePlaylist discards cached data for a playlist identifier.
func (p *Prefetcher) RemovePlaylist(playlistId string) {
	if p == nil || playlistId == "" {
		return
	}
	p.playlistInfo.Remove(playlistId)
}

func mergeFetchedClips(existing, target *PrefetchPlaylist, clipUrls []string) {
	if existing == nil {
		return
	}

	clipSet := make(map[string]struct{}, len(clipUrls))
	for _, clip := range clipUrls {
		clipSet[clip] = struct{}{}
	}

	for clip, item := range existing.fetchedClips.Items() {
		if _, keep := clipSet[clip]; keep {
			target.fetchedClips.Set(clip, item)
		}
	}
}

func NewPrefetcherWithJanitor(clipPrefetchCount int, janitorInterval time.Duration, playlistRetention time.Duration, clipRetention time.Duration) *Prefetcher {
	p := NewPrefetcher(clipPrefetchCount, playlistRetention, clipRetention)
	initJanitor(p, janitorInterval)
	return p
}
