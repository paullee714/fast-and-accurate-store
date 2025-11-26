package server_test

import (
	"bufio"
	"fmt"
	"testing"
	"time"

	"fas/pkg/protocol"
	"fas/pkg/server"
)

func TestServer_CRUD(t *testing.T) {
	port := pickPort(t)
	cfg := server.Config{Host: "127.0.0.1", Port: port}
	srv := server.NewServer(cfg)
	go func() { _ = srv.Start() }()

	conn := dialWithRetry(t, "127.0.0.1", port, false)
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := protocol.NewWriter(conn)

	if err := w.WriteCommand([]string{"PING"}); err != nil {
		t.Fatalf("ping write: %v", err)
	}
	if got := readBulk(t, r); got != "PONG" {
		t.Fatalf("want PONG got %q", got)
	}

	if err := w.WriteCommand([]string{"SET", "k1", "v1"}); err != nil {
		t.Fatalf("set write: %v", err)
	}
	if got := readLine(t, r); got != "+OK\r\n" {
		t.Fatalf("want OK got %q", got)
	}

	if err := w.WriteCommand([]string{"GET", "k1"}); err != nil {
		t.Fatalf("get write: %v", err)
	}
	if val := readBulk(t, r); val != "v1" {
		t.Fatalf("want v1 got %q", val)
	}
}

func TestServer_PubSub(t *testing.T) {
	port := pickPort(t)
	cfg := server.Config{Host: "127.0.0.1", Port: port}
	srv := server.NewServer(cfg)
	go func() { _ = srv.Start() }()

	sub := dialWithRetry(t, "127.0.0.1", port, false)
	defer sub.Close()
	sr := bufio.NewReader(sub)
	sw := protocol.NewWriter(sub)
	if err := sw.WriteCommand([]string{"SUBSCRIBE", "news"}); err != nil {
		t.Fatalf("subscribe write: %v", err)
	}
	// subscribe confirmation
	_ = readArrayLine(t, sr) // "*3"
	readBulk(t, sr)          // "subscribe"
	readBulk(t, sr)          // "news"
	readBulk(t, sr)          // "1"

	pub := dialWithRetry(t, "127.0.0.1", port, false)
	defer pub.Close()
	pw := protocol.NewWriter(pub)
	if err := pw.WriteCommand([]string{"PUBLISH", "news", "hello"}); err != nil {
		t.Fatalf("publish write: %v", err)
	}
	// skip publish reply
	bufio.NewReader(pub).ReadString('\n')

	// expect message on subscriber
	if line := readArrayLine(t, sr); line == "" {
		t.Fatal("expected message array")
	}
	if msgType := readBulk(t, sr); msgType != "message" {
		t.Fatalf("expected message got %q", msgType)
	}
	if ch := readBulk(t, sr); ch != "news" {
		t.Fatalf("expected channel news got %q", ch)
	}
	if payload := readBulk(t, sr); payload != "hello" {
		t.Fatalf("expected payload hello got %q", payload)
	}
}

func TestServer_TTL_Expiry(t *testing.T) {
	port := pickPort(t)
	cfg := server.Config{Host: "127.0.0.1", Port: port}
	srv := server.NewServer(cfg)
	go func() { _ = srv.Start() }()

	// Set TTL directly on store (command layer has no TTL yet)
	srv.Store().Set("ttlkey", "ttlval", 20*time.Millisecond)

	conn := dialWithRetry(t, "127.0.0.1", port, false)
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := protocol.NewWriter(conn)

	time.Sleep(50 * time.Millisecond)

	if err := w.WriteCommand([]string{"GET", "ttlkey"}); err != nil {
		t.Fatalf("get write: %v", err)
	}
	if got := readLine(t, r); got != "$-1\r\n" {
		t.Fatalf("expected nil after TTL, got %q", got)
	}
}

// Helpers

func readLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read line: %v", err)
	}
	return line
}

func readBulk(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	header := readLine(t, r)
	if len(header) == 0 || header[0] != '$' {
		t.Fatalf("expected bulk header, got %q", header)
	}
	var n int
	fmt.Sscanf(header, "$%d", &n)
	if n == -1 {
		return ""
	}
	buf := make([]byte, n+2)
	if _, err := r.Read(buf); err != nil {
		t.Fatalf("read bulk: %v", err)
	}
	return string(buf[:n])
}

func readArrayLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line := readLine(t, r)
	if len(line) == 0 || line[0] != '*' {
		t.Fatalf("expected array header, got %q", line)
	}
	return line
}
