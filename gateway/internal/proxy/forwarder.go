// Package proxy - 反向代理转发器.
//
// 不使用 httputil.ReverseProxy, 因为它无法精确控制响应 body 的 tee 采集.
// 自己实现一份 forwarder, 支持:
//   - 透明 SSE 流式 (按 chunk flush)
//   - 边读边采集 (TeeReader + LimitedBuffer)
//   - 客户端断连时立即停止上游读取
//   - 完整保留 upstream headers (供 capture 提取 model 等元数据)
package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prismai/llm-logger/gateway/internal/config"
)

// Forwarder 转发请求到上游.
type Forwarder struct {
	cfg    *config.Config
	target *url.URL
	client *http.Client
	logger *slog.Logger
}

// NewForwarder 创建 forwarder.
func NewForwarder(cfg *config.Config, logger *slog.Logger) (*Forwarder, error) {
	u, err := url.Parse(cfg.UpstreamURL)
	if err != nil {
		return nil, fmt.Errorf("解析 UPSTREAM_URL 失败: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("UPSTREAM_URL 必须带 scheme 和 host")
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: cfg.UpstreamTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          500,
		MaxIdleConnsPerHost:   100,
		// 禁用上游响应解压, 保持透传
		DisableCompression: true,
	}

	return &Forwarder{
		cfg:    cfg,
		target: u,
		client: &http.Client{
			Transport: transport,
			// 不在 client 上设 timeout, 由 transport 控制
		},
		logger: logger.With("component", "forwarder"),
	}, nil
}

// ForwardResult 是一次转发的结果, 供 capture 使用.
type ForwardResult struct {
	RequestBody       []byte
	ResponseBody      []byte
	ResponseTruncated bool
	StatusCode        int
	UpstreamHeaders   http.Header
	IsStream          bool
	TTFTMs            uint32
	DurationMs        uint32
	Err               error // 上游/客户端错误 (可选)
}

// Forward 把客户端请求 r 转发到上游, 边响应边采集.
//
// 调用方应在收到 result 后构造 capture.Record 并提交到 pipeline.
//
// 注意: 调用此函数会消费 r.Body 并写 w. 调用后不要再写 w.
func (f *Forwarder) Forward(w http.ResponseWriter, r *http.Request) *ForwardResult {
	result := &ForwardResult{}
	startedAt := time.Now()
	defer func() {
		result.DurationMs = uint32(time.Since(startedAt).Milliseconds())
	}()

	// ============ 1. 读请求 body (限大小) ============
	reqBody, err := io.ReadAll(io.LimitReader(r.Body, f.cfg.MaxBodyBytes))
	if err != nil {
		result.Err = fmt.Errorf("读取请求 body 失败: %w", err)
		writeProxyError(w, http.StatusBadGateway, "读取请求 body 失败")
		return result
	}
	result.RequestBody = reqBody
	_ = r.Body.Close()

	// ============ 2. 构造上游 URL ============
	upstreamURL := *f.target
	// 拼路径: target.Path + r.URL.Path (避免重复 /)
	upstreamURL.Path = strings.TrimRight(f.target.Path, "/") + r.URL.Path
	upstreamURL.RawQuery = r.URL.RawQuery

	// ============ 3. 构造上游请求 ============
	upstreamReq, err := http.NewRequestWithContext(
		r.Context(), r.Method, upstreamURL.String(),
		bytes.NewReader(reqBody),
	)
	if err != nil {
		result.Err = fmt.Errorf("构造上游请求失败: %w", err)
		writeProxyError(w, http.StatusBadGateway, "构造上游请求失败")
		return result
	}

	CopyHeaders(upstreamReq.Header, r.Header)
	// 添加 X-Forwarded-For
	if clientIP := ClientIP(r); clientIP != "" {
		if prior := upstreamReq.Header.Get("X-Forwarded-For"); prior != "" {
			upstreamReq.Header.Set("X-Forwarded-For", prior+", "+clientIP)
		} else {
			upstreamReq.Header.Set("X-Forwarded-For", clientIP)
		}
	}
	upstreamReq.Header.Set("X-Forwarded-Host", r.Host)
	upstreamReq.Header.Set("X-Forwarded-Proto", schemeOf(r))
	upstreamReq.Host = f.target.Host
	upstreamReq.ContentLength = int64(len(reqBody))

	// ============ 4. 发送 ============
	upstreamStart := time.Now()
	upstreamResp, err := f.client.Do(upstreamReq)
	if err != nil {
		result.Err = fmt.Errorf("调用上游失败: %w", err)
		// 区分客户端取消 vs 上游错误
		if errors.Is(err, context.Canceled) {
			f.logger.Debug("客户端取消请求", "path", r.URL.Path)
			return result
		}
		writeProxyError(w, http.StatusBadGateway, "上游不可达: "+err.Error())
		return result
	}
	defer upstreamResp.Body.Close()

	result.StatusCode = upstreamResp.StatusCode
	result.UpstreamHeaders = upstreamResp.Header.Clone()
	result.IsStream = IsSSEResponse(upstreamResp)

	// ============ 5. 写客户端响应 header ============
	CopyHeaders(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)

	// ============ 6. 边读边采集 + 边 flush ============
	respBuf := NewLimitedBuffer(f.cfg.MaxBodyBytes)
	teeReader := io.TeeReader(upstreamResp.Body, respBuf)

	flusher, _ := w.(http.Flusher)
	var firstByte bool
	bufSize := 8192
	if result.IsStream {
		bufSize = 4096 // SSE 小 chunk 更频繁 flush
	}
	buf := make([]byte, bufSize)

	for {
		n, rerr := teeReader.Read(buf)
		if n > 0 {
			if !firstByte {
				result.TTFTMs = uint32(time.Since(upstreamStart).Milliseconds())
				firstByte = true
			}
			if _, werr := w.Write(buf[:n]); werr != nil {
				// 客户端断连
				f.logger.Debug("客户端断连", "path", r.URL.Path, "written", respBuf.Len())
				break
			}
			if result.IsStream && flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				f.logger.Debug("上游读取结束", "err", rerr)
			}
			break
		}
	}

	if !result.IsStream && flusher != nil {
		flusher.Flush()
	}

	result.ResponseBody = respBuf.Bytes()
	result.ResponseTruncated = respBuf.Truncated()
	return result
}

// writeProxyError 在转发失败时写入错误响应.
func writeProxyError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := fmt.Sprintf(`{"error":{"message":%q,"type":"proxy_error"}}`, msg)
	_, _ = w.Write([]byte(body))
}

func schemeOf(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}
