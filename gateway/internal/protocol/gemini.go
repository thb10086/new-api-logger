// Package protocol - Google Gemini generateContent 协议解析.
//
// 路径:
//   POST /v1beta/models/{model}:generateContent
//   POST /v1beta/models/{model}:streamGenerateContent
//
// 关键结构:
//   - 请求: contents[{role,parts:[{text/inlineData/fileData/...}]}]
//   - 响应: candidates[0].content.parts[{text,...}], usageMetadata
//   - 流式: alt=sse 时是 SSE; 否则是 JSON Lines (每行一个 partial response).
//     newapi 默认走 SSE.
package protocol

import (
	"encoding/json"
	"strings"

	"github.com/thb10086/new-api-logger/gateway/internal/sse"
)

// Gemini 创建 Gemini 协议 parser.
func Gemini() Parser { return &gemini{} }

type gemini struct{}

func (p *gemini) Provider() string { return "gemini" }

// model 字段在 URL path 中, 不在 body 里;
// 由 server handler 从 path 提取后填入 record.Model.
//
// 因此 ParseRequest 的 Model 字段保持 "", 由调用方覆盖.
func (p *gemini) ParseRequest(body []byte, previewBytes int) ParsedRequest {
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

	// generationConfig 子对象
	if cfg, ok := req["generationConfig"].(map[string]any); ok {
		pr.Temperature = asFloat32Ptr(cfg["temperature"])
		pr.TopP = asFloat32Ptr(cfg["topP"])
		pr.MaxTokens = asUint32Ptr(cfg["maxOutputTokens"])
	}

	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		pr.HasTool = true
	}
	if _, ok := req["toolConfig"]; ok {
		pr.HasTool = true
	}

	var lastUserText strings.Builder
	if contents, ok := req["contents"].([]any); ok {
		pr.MessageCount = uint16(len(contents))
		for _, cc := range contents {
			c, ok := cc.(map[string]any)
			if !ok {
				continue
			}
			role, _ := c["role"].(string)
			parts, _ := c["parts"].([]any)
			for _, pp := range parts {
				part, ok := pp.(map[string]any)
				if !ok {
					continue
				}
				if txt, ok := part["text"].(string); ok && txt != "" {
					if role == "user" || role == "" {
						lastUserText.Reset()
						lastUserText.WriteString(txt)
					}
					continue
				}
				if id, ok := part["inlineData"].(map[string]any); ok {
					if mime, _ := id["mimeType"].(string); mime != "" {
						if strings.HasPrefix(mime, "image/") {
							pr.HasImage = true
						} else if strings.HasPrefix(mime, "audio/") {
							pr.HasAudio = true
						} else if strings.HasPrefix(mime, "video/") {
							pr.HasVideo = true
						}
					}
				}
				if fd, ok := part["fileData"].(map[string]any); ok {
					if mime, _ := fd["mimeType"].(string); mime != "" {
						if strings.HasPrefix(mime, "image/") {
							pr.HasImage = true
						} else if strings.HasPrefix(mime, "audio/") {
							pr.HasAudio = true
						} else if strings.HasPrefix(mime, "video/") {
							pr.HasVideo = true
						}
					}
				}
				if _, ok := part["functionCall"]; ok {
					pr.HasTool = true
				}
				if _, ok := part["functionResponse"]; ok {
					pr.HasTool = true
				}
			}
		}
	}

	// systemInstruction (Gemini 1.5+)
	if si, ok := req["systemInstruction"].(map[string]any); ok {
		_ = si // 标记为有 system 提示词,目前不单独存储
	}

	pr.TextPreview = truncate(lastUserText.String(), previewBytes)
	return pr
}

