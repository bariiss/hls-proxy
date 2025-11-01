package model

import "time"

var (
	Configuration Config
)

type Config struct {
	Prefetch           bool
	SegmentCount       int
	SegmentCache       bool
	Throttle           int
	Attempts           int
	ClipRetention      time.Duration
	PlaylistRetention  time.Duration
	JanitorInterval    time.Duration
	UseHttps           bool
	DecryptSegments    bool
	Host               string
	Port               string
	LogLevel           string
	Healthcheck        bool
	SegmentStore       bool
	SegmentStorageDir  string
	SegmentIdleEnabled bool
	SegmentIdleTimeout time.Duration
}

type ConfigInit struct {
	Prefetch           bool
	SegmentCount       int
	SegmentCache       bool
	Throttle           int
	Attempts           int
	ClipRetention      time.Duration
	PlaylistRetention  time.Duration
	JanitorInterval    time.Duration
	UseHttps           bool
	DecryptSegments    bool
	Host               string
	Port               string
	LogLevel           string
	Healthcheck        bool
	SegmentStore       bool
	SegmentStorageDir  string
	SegmentIdleEnabled bool
	SegmentIdleTimeout time.Duration
}

func InitializeConfig(opts ConfigInit) {
	Configuration = Config{
		Prefetch:           opts.Prefetch,
		SegmentCount:       opts.SegmentCount,
		Throttle:           opts.Throttle,
		Attempts:           opts.Attempts,
		ClipRetention:      opts.ClipRetention,
		PlaylistRetention:  opts.PlaylistRetention,
		JanitorInterval:    opts.JanitorInterval,
		UseHttps:           opts.UseHttps,
		DecryptSegments:    opts.DecryptSegments,
		Host:               opts.Host,
		Port:               opts.Port,
		LogLevel:           opts.LogLevel,
		Healthcheck:        opts.Healthcheck,
		SegmentStore:       opts.SegmentStore,
		SegmentCache:       opts.SegmentCache,
		SegmentStorageDir:  opts.SegmentStorageDir,
		SegmentIdleEnabled: opts.SegmentIdleEnabled,
		SegmentIdleTimeout: opts.SegmentIdleTimeout,
	}
}
