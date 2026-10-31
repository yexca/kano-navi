package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RequestLog writes one bounded JSON record per request. Unrecognized paths and
// methods use fixed labels so attacker-controlled values never reach stdout.
func RequestLog(next http.Handler, output io.Writer) http.Handler {
	var mutex sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		measured := &responseMetrics{ResponseWriter: w}
		next.ServeHTTP(measured, r)
		route := "unmatched"
		switch r.URL.Path {
		case "/api/health", "/api/visitor-region", "/api/dashboard/revision", "/api/dashboard/events", "/api/dashboard", "/api/calendar", "/api/posts", "/api/videos", "/api/websub/youtube":
			route = r.URL.Path
		default:
			switch {
			case strings.HasPrefix(r.URL.Path, "/media/lineblog/"):
				route = "/media/lineblog/:name"
			case strings.HasPrefix(r.URL.Path, "/media/profile/"):
				route = "/media/profile/:slot"
			case strings.HasPrefix(r.URL.Path, "/media/"):
				route = "/media/:id"
			}
		}
		method := r.Method
		switch method {
		case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		default:
			method = "OTHER"
		}
		if measured.status == 0 {
			measured.status = http.StatusOK
		}
		mutex.Lock()
		defer mutex.Unlock()
		_ = json.NewEncoder(output).Encode(struct {
			Route         string `json:"route"`
			Method        string `json:"method"`
			Status        int    `json:"status"`
			DurationMS    int64  `json:"duration_ms"`
			ResponseBytes int64  `json:"response_bytes"`
		}{route, method, measured.status, time.Since(start).Milliseconds(), measured.bytes})
	})
}

type responseMetrics struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseMetrics) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseMetrics) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += int64(n)
	return n, err
}

// Flush passes a streaming handler's flush to the wrapped writer.
func (w *responseMetrics) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseMetrics) Unwrap() http.ResponseWriter { return w.ResponseWriter }
