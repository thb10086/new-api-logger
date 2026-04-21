// Package server - Prometheus metrics + pprof debug 端点.
package server

import (
	"context"
	"log/slog"
	"net/http"
	_ "net/http/pprof" // 注册 pprof handlers 到 DefaultServeMux

	"github.com/thb10086/new-api-logger/gateway/internal/config"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// RunMetrics 启动 metrics + pprof server (独立端口, 不暴露给客户端).
//
// 阻塞调用,直到 server 退出.
func RunMetrics(cfg *config.Config, logger *slog.Logger) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// pprof: 仅在显式开启时挂载到自己的 mux
	if cfg.PProfEnabled {
		// pprof 自动注册到 http.DefaultServeMux,直接 Handle 即可
		mux.Handle("/debug/", http.DefaultServeMux)
	}

	srv := &http.Server{
		Addr:    cfg.MetricsAddr,
		Handler: mux,
	}

	logger.Info("Metrics server 启动", "addr", cfg.MetricsAddr, "pprof", cfg.PProfEnabled)
	return srv.ListenAndServe()
}

// 占位: 未来可在此处定义自定义 collector 与采集 pipeline 关联.
//
// 当前 pipeline.Stats() 已通过 promhttp 默认 Go runtime metrics 暴露;
// 如需业务指标 (request_count_per_provider 等), 在 sink/pipeline 中加 Counter/Gauge 并自动注册即可.
var _ = context.Canceled // 防 import 报警占位
