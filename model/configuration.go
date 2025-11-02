package model

import "time"

var (
	Configuration Config
)

type Config struct {
	Prefetch                   bool
	SegmentCount               int
	SegmentCache               bool
	SegmentStore               bool
	SegmentStorageDir          string
	SegmentIdleEnabled         bool
	SegmentIdleTimeout         time.Duration
	SegmentIdleRequireSegments bool
	SegmentBackgroundFetch     bool
	Throttle                   int
	Attempts                   int
	ClipRetention              time.Duration
	PlaylistRetention          time.Duration
	JanitorInterval            time.Duration
	UseHttps                   bool
	DecryptSegments            bool
	Host                       string
	Port                       string
	LogLevel                   string
	Healthcheck                bool
}

type ConfigInit struct {
	Prefetch                   bool
	SegmentCount               int
	SegmentCache               bool
	SegmentStore               bool
	SegmentStorageDir          string
	SegmentIdleEnabled         bool
	SegmentIdleTimeout         time.Duration
	SegmentIdleRequireSegments bool
	SegmentBackgroundFetch     bool
	Throttle                   int
	Attempts                   int
	ClipRetention              time.Duration
	PlaylistRetention          time.Duration
	JanitorInterval            time.Duration
	UseHttps                   bool
	DecryptSegments            bool
	Host                       string
	Port                       string
	LogLevel                   string
	Healthcheck                bool
}

func InitializeConfig(opts ConfigInit) {
	Configuration = Config(opts)
}
