package hls

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bariiss/hls-proxy/model"
	log "github.com/sirupsen/logrus"
)

type segmentStore interface {
	Save(manifestID, key string, data []byte) error
	Load(manifestID, key string) ([]byte, bool, error)
	Remove(manifestID string) error
}

type noopSegmentStore struct{}

func (noopSegmentStore) Save(string, string, []byte) error         { return nil }
func (noopSegmentStore) Load(string, string) ([]byte, bool, error) { return nil, false, nil }
func (noopSegmentStore) Remove(string) error                       { return nil }

type fileSegmentStore struct {
	baseDir string
	limit   int
	mu      sync.Mutex
}

func newFileSegmentStore(baseDir string, limit int) (*fileSegmentStore, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create segment directory: %w", err)
	}
	return &fileSegmentStore{baseDir: baseDir, limit: limit}, nil
}

func (s *fileSegmentStore) Save(manifestID, key string, data []byte) error {
	if len(data) == 0 || manifestID == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.pathFor(manifestID, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create segment path: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write segment temp file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		// attempt to remove temp file on failure to avoid leftovers
		_ = os.Remove(tmp)
		return fmt.Errorf("finalize segment file: %w", err)
	}

	s.enforceLimitLocked(manifestID)
	return nil
}

type storedSegmentFile struct {
	path    string
	modTime time.Time
}

func (s *fileSegmentStore) enforceLimitLocked(manifestID string) {
	if s.limit <= 0 {
		return
	}

	manifestRoot := s.manifestRoot(manifestID)
	if manifestRoot == "" {
		return
	}

	info, statErr := os.Stat(manifestRoot)
	if errors.Is(statErr, os.ErrNotExist) {
		return
	}
	if statErr != nil {
		log.Warnf("inspect segment directory for %s: %v", manifestID, statErr)
		return
	}
	if !info.IsDir() {
		return
	}

	var files []storedSegmentFile
	err := filepath.Walk(manifestRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".seg") {
			return nil
		}
		files = append(files, storedSegmentFile{path: path, modTime: info.ModTime()})
		return nil
	})
	if err != nil {
		log.Warnf("walk segment directory for %s: %v", manifestID, err)
		return
	}

	if len(files) <= s.limit {
		return
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	excess := len(files) - s.limit
	for i := range excess {
		removePath := files[i].path
		if err := os.Remove(removePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warnf("remove stale segment %s: %v", removePath, err)
			continue
		}
		cleanupEmptyDirs(filepath.Dir(removePath), manifestRoot)
	}
}

func cleanupEmptyDirs(start, stop string) {
	stop = filepath.Clean(stop)
	current := filepath.Clean(start)
	for {
		if current == stop || current == string(filepath.Separator) {
			return
		}
		entries, err := os.ReadDir(current)
		if err != nil {
			return
		}
		if len(entries) > 0 {
			return
		}
		if err := os.Remove(current); err != nil {
			return
		}
		next := filepath.Dir(current)
		if next == current {
			return
		}
		current = next
	}
}

func (s *fileSegmentStore) Load(manifestID, key string) ([]byte, bool, error) {
	if manifestID == "" {
		return nil, false, nil
	}
	path := s.pathFor(manifestID, key)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read segment file: %w", err)
	}
	return data, true, nil
}

func (s *fileSegmentStore) pathFor(manifestID, key string) string {
	sum := sha1.Sum([]byte(key))
	hexKey := hex.EncodeToString(sum[:])
	return filepath.Join(s.manifestRoot(manifestID), hexKey[:2], hexKey[2:]+".seg")
}

func (s *fileSegmentStore) manifestRoot(manifestID string) string {
	if manifestID == "" {
		return ""
	}
	return filepath.Join(s.baseDir, sanitizeManifestID(manifestID))
}

func (s *fileSegmentStore) Remove(manifestID string) error {
	if manifestID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	root := s.manifestRoot(manifestID)
	if root == "" {
		return nil
	}
	if err := os.RemoveAll(root); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove manifest segments: %w", err)
	}
	return nil
}

var (
	storeMu       sync.RWMutex
	activeStore   segmentStore = noopSegmentStore{}
	storeEnabled  bool
	storeBaseDir  string
	cleanupSignal sync.Once
)

// ConfigureSegmentStore switches the active segment storage implementation.
func ConfigureSegmentStore(enabled bool, baseDir string) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	if !enabled {
		activeStore = noopSegmentStore{}
		storeEnabled = false
		storeBaseDir = ""
		return nil
	}

	fs, err := newFileSegmentStore(baseDir, model.Configuration.SegmentCount)
	if err != nil {
		return err
	}
	activeStore = fs
	storeEnabled = true
	storeBaseDir = baseDir
	registerCleanup()
	return nil
}

// SaveSegment persists the provided payload if a store is configured.
func SaveSegment(manifestID, key string, data []byte) error {
	storeMu.RLock()
	store := activeStore
	storeMu.RUnlock()
	if !storeEnabled || manifestID == "" {
		return nil
	}
	return store.Save(manifestID, key, data)
}

// LoadSegment retrieves the stored payload for the supplied key.
func LoadSegment(manifestID, key string) ([]byte, bool, error) {
	storeMu.RLock()
	store := activeStore
	storeMu.RUnlock()
	if !storeEnabled || manifestID == "" {
		return nil, false, nil
	}
	return store.Load(manifestID, key)
}

// RemoveManifestSegments deletes all persisted segments for a manifest, if segment storage is active.
func RemoveManifestSegments(manifestID string) error {
	storeMu.RLock()
	store := activeStore
	enabled := storeEnabled
	storeMu.RUnlock()

	if !enabled || manifestID == "" {
		return nil
	}
	return store.Remove(manifestID)
}

// CleanupSegmentStore removes any persisted segments from disk.
func CleanupSegmentStore() error {
	storeMu.Lock()
	defer storeMu.Unlock()

	if !storeEnabled || storeBaseDir == "" {
		return nil
	}

	baseDir := storeBaseDir
	if err := os.RemoveAll(baseDir); err != nil {
		return err
	}
	if baseDir != "" {
		_ = os.MkdirAll(baseDir, 0o755)
	}
	storeEnabled = false
	storeBaseDir = ""
	activeStore = noopSegmentStore{}
	return nil
}

func sanitizeManifestID(id string) string {
	id = strings.TrimSpace(id)
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		"?", "_",
		"*", "_",
		"<", "_",
		">", "_",
		"|", "_",
		"\"", "_",
		":", "-",
		"+", "-",
		"=", "",
	)
	sanitized := replacer.Replace(id)
	if sanitized == "" {
		return "manifest"
	}
	if len(sanitized) > 120 {
		return sanitized[:120]
	}
	return sanitized
}

func registerCleanup() {
	cleanupSignal.Do(func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			sig := <-sigCh
			log.Infof("received signal %s, cleaning segment store", sig)
			if err := CleanupSegmentStore(); err != nil {
				log.Warnf("segment cleanup failed: %v", err)
			}
			ResetSegmentCache()
			signal.Reset(os.Interrupt, syscall.SIGTERM)
			if s, ok := sig.(syscall.Signal); ok {
				_ = syscall.Kill(os.Getpid(), s)
				return
			}
			os.Exit(0)
		}()
	})
}
