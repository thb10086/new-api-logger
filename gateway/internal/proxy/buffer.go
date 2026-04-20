// Package proxy - 限大小的内存 buffer, 用于 SSE 流式响应采集.
package proxy

import (
	"bytes"
	"sync"
)

// LimitedBuffer 是有上限的 in-memory buffer.
//
// - 写入超过 limit 后, 后续 Write 静默丢弃 (返回 len(p) 假装成功)
// - 设置 truncated 标记, 可通过 Truncated() 查询
// - 并发安全 (Write 加锁)
//
// 用途: 包在 io.TeeReader 的 sink 端,
// 防止上游响应过大撑爆代理内存.
type LimitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int64
	written   int64
	truncated bool
}

// NewLimitedBuffer 创建一个上限为 limit 字节的 buffer.
//
// limit <= 0 时不设上限 (相当于普通 bytes.Buffer).
func NewLimitedBuffer(limit int64) *LimitedBuffer {
	return &LimitedBuffer{limit: limit}
}

// Write 实现 io.Writer.
//
// 永远返回 len(p), 不返回 error (静默截断).
func (b *LimitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.limit <= 0 {
		// 不限制
		n, _ := b.buf.Write(p)
		b.written += int64(n)
		return len(p), nil
	}

	if b.written >= b.limit {
		b.truncated = true
		return len(p), nil
	}

	remaining := b.limit - b.written
	if int64(len(p)) > remaining {
		b.buf.Write(p[:remaining])
		b.written = b.limit
		b.truncated = true
		return len(p), nil
	}
	b.buf.Write(p)
	b.written += int64(len(p))
	return len(p), nil
}

// Bytes 返回累积字节 (零拷贝, 调用方不应修改).
func (b *LimitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Bytes()
}

// Len 返回已写入字节数.
func (b *LimitedBuffer) Len() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.written
}

// Truncated 是否已截断.
func (b *LimitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// Reset 清空 buffer.
func (b *LimitedBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
	b.written = 0
	b.truncated = false
}
