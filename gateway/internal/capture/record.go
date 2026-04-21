// Package capture 定义统一的采集数据结构,以及异步采集管道.
//
// Record 与 ClickHouse llm_call_logs 表的列一一对应,
// 通过 JSONEachRow 直接 INSERT.
package capture

import (
	"net/http"
	"time"
)

// Provider 字符串常量 (与 CH 表 provider 列的取值一致).
const (
	ProviderOpenAIChat      = "openai_chat"
	ProviderOpenAIResponses = "openai_responses"
	ProviderAnthropic       = "anthropic"
	ProviderGemini          = "gemini"
)

// Record 是一次 LLM 请求的完整采集记录.
//
// 字段名 (JSON tag) 必须与 ClickHouse llm_call_logs 表列名一致,
// 这样可以直接以 JSONEachRow 格式批量插入.
type Record struct {
	// ============ 标识 + 时间 ============
	RequestID       string  `json:"request_id"`
	ParentRequestID string  `json:"parent_request_id,omitempty"`
	CreatedAt       string  `json:"created_at"`            // RFC3339 with millis
	CompletedAt     *string `json:"completed_at,omitempty"`

	// ============ 客户端 ============
	ClientIP         string `json:"client_ip"`
	ClientUserAgent  string `json:"client_user_agent"`
	ClientType       string `json:"client_type"`        // claude_code | openai_sdk | anthropic_sdk | cherry_studio | browser | unknown
	ClientAppVersion string `json:"client_app_version"` // 如 "claude-cli/0.2.17"
	APIKeyHash       string `json:"api_key_hash"`
	APIKeyPrefix     string `json:"api_key_prefix"`
	UserID           string `json:"user_id"`
	OrganizationID   string `json:"organization_id"`

	// ============ 协议 ============
	Provider    string `json:"provider"`
	Endpoint    string `json:"endpoint"`
	HTTPMethod  string `json:"http_method"`
	IsStream    bool   `json:"is_stream"`

	// ============ 模型 ============
	Model           string `json:"model"`
	ModelNormalized string `json:"model_normalized"`

	// ============ 请求结构 ============
	RequestMessageCount uint16   `json:"request_message_count"`
	RequestHasImage     bool     `json:"request_has_image"`
	RequestHasAudio     bool     `json:"request_has_audio"`
	RequestHasVideo     bool     `json:"request_has_video"`
	RequestHasTool      bool     `json:"request_has_tool"`
	RequestTemperature  *float32 `json:"request_temperature,omitempty"`
	RequestTopP         *float32 `json:"request_top_p,omitempty"`
	RequestMaxTokens    *uint32  `json:"request_max_tokens,omitempty"`
	RequestSizeBytes    uint32   `json:"request_size_bytes"`

	// ============ 响应结构 ============
	ResponseFinishReason string `json:"response_finish_reason"`
	ResponseSizeBytes    uint32 `json:"response_size_bytes"`
	ResponseChunkCount   uint32 `json:"response_chunk_count"`

	// ============ Token ============
	PromptTokens     uint32 `json:"prompt_tokens"`
	CompletionTokens uint32 `json:"completion_tokens"`
	TotalTokens      uint32 `json:"total_tokens"`
	CachedTokens     uint32 `json:"cached_tokens"`
	ReasoningTokens  uint32 `json:"reasoning_tokens"`

	// ============ 性能 ============
	TTFTMs         uint32 `json:"ttft_ms"`
	DurationMs     uint32 `json:"duration_ms"`
	UpstreamStatus uint16 `json:"upstream_status"`

	// ============ 错误 ============
	IsError      bool   `json:"is_error"`
	ErrorType    string `json:"error_type"`
	ErrorMessage string `json:"error_message"`

	// ============ 内容 (preview + 原文) ============
	RequestTextPreview   string `json:"request_text_preview"`
	ResponseTextPreview  string `json:"response_text_preview"`
	RequestJSON          string `json:"request_json"`
	ResponseJSON         string `json:"response_json"`
	RequestCOSKey        string `json:"request_cos_key"`
	ResponseCOSKey       string `json:"response_cos_key"`
	ResponseStreamChunks string `json:"response_stream_chunks"`

	// ============ 工具调用 + 扩展 ============
	ToolCallsJSON string `json:"tool_calls_json"`
	MetadataJSON  string `json:"metadata_json"`

	// ============ 内部字段 (不入 CH, 用 "-" 标记忽略) ============

	// 原始 body, 由 pipeline 决定是否 offload 到 COS
	RawRequestBody  []byte `json:"-"`
	RawResponseBody []byte `json:"-"`

	// 原始上游响应 header (诊断用, 不入 CH)
	UpstreamHeaders http.Header `json:"-"`

	// 内部时间戳, 用于 pipeline 内部统计
	StartedAt time.Time `json:"-"`
}

// SetCreatedAt 设置创建时间 (UTC, 毫秒精度).
func (r *Record) SetCreatedAt(t time.Time) {
	r.CreatedAt = t.UTC().Format("2006-01-02 15:04:05.000")
	r.StartedAt = t
}

// SetCompletedAt 设置完成时间.
func (r *Record) SetCompletedAt(t time.Time) {
	s := t.UTC().Format("2006-01-02 15:04:05.000")
	r.CompletedAt = &s
}