// ParseResponse 非流式 Gemini generateContent 响应.
//
// 结构:
//   { "candidates":[{content:{parts:[{text:"..."}]},finishReason:"STOP"}],
//     "usageMetadata":{"promptTokenCount":N,"candidatesTokenCount":N,"totalTokenCount":N,
//                      "thoughtsTokenCount":N,"cachedContentTokenCount":N} }
func (p *gemini) ParseResponse(body []byte, previewBytes int) ParsedResponse {
	pr := ParsedResponse{}
	if len(body) == 0 {
		return pr
	}

	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string          `json:"text,omitempty"`
					FunctionCall json.RawMessage `json:"functionCall,omitempty"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata *struct {
			PromptTokenCount        uint32 `json:"promptTokenCount"`
			CandidatesTokenCount    uint32 `json:"candidatesTokenCount"`
			TotalTokenCount         uint32 `json:"totalTokenCount"`
			ThoughtsTokenCount      uint32 `json:"thoughtsTokenCount"`
			CachedContentTokenCount uint32 `json:"cachedContentTokenCount"`
		} `json:"usageMetadata,omitempty"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return pr
	}

	if resp.UsageMetadata != nil {
		pr.PromptTokens = resp.UsageMetadata.PromptTokenCount
		pr.CompletionTokens = resp.UsageMetadata.CandidatesTokenCount
		pr.TotalTokens = resp.UsageMetadata.TotalTokenCount
		pr.CachedTokens = resp.UsageMetadata.CachedContentTokenCount
		pr.ReasoningTokens = resp.UsageMetadata.ThoughtsTokenCount
	}

	if resp.Error != nil {
		pr.IsError = true
		pr.ErrorType = resp.Error.Status
		pr.ErrorMessage = resp.Error.Message
	}

	if len(resp.Candidates) > 0 {
		pr.FinishReason = resp.Candidates[0].FinishReason

		var textBuilder strings.Builder
		var toolCalls []json.RawMessage
		for _, part := range resp.Candidates[0].Content.Parts {
			textBuilder.WriteString(part.Text)
			if len(part.FunctionCall) > 0 {
				toolCalls = append(toolCalls, part.FunctionCall)
			}
		}
		pr.TextPreview = truncate(textBuilder.String(), previewBytes)
		if len(toolCalls) > 0 {
			pr.ToolCallsJSON = jsonStringify(toolCalls)
		}
	}
	return pr
}

// ParseStreamResponse 解析 streamGenerateContent SSE.
//
// 每个 SSE event 的 data 与非流式响应结构一致 (partial),
// 累加 candidates[0].content.parts[*].text, 取最后一个 usageMetadata 和 finishReason.
func (p *gemini) ParseStreamResponse(events []sse.Event, previewBytes int) ParsedResponse {
	pr := ParsedResponse{}
	var textBuilder strings.Builder

	for _, ev := range events {
		if ev.Data == "" {
			continue
		}
		pr.ChunkCount++

		var chunk struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text,omitempty"`
					} `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason,omitempty"`
			} `json:"candidates"`
			UsageMetadata *struct {
				PromptTokenCount        uint32 `json:"promptTokenCount"`
				CandidatesTokenCount    uint32 `json:"candidatesTokenCount"`
				TotalTokenCount         uint32 `json:"totalTokenCount"`
				ThoughtsTokenCount      uint32 `json:"thoughtsTokenCount"`
				CachedContentTokenCount uint32 `json:"cachedContentTokenCount"`
			} `json:"usageMetadata,omitempty"`
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(ev.Data), &chunk); err != nil {
			continue
		}

		if chunk.Error != nil {
			pr.IsError = true
			pr.ErrorType = chunk.Error.Status
			pr.ErrorMessage = chunk.Error.Message
		}

		if len(chunk.Candidates) > 0 {
			for _, part := range chunk.Candidates[0].Content.Parts {
				textBuilder.WriteString(part.Text)
			}
			if chunk.Candidates[0].FinishReason != "" {
				pr.FinishReason = chunk.Candidates[0].FinishReason
			}
		}

		if chunk.UsageMetadata != nil {
			pr.PromptTokens = chunk.UsageMetadata.PromptTokenCount
			pr.CompletionTokens = chunk.UsageMetadata.CandidatesTokenCount
			pr.TotalTokens = chunk.UsageMetadata.TotalTokenCount
			pr.CachedTokens = chunk.UsageMetadata.CachedContentTokenCount
			pr.ReasoningTokens = chunk.UsageMetadata.ThoughtsTokenCount
		}
	}

	pr.TextPreview = truncate(textBuilder.String(), previewBytes)
	return pr
}
