// Package sink - ClickHouse 异步批量写入器.
//
// 设计:
//   - 内部 ring buffer (受 cfg.CHBatchSize 控制)
//   - 后台 ticker 触发周期性 flush (cfg.CHBatchInterval)
//   - 批满立即 flush (主动)
//   - 直接走 CH HTTP /?query=INSERT...FORMAT+JSONEachRow 接口
//     依赖 CH 的 async_insert (在 users.d/async_insert.xml 配置)
//   - 写入失败时退避重试 (3 次, 指数退避), 仍失败则记录到 dropped 计数
//
// 不依赖第三方 CH client (避免引入大依赖, HTTP 已足够).
package sink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prismai/llm-logger/gateway/internal/capture"
	"github.com/prismai/llm-logger/gateway/internal/config"
)

// ClickHouseSink 实现 capture.CHSink.
type ClickHouseSink struct {
	cfg       *config.Config
	client    *http.Client
	logger    *slog.Logger
	insertURL string

	mu     sync.Mutex
	buffer []*capture.Record

	stopCh chan struct{}
	wg     sync.WaitGroup

	// 指标
	flushedBatches atomic.Uint64
	flushedRecords atomic.Uint64
	failedBatches  atomic.Uint64
	failedRecords  atomic.Uint64
}

// NewClickHouseSink 创建 sink 并启动后台 flush 协程.
func NewClickHouseSink(cfg *config.Config, logger *slog.Logger) *ClickHouseSink {
	q := url.Values{}
	q.Set("database", cfg.CHDatabase)
	q.Set("query", fmt.Sprintf(
		"INSERT INTO %s FORMAT JSONEachRow", cfg.CHTable,
	))
	q.Set("input_format_skip_unknown_fields", "1")
	q.Set("date_time_input_format", "best_effort")
	q.Set("async_insert", "1")
	q.Set("wait_for_async_insert", "0")

	insertURL := cfg.CHHTTPEndpoint() + "?" + q.Encode()

	s := &ClickHouseSink{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger:    logger.With("component", "ch-sink"),
		insertURL: insertURL,
		stopCh:    make(chan struct{}),
	}
	s.wg.Add(1)
	go s.flushLoop()
	return s
}

// Submit 把记录加入缓冲区.
func (s *ClickHouseSink) Submit(rec *capture.Record) {
	s.mu.Lock()
	s.buffer = append(s.buffer, rec)
	shouldFlush := len(s.buffer) >= s.cfg.CHBatchSize
	s.mu.Unlock()

	if shouldFlush {
		go s.flushOnce(context.Background())
	}
}

// Flush 立即同步 flush 所有缓冲, 用于优雅关闭.
func (s *ClickHouseSink) Flush(ctx context.Context) error {
	close(s.stopCh)
	s.wg.Wait()
	return s.flushOnce(ctx)
}

// Stats 当前指标快照.
func (s *ClickHouseSink) Stats() CHStats {
	s.mu.Lock()
	pending := len(s.buffer)
	s.mu.Unlock()
	return CHStats{
		Pending:        pending,
		FlushedBatches: s.flushedBatches.Load(),
		FlushedRecords: s.flushedRecords.Load(),
		FailedBatches:  s.failedBatches.Load(),
		FailedRecords:  s.failedRecords.Load(),
	}
}

// CHStats 用于监控.
type CHStats struct {
	Pending        int
	FlushedBatches uint64
	FlushedRecords uint64
	FailedBatches  uint64
	FailedRecords  uint64
}

// ============ 内部 ============

func (s *ClickHouseSink) flushLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.cfg.CHBatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			_ = s.flushOnce(context.Background())
		}
	}
}

// flushOnce 取出当前 buffer 全部内容并发送.
func (s *ClickHouseSink) flushOnce(ctx context.Context) error {
	s.mu.Lock()
	if len(s.buffer) == 0 {
		s.mu.Unlock()
		return nil
	}
	batch := s.buffer
	s.buffer = make([]*capture.Record, 0, s.cfg.CHBatchSize)
	s.mu.Unlock()

	return s.sendWithRetry(ctx, batch)
}

func (s *ClickHouseSink) sendWithRetry(ctx context.Context, batch []*capture.Record) error {
	const maxRetries = 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			// 指数退避: 200ms / 600ms / 1.8s
			backoff := time.Duration(200*pow3(i)) * time.Millisecond
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
				break
			case <-time.After(backoff):
			}
		}

		err := s.send(ctx, batch)
		if err == nil {
			s.flushedBatches.Add(1)
			s.flushedRecords.Add(uint64(len(batch)))
			return nil
		}
		lastErr = err
		s.logger.Warn("CH 批量写入失败 (重试中)",
			"attempt", i+1, "max", maxRetries,
			"size", len(batch), "err", err)
	}

	s.failedBatches.Add(1)
	s.failedRecords.Add(uint64(len(batch)))
	s.logger.Error("CH 批量写入最终失败,数据已丢弃",
		"size", len(batch), "err", lastErr)
	// TODO: 可选 - 落本地 NDJSON 文件做兜底, 由 ops 工具回放
	return lastErr
}

func (s *ClickHouseSink) send(ctx context.Context, batch []*capture.Record) error {
	// 构造 JSONEachRow body
	var buf bytes.Buffer
	buf.Grow(len(batch) * 4096)
	enc := json.NewEncoder(&buf)
	for _, rec := range batch {
		if err := enc.Encode(rec); err != nil {
			s.logger.Warn("记录序列化失败,跳过", "err", err, "request_id", rec.RequestID)
			continue
		}
	}

	if buf.Len() == 0 {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.insertURL, &buf)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	if s.cfg.CHUser != "" {
		req.SetBasicAuth(s.cfg.CHUser, s.cfg.CHPassword)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送 HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("CH HTTP %d: %s", resp.StatusCode, string(body))
	}

	// 读完 body, 让连接复用
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func pow3(n int) int {
	r := 1
	for i := 0; i < n; i++ {
		r *= 3
	}
	return r
}
