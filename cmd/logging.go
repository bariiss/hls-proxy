package cmd

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
)

type countingResponseWriter struct {
	http.ResponseWriter
	bytes  int64
	status int
}

func (w *countingResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *countingResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *countingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *countingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, errors.New("http hijacking not supported")
}

func (w *countingResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (w *countingResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err := readerFrom.ReadFrom(r)
		w.bytes += n
		return n, err
	}
	n, err := io.Copy(w.ResponseWriter, r)
	w.bytes += n
	return n, err
}

type countingReadCloser struct {
	io.ReadCloser
	bytes int64
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.bytes += int64(n)
	return n, err
}

func jsonLoggerMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			res := c.Response()

			start := time.Now()

			writer := &countingResponseWriter{
				ResponseWriter: res.Writer,
				status:         res.Status,
			}
			res.Writer = writer

			var bodyCounter *countingReadCloser
			if req.Body != nil && req.Body != http.NoBody {
				bodyCounter = &countingReadCloser{ReadCloser: req.Body}
				req.Body = bodyCounter
				defer func() { req.Body = bodyCounter.ReadCloser }()
			}

			err := next(c)
			if err != nil {
				c.Error(err)
			}

			latency := time.Since(start)

			status := res.Status
			if status == 0 {
				status = http.StatusOK
			}

			bytesOut := writer.bytes

			var bytesIn int64
			if req.ContentLength > 0 {
				bytesIn = req.ContentLength
			}
			if bodyCounter != nil && bodyCounter.bytes > 0 {
				bytesIn = bodyCounter.bytes
			}

			// upstream bytes (bytes downloaded from origin by the proxy)
			var bytesUpstream int64
			if v := c.Get("bytes_upstream"); v != nil {
				switch t := v.(type) {
				case int:
					bytesUpstream = int64(t)
				case int64:
					bytesUpstream = t
				case uint64:
					bytesUpstream = int64(t)
				case float64:
					bytesUpstream = int64(t)
				}
			}

			var errText string
			if err != nil {
				errText = err.Error()
			}

			log.WithFields(log.Fields{
				"remote_ip":     c.RealIP(),
				"host":          req.Host,
				"method":        req.Method,
				"uri":           req.RequestURI,
				"status":        status,
				"latency_ns":    latency.Nanoseconds(),
				"latency_human": latency.String(),
				"bytes_in":      bytesIn,
				"bytes_up":      bytesUpstream,
				"bytes_out":     bytesOut,
				"user_agent":    req.UserAgent(),
				"error":         errText,
			}).Info("request completed")

			return err
		}
	}
}
