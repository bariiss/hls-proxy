package http_retry

import (
	"crypto/tls"
	"net"
	"net/http"

	"github.com/bariiss/hls-proxy/config"
)

// DefaultHttpClient is the default http client used by the proxy
// Timeout values can be overridden via environment variables; see config package defaults
var DefaultHttpClient = http.Client{
	Timeout: config.Settings.HTTPClientTimeout,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		Dial: (&net.Dialer{
			Timeout: config.Settings.HTTPDialTimeout,
		}).Dial,
	},
}
