package main

import (
	"testing"
)

func TestStoredSessionRoundTrip(t *testing.T) {
	root := t.TempDir()
	sess := storedSession{Info: SessionInfo{ID: "sc-1", Title: "旧标题"}}
	appendTurn(&sess, "/tmp/proj", "gpt-4o-mini", "第一问", "第一答", "想了想")
	if err := saveStoredSession(root, sess); err != nil {
		t.Fatal(err)
	}
	got, err := loadStoredSession(root, "sc-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Info.Cwd != "/tmp/proj" || got.Info.Model != "gpt-4o-mini" {
		t.Fatalf("info=%+v", got.Info)
	}
	if len(got.Messages) != 3 || got.Messages[0].Role != "user" || got.Messages[1].Role != "thinking" {
		t.Fatalf("messages=%#v", got.Messages)
	}
	list, err := ListStoredSessions(root, "第一", 10)
	if err != nil || len(list) != 1 || list[0].ID != "sc-1" {
		t.Fatalf("list=%v err=%v", list, err)
	}
}
