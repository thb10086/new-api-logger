// Package compress 封装 zstd 压缩 (用于 COS 上传).
package compress

import (
	"sync"

	"github.com/klauspost/compress/zstd"
)

var (
	encoderOnce sync.Once
	encoder     *zstd.Encoder
)

// initEncoder 创建一个 level 3 的 encoder (压缩率/速度均衡).
func initEncoder() {
	encoderOnce.Do(func() {
		enc, err := zstd.NewWriter(
			nil,
			zstd.WithEncoderLevel(zstd.SpeedDefault), // ~level 3
			zstd.WithEncoderConcurrency(1),
		)
		if err != nil {
			panic("zstd encoder 初始化失败: " + err.Error())
		}
		encoder = enc
	})
}

// EncodeAll 压缩 src,返回压缩后字节.并发安全.
func EncodeAll(src []byte) []byte {
	initEncoder()
	return encoder.EncodeAll(src, nil)
}
