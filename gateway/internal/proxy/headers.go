// Package proxy - HTTP header 工具.
package proxy

import (
	"net"
	"net/http"
	"strings"
)

// hop-by-hop headers 不应转发, 详见 RFC 7230 §6.1.
var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"proxy-connection":    {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
}

// IsHopHeader 判断是否为 hop-by-hop header.
func IsHopHeader(name string) bool {
	_, ok := hopByHopHeaders[strings.ToLower(name)]
	return ok
}

// CopyHeaders 复制 src → dst, 跳过 hop-by-hop.
//
// 同时移除 Connection 中列出的 header (RFC 要求).
func CopyHeaders(dst, src http.Header) {
	// 收集 Connection 中列出的 header
	connHeaders := map[string]struct{}{}
	for _, h := range src["Connection"] {
		for _, name := range strings.Split(h, ",") {
			name = strings.TrimSpace(strings.ToLower(name))
			if name != "" {
				connHeaders[name] = struct{}{}
			}
		}
	}

	for k, vs := range src {
		lower := strings.ToLower(k)
		if IsHopHeader(lower) {
			continue
		}
		if _, ok := connHeaders[lower]; ok {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// ClientIP 解析客户端 IP, 优先 X-Forwarded-For, 次 X-Real-IP, 最后 RemoteAddr.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// 取第一个 (最原始的客户端)
		parts := strings.Split(xff, ",")
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	if r.RemoteAddr != "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			return host
		}
		return r.RemoteAddr
	}
	return ""
}

// IsSSEResponse 判断响应是否为 SSE.
func IsSSEResponse(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	return strings.HasPrefix(strings.ToLower(ct), "text/event-stream")
}

// ExtractAPIKey 从请求中提取 API Key:
//   - Authorization: Bearer xxx
//   - x-api-key: xxx (Anthropic)
//   - ?key=xxx (Gemini)
//
// 返回原始 key 字符串 (不做哈希).
func ExtractAPIKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		// "Bearer sk-xxx"
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			return strings.TrimSpace(auth[7:])
		}
		return strings.TrimSpace(auth)
	}
	if k := r.Header.Get("x-api-key"); k != "" {
		return strings.TrimSpace(k)
	}
	if k := r.URL.Query().Get("key"); k != "" {
		return strings.TrimSpace(k)
	}
	return ""
}
