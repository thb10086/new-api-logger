// Package protocol 提供 4 种 LLM 协议的请求/响应解析:
//   - OpenAI Chat       (/v1/chat/completions)
//   - OpenAI Responses  (/v1/responses)
//   - Anthropic         (/v1/messages)
//   - Gemini            (/v1beta/models/{model}:generateContent)
//
// 解析目标: 抽取 Capture Record 所需的元信息 (model / token / preview / 多模态标记 / tool_calls).
// 不存储完整的归一化 messages 结构, 原始 JSON 由 capture 层保留.
package protocol

import (
	"github.com/prismai/llm-logger/gateway/internal/sse"
)

// ParsedRequest 是请求体解析结果.
//
// 所有字段如解析失败默认零值,不返回 error (容错优先,采集场景容忍部分字段缺失).
type ParsedRequest struct {
	Model        string
	MessageCount uint16
	HasImage     bool
	HasAudio     bool
	HasVideo     bool
	HasTool      bool
	Temperature  *float32
	TopP         *float32
	MaxTokens    *uint32
	Stream       bool
	TextPreview  string // 最后一条 user/input 文本的截断
}

// ParsedResponse 是响应体解析结果.
type ParsedResponse struct {
	FinishReason     string
	PromptTokens     uint32
	CompletionTokens uint32
	TotalTokens      uint32
	CachedTokens     uint32
	ReasoningTokens  uint32
	ChunkCount       uint32
	TextPreview      string // assistant 完整回复的截断
	ToolCallsJSON    string

	IsError      bool
	ErrorType    string
	ErrorMessage string
}

// Parser 是各协议的统一解析接口.
type Parser interface {
	// Provider 返回协议标识符 (写入 CH 的 provider 列).
	Provider() string

	// ParseRequest 解析请求体. body 为客户端发给上游的原始 JSON.
	ParseRequest(body []byte, previewBytes int) ParsedRequest

	// ParseResponse 解析非流式响应体.
	ParseResponse(body []byte, previewBytes int) ParsedResponse

	// ParseStreamResponse 解析流式响应 (已切分好的 SSE events).
	ParseStreamResponse(events []sse.Event, previewBytes int) ParsedResponse
}
