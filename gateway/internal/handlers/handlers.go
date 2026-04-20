// Package handlers 实现 4 种 LLM 协议的 HTTP 路由处理.
//
// 所有 handler 共享同一段逻辑:
//   1. 准备 capture.Record (timestamps, client info, api_key hash)
//   2. 调用 forwarder.Forward() 透明转发并采集
//   3. 调用对应 Parser 解析请求/响应,填充 record 字段
//   4. 提交 record 到 pipeline
package handlers

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/prismai/llm-logger/gateway/internal/capture"
	"github.com/prismai/llm-logger/gateway/internal/config"
	"github.com/prismai/llm-logger/gateway/internal/pkg/hash"
	"github.com/prismai/llm-logger/gateway/internal/pkg/id"
	"github.com/prismai/llm-logger/gateway/internal/protocol"
	"github.com/prismai/llm-logger/gateway/internal/proxy"
	"github.com/prismai/llm-logger/gateway/internal/sse"
)

// Handler 是单个协议的 HTTP handler.
type Handler struct {
	cfg       *config.Config
	forwarder *proxy.Forwarder
	pipeline  *capture.Pipeline
	parser    protocol.Parser
	logger    *slog.Logger

	// 部分协议 (Gemini) 的 model 在 URL path 中, 需要通过 PathValue 提取.
	modelFromPath string // PathValue 名, 空则不从 path 取
}

// New 创建一个协议 handler.
func New(cfg *config.Config, fw *proxy.Forwarder, pl *capture.Pipeline, parser protocol.Parser, logger *slog.Logger) *Handler {
	return &Handler{
		cfg:       cfg,
		forwarder: fw,
		pipeline:  pl,
		parser:    parser,
		logger:    logger.With("provider", parser.Provider()),
	}
}

// WithModelFromPath 配置从 URL PathValue 提取 model 字段.
//
// Gemini 的 model 在 path 中 (如 /v1beta/models/gemini-2.0-flash:generateContent),
// 用 path pattern POST /v1beta/models/{model_with_action} 后调用 r.PathValue("model_with_action").
func (h *Handler) WithModelFromPath(pathValueName string) *Handler {
	clone := *h
	clone.modelFromPath = pathValueName
	return &clone
}

