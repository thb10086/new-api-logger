// Package capture - 异步采集管道.
//
// 流程:
//   1. handler 完成请求转发后调用 Submit(record)
//   2. record 进入 buffered channel
//   3. workers 并发消费, 决定是否将 body offload 到 COS
//   4. workers 把记录推到 ClickHouseSink (内部再批量 flush)
//
// 设计原则:
//   - 主链路不阻塞: 采集队列满则丢弃 (有指标可观测)
//   - workers 并发处理 COS 上传 + CH 写入
//   - 优雅关闭: drain 队列后 flush sink
package capture

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/thb10086/new-api-logger/gateway/internal/config"
)

// COSSink 抽象大 body 上传接口 (避免 import 循环).
type COSSink interface {
	Upload(ctx context.Context, key string, data []byte, contentType string) error
}

// CHSink 抽象 ClickHouse 批量写入接口.
type CHSink interface {
	Submit(rec *Record)
	Flush(ctx context.Context) error
}

// Pipeline 异步采集管道.
type Pipeline struct {
	cfg     *config.Config
	chSink  CHSink
	cosSink COSSink
	logger  *slog.Logger

	queue chan *Record
	wg    sync.WaitGroup

	// 指标
	submitted atomic.Uint64
	processed atomic.Uint64
	dropped   atomic.Uint64
	cosUpload atomic.Uint64
	cosFailed atomic.Uint64
}

// NewPipeline 创建管道.
func NewPipeline(cfg *config.Config, chSink CHSink, cosSink COSSink, logger *slog.Logger) *Pipeline {
	return &Pipeline{
		cfg:     cfg,
		chSink:  chSink,
		cosSink: cosSink,
		logger:  logger.With("component", "capture-pipeline"),
		queue:   make(chan *Record, cfg.CaptureBuffer),
	}
}

// Start 启动 worker.
func (p *Pipeline) Start(ctx context.Context) {
	for i := 0; i < p.cfg.CaptureWorkers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
	p.logger.Info("采集 pipeline 启动",
		"workers", p.cfg.CaptureWorkers,
		"buffer", p.cfg.CaptureBuffer,
		"cos_offload_threshold", p.cfg.COSOffloadThreshold,
	)
}

// Submit 提交一条采集记录. 非阻塞.
//
// 队列满时丢弃并计数, 不返回错误 (主链路不应感知).
func (p *Pipeline) Submit(rec *Record) {
	p.submitted.Add(1)
	select {
	case p.queue <- rec:
	default:
		p.dropped.Add(1)
		// 限流日志, 避免刷屏
		if p.dropped.Load()%100 == 0 {
			p.logger.Warn("采集队列已满, 丢弃记录",
				"dropped_total", p.dropped.Load(),
				"request_id", rec.RequestID,
			)
		}
	}
}

// Stats 返回当前采集统计 (供 metrics 端点和健康检查).
func (p *Pipeline) Stats() PipelineStats {
	return PipelineStats{
		Submitted:    p.submitted.Load(),
		Processed:    p.processed.Load(),
		Dropped:      p.dropped.Load(),
		COSUpload:    p.cosUpload.Load(),
		COSFailed:    p.cosFailed.Load(),
		QueueLength:  len(p.queue),
		QueueCap:     cap(p.queue),
	}
}

// PipelineStats 用于监控.
type PipelineStats struct {
	Submitted   uint64
	Processed   uint64
	Dropped     uint64
	COSUpload   uint64
	COSFailed   uint64
	QueueLength int
	QueueCap    int
}

// Shutdown 优雅关闭: 关闭队列, 等待所有 worker 退出, 最终 flush sink.
func (p *Pipeline) Shutdown(ctx context.Context) error {
	p.logger.Info("Pipeline 开始关闭", "queue_remaining", len(p.queue))
	close(p.queue)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("所有 worker 已退出")
	case <-ctx.Done():
		p.logger.Warn("等待 worker 超时, 强制退出", "queue_remaining", len(p.queue))
		return ctx.Err()
	}

	// 最终 flush CH sink
	flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.chSink.Flush(flushCtx); err != nil {
		p.logger.Error("最终 flush 失败", "err", err)
		return err
	}

	stats := p.Stats()
	p.logger.Info("Pipeline 关闭完成",
		"submitted", stats.Submitted,
		"processed", stats.Processed,
		"dropped", stats.Dropped,
		"cos_upload", stats.COSUpload,
		"cos_failed", stats.COSFailed,
	)
	return nil
}

// ============ 内部 ============

func (p *Pipeline) worker(ctx context.Context, id int) {
	defer p.wg.Done()
	logger := p.logger.With("worker", id)
	logger.Debug("worker 启动")

	for rec := range p.queue {
		p.process(ctx, rec)
		p.processed.Add(1)
	}
	logger.Debug("worker 退出")
}

func (p *Pipeline) process(ctx context.Context, rec *Record) {
	// ============ 1. 大 body offload 到 COS ============
	if len(rec.RawRequestBody) > p.cfg.COSOffloadThreshold {
		key := makeCOSKey(rec, "request")
		if err := p.uploadCOS(ctx, key, rec.RawRequestBody); err == nil {
			rec.RequestCOSKey = key
			rec.RequestJSON = "" // 不再内联
		} else {
			// 上传失败, 退化到内联存储 (尽量保留数据)
			rec.RequestJSON = string(rec.RawRequestBody)
		}
	} else {
		rec.RequestJSON = string(rec.RawRequestBody)
	}

	if len(rec.RawResponseBody) > p.cfg.COSOffloadThreshold {
		key := makeCOSKey(rec, "response")
		if err := p.uploadCOS(ctx, key, rec.RawResponseBody); err == nil {
			rec.ResponseCOSKey = key
			rec.ResponseJSON = ""
		} else {
			rec.ResponseJSON = string(rec.RawResponseBody)
		}
	} else {
		rec.ResponseJSON = string(rec.RawResponseBody)
	}

	// ============ 2. 释放原始 body 内存 ============
	rec.RawRequestBody = nil
	rec.RawResponseBody = nil

	// ============ 3. 提交 CH ============
	p.chSink.Submit(rec)
}

func (p *Pipeline) uploadCOS(ctx context.Context, key string, data []byte) error {
	uploadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := p.cosSink.Upload(uploadCtx, key, data, "application/json"); err != nil {
		p.cosFailed.Add(1)
		p.logger.Warn("COS 上传失败", "key", key, "err", err, "size", len(data))
		return err
	}
	p.cosUpload.Add(1)
	return nil
}

// makeCOSKey 生成 COS object key.
//   格式: yyyy/mm/dd/hh/{request_id}-{kind}.json
//   按时间分层避免单目录文件爆炸,kind 区分请求/响应
func makeCOSKey(rec *Record, kind string) string {
	t := rec.StartedAt.UTC()
	return fmt.Sprintf("%04d/%02d/%02d/%02d/%s-%s.json",
		t.Year(), t.Month(), t.Day(), t.Hour(),
		rec.RequestID, kind,
	)
}
