package cmd

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bariiss/hls-proxy/config"
	"github.com/bariiss/hls-proxy/model"
	"github.com/bariiss/hls-proxy/parsing"
	"github.com/bariiss/hls-proxy/proxy"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use:           "hls-proxy",
		Short:         "Proxy and transform HLS manifests",
		Long:          "hls-proxy fetches, rewrites, and optionally prefetches HLS manifests and segments.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := applyConfiguration(); err != nil {
				return err
			}

			if flagValues.healthcheck {
				return runHealthcheck()
			}

			proxy.InitPrefetcher(&model.Configuration)
			log.Infof("Configuration: %+v", model.Configuration)

			portInt, err := strconv.Atoi(flagValues.port)
			if err != nil {
				return fmt.Errorf("invalid port %q: %w", flagValues.port, err)
			}

			launchServer(flagValues.host, portInt)
			return nil
		},
	}

	flagValues struct {
		prefetch           bool
		segments           int
		throttle           int
		janitor            time.Duration
		attempts           int
		clipRetention      time.Duration
		playlistRet        time.Duration
		https              bool
		decrypt            bool
		host               string
		port               string
		logLevel           string
		healthcheck        bool
		segmentStore       bool
		segmentCache       bool
		segmentDir         string
		segmentIdle        time.Duration
		segmentIdleEnabled bool
	}
)

func init() {
	rootCmd.Flags().BoolVar(&flagValues.prefetch, "prefetch", config.Settings.Prefetch, "Prefetch TS segments before they are requested")
	rootCmd.Flags().IntVar(&flagValues.segments, "segments", config.Settings.SegmentCount, "Number of segments to prefetch")
	rootCmd.Flags().IntVar(&flagValues.throttle, "throttle", config.Settings.Throttle, "Requests per second limit for prefetching")
	rootCmd.Flags().DurationVar(&flagValues.janitor, "janitor-interval", config.Settings.JanitorInterval, "Interval for cleaning cached playlists and clips")
	rootCmd.Flags().IntVar(&flagValues.attempts, "attempts", config.Settings.Attempts, "Retry attempts for segment fetches")
	rootCmd.Flags().DurationVar(&flagValues.clipRetention, "clip-retention", config.Settings.ClipRetention, "Duration to keep segments cached")
	rootCmd.Flags().DurationVar(&flagValues.playlistRet, "playlist-retention", config.Settings.PlaylistRetention, "Duration to keep playlists cached")
	rootCmd.Flags().BoolVar(&flagValues.https, "https", config.Settings.UseHTTPS, "Serve proxied URLs with HTTPS scheme")
	rootCmd.Flags().BoolVar(&flagValues.decrypt, "decrypt", config.Settings.DecryptSegments, "Decrypt AES-128 segments when keys are provided")
	rootCmd.Flags().StringVar(&flagValues.host, "host", defaultHost(config.Settings.Host), "Host address to bind and advertise in rewritten manifests")
	rootCmd.Flags().StringVar(&flagValues.port, "port", config.Settings.Port, "Port to bind the HTTP server")
	rootCmd.Flags().StringVar(&flagValues.logLevel, "log-level", strings.ToUpper(config.Settings.LogLevel), "Log level (DEBUG, INFO, WARN, ERROR)")
	rootCmd.Flags().BoolVar(&flagValues.healthcheck, "healthcheck", false, "Run healthcheck against the configured server and exit")
	rootCmd.Flags().BoolVar(&flagValues.segmentStore, "segment-store", config.Settings.SegmentStore, "Persist fetched segments to disk for replay")
	rootCmd.Flags().BoolVar(&flagValues.segmentCache, "segment-cache", config.Settings.SegmentCache, "Cache fetched segments in memory for replay")
	rootCmd.Flags().StringVar(&flagValues.segmentDir, "segment-dir", config.Settings.SegmentStorageDir, "Directory for persisted segments when segment storage is enabled")
	rootCmd.Flags().DurationVar(&flagValues.segmentIdle, "segment-idle-timeout", config.Settings.SegmentIdleTimeout, "Duration with no requests before manifest cache and stored segments are purged")
	rootCmd.Flags().BoolVar(&flagValues.segmentIdleEnabled, "segment-idle-enabled", config.Settings.SegmentIdleEnabled, "Enable purging manifests and stored segments after periods of inactivity")
}

func Execute() error {
	return rootCmd.Execute()
}

func applyConfiguration() error {
	if flagValues.segmentStore && flagValues.segmentCache {
		log.Warn("segment cache disabled because segment store is enabled")
		flagValues.segmentCache = false
	}

	options := model.ConfigInit{
		Prefetch:           flagValues.prefetch,
		SegmentCount:       flagValues.segments,
		Throttle:           flagValues.throttle,
		Attempts:           flagValues.attempts,
		ClipRetention:      flagValues.clipRetention,
		PlaylistRetention:  flagValues.playlistRet,
		JanitorInterval:    flagValues.janitor,
		UseHttps:           flagValues.https,
		DecryptSegments:    flagValues.decrypt,
		Host:               flagValues.host,
		Port:               flagValues.port,
		LogLevel:           flagValues.logLevel,
		Healthcheck:        flagValues.healthcheck,
		SegmentStore:       flagValues.segmentStore,
		SegmentCache:       flagValues.segmentCache,
		SegmentStorageDir:  flagValues.segmentDir,
		SegmentIdleEnabled: flagValues.segmentIdleEnabled,
		SegmentIdleTimeout: flagValues.segmentIdle,
	}

	model.InitializeConfig(options)
	return setLogLevel(flagValues.logLevel)
}

func setLogLevel(level string) error {
	switch strings.ToUpper(level) {
	case "DEBUG":
		log.SetLevel(log.DebugLevel)
	case "INFO", "PRODUCTION", "":
		log.SetLevel(log.InfoLevel)
	case "WARN", "WARNING":
		log.SetLevel(log.WarnLevel)
	case "ERROR":
		log.SetLevel(log.ErrorLevel)
	default:
		return fmt.Errorf("unsupported log level %q", level)
	}
	return nil
}

func launchServer(host string, port int) {
	godotenv.Load()
	e := echo.New()

	e.Use(middleware.CORS())
	e.Use(jsonLoggerMiddleware())
	e.Use(middleware.Recover())

	e.GET("/health", handleHealth)
	e.GET("/:input", handleRequest)

	address := fmt.Sprintf("%s:%d", host, port)
	e.Logger.Fatal(e.Start(address))
}

func handleRequest(c echo.Context) error {
	switch c.Param("input") {
	case "favicon.ico", "apple-touch-icon.png", "apple-touch-icon-precomposed.png":
		return echo.NewHTTPError(http.StatusNotFound, "resource not available")
	}

	input, err := parsing.ParseInputUrl(c.Param("input"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	parsedURL, err := url.Parse(input.Url)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "malformed URL in request")
	}

	if strings.HasSuffix(parsedURL.Path, ".m3u8") {
		return proxy.ManifestProxy(c, input)
	}
	return proxy.TsProxy(c, input)
}

func handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func runHealthcheck() error {
	host := model.Configuration.Host
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}

	port := model.Configuration.Port
	if port == "" {
		port = "1323"
	}

	url := fmt.Sprintf("http://%s:%s/health", host, port)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck failed: status %d", resp.StatusCode)
	}
	return nil
}

func defaultHost(current string) string {
	if strings.TrimSpace(current) == "" {
		return "127.0.0.1"
	}
	return current
}
