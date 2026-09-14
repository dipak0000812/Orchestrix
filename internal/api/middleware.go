package api

import "net/http"

// MaxBodySizeMiddleware wraps every request body in http.MaxBytesReader,
// so a caller can't force the server to buffer an unbounded amount of
// data by sending an oversized request body. Applied globally (rather
// than only inside CreateJob, the one handler that currently reads a
// body) so any future handler that reads a request body is covered
// automatically, without needing to remember to add this per-handler.
//
// When the limit is exceeded, the eventual json.Decode call in the
// handler fails with an opaque "http: request body too large" error,
// which handlers already turn into a generic 400 response rather than
// echoing the raw error back to the caller.
func MaxBodySizeMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
