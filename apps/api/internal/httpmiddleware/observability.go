// Package httpmiddleware contains the shared HTTP observability boundary used
// by both the public and admin APIs.
package httpmiddleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

const (
	RequestIDHeader = "X-Request-ID"
	maxRequestIDLen = 128
	randomIDBytes   = 16
)

type requestIDContextKey struct{}

// RequestIDFromContext returns the validated or generated request ID attached
// to a request by Observe.
func RequestIDFromContext(r *http.Request) string {
	requestID, _ := r.Context().Value(requestIDContextKey{}).(string)
	return requestID
}

// Observe applies middleware in the deliberate order request ID -> completion
// logging -> panic recovery -> application handler. This ensures recovered
// panics are recorded as one completed 500 request with the same request ID.
func Observe(next http.Handler) http.Handler {
	return withRequestID(withCompletionLog(withRecovery(next)))
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := acceptedRequestID(r.Header.Values(RequestIDHeader))
		if requestID == "" {
			requestID = newRequestID()
		}

		w.Header().Set(RequestIDHeader, requestID)
		ctx := contextWithRequestID(r, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func contextWithRequestID(r *http.Request, requestID string) context.Context {
	return context.WithValue(r.Context(), requestIDContextKey{}, requestID)
}

func acceptedRequestID(values []string) string {
	if len(values) != 1 || !validRequestID(values[0]) {
		return ""
	}
	return values[0]
}

func validRequestID(value string) bool {
	if len(value) < 1 || len(value) > maxRequestIDLen {
		return false
	}
	for index := range value {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func newRequestID() string {
	random := make([]byte, randomIDBytes)
	if _, err := rand.Read(random); err != nil {
		// Failure of the operating system entropy source is exceptional. Retain a
		// bounded, safe correlation value and log the fault without exposing it to
		// downstream request data.
		slog.Error("request ID generation failed")
		return "request-id-unavailable"
	}
	return hex.EncodeToString(random)
}

type responseMetrics struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (metrics *responseMetrics) WriteHeader(status int) {
	if metrics.wroteHeader {
		return
	}
	metrics.wroteHeader = true
	metrics.status = status
	metrics.ResponseWriter.WriteHeader(status)
}

func (metrics *responseMetrics) Write(body []byte) (int, error) {
	if !metrics.wroteHeader {
		metrics.WriteHeader(http.StatusOK)
	}
	written, err := metrics.ResponseWriter.Write(body)
	metrics.bytes += int64(written)
	return written, err
}

// Unwrap allows http.ResponseController to preserve optional capabilities such
// as flushing and connection hijacking without falsely advertising interfaces
// unsupported by the underlying writer.
func (metrics *responseMetrics) Unwrap() http.ResponseWriter {
	return metrics.ResponseWriter
}

func withCompletionLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		metrics := &responseMetrics{ResponseWriter: w, status: http.StatusOK}

		defer func() {
			slog.InfoContext(r.Context(), "http request completed",
				"request_id", RequestIDFromContext(r),
				"method", r.Method,
				"path", safePath(r),
				"status", metrics.status,
				"duration", time.Since(started),
				"response_bytes", metrics.bytes,
			)
		}()

		next.ServeHTTP(metrics, r)
	})
}

func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.ErrorContext(r.Context(), "http request panic recovered",
					"request_id", RequestIDFromContext(r),
					"method", r.Method,
					"path", safePath(r),
					"panic_type", panicType(recovered),
					"stack", string(debug.Stack()),
				)
				writePanicResponse(w)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func safePath(r *http.Request) string {
	path := r.URL.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

func panicType(recovered any) string {
	return fmt.Sprintf("%T", recovered)
}

func writePanicResponse(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    "panic",
			"message": "internal error",
		},
	})
}
