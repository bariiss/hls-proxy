package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/bariiss/hls-proxy/config"
	"github.com/bariiss/hls-proxy/model"
	parsing "github.com/bariiss/hls-proxy/parsing"
	proxy "github.com/bariiss/hls-proxy/proxy"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "prefetch",
				Usage: "prefetch ts files",
				Value: config.Settings.Prefetch,
			},
			&cli.IntFlag{
				Name:  "segments",
				Usage: "how many segments to prefetch",
				Value: config.Settings.SegmentCount,
			},
			&cli.IntFlag{
				Name:  "throttle",
				Usage: "how much to throttle prefetch requests (requests per second)",
				Value: config.Settings.Throttle,
			},
			&cli.DurationFlag{
				Name:  "janitor-interval",
				Usage: "how often should the janitor clean the cache",
				Value: config.Settings.JanitorInterval,
			},
			&cli.IntFlag{
				Name:  "attempts",
				Usage: "how many times to retry a request for a ts file",
				Value: config.Settings.Attempts,
			},
			&cli.DurationFlag{
				Name:  "clip-retention",
				Usage: "how long to keep ts files in cache",
				Value: config.Settings.ClipRetention,
			},
			&cli.DurationFlag{
				Name:  "playlist-retention",
				Usage: "how long to keep playlists in cache",
				Value: config.Settings.PlaylistRetention,
			},
			&cli.BoolFlag{
				Name:  "https",
				Usage: "use https instead of http",
				Value: false,
			},
			&cli.StringFlag{
				Name:  "host",
				Usage: "hostname to attach to proxy url",
				Value: config.Settings.Host,
			},
			&cli.StringFlag{
				Name:  "port",
				Usage: "port to attach to proxy url",
				Value: config.Settings.Port,
			},
			&cli.BoolFlag{
				Name:  "decrypt",
				Usage: "decrypt encrypted segments",
				Value: false,
			},
			&cli.StringFlag{
				Name:  "log-level",
				Usage: "log level",
				Value: config.Settings.LogLevel,
			},
			&cli.BoolFlag{
				Name:  "healthcheck",
				Usage: "run healthcheck and exit",
				Value: false,
			},
		},
		Name:  "hls-proxy",
		Usage: "start hls proxy server",
		Action: func(c *cli.Context) error {
			model.InitializeConfig(c)
			if c.Bool("healthcheck") {
				return runHealthcheck()
			}
			proxy.InitPrefetcher(&model.Configuration)
			fmt.Printf("%v", model.Configuration)
			host := c.String("host")
			portStr := c.String("port")
			portInt, err := strconv.Atoi(portStr)
			if err != nil {
				return fmt.Errorf("invalid port %q: %w", portStr, err)
			}
			launch_server(host, portInt, c.String("log-level"))
			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}

}

func launch_server(host string, port int, logLevel string) {
	godotenv.Load()
	// Echo instance
	e := echo.New()

	if logLevel == "DEBUG" {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}

	// Middleware
	e.Use(middleware.CORS())
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Routes
	e.GET("/health", handleHealth)
	e.GET("/:input", handle_request)

	// Start server

	e.Logger.Fatal(e.Start(fmt.Sprintf("%s:%d", host, port)))

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

func handle_request(c echo.Context) error {
	input, e := parsing.ParseInputUrl(c.Param("input"))
	if e != nil {
		return e
	}

	inputUrl, err := url.Parse(input.Url)

	if err != nil {
		return err
	}
	//TODO: Not all m3u8 files end with m3u8
	if strings.HasSuffix(inputUrl.Path, ".m3u8") {
		return proxy.ManifestProxy(c, input)
	} else {
		return proxy.TsProxy(c, input)
	}
}

// Handler
