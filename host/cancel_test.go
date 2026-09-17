package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func hangingCompletionsServer(started chan struct{}) *httptest.Server {
	var once sync.Once
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		once.Do(func() { close(started) })
		<-r.Context().Done()
	}))
}

func TestRunner_CancelAbortsHTTPStream(t *testing.T) {
	started := make(chan struct{})
	srv := hangingCompletionsServer(started)
	defer srv.Close()

	r := &Runner{StoreDir: t.TempDir()}
	done := make(chan TurnResult, 1)
	go func() {
		done <- r.RunTurn(context.Background(), TurnRequest{
			UserText: "cancel-me",
			APIBase:  srv.URL + "/v1",
			APIKey:   "test",
			Model:    "demo",
		}, func(StreamEvent) {})
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP stream never started")
	}

	if err := r.Cancel(); err != nil {
		t.Logf("Cancel returned: %v", err)
	}

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("RunTurn did not return within 4s after Cancel")
	}
}

func TestServeLoop_CancelWhileSendInFlight(t *testing.T) {
	started := make(chan struct{})
	srv := hangingCompletionsServer(started)
	defer srv.Close()

	r := &Runner{StoreDir: t.TempDir()}
	pr, pw := io.Pipe()
	var outMu sync.Mutex
	var outBuf bytes.Buffer
	out := &lockedWriter{mu: &outMu, w: &outBuf}

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- serveLoop(pr, out, r, "", true)
	}()

	time.Sleep(50 * time.Millisecond)

	if err := WriteJSONLine(pw, ClientMessage{
		Op:        "send",
		RequestID: "s1",
		Text:      "long turn",
		APIBase:   srv.URL + "/v1",
		APIKey:    "test",
		Model:     "demo",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatalf("expected live HTTP stream; out=%s", outSnapshot(&outMu, &outBuf))
	}

	if err := WriteJSONLine(pw, ClientMessage{Op: "cancel", RequestID: "c1"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var sawCancelled, sawSendDone bool
	for time.Now().Before(deadline) {
		msgs := parseJSONLines(outSnapshot(&outMu, &outBuf))
		for _, m := range msgs {
			if m.Op == "cancelled" && m.RequestID == "c1" {
				sawCancelled = true
			}
			if m.Op == "send_done" && m.RequestID == "s1" {
				sawSendDone = true
			}
		}
		if sawCancelled && sawSendDone {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawCancelled {
		t.Fatalf("did not observe cancelled response; out=%s", outSnapshot(&outMu, &outBuf))
	}
	if !sawSendDone {
		t.Fatalf("did not observe send_done after cancel; out=%s", outSnapshot(&outMu, &outBuf))
	}

	_ = pw.Close()
	select {
	case <-loopDone:
	case <-time.After(2 * time.Second):
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func outSnapshot(mu *sync.Mutex, buf *bytes.Buffer) string {
	mu.Lock()
	defer mu.Unlock()
	return buf.String()
}

func parseJSONLines(s string) []HostMessage {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	var out []HostMessage
	for {
		var m HostMessage
		if err := dec.Decode(&m); err != nil {
			break
		}
		out = append(out, m)
	}
	return out
}
