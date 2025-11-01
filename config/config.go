package config

import (
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

type Values struct {
	HTTPClientTimeout time.Duration
	HTTPDialTimeout   time.Duration
	RetryRequestDelay time.Duration
	RetryClipDelay    time.Duration
	UserAgent         string
}

var Settings = load()

func load() Values {
	return Values{
		HTTPClientTimeout: getDuration("HTTP_CLIENT_TIMEOUT", 60*time.Second),
		HTTPDialTimeout:   getDuration("HTTP_DIAL_TIMEOUT", 15*time.Second),
		RetryRequestDelay: getDuration("HTTP_RETRY_REQUEST_DELAY", 3*time.Second),
		RetryClipDelay:    getDuration("HTTP_RETRY_CLIP_DELAY", 2*time.Second),
		UserAgent:         getString("HTTP_USER_AGENT", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"),
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
