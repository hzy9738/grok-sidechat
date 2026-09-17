package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestNativeMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := ClientMessage{Op: "send", RequestID: "r1", Text: "hi", Browser: BrowserContext{Title: "T", URL: "https://u"}}
	if err := WriteNativeMessage(&buf, in); err != nil {
		t.Fatal(err)
	}
	// length prefix sanity
	if buf.Len() < 4 {
		t.Fatal("too short")
	}
	n := binary.LittleEndian.Uint32(buf.Bytes()[:4])
	if int(n) != buf.Len()-4 {
		t.Fatalf("length mismatch %d vs %d", n, buf.Len()-4)
	}
	var out ClientMessage
	if err := ReadNativeMessage(&buf, &out); err != nil {
		t.Fatal(err)
	}
	if out.Op != "send" || out.Text != "hi" || out.Browser.Title != "T" {
		t.Fatalf("roundtrip: %#v", out)
	}
}

func TestJSONLineRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	msg := HostMessage{Op: "event", OK: true, Event: &StreamEvent{Type: EventText, Text: "x"}}
	if err := WriteJSONLine(&buf, msg); err != nil {
		t.Fatal(err)
	}
	var out HostMessage
	if err := ReadJSONLine(&buf, &out); err != nil {
		t.Fatal(err)
	}
	if out.Event == nil || out.Event.Text != "x" {
		b, _ := json.Marshal(out)
		t.Fatalf("got %s", b)
	}
}
