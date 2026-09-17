package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EventType is a typed stream event sent to the Side Chat.
type EventType string

const (
	EventThinking   EventType = "thinking"
	EventText       EventType = "text"
	EventToolUse    EventType = "tool_use"
	EventToolResult EventType = "tool_result"
	EventSession    EventType = "session"
	EventUsage      EventType = "usage"
	EventError      EventType = "error"
	EventDone       EventType = "done"
	EventPartial    EventType = "partial"
)

// StreamEvent is one typed event derived from a headless NDJSON line.
type StreamEvent struct {
	Type      EventType      `json:"type"`
	Text      string         `json:"text,omitempty"`
	SessionID string         `json:"sessionId,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     string         `json:"input,omitempty"`
	ID        string         `json:"id,omitempty"`
	Usage     map[string]any `json:"usage,omitempty"`
	RawType   string         `json:"rawType,omitempty"`
}

// TurnStream is a stateful parser for one headless turn.
// Modeled after cc-connect agent/grok session streamState (PR #1635):
// stream_event envelopes → thinking_delta / text_delta accumulation + flush,
// assistant fallback with missingSuffix to avoid double-emitting.
type TurnStream struct {
	blocks          map[int]*streamBlock
	streamThinking  strings.Builder // full thinking seen via partials
	streamText      strings.Builder // full text seen via partials
	pendingThinking strings.Builder // not-yet-emitted thinking chunk
	pendingText     strings.Builder
	emittedTool     map[string]bool
	sessionID       string
}

type streamBlock struct {
	kind      string
	id        string
	name      string
	input     strings.Builder
	inputSeed string
}

// NewTurnStream constructs a parser for one grok headless process.
func NewTurnStream() *TurnStream {
	return &TurnStream{
		blocks:      make(map[int]*streamBlock),
		emittedTool: make(map[string]bool),
	}
}

// FeedLine parses one NDJSON line and returns zero or more typed events.
func (s *TurnStream) FeedLine(line string) ([]StreamEvent, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, fmt.Errorf("stream json: %w", err)
	}
	return s.handleRaw(raw), nil
}

// Flush emits any buffered thinking/text (e.g. process exit without message_stop).
func (s *TurnStream) Flush() []StreamEvent {
	var out []StreamEvent
	out = append(out, s.flushThinking()...)
	out = append(out, s.flushText()...)
	return out
}

func (s *TurnStream) handleRaw(raw map[string]any) []StreamEvent {
	var out []StreamEvent
	if sid := firstString(raw, "session_id", "sessionId"); sid != "" {
		if s.sessionID == "" {
			s.sessionID = sid
			out = append(out, StreamEvent{Type: EventSession, SessionID: sid, RawType: stringValue(raw["type"])})
		} else if sid != s.sessionID {
			s.sessionID = sid
			out = append(out, StreamEvent{Type: EventSession, SessionID: sid, RawType: stringValue(raw["type"])})
		}
	}

	rawType := strings.ToLower(stringValue(raw["type"]))
	switch rawType {
	case "system":
		// init carries session_id (handled above)
		return out
	case "stream_event":
		event, _ := raw["event"].(map[string]any)
		out = append(out, s.handlePartial(event)...)
		return out
	case "assistant":
		out = append(out, s.handleAssistantFallback(raw)...)
		return out
	case "user":
		out = append(out, s.handleUserToolResults(raw)...)
		return out
	case "result":
		out = append(out, s.flushThinking()...)
		out = append(out, s.flushText()...)
		if usage, ok := raw["usage"].(map[string]any); ok {
			out = append(out, StreamEvent{Type: EventUsage, Usage: usage, RawType: rawType})
		}
		if t := stringValue(raw["result"]); t != "" {
			out = append(out, StreamEvent{Type: EventText, Text: t, RawType: rawType})
		}
		out = append(out, StreamEvent{Type: EventDone, RawType: rawType, SessionID: s.sessionID})
		return out
	case "error":
		out = append(out, s.flushThinking()...)
		out = append(out, s.flushText()...)
		msg := firstNonEmpty(stringValue(raw["message"]), stringValue(raw["error"]), extractText(raw))
		out = append(out, StreamEvent{Type: EventError, Text: msg, RawType: rawType})
		return out
	default:
		// Stateless fallback for older fixtures / unexpected shapes
		if events, err := parseStreamLineLegacy(raw, rawType); err == nil {
			out = append(out, events...)
		}
		return out
	}
}

func (s *TurnStream) handlePartial(event map[string]any) []StreamEvent {
	if event == nil {
		return nil
	}
	var out []StreamEvent
	switch strings.ToLower(stringValue(event["type"])) {
	case "message_start":
		s.blocks = make(map[int]*streamBlock)
		// keep streamThinking/streamText for assistant missingSuffix across message? reset per message
		// PR resets stream buffers on message_start
		s.streamThinking.Reset()
		s.streamText.Reset()
	case "content_block_start":
		index := intFromAny(event["index"])
		content, _ := event["content_block"].(map[string]any)
		kind := strings.ToLower(stringValue(content["type"]))
		block := &streamBlock{
			kind: kind,
			id:   stringValue(content["id"]),
			name: stringValue(content["name"]),
		}
		switch kind {
		case "thinking":
			// Grok uses "thinking" field, not "text"
			if text := firstNonEmpty(stringValue(content["thinking"]), stringValue(content["text"])); text != "" {
				s.pendingThinking.WriteString(text)
				s.streamThinking.WriteString(text)
				// live: emit delta immediately for side panel
				out = append(out, StreamEvent{Type: EventThinking, Text: text, RawType: "content_block_start"})
			}
		case "text":
			if text := stringValue(content["text"]); text != "" {
				s.pendingText.WriteString(text)
				s.streamText.WriteString(text)
				out = append(out, StreamEvent{Type: EventPartial, Text: text, RawType: "content_block_start"})
			}
		case "tool_use", "server_tool_use":
			if input, ok := content["input"].(map[string]any); ok && len(input) > 0 {
				encoded, _ := json.Marshal(input)
				block.inputSeed = string(encoded)
			}
		}
		s.blocks[index] = block
	case "content_block_delta":
		index := intFromAny(event["index"])
		delta, _ := event["delta"].(map[string]any)
		if delta == nil {
			return out
		}
		switch strings.ToLower(stringValue(delta["type"])) {
		case "thinking_delta", "thinking":
			// PR #1635: delta["thinking"] is authoritative
			text := firstNonEmpty(stringValue(delta["thinking"]), stringValue(delta["text"]))
			if text == "" {
				return out
			}
			s.pendingThinking.WriteString(text)
			s.streamThinking.WriteString(text)
			// Live stream each delta (side panel needs progressive thinking)
			out = append(out, StreamEvent{Type: EventThinking, Text: text, RawType: "thinking_delta"})
		case "text_delta", "text":
			text := stringValue(delta["text"])
			if text == "" {
				return out
			}
			s.pendingText.WriteString(text)
			s.streamText.WriteString(text)
			out = append(out, StreamEvent{Type: EventPartial, Text: text, RawType: "text_delta"})
		case "input_json_delta":
			block := s.blocks[index]
			if block != nil {
				block.input.WriteString(stringValue(delta["partial_json"]))
			}
		}
	case "content_block_stop":
		index := intFromAny(event["index"])
		block := s.blocks[index]
		if block == nil {
			return out
		}
		switch block.kind {
		case "thinking":
			// PR flushes whole block here; we already streamed deltas — just clear pending
			s.pendingThinking.Reset()
		case "text":
			s.pendingText.Reset()
		case "tool_use", "server_tool_use":
			out = append(out, s.flushThinking()...)
			out = append(out, s.flushText()...)
			input := block.input.String()
			if input == "" {
				input = block.inputSeed
			}
			out = append(out, s.emitToolUse(block.id, block.name, input)...)
		}
		delete(s.blocks, index)
	case "message_stop":
		// Deltas already emitted live; clear buffers
		s.pendingThinking.Reset()
		s.pendingText.Reset()
	}
	return out
}

func (s *TurnStream) handleAssistantFallback(raw map[string]any) []StreamEvent {
	var out []StreamEvent
	// Prefer message.content (Anthropic wire); also accept top-level content
	message, _ := raw["message"].(map[string]any)
	var contents []any
	if message != nil {
		contents, _ = message["content"].([]any)
	}
	if contents == nil {
		contents, _ = raw["content"].([]any)
	}
	for _, value := range contents {
		block, _ := value.(map[string]any)
		if block == nil {
			continue
		}
		switch strings.ToLower(stringValue(block["type"])) {
		case "thinking", "thought", "reasoning":
			// PR: complete := content["thinking"] — not text
			complete := firstNonEmpty(stringValue(block["thinking"]), stringValue(block["text"]))
			if suffix := missingSuffix(complete, s.streamThinking.String()); suffix != "" {
				s.pendingThinking.WriteString(suffix)
				s.streamThinking.WriteString(suffix)
				out = append(out, StreamEvent{Type: EventThinking, Text: suffix, RawType: "assistant.thinking"})
			}
			s.pendingThinking.Reset()
		case "text":
			complete := stringValue(block["text"])
			if suffix := missingSuffix(complete, s.streamText.String()); suffix != "" {
				s.pendingText.WriteString(suffix)
				s.streamText.WriteString(suffix)
				out = append(out, StreamEvent{Type: EventText, Text: suffix, RawType: "assistant.text"})
			}
			s.pendingText.Reset()
		case "tool_use", "server_tool_use":
			id := stringValue(block["id"])
			input, _ := json.Marshal(block["input"])
			out = append(out, s.emitToolUse(id, stringValue(block["name"]), string(input))...)
		}
	}
	// Top-level string content
	if len(contents) == 0 {
		if t := stringValue(raw["text"]); t != "" {
			if suffix := missingSuffix(t, s.streamText.String()); suffix != "" {
				out = append(out, StreamEvent{Type: EventText, Text: suffix, RawType: "assistant"})
				s.streamText.WriteString(suffix)
			}
		}
	}
	return out
}

func (s *TurnStream) handleUserToolResults(raw map[string]any) []StreamEvent {
	var out []StreamEvent
	message, _ := raw["message"].(map[string]any)
	var contents []any
	if message != nil {
		contents, _ = message["content"].([]any)
	}
	if contents == nil {
		contents, _ = raw["content"].([]any)
	}
	for _, value := range contents {
		block, _ := value.(map[string]any)
		if block == nil {
			continue
		}
		if strings.ToLower(stringValue(block["type"])) != "tool_result" {
			continue
		}
		id := stringValue(block["tool_use_id"])
		text := extractText(block)
		if text == "" {
			switch c := block["content"].(type) {
			case string:
				text = c
			default:
				b, _ := json.Marshal(c)
				text = string(b)
			}
		}
		out = append(out, StreamEvent{Type: EventToolResult, ID: id, Text: text, RawType: "tool_result"})
	}
	return out
}

func (s *TurnStream) emitToolUse(id, name, input string) []StreamEvent {
	if id != "" && s.emittedTool[id] {
		return nil
	}
	if id != "" {
		s.emittedTool[id] = true
	}
	if name == "" && input == "" {
		return nil
	}
	return []StreamEvent{{Type: EventToolUse, Name: name, ID: id, Input: input, RawType: "tool_use"}}
}

func (s *TurnStream) flushThinking() []StreamEvent {
	if s.pendingThinking.Len() == 0 {
		return nil
	}
	text := s.pendingThinking.String()
	s.pendingThinking.Reset()
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []StreamEvent{{Type: EventThinking, Text: text, RawType: "flush.thinking"}}
}

func (s *TurnStream) flushText() []StreamEvent {
	if s.pendingText.Len() == 0 {
		return nil
	}
	text := s.pendingText.String()
	s.pendingText.Reset()
	if text == "" {
		return nil
	}
	return []StreamEvent{{Type: EventText, Text: text, RawType: "flush.text"}}
}

// missingSuffix returns complete[len(streamed):] when complete extends streamed (PR #1635).
func missingSuffix(complete, streamed string) string {
	if complete == "" || complete == streamed {
		return ""
	}
	if strings.HasPrefix(complete, streamed) {
		return strings.TrimPrefix(complete, streamed)
	}
	if streamed == "" {
		return complete
	}
	// Diverged partial stream — do not double-emit
	return ""
}

// ParseStreamLine is the legacy single-line API used by tests and dry fixtures.
// Prefer TurnStream for multi-line turns (correct thinking_delta handling).
func ParseStreamLine(line string) ([]StreamEvent, error) {
	ts := NewTurnStream()
	return ts.FeedLine(line)
}

func parseStreamLineLegacy(raw map[string]any, rawType string) ([]StreamEvent, error) {
	var out []StreamEvent
	switch strings.ToLower(rawType) {
	case "assistant", "message", "text", "content_block_delta", "content_block":
		out = append(out, parseContentLike(raw, rawType)...)
	case "thinking", "thought", "reasoning":
		if t := extractText(raw); t != "" {
			out = append(out, StreamEvent{Type: EventThinking, Text: t, RawType: rawType})
		}
	case "tool_use", "tool-use", "tool_call":
		out = append(out, parseToolUse(raw, rawType)...)
	case "tool_result", "tool-result":
		out = append(out, parseToolResult(raw, rawType)...)
	case "stream_event":
		// Should not reach here if handleRaw works; unwrap anyway
		if ev, ok := raw["event"].(map[string]any); ok {
			ts := NewTurnStream()
			out = append(out, ts.handlePartial(ev)...)
		}
	case "partial", "delta":
		out = append(out, parsePartial(raw, rawType)...)
	default:
		if events := parseContentLike(raw, rawType); len(events) > 0 {
			out = append(out, events...)
		}
	}
	return out, nil
}

func parseContentLike(raw map[string]any, rawType string) []StreamEvent {
	var out []StreamEvent
	if t := stringValue(raw["text"]); t != "" {
		out = append(out, StreamEvent{Type: EventText, Text: t, RawType: rawType})
	}
	if t := stringValue(raw["thinking"]); t != "" {
		out = append(out, StreamEvent{Type: EventThinking, Text: t, RawType: rawType})
	}
	switch content := raw["content"].(type) {
	case string:
		if strings.TrimSpace(content) != "" {
			out = append(out, StreamEvent{Type: EventText, Text: content, RawType: rawType})
		}
	case []any:
		for _, item := range content {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			bt := strings.ToLower(stringValue(block["type"]))
			switch bt {
			case "thinking", "thought", "reasoning":
				// Prefer "thinking" field (Grok Build / PR #1635)
				if t := firstNonEmpty(stringValue(block["thinking"]), stringValue(block["text"])); t != "" {
					out = append(out, StreamEvent{Type: EventThinking, Text: t, RawType: bt})
				}
			case "text":
				if t := stringValue(block["text"]); t != "" {
					out = append(out, StreamEvent{Type: EventText, Text: t, RawType: bt})
				}
			case "tool_use", "tool-use":
				out = append(out, parseToolUse(block, bt)...)
			case "tool_result":
				out = append(out, parseToolResult(block, bt)...)
			default:
				if t := stringValue(block["text"]); t != "" {
					out = append(out, StreamEvent{Type: EventText, Text: t, RawType: bt})
				}
			}
		}
	}
	if msg, ok := raw["message"].(map[string]any); ok {
		out = append(out, parseContentLike(msg, rawType)...)
	}
	if delta, ok := raw["delta"].(map[string]any); ok {
		if t := stringValue(delta["thinking"]); t != "" {
			out = append(out, StreamEvent{Type: EventThinking, Text: t, RawType: rawType})
		}
		if t := stringValue(delta["text"]); t != "" {
			out = append(out, StreamEvent{Type: EventPartial, Text: t, RawType: rawType})
		}
	}
	return out
}

func parsePartial(raw map[string]any, rawType string) []StreamEvent {
	var out []StreamEvent
	if t := extractText(raw); t != "" {
		out = append(out, StreamEvent{Type: EventPartial, Text: t, RawType: rawType})
	}
	if delta, ok := raw["delta"].(map[string]any); ok {
		if t := stringValue(delta["text"]); t != "" {
			out = append(out, StreamEvent{Type: EventPartial, Text: t, RawType: rawType})
		}
		if t := stringValue(delta["thinking"]); t != "" {
			out = append(out, StreamEvent{Type: EventThinking, Text: t, RawType: rawType})
		}
	}
	return out
}

func parseToolUse(raw map[string]any, rawType string) []StreamEvent {
	name := firstNonEmpty(stringValue(raw["name"]), stringValue(raw["tool"]))
	id := firstNonEmpty(stringValue(raw["id"]), stringValue(raw["tool_use_id"]))
	input := ""
	switch v := raw["input"].(type) {
	case string:
		input = v
	case map[string]any, []any:
		b, _ := json.Marshal(v)
		input = string(b)
	}
	if name == "" && input == "" {
		return nil
	}
	return []StreamEvent{{Type: EventToolUse, Name: name, ID: id, Input: input, RawType: rawType}}
}

func parseToolResult(raw map[string]any, rawType string) []StreamEvent {
	id := firstNonEmpty(stringValue(raw["tool_use_id"]), stringValue(raw["id"]))
	text := extractText(raw)
	if text == "" {
		switch c := raw["content"].(type) {
		case string:
			text = c
		default:
			b, _ := json.Marshal(c)
			text = string(b)
		}
	}
	return []StreamEvent{{Type: EventToolResult, ID: id, Text: text, RawType: rawType}}
}

func extractText(raw map[string]any) string {
	for _, key := range []string{"text", "thinking", "content", "message", "result", "error"} {
		if s := stringValue(raw[key]); s != "" {
			if key == "content" {
				if _, ok := raw[key].([]any); ok {
					continue
				}
				if _, ok := raw[key].(map[string]any); ok {
					continue
				}
			}
			if key == "message" {
				if _, ok := raw[key].(map[string]any); ok {
					continue
				}
			}
			return s
		}
	}
	return ""
}

func stringValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case float64:
		return fmt.Sprintf("%g", t)
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func firstString(raw map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := stringValue(raw[k]); s != "" {
			return s
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func intFromAny(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	case string:
		var n int
		fmt.Sscanf(t, "%d", &n)
		return n
	default:
		return 0
	}
}
