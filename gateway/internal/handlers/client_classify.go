// Package handlers - 客户端类型识别.
//
// 根据 User-Agent 把请求来源归类到有限的 client_type 枚举值,
// 用于 admin-ui 一键过滤 (如筛选所有 Claude Code 用户).
//
// 识别规则按顺序匹配, 先命中先返回.
package handlers

import (
	"strings"
	"unicode/utf8"
)

// ClientType 枚举值, 与 ClickHouse client_type 列值保持一致.
const (
	ClientTypeClaudeCode   = "claude_code"    // Anthropic 官方 Claude Code CLI
	ClientTypeAnthropicSDK = "anthropic_sdk"  // anthropic-sdk-python / anthropic-sdk-typescript
	ClientTypeOpenAISDK    = "openai_sdk"     // openai-python / openai-node
	ClientTypeLangChain    = "langchain"      // langchain / langchain-community
	ClientTypeCherryStudio = "cherry_studio"  // Cherry Studio 桌面客户端
	ClientTypeChatbox      = "chatbox"        // Chatbox 桌面客户端
	ClientTypeLobeChat     = "lobechat"       // LobeChat Web
	ClientTypeCursor       = "cursor"         // Cursor IDE
	ClientTypeContinue     = "continue"       // Continue.dev VSCode
	ClientTypeBrowser      = "browser"        // 浏览器直接请求
	ClientTypeCurl         = "curl"           // curl / wget 调试
	ClientTypeUnknown      = "unknown"        // 无法识别
)

// ClassifyClient 按 User-Agent 识别客户端类型和应用版本.
//
// 返回:
//   - clientType: 归一化枚举值 (见上方常量)
//   - appVersion: 原始 UA 前 128 字符 (用于详情页展示)
//
// UA 取小写后匹配, 保证大小写无关.
func ClassifyClient(ua string) (clientType, appVersion string) {
	if ua == "" {
		return ClientTypeUnknown, ""
	}

	// 截取前 128 字符作为 app_version (LowCardinality 友好, 压缩好)
	appVersion = truncateUA(ua, 128)
	low := strings.ToLower(ua)

	switch {
	// ============ Claude Code 优先 (和 anthropic-sdk 区分开) ============
	case strings.Contains(low, "claude-cli/"),
		strings.Contains(low, "claude-code/"),
		strings.Contains(low, "anthropic-cli"):
		return ClientTypeClaudeCode, appVersion

	// ============ 各厂商 SDK ============
	case strings.Contains(low, "anthropic-sdk"),
		strings.Contains(low, "anthropic-python"),
		strings.Contains(low, "anthropic-typescript"),
		strings.Contains(low, "anthropic-ai/sdk"):
		return ClientTypeAnthropicSDK, appVersion

	case strings.Contains(low, "openai/python"),
		strings.Contains(low, "openai-python"),
		strings.Contains(low, "openai/node"),
		strings.Contains(low, "openai-node"),
		strings.HasPrefix(low, "openai/"):
		return ClientTypeOpenAISDK, appVersion

	case strings.Contains(low, "langchain"):
		return ClientTypeLangChain, appVersion

	// ============ 桌面/Web 客户端 ============
	case strings.Contains(low, "cherrystudio"),
		strings.Contains(low, "cherry-studio"),
		strings.Contains(low, "cherry studio"):
		return ClientTypeCherryStudio, appVersion

	case strings.Contains(low, "chatbox"):
		return ClientTypeChatbox, appVersion

	case strings.Contains(low, "lobechat"), strings.Contains(low, "lobe-chat"):
		return ClientTypeLobeChat, appVersion

	case strings.Contains(low, "cursor/"), strings.Contains(low, "cursor-ai"):
		return ClientTypeCursor, appVersion

	case strings.Contains(low, "continue/"), strings.Contains(low, "continue.dev"):
		return ClientTypeContinue, appVersion

	// ============ 调试工具 ============
	case strings.HasPrefix(low, "curl/"), strings.HasPrefix(low, "wget/"),
		strings.HasPrefix(low, "httpie/"), strings.HasPrefix(low, "postmanruntime/"):
		return ClientTypeCurl, appVersion

	// ============ 浏览器 (放最后, 避免误匹配) ============
	case strings.Contains(low, "mozilla/"),
		strings.Contains(low, "chrome/"),
		strings.Contains(low, "safari/"),
		strings.Contains(low, "firefox/"),
		strings.Contains(low, "edge/"):
		return ClientTypeBrowser, appVersion

	default:
		return ClientTypeUnknown, appVersion
	}
}

// truncateUA 按字节长度上限截断, 但保证不切断多字节 UTF-8 字符.
// 常见客户端 UA 是 ASCII, 但部分带中文 / emoji 的浏览器 UA 需要安全处理,
// 避免向 ClickHouse 插入无效 UTF-8 导致 LowCardinality 列序列化告警.
func truncateUA(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// 从 n 位置向前找到最近的 rune 起始位置
	for i := n; i > 0; i-- {
		if utf8.RuneStart(s[i]) {
			return s[:i]
		}
	}
	return s[:n]
}
