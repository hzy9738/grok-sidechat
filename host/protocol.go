package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Native Messaging framing: 4-byte little-endian length + JSON payload.

// ClientMessage is extension → host.
type ClientMessage struct {
	Op              string         `json:"op"`
	RequestID       string         `json:"requestId,omitempty"`
	SessionID       string         `json:"sessionId,omitempty"`
	Cwd             string         `json:"cwd,omitempty"`
	Text            string         `json:"text,omitempty"`
	Browser         BrowserContext `json:"browser,omitempty"`
	Model           string         `json:"model,omitempty"`
	Mode            string         `json:"mode,omitempty"`
	ReasoningEffort string         `json:"reasoningEffort,omitempty"`
	MaxTurns        int            `json:"maxTurns,omitempty"`
	AlwaysApprove   bool           `json:"alwaysApprove,omitempty"`
	// DryRun skips the HTTP call and still builds diagnostic argv + fixture stream.
	DryRun bool `json:"dryRun,omitempty"`
	// OpenAI-compatible Chat Completions endpoint, e.g. https://api.openai.com/v1
	APIBase string `json:"apiBase,omitempty"`
	// APIKey is sent only on the native-messaging pipe; never echoed in events/argv.
	APIKey string `json:"apiKey,omitempty"`
	// Query / Limit for list_sessions.
	Query string `json:"query,omitempty"`
	Limit int    `json:"limit,omitempty"`
	// Legacy browser-tool fields are accepted for wire compatibility and ignored.
	CallID        string `json:"callId,omitempty"`
	BrowserResult any    `json:"result,omitempty"`
	OK            bool   `json:"ok,omitempty"`
	Error         string `json:"error,omitempty"`
}

// HostMessage is host → extension.
type HostMessage struct {
	Op        string           `json:"op"`
	RequestID string           `json:"requestId,omitempty"`
	Event     *StreamEvent     `json:"event,omitempty"`
	SessionID string           `json:"sessionId,omitempty"`
	Argv      []string         `json:"argv,omitempty"`
	Prompt    string           `json:"prompt,omitempty"`
	OK        bool             `json:"ok,omitempty"`
	Error     string           `json:"error,omitempty"`
	Version   string           `json:"version,omitempty"`
	Info      map[string]any   `json:"info,omitempty"`
	Sessions  []SessionInfo    `json:"sessions,omitempty"`
	Messages  []SessionMessage `json:"messages,omitempty"`
	Session   *SessionInfo     `json:"session,omitempty"`
	// Legacy browser-action fields; current hosts never emit them.
	CallID    string         `json:"callId,omitempty"`
	Action    string         `json:"action,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ReadNativeMessage reads one Chrome native-messaging framed JSON object.
func ReadNativeMessage(r io.Reader, dst any) error {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return err
	}
	n := binary.LittleEndian.Uint32(lenBuf[:])
	if n == 0 || n > 64<<20 {
		return fmt.Errorf("invalid native message length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	return json.Unmarshal(buf, dst)
}

// WriteNativeMessage writes one Chrome native-messaging framed JSON object.
func WriteNativeMessage(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// ReadJSONLine reads one newline-delimited JSON object (loopback / CLI mode).
func ReadJSONLine(r io.Reader, dst any) error {
	dec := json.NewDecoder(r)
	return dec.Decode(dst)
}

// WriteJSONLine writes one JSON object followed by newline.
func WriteJSONLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
