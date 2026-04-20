// Package protocol - 解析工具函数.
package protocol

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// truncate 截断字符串到 maxBytes (按 UTF-8 边界, 不切坏字符).
func truncate(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	// 找到 maxBytes 之前最近的 UTF-8 边界
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// firstStringField 在 map 里找第一个非空 string 字段.
func firstStringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// asFloat32Ptr 把 any 转为 *float32 (容错).
func asFloat32Ptr(v any) *float32 {
	switch x := v.(type) {
	case float64:
		f := float32(x)
		return &f
	case float32:
		return &x
	case json.Number:
		if f, err := x.Float64(); err == nil {
			f32 := float32(f)
			return &f32
		}
	}
	return nil
}

// asUint32Ptr 把 any 转为 *uint32 (容错).
func asUint32Ptr(v any) *uint32 {
	switch x := v.(type) {
	case float64:
		if x < 0 {
			return nil
		}
		u := uint32(x)
		return &u
	case int:
		if x < 0 {
			return nil
		}
		u := uint32(x)
		return &u
	case int64:
		if x < 0 {
			return nil
		}
		u := uint32(x)
		return &u
	case json.Number:
		if i, err := x.Int64(); err == nil && i >= 0 {
			u := uint32(i)
			return &u
		}
	}
	return nil
}

// asBool 判断 map 字段是否为 true.
func asBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// concatStrings 拼接字符串列表 (避免 strings.Builder 在小列表时的开销).
func concatStrings(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, "")
}

// jsonStringify 把任意值序列化为 JSON 字符串 (失败返回 "").
func jsonStringify(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
