package integration_test

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"fas/pkg/protocol"
)

func TestIntegration_ServerCLI(t *testing.T) {
	port := pickPort(t)
	root := repoRoot(t)
	bin := filepath.Join(root, "bin", "fas")

	// Build server if missing
	if _, err := exec.LookPath(bin); err != nil {
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/fas")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build fas: %v (%s)", err, out)
		}
	}

	cmd := exec.Command(bin, "-host", "127.0.0.1", "-port", fmt.Sprint(port), "-aof", "")
	cmd.Dir = root
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer cmd.Process.Kill()

	conn := dialWithRetry(t, "127.0.0.1", port)
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := protocol.NewWriter(conn)

	// Basic PING/SET/GET
	mustWrite(t, w, []string{"PING"})
	if got := readBulk(t, r); got != "PONG" {
		t.Fatalf("PING -> %q", got)
	}

	mustWrite(t, w, []string{"SET", "k", "v"})
	expectLine(t, r, "+OK\r\n")

	mustWrite(t, w, []string{"GET", "k"})
	if got := readBulk(t, r); got != "v" {
		t.Fatalf("GET k -> %q", got)
	}
}

func TestIntegration_PubSub(t *testing.T) {
	port := pickPort(t)
	root := repoRoot(t)
	bin := filepath.Join(root, "bin", "fas")

	if _, err := exec.LookPath(bin); err != nil {
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/fas")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build fas: %v (%s)", err, out)
		}
	}

	cmd := exec.Command(bin, "-host", "127.0.0.1", "-port", fmt.Sprint(port), "-aof", "")
	cmd.Dir = root
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer cmd.Process.Kill()

	// Subscriber
	sub := dialWithRetry(t, "127.0.0.1", port)
	defer sub.Close()
	sr := bufio.NewReader(sub)
	sw := protocol.NewWriter(sub)
	mustWrite(t, sw, []string{"SUBSCRIBE", "news"})
	readArrayLine(t, sr) // header
	readBulk(t, sr)      // "subscribe"
	readBulk(t, sr)      // "news"
	readBulk(t, sr)      // count

	// Publisher
	pub := dialWithRetry(t, "127.0.0.1", port)
	defer pub.Close()
	pw := protocol.NewWriter(pub)
	mustWrite(t, pw, []string{"PUBLISH", "news", "hello"})
	bufio.NewReader(pub).ReadString('\n') // discard publish reply

	// Expect message
	readArrayLine(t, sr)
	if msgType := readBulk(t, sr); msgType != "message" {
		t.Fatalf("expected message got %q", msgType)
	}
	if ch := readBulk(t, sr); ch != "news" {
		t.Fatalf("channel mismatch: %q", ch)
	}
	if payload := readBulk(t, sr); payload != "hello" {
		t.Fatalf("payload mismatch: %q", payload)
	}
}

func TestIntegration_Migrate(t *testing.T) {
	root := repoRoot(t)
	bin := filepath.Join(root, "bin", "fas")

	// Always build fresh for integration
	{
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/fas")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build fas: %v (%s)", err, out)
		}
	}

	port1 := pickPort(t)
	port2 := pickPort(t)

	s1 := exec.Command(bin, "-host", "127.0.0.1", "-port", fmt.Sprint(port1), "-aof", "")
	s1.Dir = root
	if err := s1.Start(); err != nil {
		t.Fatalf("start server1: %v", err)
	}
	defer s1.Process.Kill()

	s2 := exec.Command(bin, "-host", "127.0.0.1", "-port", fmt.Sprint(port2), "-aof", "")
	s2.Dir = root
	if err := s2.Start(); err != nil {
		t.Fatalf("start server2: %v", err)
	}
	defer s2.Process.Kill()

	// Set key on server1
	conn1 := dialWithRetry(t, "127.0.0.1", port1)
	defer conn1.Close()
	r1 := bufio.NewReader(conn1)
	w1 := protocol.NewWriter(conn1)

	mustWrite(t, w1, []string{"SET", "mk", "mv"})
	expectLine(t, r1, "+OK\r\n")

	// Migrate key to server2
	target := fmt.Sprintf("127.0.0.1:%d", port2)
	mustWrite(t, w1, []string{"MIGRATE", "mk", target})
	expectLine(t, r1, "+OK\r\n")

	// Verify on server2
	conn2 := dialWithRetry(t, "127.0.0.1", port2)
	defer conn2.Close()
	r2 := bufio.NewReader(conn2)
	w2 := protocol.NewWriter(conn2)
	mustWrite(t, w2, []string{"GET", "mk"})
	if got := readBulk(t, r2); got != "mv" {
		t.Fatalf("migrated value mismatch: %q", got)
	}
}

// Helpers
func mustWrite(t *testing.T, w *protocol.Writer, args []string) {
	t.Helper()
	if err := w.WriteCommand(args); err != nil {
		t.Fatalf("write %v: %v", args, err)
	}
}

func expectLine(t *testing.T, r *bufio.Reader, want string) {
	t.Helper()
	got, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read line: %v", err)
	}
	if got != want {
		t.Fatalf("want %q got %q", want, got)
	}
}

func readBulk(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	header, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read bulk header: %v", err)
	}
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

func dialWithRetry(t *testing.T, host string, port int) net.Conn {
	t.Helper()
	addr := fmt.Sprintf("%s:%d", host, port)
	var conn net.Conn
	var err error
	for i := 0; i < 20; i++ {
		conn, err = net.Dial("tcp", addr)
		if err == nil {
			return conn
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("dial failed: %v", err)
	return nil
}

func pickPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readArrayLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read array: %v", err)
	}
	if len(line) == 0 || line[0] != '*' {
		t.Fatalf("expected array header, got %q", line)
	}
	return line
}
