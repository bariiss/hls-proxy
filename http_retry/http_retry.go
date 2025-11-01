package http_retry

import (
	"errors"
	"io"
	"net/http"

	"github.com/avast/retry-go"
	"github.com/bariiss/hls-proxy/config"
	log "github.com/sirupsen/logrus"
)

/*
 * The two functions in here differ in that one returns the response body as a byte array,
 * and the other returns the response object that has to be handeled by the caller.
 */

func ExecuteRetryableRequest(request *http.Request, attempts int) (*http.Response, error) {
	request.Close = true
	var resp *http.Response
	err := retry.Do(
		func() error {
			var err error
			resp, err = DefaultHttpClient.Do(request)
			if err != nil {
				return err
			}

			if valid := statusOK(resp.StatusCode); !valid {
				log.WithField("status", resp.StatusCode).Warn("non 2xx status code")
				return errors.New("non 2xx status code")
			}

			return nil
		},
		retry.Attempts(uint(attempts)),
		retry.Delay(config.Settings.RetryRequestDelay),
		retry.OnRetry(func(n uint, err error) {
			log.Error("Retrying request after error:", err, n)
		}),
	)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func ExecuteRetryClipRequest(request *http.Request, attempts int) ([]byte, error) {
	request.Close = true
	var responseBytes []byte
	err := retry.Do(
		func() error {
			resp, err := DefaultHttpClient.Do(request)
			if err != nil {
				return err
			}

			if !statusOK(resp.StatusCode) {
				return errors.New("non 2xx status code")
			}

			bytes, err := readResponse(resp)
			if err != nil {
				return err
			}

			responseBytes = bytes
			return nil
		},
		retry.Attempts(uint(attempts)),
		retry.Delay(config.Settings.RetryClipDelay),
		retry.OnRetry(func(n uint, err error) {
			log.Error("Retrying request after error:", err, n)
		}),
	)
	if err != nil {
		return nil, err
	}

	return responseBytes, nil
}

func statusOK(status int) bool {
	return status >= 200 && status < 300
}

func readResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
