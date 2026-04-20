// Package server - HTTP 中间件.
package server

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// statusRecorder 包装 ResponseWriter 以捕获实际响应状态码.
type statusRecorder struct {
	http.ResponseWriter
	status     int
	wroteHead  bool
	bytesWrote int64
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHead {
		r.status = code
		r.wroteHead = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHead {
		r.status = http.StatusOK
		r.wroteHead = true
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytesWrote += int64(n)
	return n, err
}

// 实现 http.Flusher (代理 SSE 必需)
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// withAccessLog 记录每次请求的方法/路径/状态码/耗时.
//
// /healthz / /readyz / /metrics 等路径不打印 (避免噪音).
func withAccessLog(next http.Handler, logger *slog.Logger) http.Handler {
	skip := map[string]bool{
		"/healthz": true,
		"/readyz":  true,
		"/metrics": true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if skip[r.URL.Path] {
			return
		}
		logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytesWrote,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

// withRecover 捕获 panic, 避免单个请求 panic 导致整个进程退出.
func withRecover(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("handler panic",
					"err", rec,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				// 尽量返回 500
				if w.Header().Get("Content-Type") == "" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":{"message":"internal server error","type":"panic"}}`))
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}
