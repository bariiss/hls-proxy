package model

import (
	"time"

	"github.com/bariiss/hls-proxy/config"
	"github.com/urfave/cli/v2"
)

var (
	Configuration Config
)

type Config struct {
	Prefetch          bool
	SegmentCount      int
	Throttle          int
	Attempts          int
	ClipRetention     time.Duration
	PlaylistRetention time.Duration
	JanitorInterval   time.Duration
	UseHttps          bool
	DecryptSegments   bool
	Host              string
	Port              string
}

func InitializeConfig(c *cli.Context) {
	cfg := Config{
		Prefetch:          config.Settings.Prefetch,
		SegmentCount:      config.Settings.SegmentCount,
		Throttle:          config.Settings.Throttle,
		Attempts:          config.Settings.Attempts,
		ClipRetention:     config.Settings.ClipRetention,
		PlaylistRetention: config.Settings.PlaylistRetention,
		JanitorInterval:   config.Settings.JanitorInterval,
		DecryptSegments:   config.Settings.DecryptSegments,
		UseHttps:          config.Settings.UseHTTPS,
		Host:              config.Settings.Host,
		Port:              config.Settings.Port,
	}

	if c.IsSet("prefetch") {
		cfg.Prefetch = c.Bool("prefetch")
	}
	if c.IsSet("segments") {
		cfg.SegmentCount = c.Int("segments")
	}
	if c.IsSet("throttle") {
		cfg.Throttle = c.Int("throttle")
	}
	if c.IsSet("attempts") {
		cfg.Attempts = c.Int("attempts")
	}
	if c.IsSet("clip-retention") {
		cfg.ClipRetention = c.Duration("clip-retention")
	}
	if c.IsSet("playlist-retention") {
		cfg.PlaylistRetention = c.Duration("playlist-retention")
	}
	if c.IsSet("janitor-interval") {
		cfg.JanitorInterval = c.Duration("janitor-interval")
	}
	if c.IsSet("decrypt") {
		cfg.DecryptSegments = c.Bool("decrypt")
	}
	if c.IsSet("https") {
		cfg.UseHttps = c.Bool("https")
	}
	if c.IsSet("host") {
		cfg.Host = c.String("host")
	}
	if c.IsSet("port") {
		cfg.Port = c.String("port")
	}

	Configuration = cfg
}
