// Package protocol - Anthropic Messages 协议解析.
//
// 路径: POST /v1/messages
// 文档: https://docs.anthropic.com/en/api/messages
//
// 关键结构:
//   - messages[{role,content}], content 为 string 或 ContentBlock[]
//   - 流式 SSE 事件 (event 字段):
//       message_start          (含初始 message + usage.input_tokens)
//       content_block_start    (text / tool_use / thinking 等)
//       content_block_delta    (text_delta / input_json_delta / thinking_delta)
//       content_block_stop
//       message_delta          (含 stop_reason + usage.output_tokens)
//       message_stop
package protocol

import (
	"encoding/json"
	"strings"

	"github.com/prismai/llm-logger/gateway/internal/sse"
)

// Anthropic 创建 Anthropic 协议 parser.
func Anthropic() Parser { return &anthropic{} }

type anthropic struct{}

func (p *anthropic) Provider() string { return "anthropic" }

func (p *anthropic) ParseRequest(body []byte, previewBytes int) ParsedRequest {
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

	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		pr.HasTool = true
	}

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
					case "image":
						pr.HasImage = true
					case "document":
						// PDF 等文档
					case "tool_use", "tool_result":
						pr.HasTool = true
					case "thinking":
						// extended thinking input (claude 3.7+)
					}
				}
			}
		}
	}
	pr.TextPreview = truncate(lastUserText.String(), previewBytes)
	return pr
}

// ParseResponse 非流式 Anthropic 响应.
//
// 结构:
//   { "id":"msg_xxx", "type":"message", "role":"assistant",
//     "content":[{type:"text",text:"..."},{type:"tool_use",...}],
//     "model":"...", "stop_reason":"end_turn",
//     "usage":{"input_tokens":N, "output_tokens":N, "cache_read_input_tokens":N} }
func (p *anthropic) ParseResponse(body []byte, previewBytes int) ParsedResponse {
	pr := ParsedResponse{}
	if len(body) == 0 {
		return pr
	}

	var resp struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text,omitempty"`
			ID    string          `json:"id,omitempty"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      *struct {
			InputTokens              uint32 `json:"input_tokens"`
			OutputTokens             uint32 `json:"output_tokens"`
			CacheCreationInputTokens uint32 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     uint32 `json:"cache_read_input_tokens"`
		} `json:"usage,omitempty"`
		Type    string `json:"type"`
		Error   *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return pr
	}

	if resp.Usage != nil {
		pr.PromptTokens = resp.Usage.InputTokens
		pr.CompletionTokens = resp.Usage.OutputTokens
		pr.TotalTokens = resp.Usage.InputTokens + resp.Usage.OutputTokens
		pr.CachedTokens = resp.Usage.CacheReadInputTokens
	}

	if resp.Type == "error" || resp.Error != nil {
		pr.IsError = true
		if resp.Error != nil {
			pr.ErrorType = resp.Error.Type
			pr.ErrorMessage = resp.Error.Message
		}
	}

	pr.FinishReason = resp.StopReason

	var textBuilder strings.Builder
	var tools []map[string]any
	for _, c := range resp.Content {
		switch c.Type {
		case "text":
			textBuilder.WriteString(c.Text)
		case "tool_use":
			tools = append(tools, map[string]any{
				"id":    c.ID,
				"name":  c.Name,
				"input": json.RawMessage(c.Input),
			})
		}
	}
	pr.TextPreview = truncate(textBuilder.String(), previewBytes)
	if len(tools) > 0 {
		pr.ToolCallsJSON = jsonStringify(tools)
	}
	return pr
}

// ParseStreamResponse 解析 Anthropic SSE 流.
//
// 累加 content_block_delta 中的 text_delta / thinking_delta;
// 从 message_delta 提取 stop_reason 和 output_tokens;
// 从 message_start 提取 input_tokens.
func (p *anthropic) ParseStreamResponse(events []sse.Event, previewBytes int) ParsedResponse {
	pr := ParsedResponse{}
	var textBuilder strings.Builder
	var toolUseByIdx = map[int]map[string]any{}
	var toolInputBuilders = map[int]*strings.Builder{}

	for _, ev := range events {
		if ev.Data == "" {
			continue
		}
		pr.ChunkCount++

		switch ev.Event {
		case "message_start":
			var d struct {
				Message struct {
					Usage struct {
						InputTokens          uint32 `json:"input_tokens"`
						CacheReadInputTokens uint32 `json:"cache_read_input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &d); err == nil {
				pr.PromptTokens = d.Message.Usage.InputTokens
				pr.CachedTokens = d.Message.Usage.CacheReadInputTokens
			}

		case "content_block_start":
			var d struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id,omitempty"`
					Name string `json:"name,omitempty"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &d); err == nil {
				if d.ContentBlock.Type == "tool_use" {
					toolUseByIdx[d.Index] = map[string]any{
						"id":   d.ContentBlock.ID,
						"name": d.ContentBlock.Name,
					}
					toolInputBuilders[d.Index] = &strings.Builder{}
				}
			}

		case "content_block_delta":
			var d struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text,omitempty"`         // text_delta
					Thinking    string `json:"thinking,omitempty"`     // thinking_delta
					PartialJSON string `json:"partial_json,omitempty"` // input_json_delta
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &d); err == nil {
				switch d.Delta.Type {
				case "text_delta":
					textBuilder.WriteString(d.Delta.Text)
				case "input_json_delta":
					if b, ok := toolInputBuilders[d.Index]; ok {
						b.WriteString(d.Delta.PartialJSON)
					}
				}
			}

		case "message_delta":
			var d struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens uint32 `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &d); err == nil {
				if d.Delta.StopReason != "" {
					pr.FinishReason = d.Delta.StopReason
				}
				pr.CompletionTokens = d.Usage.OutputTokens
			}

		case "error":
			var d struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &d); err == nil {
				pr.IsError = true
				pr.ErrorType = d.Error.Type
				pr.ErrorMessage = d.Error.Message
			}
		}
	}

	if pr.PromptTokens > 0 || pr.CompletionTokens > 0 {
		pr.TotalTokens = pr.PromptTokens + pr.CompletionTokens
	}

	pr.TextPreview = truncate(textBuilder.String(), previewBytes)

	// 整理 tool_calls 输出
	if len(toolUseByIdx) > 0 {
		var tools []map[string]any
		for i := 0; i < len(toolUseByIdx); i++ {
			t, ok := toolUseByIdx[i]
			if !ok {
				continue
			}
			if b, ok := toolInputBuilders[i]; ok {
				t["input_raw"] = b.String()
			}
			tools = append(tools, t)
		}
		pr.ToolCallsJSON = jsonStringify(tools)
	}
	return pr
}
