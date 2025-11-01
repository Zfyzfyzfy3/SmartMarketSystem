package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

type ctxKey string

const requestIDKey ctxKey = "gateway-request-id"

// RequestID ensures every request carries an X-Request-ID header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if reqID == "" {
			reqID = generateID()
			r.Header.Set("X-Request-ID", reqID)
		}

		ctx := context.WithValue(r.Context(), requestIDKey, reqID)
		w.Header().Set("X-Request-ID", reqID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID extracts the request id from context.
func GetRequestID(ctx context.Context) string {
	val, _ := ctx.Value(requestIDKey).(string)
	return val
}

func generateID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// Fallback to pseudo-random if crypto fails.
		for i := range buf {
			buf[i] = byte(i * 31)
		}
	}
	return hex.EncodeToString(buf)
}
