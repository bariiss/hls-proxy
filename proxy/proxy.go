package proxy

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bariiss/hls-proxy/config"
	"github.com/bariiss/hls-proxy/encryption"
	"github.com/bariiss/hls-proxy/hls"
	"github.com/bariiss/hls-proxy/http_retry"
	"github.com/bariiss/hls-proxy/model"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
)

var preFetcher *hls.Prefetcher

// simpleCounterWriter counts bytes written to it (used with io.TeeReader)
type simpleCounterWriter struct{ n int64 }

func (w *simpleCounterWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

func InitPrefetcher(c *model.Config) {
	preFetcher = hls.NewPrefetcherWithJanitor(c.SegmentCount, c.JanitorInterval, c.PlaylistRetention, c.ClipRetention)
	if err := hls.ConfigureSegmentStore(c.SegmentStore, c.SegmentStorageDir); err != nil {
		log.Errorf("segment persistence disabled: %v", err)
	} else if c.SegmentStore {
		log.Infof("Persisting segments to %s", c.SegmentStorageDir)
	}
	hls.ConfigureSegmentCache(c.SegmentCache, c.SegmentCount)
	if c.SegmentCache {
		log.Infof("In-memory segment cache enabled with limit %d", c.SegmentCount)
	}
	if c.SegmentBackgroundFetch {
		log.Info("Background segment fetch enabled; manifests will trigger proactive downloads")
	}
	if c.SegmentIdleEnabled && c.SegmentIdleTimeout > 0 {
		hls.StartManifestInactivityJanitor(preFetcher, c.SegmentIdleTimeout)
	} else {
		log.Debug("Manifest inactivity janitor disabled")
	}
}

func ManifestProxy(c echo.Context, input *model.Input) error {
	req, err := http.NewRequest("GET", input.Url, nil)
	if err != nil {
		return err
	}
	addBaseHeaders(req, input)

	resp, err := http_retry.ExecuteRetryableRequest(req, 3)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL

	start := time.Now()

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// record upstream manifest size for logging
	c.Set("bytes_upstream", int64(len(bytes)))

	res, err := hls.ModifyM3u8(string(bytes), finalURL, preFetcher, input, c.Request().Host)
	if err != nil {
		return err
	}

	elapsed := time.Since(start)
	log.Debug("Modifying manifest took ", elapsed)
	c.Response().Status = http.StatusOK
	c.Response().Writer.Write([]byte(res))
	return nil
}

func TsProxy(c echo.Context, input *model.Input) error {
	//parse incomming base64 query string and decde it into model struct

	pId := c.QueryParam("pId")
	manifestID := pId
	if manifestID == "" {
		manifestID = input.Encoded
	}
	if manifestID == "" {
		manifestID = input.Url
	}
	hls.TouchManifest(manifestID)
	hls.RecordSegmentRequest(manifestID)

	//check if we have the ts file in cache

	decryptionKey := c.QueryParam("key")
	initialVector := c.QueryParam("iv")

	req, err := http.NewRequest("GET", input.Url, nil)

	addBaseHeaders(req, input)

	if err != nil {
		return err
	}

	//copy over range header if applicable
	if c.Request().Header.Get("Range") != "" {
		req.Header.Add("Range", c.Request().Header.Get("Range"))
	}

	rangeHeader := c.Request().Header.Get("Range")

	var (
		rawData []byte
		found   bool
	)

	if pId != "" && model.Configuration.Prefetch {
		start := time.Now()
		rawData, found = preFetcher.GetFetchedClip(pId, input.Url)
		log.Debug("Fetching clip from cache took ", time.Since(start))
	}

	if !found && model.Configuration.SegmentCache && rangeHeader == "" {
		rawData, found = hls.LoadSegmentCache(manifestID, input.Url)
	}

	if !found && model.Configuration.SegmentStore && rangeHeader == "" {
		var loadErr error
		rawData, found, loadErr = hls.LoadSegment(manifestID, input.Url)
		if loadErr != nil {
			log.Error("Error loading segment from store: ", loadErr)
		}
	}

	if found {
		if decryptionKey != "" {
			decrypted, err := encryption.DecryptSegment(rawData, decryptionKey, initialVector)
			if err != nil {
				log.Error("Error decrypting stored segment ", err)
				return err
			}
			setContentTypeHeader(c, input.Url, "")
			c.Response().Writer.Write(decrypted)
			return nil
		}
		setContentTypeHeader(c, input.Url, "")
		c.Response().Writer.Write(rawData)
		return nil
	}

	log.Debug("Fetching clip from origin")

	//send request to original host
	resp, err := http_retry.ExecuteRetryableRequest(req, model.Configuration.Attempts)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Range") != "" {
		c.Response().Writer.Header().Set("Content-Range", resp.Header.Get("Content-Range"))
	}

	setContentTypeHeader(c, input.Url, resp.Header.Get("Content-Type"))

	readBody := decryptionKey != "" || (model.Configuration.SegmentCache && rangeHeader == "") || model.Configuration.SegmentStore
	if readBody {
		rawData, err = io.ReadAll(resp.Body)
		if err != nil {
			log.Error("Error reading segment ", err)
			return err
		}

		// record upstream segment size for logging
		c.Set("bytes_upstream", int64(len(rawData)))
		if model.Configuration.SegmentStore && rangeHeader == "" {
			if err := hls.SaveSegment(manifestID, input.Url, rawData); err != nil {
				log.Warn("Failed to persist segment from origin: ", err)
			}
		}
		if model.Configuration.SegmentCache && rangeHeader == "" {
			hls.SaveSegmentCache(manifestID, input.Url, rawData)
		}
		if decryptionKey != "" {
			rawData, err = encryption.DecryptSegment(rawData, decryptionKey, initialVector)
			if err != nil {
				log.Error("Error decrypting segment ", err)
				return err
			}
		}
		c.Response().Writer.Write(rawData)
		return nil
	}

	// When not reading the body into memory, stream and count upstream bytes
	cw := &simpleCounterWriter{}
	tee := io.TeeReader(resp.Body, cw)
	// copy to response writer so our countingResponseWriter captures bytes_out
	if _, err := io.Copy(c.Response().Writer, tee); err != nil {
		resp.Body.Close()
		return err
	}
	resp.Body.Close()
	c.Set("bytes_upstream", cw.n)
	return nil
}

func setContentTypeHeader(c echo.Context, name string, override string) string {
	contentType := override
	if contentType == "" {
		contentType = detectContentType(name)
	}
	if contentType != "" {
		c.Response().Writer.Header().Set("Content-Type", contentType)
	}
	return contentType
}

func addBaseHeaders(req *http.Request, input *model.Input) {
	//add headers if applicable
	if input.Referer != "" {
		req.Header.Add("Referer", input.Referer)
	}
	if input.Origin != "" {
		req.Header.Add("Origin", input.Origin)
	}
	req.Header.Add("User-Agent", config.Settings.UserAgent)
}

func detectContentType(name string) string {
	lname := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lname, ".aac"):
		return "audio/aac"
	case strings.HasSuffix(lname, ".m4a"):
		return "audio/mp4"
	case strings.HasSuffix(lname, ".m4s"), strings.HasSuffix(lname, ".mp4"), strings.HasSuffix(lname, ".m4v"):
		return "video/mp4"
	case strings.HasSuffix(lname, ".mp3"):
		return "audio/mpeg"
	case strings.HasSuffix(lname, ".m3u8"), strings.HasSuffix(lname, ".m3u"):
		return "application/vnd.apple.mpegurl"
	default:
		return "video/mp2t"
	}
}
