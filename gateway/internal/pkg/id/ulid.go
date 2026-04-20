// Package id 提供唯一 ID 生成 (ULID).
//
// 选择 ULID 而非 UUID 的理由:
//   - 26 字符 vs 36 字符,更短
//   - 字典序 = 时间序,天然支持时间范围扫描
//   - 高性能 (单核每秒数百万)
package id

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	entropyMu sync.Mutex
	entropy   = ulid.Monotonic(rand.Reader, 0)
)

// New 生成一个新的 ULID 字符串 (26 字符, 大写, base32 字典序).
//
// 并发安全.
func New() string {
	entropyMu.Lock()
	defer entropyMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}

// NewAt 生成一个指定时间的 ULID (用于测试或回填).
func NewAt(t time.Time) string {
	entropyMu.Lock()
	defer entropyMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(t), entropy).String()
}
