// Package protocol - OpenAI Responses API 协议解析.
//
// 路径: POST /v1/responses
// 文档: https://platform.openai.com/docs/api-reference/responses
//
// 与 Chat Completions 的关键差异:
//   - 入参用 input (string | InputItem[]), 不是 messages
//   - 输出用 output (Item[]), 一个 message 是一种 Item
//   - 流式 SSE 事件类型多 (response.created / response.output_text.delta /
//     response.tool_call_item.added / response.completed 等)
package protocol

import (
	"encoding/json"
	"strings"

	"github.com/thb10086/new-api-logger/gateway/internal/sse"
)

// OpenAIResponses 创建 OpenAI Responses 协议 parser.
func OpenAIResponses() Parser { return &openaiResponses{} }

type openaiResponses struct{}

func (p *openaiResponses) Provider() string { return "openai_responses" }

func (p *openaiResponses) ParseRequest(body []byte, previewBytes int) ParsedRequest {
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
	pr.MaxTokens = asUint32Ptr(req["max_output_tokens"])

	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		pr.HasTool = true
	}

	// 解析 input: 可以是 string 或 InputItem[]
	var lastText strings.Builder
	switch in := req["input"].(type) {
	case string:
		pr.MessageCount = 1
		lastText.WriteString(in)
	case []any:
		pr.MessageCount = uint16(len(in))
		for _, it := range in {
			item, ok := it.(map[string]any)
			if !ok {
				continue
			}
			role, _ := item["role"].(string)
			switch c := item["content"].(type) {
			case string:
				if role == "user" {
					lastText.Reset()
					lastText.WriteString(c)
				}
			case []any:
				for _, blk := range c {
					b, ok := blk.(map[string]any)
					if !ok {
						continue
					}
					t, _ := b["type"].(string)
					switch t {
					case "input_text", "text":
						if txt, ok := b["text"].(string); ok && (role == "user" || role == "") {
							lastText.Reset()
							lastText.WriteString(txt)
						}
					case "input_image", "image":
						pr.HasImage = true
					case "input_audio", "audio":
						pr.HasAudio = true
					case "input_video", "video":
						pr.HasVideo = true
					}
				}
			}
		}
	}
	pr.TextPreview = truncate(lastText.String(), previewBytes)
	return pr
}

// ParseResponse 解析非流式 Responses 响应.
//
// 结构:
//   { "id":"resp_xxx", "model":"...", "output":[{type:"message",content:[...]}], "usage":{...} }
func (p *openaiResponses) ParseResponse(body []byte, previewBytes int) ParsedResponse {
	pr := ParsedResponse{}
	if len(body) == 0 {
		return pr
	}

	var resp struct {
		Output []struct {
			Type    string `json:"type"`
			Status  string `json:"status,omitempty"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"content,omitempty"`
		} `json:"output"`
		Status string `json:"status"` // completed / incomplete / failed
		Usage  *struct {
			InputTokens         uint32 `json:"input_tokens"`
			OutputTokens        uint32 `json:"output_tokens"`
			TotalTokens         uint32 `json:"total_tokens"`
			InputTokensDetails  struct {
				CachedTokens uint32 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			OutputTokensDetails struct {
				ReasoningTokens uint32 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage,omitempty"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details,omitempty"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return pr
	}

	if resp.Usage != nil {
		pr.PromptTokens = resp.Usage.InputTokens
		pr.CompletionTokens = resp.Usage.OutputTokens
		pr.TotalTokens = resp.Usage.TotalTokens
		pr.CachedTokens = resp.Usage.InputTokensDetails.CachedTokens
		pr.ReasoningTokens = resp.Usage.OutputTokensDetails.ReasoningTokens
	}

	if resp.Error != nil {
		pr.IsError = true
		pr.ErrorMessage = resp.Error.Message
		pr.ErrorType = resp.Error.Code
	}

	pr.FinishReason = resp.Status
	if resp.IncompleteDetails != nil {
		pr.FinishReason = "incomplete:" + resp.IncompleteDetails.Reason
	}

	// 拼接 message 类型 output 的所有 text content
	var textBuilder strings.Builder
	var toolCalls []map[string]any
	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				if c.Type == "output_text" || c.Type == "text" {
					textBuilder.WriteString(c.Text)
				}
			}
		case "function_call", "tool_call":
			// 记录原始 item 作为 tool call
			toolCalls = append(toolCalls, map[string]any{
				"type":   item.Type,
				"status": item.Status,
			})
		}
	}
	pr.TextPreview = truncate(textBuilder.String(), previewBytes)
	if len(toolCalls) > 0 {
		pr.ToolCallsJSON = jsonStringify(toolCalls)
	}
	return pr
}

// ParseStreamResponse 解析 Responses API 的 SSE 流.
//
// SSE 事件类型 (节选, 完整列表见官方文档):
//   - response.created
//   - response.in_progress
//   - response.output_item.added         (message / tool_call)
//   - response.output_text.delta         (text 增量)
//   - response.output_text.done
//   - response.tool_call_item.added
//   - response.function_call_arguments.delta
//   - response.completed                 (含完整 response 对象 + usage)
//   - response.failed
//   - error
func (p *openaiResponses) ParseStreamResponse(events []sse.Event, previewBytes int) ParsedResponse {
	pr := ParsedResponse{}
	var textBuilder strings.Builder

	for _, ev := range events {
		if ev.Data == "" {
			continue
		}
		pr.ChunkCount++

		switch ev.Event {
		case "response.output_text.delta":
			var d struct {
				Delta string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &d); err == nil {
				textBuilder.WriteString(d.Delta)
			}

		case "response.completed":
			// 含完整 response 对象, 可一次性提取 usage 和 status
			var d struct {
				Response struct {
					Status string `json:"status"`
					Usage  struct {
						InputTokens         uint32 `json:"input_tokens"`
						OutputTokens        uint32 `json:"output_tokens"`
						TotalTokens         uint32 `json:"total_tokens"`
						InputTokensDetails  struct {
							CachedTokens uint32 `json:"cached_tokens"`
						} `json:"input_tokens_details"`
						OutputTokensDetails struct {
							ReasoningTokens uint32 `json:"reasoning_tokens"`
						} `json:"output_tokens_details"`
					} `json:"usage"`
				} `json:"response"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &d); err == nil {
				pr.FinishReason = d.Response.Status
				pr.PromptTokens = d.Response.Usage.InputTokens
				pr.CompletionTokens = d.Response.Usage.OutputTokens
				pr.TotalTokens = d.Response.Usage.TotalTokens
				pr.CachedTokens = d.Response.Usage.InputTokensDetails.CachedTokens
				pr.ReasoningTokens = d.Response.Usage.OutputTokensDetails.ReasoningTokens
			}

		case "response.failed", "error":
			var d struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &d); err == nil {
				pr.IsError = true
				pr.ErrorType = d.Error.Code
				pr.ErrorMessage = d.Error.Message
			}
		}
	}

	pr.TextPreview = truncate(textBuilder.String(), previewBytes)
	return pr
}
