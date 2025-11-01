package config

import (
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

type Values struct {
	Prefetch          bool
	SegmentCount      int
	Throttle          int
	JanitorInterval   time.Duration
	Attempts          int
	ClipRetention     time.Duration
	PlaylistRetention time.Duration
	Host              string
	Port              string
	LogLevel          string
	UseHTTPS          bool
	DecryptSegments   bool
	HTTPClientTimeout time.Duration
	HTTPDialTimeout   time.Duration
	RetryRequestDelay time.Duration
	RetryClipDelay    time.Duration
	UserAgent         string
}

var Settings = load()

func load() Values {
	return Values{
		Prefetch:          getBool("PREFETCH", true),
		SegmentCount:      getInt("SEGMENTS", 30),
		Throttle:          getInt("THROTTLE", 5),
		JanitorInterval:   getDuration("JANITOR_INTERVAL", 20*time.Second),
		Attempts:          getInt("ATTEMPTS", 3),
		ClipRetention:     getDuration("CLIP_RETENTION", 30*time.Minute),
		PlaylistRetention: getDuration("PLAYLIST_RETENTION", 5*time.Hour),
		Host:              getString("HOST", ""),
		Port:              getString("PORT", "1323"),
		LogLevel:          getString("LOG_LEVEL", "PRODUCTION"),
		HTTPClientTimeout: getDuration("HTTP_CLIENT_TIMEOUT", 60*time.Second),
		HTTPDialTimeout:   getDuration("HTTP_DIAL_TIMEOUT", 15*time.Second),
		RetryRequestDelay: getDuration("HTTP_RETRY_REQUEST_DELAY", 3*time.Second),
		RetryClipDelay:    getDuration("HTTP_RETRY_CLIP_DELAY", 2*time.Second),
		UserAgent:         getString("HTTP_USER_AGENT", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"),
		UseHTTPS:          getBool("HTTPS", false),
		DecryptSegments:   getBool("DECRYPT", false),
	}
}

func getDuration(envKey string, fallback time.Duration) time.Duration {
	if value := os.Getenv(envKey); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
		log.Warnf("Invalid duration provided for %s: %s. Falling back to default %s", envKey, value, fallback)
	}

	return fallback
}

func getString(envKey, fallback string) string {
	if value := os.Getenv(envKey); value != "" {
		return value
	}
	return fallback
}

func getInt(envKey string, fallback int) int {
	if value := os.Getenv(envKey); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			log.Warnf("Invalid integer provided for %s: %s. Falling back to default %d", envKey, value, fallback)
			return fallback
		}
		return parsed
	}
	return fallback
}

func getBool(envKey string, fallback bool) bool {
	if value := os.Getenv(envKey); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			log.Warnf("Invalid boolean provided for %s: %s. Falling back to default %t", envKey, value, fallback)
			return fallback
		}
		return parsed
	}
	return fallback
}
