// Package sse 提供轻量的 SSE (Server-Sent Events) 解析.
//
// 用于把上游 SSE 流的 raw bytes 切分成独立的 event,
// 供协议层 (openai/anthropic/gemini) 进一步重组完整响应.
//
// 不依赖第三方库, 仅用 bufio.
package sse

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

// Event 表示一个 SSE 事件.
//
// 标准 SSE 格式:
//   event: <name>      (可选)
//   data: <line1>
//   data: <line2>      (多行 data 自动 \n 拼接)
//   id: <id>           (可选)
//   retry: <ms>        (可选)
//   <空行>             (event 结束标志)
type Event struct {
	Event string
	Data  string
	ID    string
	Retry string
}

// IsDone 判断是否为流结束信号 ("data: [DONE]").
//
// OpenAI / Deepseek 等使用 [DONE]; Anthropic 使用 message_stop event;
// Gemini 在响应结束时直接关闭 SSE.
func (e *Event) IsDone() bool {
	return e.Data == "[DONE]"
}

// Parse 把 SSE raw bytes 解析为 Event 列表.
//
// 适用于已完整接收的 buffer (如 capture 的 LimitedBuffer.Bytes()).
// 流式逐 event 解析请使用 Scanner.
func Parse(data []byte) []Event {
	scanner := NewScanner(bytes.NewReader(data))
	var events []Event
	for {
		ev, err := scanner.Next()
		if err != nil {
			break
		}
		events = append(events, ev)
	}
	return events
}

// Scanner 是流式 SSE 解析器.
//
// 用法:
//   sc := sse.NewScanner(reader)
//   for {
//       ev, err := sc.Next()
//       if err == io.EOF { break }
//       if err != nil    { return err }
//       // 处理 ev
//   }
type Scanner struct {
	br  *bufio.Reader
	buf []string // 当前 event 累积的 data 行
	ev  Event
}

// NewScanner 创建 SSE 流式扫描器.
//
// 内部 buffer 默认 1 MB, 适合大多数 LLM chunk;
// 多模态大 base64 chunk 可能超出, 此场景请预先 io.ReadAll 后用 Parse.
func NewScanner(r io.Reader) *Scanner {
	br := bufio.NewReaderSize(r, 1024*1024)
	return &Scanner{br: br}
}

// Next 返回下一个 SSE 事件.
//
// 返回 io.EOF 表示流结束.
// 解析过程会跳过注释行 (以 ":" 开头).
func (s *Scanner) Next() (Event, error) {
	s.ev = Event{}
	s.buf = s.buf[:0]

	for {
		line, err := s.br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if len(s.buf) > 0 || s.ev.Event != "" || s.ev.ID != "" {
					s.flushData()
					return s.ev, nil
				}
				return Event{}, io.EOF
			}
			return Event{}, err
		}

		line = strings.TrimRight(line, "\r\n")

		// 空行 = event 边界
		if line == "" {
			if len(s.buf) > 0 || s.ev.Event != "" || s.ev.ID != "" {
				s.flushData()
				return s.ev, nil
			}
			continue // 多个空行
		}

		// 注释
		if strings.HasPrefix(line, ":") {
			continue
		}

		// field:value 格式
		idx := strings.IndexByte(line, ':')
		var field, value string
		if idx < 0 {
			field = line
			value = ""
		} else {
			field = line[:idx]
			value = line[idx+1:]
			// 字段值开头一个空格按 spec 应去除
			if len(value) > 0 && value[0] == ' ' {
				value = value[1:]
			}
		}

		switch field {
		case "data":
			s.buf = append(s.buf, value)
		case "event":
			s.ev.Event = value
		case "id":
			s.ev.ID = value
		case "retry":
			s.ev.Retry = value
		default:
			// 未知字段, 按 spec 忽略
		}
	}
}

func (s *Scanner) flushData() {
	if len(s.buf) > 0 {
		s.ev.Data = strings.Join(s.buf, "\n")
	}
}
