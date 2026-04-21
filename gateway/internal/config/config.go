// Package config 加载和校验 Gateway 配置.
//
// 配置优先级: 环境变量 (前缀 LLMLOG_) > 默认值
// 不支持配置文件,所有配置走环境变量,与云原生 12-Factor 一致.
package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config 是 Gateway 全部配置.
type Config struct {
	// ============ HTTP server ============
	ListenAddr      string        `envconfig:"LISTEN_ADDR" default:"0.0.0.0:8080"`
	MetricsAddr     string        `envconfig:"METRICS_ADDR" default:"0.0.0.0:9090"`
	PProfEnabled    bool          `envconfig:"PPROF_ENABLED" default:"false"`
	ShutdownTimeout time.Duration `envconfig:"SHUTDOWN_TIMEOUT" default:"30s"`

	// ============ 上游 newapi ============
	UpstreamURL     string        `envconfig:"UPSTREAM_URL" required:"true"`
	UpstreamTimeout time.Duration `envconfig:"UPSTREAM_TIMEOUT" default:"600s"`

	// ============ 请求/响应限制 ============
	MaxBodyBytes        int64 `envconfig:"MAX_BODY_BYTES" default:"104857600"`         // 100 MB
	COSOffloadThreshold int   `envconfig:"COS_OFFLOAD_THRESHOLD" default:"32768"`      // 32 KB
	PreviewBytes        int   `envconfig:"PREVIEW_BYTES" default:"2048"`               // 2 KB
	StreamChunksKeep    bool  `envconfig:"STREAM_CHUNKS_KEEP" default:"false"`         // debug 用

	// ============ 采集管道 ============
	CaptureBuffer    int           `envconfig:"CAPTURE_BUFFER" default:"10000"`
	CaptureWorkers   int           `envconfig:"CAPTURE_WORKERS" default:"4"`
	CHBatchSize      int           `envconfig:"CH_BATCH_SIZE" default:"200"`
	CHBatchInterval  time.Duration `envconfig:"CH_BATCH_INTERVAL" default:"500ms"`
	COSUploadWorkers int           `envconfig:"COS_UPLOAD_WORKERS" default:"8"`

	// ============ 磁盘兜底队列 (CH 连续失败时启用, 保证零数据丢失) ============
	// DiskSpoolEnabled=true 时, CH 批量写入彻底失败的 batch 会落到本地 NDJSON 文件,
	// 后台 replayer 定期扫描 spool 目录, 尝试重新 INSERT. 成功后删除文件.
	DiskSpoolEnabled  bool          `envconfig:"DISK_SPOOL_ENABLED" default:"true"`
	DiskSpoolDir      string        `envconfig:"DISK_SPOOL_DIR" default:"/var/lib/llmlog/spool"`
	DiskSpoolMaxBytes int64         `envconfig:"DISK_SPOOL_MAX_BYTES" default:"10737418240"` // 10 GB
	DiskSpoolInterval time.Duration `envconfig:"DISK_SPOOL_INTERVAL" default:"30s"`          // 重放扫描周期

	// ============ ClickHouse ============
	CHHost     string `envconfig:"CH_HOST" default:"localhost"`
	CHPort     int    `envconfig:"CH_PORT" default:"8123"`
	CHDatabase string `envconfig:"CH_DATABASE" default:"llm_logger"`
	CHUser     string `envconfig:"CH_USER" default:"default"`
	CHPassword string `envconfig:"CH_PASSWORD" default:""`
	CHTable    string `envconfig:"CH_TABLE" default:"llm_call_logs"`

	// ============ 对象存储 (S3 兼容) ============
	S3Endpoint     string `envconfig:"S3_ENDPOINT" required:"true"`
	S3Region       string `envconfig:"S3_REGION" default:"us-east-1"`
	S3Bucket       string `envconfig:"S3_BUCKET" required:"true"`
	S3AccessKey    string `envconfig:"S3_ACCESS_KEY" required:"true"`
	S3SecretKey    string `envconfig:"S3_SECRET_KEY" required:"true"`
	S3UsePathStyle bool   `envconfig:"S3_USE_PATH_STYLE" default:"true"`
	S3KeyPrefix    string `envconfig:"S3_KEY_PREFIX" default:"llm-logs"`

	// ============ 日志 ============
	LogLevel  string `envconfig:"LOG_LEVEL" default:"info"`
	LogFormat string `envconfig:"LOG_FORMAT" default:"json"` // json|text
}

// Load 从环境变量加载配置.
func Load() (*Config, error) {
	var c Config
	if err := envconfig.Process("LLMLOG", &c); err != nil {
		return nil, fmt.Errorf("加载配置失败: %w", err)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) validate() error {
	if _, err := url.Parse(c.UpstreamURL); err != nil {
		return fmt.Errorf("UPSTREAM_URL 无效: %w", err)
	}
	c.UpstreamURL = strings.TrimRight(c.UpstreamURL, "/")

	if c.MaxBodyBytes <= 0 {
		return fmt.Errorf("MAX_BODY_BYTES 必须 > 0")
	}
	if c.COSOffloadThreshold < 0 {
		return fmt.Errorf("COS_OFFLOAD_THRESHOLD 必须 >= 0")
	}
	if c.CaptureBuffer < 100 {
		return fmt.Errorf("CAPTURE_BUFFER 太小 (建议 >= 1000)")
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "warning", "error":
	default:
		return fmt.Errorf("LOG_LEVEL 无效: %s (debug|info|warn|error)", c.LogLevel)
	}
	switch strings.ToLower(c.LogFormat) {
	case "json", "text":
	default:
		return fmt.Errorf("LOG_FORMAT 无效: %s (json|text)", c.LogFormat)
	}
	return nil
}

// CHHTTPEndpoint 返回 ClickHouse HTTP 端点 URL (用于 INSERT).
func (c *Config) CHHTTPEndpoint() string {
	return fmt.Sprintf("http://%s:%d/", c.CHHost, c.CHPort)
}
