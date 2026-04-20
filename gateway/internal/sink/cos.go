// Package sink - 对象存储 (COS / S3 兼容) 上传器.
//
// 用 aws-sdk-go-v2 + path style addressing,
// 兼容: AWS S3 / 腾讯云 COS / 阿里云 OSS / MinIO / Cloudflare R2 等.
//
// 上传流程:
//   1. zstd 压缩 (level 3)
//   2. PutObject 到 bucket/{prefix}/{key}.zst
//   3. ContentType: application/json, ContentEncoding: zstd
//
// 调用方典型用法 (capture pipeline):
//   key := makeCOSKey(rec, "request")  // "2026/04/20/09/01H..AB-request.json"
//   err := cosSink.Upload(ctx, key, rec.RawRequestBody, "application/json")
package sink

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/prismai/llm-logger/gateway/internal/config"
	"github.com/prismai/llm-logger/gateway/internal/pkg/compress"
)

// COSSink 实现 capture.COSSink.
type COSSink struct {
	cfg    *config.Config
	client *s3.Client
	bucket string
	prefix string
	logger *slog.Logger
}

// NewCOSSink 创建 sink, 验证 bucket 可访问 (skip 校验避免 minio 启动慢导致 fail-fast).
func NewCOSSink(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*COSSink, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.S3Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.S3AccessKey, cfg.S3SecretKey, "",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("加载 AWS 配置失败: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		}
		o.UsePathStyle = cfg.S3UsePathStyle
		// 默认 retry 已经够用
	})

	prefix := strings.Trim(cfg.S3KeyPrefix, "/")
	logger.Info("COS sink 初始化",
		"endpoint", cfg.S3Endpoint,
		"region", cfg.S3Region,
		"bucket", cfg.S3Bucket,
		"prefix", prefix,
		"path_style", cfg.S3UsePathStyle,
	)
	return &COSSink{
		cfg:    cfg,
		client: client,
		bucket: cfg.S3Bucket,
		prefix: prefix,
		logger: logger.With("component", "cos-sink"),
	}, nil
}

// Upload 压缩并上传到 bucket/{prefix}/{key}.zst.
//
// data 较大时会进入 zstd 压缩,在常见 LLM 上下文上压缩比 4-8 倍.
func (s *COSSink) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	compressed := compress.EncodeAll(data)

	fullKey := key
	if s.prefix != "" {
		fullKey = s.prefix + "/" + strings.TrimLeft(key, "/")
	}
	if !strings.HasSuffix(fullKey, ".zst") {
		fullKey += ".zst"
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	uploadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err := s.client.PutObject(uploadCtx, &s3.PutObjectInput{
		Bucket:          aws.String(s.bucket),
		Key:             aws.String(fullKey),
		Body:            bytes.NewReader(compressed),
		ContentType:     aws.String(contentType),
		ContentEncoding: aws.String("zstd"),
		Metadata: map[string]string{
			"original-size":   fmt.Sprintf("%d", len(data)),
			"compressed-size": fmt.Sprintf("%d", len(compressed)),
		},
	})
	if err != nil {
		return fmt.Errorf("PutObject %s 失败: %w", fullKey, err)
	}
	return nil
}
