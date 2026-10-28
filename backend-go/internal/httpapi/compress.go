package httpapi

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const compressThreshold = 1024

// compressedPublicReads maps the public reads whose bodies depend only on the
// stored snapshot and the query, never on credentials, to the content type
// they are compressed as. The revision stream /api/dashboard/events is not
// one of them: gzip would buffer it.
var compressedPublicReads = map[string]string{
	"/api/dashboard": "application/json", "/api/dashboard/revision": "application/json",
	"/api/health": "application/json", "/api/calendar": "application/json",
	"/api/posts": "application/json", "/api/videos": "application/json",
	"/api/calendar.ics": "text/calendar", "/api/feed.xml": "application/atom+xml",
}

var gzipWriters = sync.Pool{New: func() any {
	writer, _ := gzip.NewWriterLevel(nil, 5)
	return writer
}}

// Compress gzips only credential-independent public reads and build text.
// Admin and authentication responses, reflected errors, MCP and runtime media
// stay outside compression, which avoids secret-plus-attacker-input
// compression side channels.
func Compress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		response := &compressWriter{ResponseWriter: w, path: r.URL.Path}
		defer response.close()
		next.ServeHTTP(response, r)
	})
}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		coding, parameters, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(coding), "gzip") && strings.TrimSpace(coding) != "*" {
			continue
		}
		quality := strings.TrimSpace(parameters)
		if strings.HasPrefix(quality, "q=") {
			if value, err := strconv.ParseFloat(strings.TrimPrefix(quality, "q="), 64); err == nil && value == 0 {
				return false
			}
		}
		return true
	}
	return false
}

type compressWriter struct {
	http.ResponseWriter
	path        string
	status      int
	wroteHeader bool
	eligible    bool
	gzip        *gzip.Writer
}

func (c *compressWriter) WriteHeader(status int) {
	if c.wroteHeader {
		return
	}
	c.status = status
	c.eligible = c.shouldCompress(status)
	if c.eligible {
		// The length is only known once the first body chunk arrives.
		if length, err := strconv.Atoi(c.Header().Get("Content-Length")); err == nil {
			if length < compressThreshold {
				c.eligible = false
			} else {
				c.start()
			}
		}
	}
	if !c.eligible || c.gzip != nil {
		c.wroteHeader = true
		c.ResponseWriter.WriteHeader(status)
	}
}

func (c *compressWriter) shouldCompress(status int) bool {
	header := c.Header()
	if status != http.StatusOK || header.Get("Set-Cookie") != "" || header.Get("Content-Encoding") != "" {
		return false
	}
	contentType, _, _ := strings.Cut(header.Get("Content-Type"), ";")
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if want, ok := compressedPublicReads[c.path]; ok {
		return contentType == want
	}
	for _, prefix := range []string{"/api", "/media", "/mcp"} {
		if c.path == prefix || strings.HasPrefix(c.path, prefix+"/") {
			return false
		}
	}
	switch contentType {
	case "text/html", "text/css", "text/javascript", "application/javascript":
		return true
	}
	return false
}

func (c *compressWriter) start() {
	header := c.Header()
	header.Del("Content-Length")
	header.Del("Accept-Ranges")
	header.Set("Content-Encoding", "gzip")
	if !strings.Contains(header.Get("Vary"), "Accept-Encoding") {
		header.Add("Vary", "Accept-Encoding")
	}
	writer := gzipWriters.Get().(*gzip.Writer)
	writer.Reset(c.ResponseWriter)
	c.gzip = writer
}

func (c *compressWriter) Write(data []byte) (int, error) {
	if c.status == 0 {
		c.WriteHeader(http.StatusOK)
	}
	if c.eligible && c.gzip == nil {
		// No declared length: the handlers that reach here write the whole
		// body at once, so the first chunk decides.
		if len(data) < compressThreshold {
			c.eligible = false
		} else {
			c.start()
		}
		c.wroteHeader = true
		c.ResponseWriter.WriteHeader(c.status)
	}
	if c.gzip != nil {
		return c.gzip.Write(data)
	}
	return c.ResponseWriter.Write(data)
}

func (c *compressWriter) close() {
	if c.gzip != nil {
		_ = c.gzip.Close()
		c.gzip.Reset(nil)
		gzipWriters.Put(c.gzip)
		c.gzip = nil
		return
	}
	// A handler that set headers but wrote no body (HEAD, empty responses).
	if c.status != 0 && !c.wroteHeader {
		c.wroteHeader = true
		c.ResponseWriter.WriteHeader(c.status)
	}
}

func (c *compressWriter) Flush() {
	if c.gzip != nil {
		_ = c.gzip.Flush()
	}
	if flusher, ok := c.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (c *compressWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }
