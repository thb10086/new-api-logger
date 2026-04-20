// Command gateway 是 LLM Logger 的反向代理 + 上下文采集服务.
//
// 启动顺序:
//  1. 加载配置 (env)
//  2. 初始化 Sink (ClickHouse + COS)
//  3. 初始化 Capture Pipeline
//  4. 启动 HTTP server (代理 + 路由)
//  5. 启动 Metrics server (Prometheus)
//  6. 等待信号优雅关闭
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prismai/llm-logger/gateway/internal/capture"
	"github.com/prismai/llm-logger/gateway/internal/config"
	"github.com/prismai/llm-logger/gateway/internal/server"
	"github.com/prismai/llm-logger/gateway/internal/sink"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// 配置阶段失败直接 stderr + exit, 不依赖 logger
		println("配置加载失败:", err.Error())
		os.Exit(2)
	}

	logger := setupLogger(cfg)
	slog.SetDefault(logger)

	logger.Info("LLM Logger Gateway 启动中",
		"listen", cfg.ListenAddr,
		"upstream", cfg.UpstreamURL,
		"clickhouse", cfg.CHHTTPEndpoint(),
		"s3_endpoint", cfg.S3Endpoint,
		"s3_bucket", cfg.S3Bucket,
	)

	rootCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// ============ Sinks ============
	chSink := sink.NewClickHouseSink(cfg, logger)
	cosSink, err := sink.NewCOSSink(rootCtx, cfg, logger)
	if err != nil {
		logger.Error("COS sink 初始化失败", "err", err)
		os.Exit(3)
	}

	// ============ Capture Pipeline ============
	pipeline := capture.NewPipeline(cfg, chSink, cosSink, logger)
	pipeline.Start(rootCtx)

	// ============ HTTP Server ============
	srv := server.New(cfg, pipeline, logger)
	httpDone := make(chan error, 1)
	go func() {
		if err := srv.Run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpDone <- err
			return
		}
		httpDone <- nil
	}()

	// ============ Metrics Server ============
	metricsDone := make(chan error, 1)
	go func() {
		if err := server.RunMetrics(cfg, logger); err != nil && !errors.Is(err, http.ErrServerClosed) {
			metricsDone <- err
			return
		}
		metricsDone <- nil
	}()

	logger.Info("Gateway 启动完成", "pid", os.Getpid())

	// ============ 等待退出信号 ============
	select {
	case <-rootCtx.Done():
		logger.Info("收到退出信号,开始优雅关闭")
	case err := <-httpDone:
		logger.Error("HTTP server 退出", "err", err)
	case err := <-metricsDone:
		logger.Error("Metrics server 退出", "err", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server 关闭异常", "err", err)
	}

	if err := pipeline.Shutdown(shutdownCtx); err != nil {
		logger.Error("Capture pipeline 关闭异常", "err", err)
	}

	logger.Info("Gateway 已退出")
}

// setupLogger 根据配置创建 slog logger.
func setupLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: false,
	}

	var handler slog.Handler
	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler).With(
		"service", "llm-logger-gateway",
		"ts", time.Now().Format(time.RFC3339),
	)
}
