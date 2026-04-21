// Package protocol - OpenAI Chat Completions 协议解析.
//
// 路径: POST /v1/chat/completions
// 格式: messages[{role,content}], 流式时 SSE chunk 含 choices[0].delta.content
//
// 兼容: Deepseek (chat + reasoner) / Qwen / Moonshot / Zhipu 等 OpenAI 兼容协议.
//   - Deepseek reasoner 多 reasoning_content 字段
//   - 部分提供商在最后 SSE chunk 才返回 usage
package protocol

import (
	"encoding/json"
	"strings"

	"github.com/thb10086/new-api-logger/gateway/internal/sse"
)

// OpenAIChat 创建 OpenAI Chat 协议 parser.
func OpenAIChat() Parser { return &openaiChat{} }

type openaiChat struct{}

func (p *openaiChat) Provider() string { return "openai_chat" }

// ParseRequest 解析 chat/completions 请求.
func (p *openaiChat) ParseRequest(body []byte, previewBytes int) ParsedRequest {
	pr := ParsedRequest{}
	if len(body) == 0 {
		return pr
	}

	var req map[string]any
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	if err := dec.Decode(&req); err != nil {
		return pr
	}

	if m, ok := req["model"].(string); ok {
		pr.Model = m
	}
	pr.Stream = asBool(req["stream"])
	pr.Temperature = asFloat32Ptr(req["temperature"])
	pr.TopP = asFloat32Ptr(req["top_p"])
	pr.MaxTokens = asUint32Ptr(req["max_tokens"])
	if pr.MaxTokens == nil {
		pr.MaxTokens = asUint32Ptr(req["max_completion_tokens"])
	}

	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		pr.HasTool = true
	}
	if funcs, ok := req["functions"].([]any); ok && len(funcs) > 0 {
		pr.HasTool = true
	}

	// 解析 messages
	var lastUserText strings.Builder
	if msgs, ok := req["messages"].([]any); ok {
		pr.MessageCount = uint16(len(msgs))
		for _, mm := range msgs {
			m, ok := mm.(map[string]any)
			if !ok {
				continue
			}
			role, _ := m["role"].(string)
			switch c := m["content"].(type) {
			case string:
				if role == "user" {
					lastUserText.Reset()
					lastUserText.WriteString(c)
				}
			case []any:
				for _, blk := range c {
					b, ok := blk.(map[string]any)
					if !ok {
						continue
					}
					t, _ := b["type"].(string)
					switch t {
					case "text":
						if txt, ok := b["text"].(string); ok && role == "user" {
							lastUserText.Reset()
							lastUserText.WriteString(txt)
						}
					case "image_url", "image":
						pr.HasImage = true
					case "input_audio", "audio":
						pr.HasAudio = true
					case "video_url", "video":
						pr.HasVideo = true
					}
				}
			}
		}
	}
	pr.TextPreview = truncate(lastUserText.String(), previewBytes)
	return pr
}

// ParseResponse 解析非流式 chat/completions 响应.
func (p *openaiChat) ParseResponse(body []byte, previewBytes int) ParsedResponse {
	pr := ParsedResponse{}
	if len(body) == 0 {
		return pr
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []any  `json:"tool_calls,omitempty"`
				// Deepseek reasoner
				ReasoningContent string `json:"reasoning_content,omitempty"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens        uint32 `json:"prompt_tokens"`
			CompletionTokens    uint32 `json:"completion_tokens"`
			TotalTokens         uint32 `json:"total_tokens"`
			PromptTokensDetails struct {
				CachedTokens uint32 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokensDetails struct {
				ReasoningTokens uint32 `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage,omitempty"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return pr
	}

	if resp.Usage != nil {
		pr.PromptTokens = resp.Usage.PromptTokens
		pr.CompletionTokens = resp.Usage.CompletionTokens
		pr.TotalTokens = resp.Usage.TotalTokens
		pr.CachedTokens = resp.Usage.PromptTokensDetails.CachedTokens
		pr.ReasoningTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
	}

	if resp.Error != nil {
		pr.IsError = true
		pr.ErrorMessage = resp.Error.Message
		pr.ErrorType = resp.Error.Type
	}

	if len(resp.Choices) > 0 {
		pr.FinishReason = resp.Choices[0].FinishReason
		pr.TextPreview = truncate(resp.Choices[0].Message.Content, previewBytes)
		if len(resp.Choices[0].Message.ToolCalls) > 0 {
			pr.ToolCallsJSON = jsonStringify(resp.Choices[0].Message.ToolCalls)
		}
	}
	return pr
}

// ParseStreamResponse 解析流式 chat/completions 响应.
//
// 处理要点:
//   - 累加 choices[0].delta.content
//   - finish_reason 取最后一条 chunk 的非空值
//   - usage 取最后包含 usage 字段的 chunk (OpenAI 在带 stream_options.include_usage 时返回)
//   - tool_calls.delta 累积到一个聚合数组 (粗略, 索引按 index 字段)
func (p *openaiChat) ParseStreamResponse(events []sse.Event, previewBytes int) ParsedResponse {
	pr := ParsedResponse{}
	var contentBuilder strings.Builder
	toolCallsByIdx := map[int]map[string]any{}

	for _, ev := range events {
		if ev.IsDone() || ev.Data == "" {
			continue
		}
		pr.ChunkCount++

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int             `json:"index"`
						ID       string          `json:"id,omitempty"`
						Type     string          `json:"type,omitempty"`
						Function json.RawMessage `json:"function,omitempty"`
					} `json:"tool_calls,omitempty"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason,omitempty"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens        uint32 `json:"prompt_tokens"`
				CompletionTokens    uint32 `json:"completion_tokens"`
				TotalTokens         uint32 `json:"total_tokens"`
				PromptTokensDetails struct {
					CachedTokens uint32 `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
				CompletionTokensDetails struct {
					ReasoningTokens uint32 `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			} `json:"usage,omitempty"`
		}
		if err := json.Unmarshal([]byte(ev.Data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 {
			ch := chunk.Choices[0]
			if ch.Delta.Content != "" {
				contentBuilder.WriteString(ch.Delta.Content)
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				pr.FinishReason = *ch.FinishReason
			}
			for _, tc := range ch.Delta.ToolCalls {
				cur, ok := toolCallsByIdx[tc.Index]
				if !ok {
					cur = map[string]any{"index": tc.Index}
					toolCallsByIdx[tc.Index] = cur
				}
				if tc.ID != "" {
					cur["id"] = tc.ID
				}
				if tc.Type != "" {
					cur["type"] = tc.Type
				}
				if len(tc.Function) > 0 {
					cur["function"] = json.RawMessage(tc.Function)
				}
			}
		}

		if chunk.Usage != nil {
			pr.PromptTokens = chunk.Usage.PromptTokens
			pr.CompletionTokens = chunk.Usage.CompletionTokens
			pr.TotalTokens = chunk.Usage.TotalTokens
			pr.CachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
			pr.ReasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
		}
	}

	pr.TextPreview = truncate(contentBuilder.String(), previewBytes)
	if len(toolCallsByIdx) > 0 {
		// 按 index 排序输出
		var tools []map[string]any
		for i := 0; i < len(toolCallsByIdx); i++ {
			if t, ok := toolCallsByIdx[i]; ok {
				tools = append(tools, t)
			}
		}
		// 兜底: 若 index 不连续, 把剩余的 append 上
		for i, t := range toolCallsByIdx {
			if i >= len(tools) {
				tools = append(tools, t)
			}
		}
		pr.ToolCallsJSON = jsonStringify(tools)
	}
	return pr
}
