package main

import (
	"strings"
	"testing"
)

func TestParseStreamLine_TextAndSession(t *testing.T) {
	events, err := ParseStreamLine(`{"type":"assistant","content":[{"type":"text","text":"Hello from Grok"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, EventText, "Hello from Grok") {
		t.Fatalf("expected text event, got %#v", events)
	}

	events, err = ParseStreamLine(`{"type":"result","session_id":"sess-abc-123","subtype":"success"}`)
	if err != nil {
		t.Fatal(err)
	}
	foundSession := false
	foundDone := false
	for _, ev := range events {
		if ev.Type == EventSession && ev.SessionID == "sess-abc-123" {
			foundSession = true
		}
		if ev.Type == EventDone {
			foundDone = true
		}
	}
	if !foundSession {
		t.Fatalf("session id not captured: %#v", events)
	}
	if !foundDone {
		t.Fatalf("done not emitted: %#v", events)
	}
}

func TestParseStreamLine_ThinkingAndTool(t *testing.T) {
	events, err := ParseStreamLine(`{"type":"assistant","content":[{"type":"thinking","text":"plan step"},{"type":"tool_use","id":"t1","name":"bash","input":{"cmd":"ls"}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, EventThinking, "plan step") {
		t.Fatalf("thinking missing: %#v", events)
	}
	foundTool := false
	for _, ev := range events {
		if ev.Type == EventToolUse && ev.Name == "bash" && ev.ID == "t1" {
			foundTool = true
		}
	}
	if !foundTool {
		t.Fatalf("tool_use missing: %#v", events)
	}
}

func TestParseStreamLine_PartialDelta(t *testing.T) {
	events, err := ParseStreamLine(`{"type":"content_block_delta","delta":{"text":"hel"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, EventPartial, "hel") && !hasEvent(events, EventText, "hel") {
		t.Fatalf("partial text missing: %#v", events)
	}
}

// Real Grok Build headless frames (--include-partial-messages).
func TestParseStreamLine_GrokStreamEventThinkingDelta(t *testing.T) {
	line := `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"The user"}},"session_id":"sess-1"}`
	events, err := ParseStreamLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, EventThinking, "The user") {
		t.Fatalf("thinking_delta not mapped: %#v", events)
	}
}

func TestParseStreamLine_GrokStreamEventTextDelta(t *testing.T) {
	line := `{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"你好"}}}`
	events, err := ParseStreamLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, EventPartial, "你好") && !hasEvent(events, EventText, "你好") {
		t.Fatalf("text_delta not mapped: %#v", events)
	}
}

func TestParseStreamLine_GrokStreamEventThinkingBlockStart(t *testing.T) {
	// empty start should not produce an event
	line := `{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}}`
	events, err := ParseStreamLine(line)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Type == EventThinking && ev.Text == "" {
			t.Fatalf("empty thinking start should be skipped: %#v", events)
		}
	}
}

func TestParseStreamLine_GrokStreamEventThinkingSequence(t *testing.T) {
	// Stateful TurnStream (same as process.consumeStdout) — required for multi-line turns.
	frames := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"先"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"看"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"答"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"先看"},{"type":"text","text":"答"}]}}`,
	}
	ts := NewTurnStream()
	var thinking strings.Builder
	var text strings.Builder
	for _, f := range frames {
		evs, err := ts.FeedLine(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range evs {
			switch ev.Type {
			case EventThinking:
				thinking.WriteString(ev.Text)
			case EventPartial, EventText:
				text.WriteString(ev.Text)
			}
		}
	}
	if thinking.String() != "先看" {
		t.Fatalf("thinking concat got %q", thinking.String())
	}
	if text.String() != "答" {
		t.Fatalf("text concat got %q", text.String())
	}
}

func TestTurnStream_AssistantFallbackDoesNotDoubleEmit(t *testing.T) {
	ts := NewTurnStream()
	frames := []string{
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"abc"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"xy"}}}`,
		// complete equals streamed — no extra suffix
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"abc"},{"type":"text","text":"xy"}]}}`,
	}
	var thinkN, textN int
	var think, text strings.Builder
	for _, f := range frames {
		evs, _ := ts.FeedLine(f)
		for _, ev := range evs {
			if ev.Type == EventThinking {
				thinkN++
				think.WriteString(ev.Text)
			}
			if ev.Type == EventPartial || ev.Type == EventText {
				textN++
				text.WriteString(ev.Text)
			}
		}
	}
	if think.String() != "abc" || text.String() != "xy" {
		t.Fatalf("got think=%q text=%q counts %d/%d", think.String(), text.String(), thinkN, textN)
	}
}

func TestParseStreamLine_EmptyAndInvalid(t *testing.T) {
	events, err := ParseStreamLine("   ")
	if err != nil || events != nil {
		t.Fatalf("empty should be nil,nil got %#v %v", events, err)
	}
	_, err = ParseStreamLine(`{not-json`)
	if err == nil {
		t.Fatal("expected json error")
	}
}

func TestParseStreamLine_ErrorFrame(t *testing.T) {
	events, err := ParseStreamLine(`{"type":"error","message":"boom"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, EventError, "boom") {
		t.Fatalf("error missing: %#v", events)
	}
}

func hasEvent(events []StreamEvent, typ EventType, text string) bool {
	for _, ev := range events {
		if ev.Type == typ && (text == "" || ev.Text == text) {
			return true
		}
	}
	return false
}
