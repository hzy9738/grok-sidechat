package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestListGrokSessions_FromSummaryFiles(t *testing.T) {
	home := t.TempDir()
	// ~/.grok/sessions/<cwd-enc>/<id>/summary.json
	dir := filepath.Join(home, "sessions", "%2Fproj", "019faaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := map[string]any{
		"info":              map[string]any{"id": "019faaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "cwd": "/proj"},
		"session_summary":   "修复登录页",
		"created_at":        "2026-08-01T10:00:00Z",
		"updated_at":        "2026-08-06T12:00:00Z",
		"num_chat_messages": 12,
		"current_model_id":  "grok-4.6",
	}
	b, _ := json.Marshal(sum)
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	// older empty title
	dir2 := filepath.Join(home, "sessions", "%2Fproj", "019fbbbb-bbbb-cccc-dddd-eeeeeeeeeeee")
	_ = os.MkdirAll(dir2, 0o755)
	sum2 := map[string]any{
		"info":         map[string]any{"id": "019fbbbb-bbbb-cccc-dddd-eeeeeeeeeeee", "cwd": "/proj"},
		"created_at":   "2026-07-01T10:00:00Z",
		"updated_at":   "2026-07-02T12:00:00Z",
		"num_messages": 2,
	}
	b2, _ := json.Marshal(sum2)
	_ = os.WriteFile(filepath.Join(dir2, "summary.json"), b2, 0o644)

	list, err := ListGrokSessions(home, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 got %d %#v", len(list), list)
	}
	// newest first
	if list[0].ID != "019faaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("order wrong: %#v", list)
	}
	if list[0].Title != "修复登录页" {
		t.Fatalf("title: %q", list[0].Title)
	}

	// query filter
	q, err := ListGrokSessions(home, "登录", 10)
	if err != nil || len(q) != 1 {
		t.Fatalf("query: %v %#v", err, q)
	}
}

func TestLoadSessionMessages_ChatHistory(t *testing.T) {
	home := t.TempDir()
	id := "019fcccc-bbbb-cccc-dddd-eeeeeeeeeeee"
	dir := filepath.Join(home, "sessions", "%2Fwork", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := map[string]any{
		"info":            map[string]any{"id": id, "cwd": "/work"},
		"session_summary": "demo",
		"updated_at":      "2026-08-06T12:00:00Z",
	}
	b, _ := json.Marshal(sum)
	_ = os.WriteFile(filepath.Join(dir, "summary.json"), b, 0o644)

	hist := "" +
		`{"type":"system","content":"sys"}` + "\n" +
		`{"type":"user","content":[{"type":"text","text":"<user_query>\n你好世界\n</user_query>"}]}` + "\n" +
		`{"type":"assistant","content":"你好！"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "chat_history.jsonl"), []byte(hist), 0o644); err != nil {
		t.Fatal(err)
	}

	msgs, meta, err := LoadSessionMessages(home, id, 50)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != id || meta.Title != "demo" {
		t.Fatalf("meta %#v", meta)
	}
	if len(msgs) != 2 {
		t.Fatalf("msgs %#v", msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Text != "你好世界" {
		t.Fatalf("user %#v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Text != "你好！" {
		t.Fatalf("assistant %#v", msgs[1])
	}
}
