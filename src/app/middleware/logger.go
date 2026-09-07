package middleware

import (
	"log"
	"net/http"
	"time"
)

// RequestLogger logs one line per request: method, path, status, duration.
//
// It exists instead of chi's Logger for one reason — chi logs r.RequestURI,
// which includes the query string. Generated list endpoints take
// ?filter=<column>:<value>, so a search for a customer by email would write
// that email into /logs/app.log, a world-readable file on a host bind mount.
// The path alone answers every question an access log is actually asked.
//
// Nothing here records the client address either. Correlating a request to a
// caller is the rate limiter's job, and it keys on a hash.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

// statusRecorder remembers the status code so the log line can report it.
// WriteHeader may never be called — a handler that only calls Write implies
// 200, which is the zero value this is initialized to.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}
