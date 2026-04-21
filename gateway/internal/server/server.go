// Package server - Gateway 主 HTTP server.
//
// 路由:
//   POST /v1/chat/completions                              -> OpenAI Chat
//   POST /v1/responses                                     -> OpenAI Responses
//   POST /v1/messages                                      -> Anthropic Messages
//   POST /v1beta/models/{model_action}                     -> Gemini generateContent / streamGenerateContent
//
// 其他路径 (如 /v1/embeddings / /v1/audio/*) 不在监听范围,
// 默认透明转发但不采集 (passthrough only).
package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/thb10086/new-api-logger/gateway/internal/capture"
	"github.com/thb10086/new-api-logger/gateway/internal/config"
	"github.com/thb10086/new-api-logger/gateway/internal/handlers"
	"github.com/thb10086/new-api-logger/gateway/internal/protocol"
	"github.com/thb10086/new-api-logger/gateway/internal/proxy"
)

// Server 是 Gateway HTTP 服务实例.
type Server struct {
	cfg     *config.Config
	srv     *http.Server
	logger  *slog.Logger
	pipeline *capture.Pipeline
	forwarder *proxy.Forwarder
}

// New 创建 Server.
func New(cfg *config.Config, pipeline *capture.Pipeline, logger *slog.Logger) *Server {
	fw, err := proxy.NewForwarder(cfg, logger)
	if err != nil {
		logger.Error("Forwarder 初始化失败", "err", err)
		panic(err)
	}

	mux := http.NewServeMux()

	// ============ 4 个监听 LLM 协议路由 ============
	hOpenAIChat := handlers.New(cfg, fw, pipeline, protocol.OpenAIChat(), logger)
	mux.Handle("POST /v1/chat/completions", hOpenAIChat)

	hOpenAIResponses := handlers.New(cfg, fw, pipeline, protocol.OpenAIResponses(), logger)
	mux.Handle("POST /v1/responses", hOpenAIResponses)

	hAnthropic := handlers.New(cfg, fw, pipeline, protocol.Anthropic(), logger)
	mux.Handle("POST /v1/messages", hAnthropic)

	hGemini := handlers.New(cfg, fw, pipeline, protocol.Gemini(), logger).WithModelFromPath("model_action")
	mux.Handle("POST /v1beta/models/{model_action}", hGemini)
	// 兼容 alt=sse 等查询参数, path 段保持不变
	mux.Handle("POST /v1beta/openai/chat/completions", hOpenAIChat) // Gemini 也提供 OpenAI 兼容端点

	// ============ 其他路径: 透明转发, 不采集 ============
	mux.HandleFunc("/", makePassthroughHandler(cfg, fw, logger))

	// ============ Liveness ============
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		stats := pipeline.Stats()
		if stats.QueueLength*100 > stats.QueueCap*90 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("queue near full"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	// ============ 包装中间件 (logging + recover) ============
	handler := withRecover(withAccessLog(mux, logger), logger)

	return &Server{
		cfg:       cfg,
		logger:    logger.With("component", "server"),
		pipeline:  pipeline,
		forwarder: fw,
		srv: &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           handler,
			ReadHeaderTimeout: 30 * time.Second,
			// SSE 长连接, 不设 WriteTimeout
		},
	}
}

// Run 阻塞运行 HTTP server.
func (s *Server) Run() error {
	s.logger.Info("HTTP server 监听", "addr", s.cfg.ListenAddr)
	return s.srv.ListenAndServe()
}

// Shutdown 优雅关闭.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// makePassthroughHandler 创建一个仅转发不采集的 handler, 用于非 LLM 路径.
//
// 这样客户端可以把 newapi 全部路径都指向这个 gateway,
// embeddings/image/audio 等会原样转发, 只有 4 个 LLM 路径会被采集.
func makePassthroughHandler(cfg *config.Config, fw *proxy.Forwarder, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 排除一些不需要透传的路径 (如 /, /favicon.ico)
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"llm-logger-gateway","status":"ok"}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/healthz") || strings.HasPrefix(r.URL.Path, "/readyz") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = fw.Forward(w, r)
	}
}