// ServeHTTP 处理一次请求.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// ============ 1. 创建 record ============
	requestID := id.New()
	rec := &capture.Record{
		RequestID:       requestID,
		Provider:        h.parser.Provider(),
		Endpoint:        r.URL.Path,
		HTTPMethod:      r.Method,
		ClientIP:        proxy.ClientIP(r),
		ClientUserAgent: truncateString(r.UserAgent(), 256),
	}
	rec.SetCreatedAt(time.Now())

	apiKey := proxy.ExtractAPIKey(r)
	if apiKey != "" {
		rec.APIKeyHash = hash.SHA256Hex(apiKey)
		rec.APIKeyPrefix = hash.APIKeyPrefix(apiKey)
	}

	// 给客户端注入 request_id (便于客户端关联日志)
	w.Header().Set("X-LLM-Logger-Request-Id", requestID)

	// ============ 2. 转发并采集 ============
	result := h.forwarder.Forward(w, r)
	if result == nil {
		return
	}
	rec.SetCompletedAt(time.Now())
	rec.UpstreamStatus = uint16(result.StatusCode)
	rec.IsStream = result.IsStream
	rec.TTFTMs = result.TTFTMs
	rec.DurationMs = result.DurationMs
	rec.RequestSizeBytes = uint32(len(result.RequestBody))
	rec.ResponseSizeBytes = uint32(len(result.ResponseBody))
	rec.RawRequestBody = result.RequestBody
	rec.RawResponseBody = result.ResponseBody
	rec.UpstreamHeaders = result.UpstreamHeaders

	if result.Err != nil {
		rec.IsError = true
		rec.ErrorType = "proxy"
		rec.ErrorMessage = truncateString(result.Err.Error(), 1024)
	} else if result.StatusCode >= 400 {
		rec.IsError = true
		if rec.ErrorType == "" {
			rec.ErrorType = "upstream_http"
		}
	}

	// ============ 3. 解析协议 ============
	req := h.parser.ParseRequest(result.RequestBody, h.cfg.PreviewBytes)

	// 从 path 提取 model (Gemini)
	if h.modelFromPath != "" {
		if pv := r.PathValue(h.modelFromPath); pv != "" {
			rec.Model = parseModelFromPath(pv)
		}
	}
	if rec.Model == "" {
		rec.Model = req.Model
	}
	rec.ModelNormalized = normalizeModel(rec.Model)

	rec.RequestMessageCount = req.MessageCount
	rec.RequestHasImage = req.HasImage
	rec.RequestHasAudio = req.HasAudio
	rec.RequestHasVideo = req.HasVideo
	rec.RequestHasTool = req.HasTool
	rec.RequestTemperature = req.Temperature
	rec.RequestTopP = req.TopP
	rec.RequestMaxTokens = req.MaxTokens
	rec.RequestTextPreview = req.TextPreview

	// 解析响应 (流式 / 非流式)
	var resp protocol.ParsedResponse
	if result.IsStream {
		events := sse.Parse(result.ResponseBody)
		resp = h.parser.ParseStreamResponse(events, h.cfg.PreviewBytes)
		if h.cfg.StreamChunksKeep {
			rec.ResponseStreamChunks = string(result.ResponseBody)
		}
	} else if !rec.IsError && len(result.ResponseBody) > 0 {
		resp = h.parser.ParseResponse(result.ResponseBody, h.cfg.PreviewBytes)
	} else if rec.IsError && len(result.ResponseBody) > 0 {
		// 错误也尝试解析 (上游可能返回结构化 error)
		resp = h.parser.ParseResponse(result.ResponseBody, h.cfg.PreviewBytes)
	}

	rec.ResponseFinishReason = resp.FinishReason
	rec.ResponseChunkCount = resp.ChunkCount
	rec.ResponseTextPreview = resp.TextPreview
	rec.PromptTokens = resp.PromptTokens
	rec.CompletionTokens = resp.CompletionTokens
	rec.TotalTokens = resp.TotalTokens
	rec.CachedTokens = resp.CachedTokens
	rec.ReasoningTokens = resp.ReasoningTokens
	rec.ToolCallsJSON = resp.ToolCallsJSON

	if resp.IsError {
		rec.IsError = true
		if resp.ErrorType != "" {
			rec.ErrorType = resp.ErrorType
		}
		if resp.ErrorMessage != "" {
			rec.ErrorMessage = truncateString(resp.ErrorMessage, 1024)
		}
	}

	// ============ 4. 提交 pipeline ============
	h.pipeline.Submit(rec)

	h.logger.Debug("请求完成",
		"request_id", requestID,
		"model", rec.Model,
		"status", rec.UpstreamStatus,
		"stream", rec.IsStream,
		"duration_ms", rec.DurationMs,
		"prompt_tokens", rec.PromptTokens,
		"completion_tokens", rec.CompletionTokens,
	)
}

// parseModelFromPath 从 Gemini path 段中提取 model 名.
//
// 输入示例:
//   "gemini-2.0-flash:generateContent"
//   "gemini-1.5-pro-latest:streamGenerateContent"
// 输出: "gemini-2.0-flash" / "gemini-1.5-pro-latest"
func parseModelFromPath(seg string) string {
	if i := strings.IndexByte(seg, ':'); i > 0 {
		return seg[:i]
	}
	return seg
}

// normalizeModel 把 model 名做归一化, 用于跨厂商聚合查询.
//
// 示例:
//   "gpt-4o-2024-08-06" -> "gpt-4o"
//   "claude-3-5-sonnet-20241022" -> "claude-3-5-sonnet"
//   "gemini-2.0-flash-001" -> "gemini-2.0-flash"
//
// 简单实现: 去除末尾的日期/版本后缀.
func normalizeModel(m string) string {
	if m == "" {
		return ""
	}
	parts := strings.Split(m, "-")
	for i := len(parts) - 1; i > 0; i-- {
		p := parts[i]
		if isDateOrVersion(p) {
			parts = parts[:i]
			continue
		}
		break
	}
	return strings.Join(parts, "-")
}

func isDateOrVersion(s string) bool {
	if len(s) == 0 {
		return false
	}
	// 全数字
	allDigit := true
	for _, c := range s {
		if c < '0' || c > '9' {
			allDigit = false
			break
		}
	}
	if allDigit && (len(s) == 6 || len(s) == 8 || len(s) == 3 || len(s) == 4) {
		return true
	}
	if s == "latest" {
		return true
	}
	return false
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
