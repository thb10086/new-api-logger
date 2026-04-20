// Package hash 提供敏感信息脱敏哈希.
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// SHA256Hex 返回输入字符串的 SHA-256 十六进制 (64 字符).
//
// 用于 api_key 脱敏存储 - 保留 hash 用于查询关联,不可逆推原文.
func SHA256Hex(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// APIKeyPrefix 返回 api_key 的可读前后缀 (用于 UI 展示),
// 如 "sk-abcd...wxyz" 这样既识别又脱敏.
//
// 输入太短时返回 "***".
func APIKeyPrefix(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	apiKey = strings.TrimPrefix(apiKey, "Bearer ")
	apiKey = strings.TrimPrefix(apiKey, "bearer ")
	if len(apiKey) < 12 {
		return "***"
	}
	return apiKey[:6] + "..." + apiKey[len(apiKey)-4:]
}
