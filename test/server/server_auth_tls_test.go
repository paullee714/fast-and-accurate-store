package server_test

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fas/pkg/persistence"
	"fas/pkg/protocol"
	"fas/pkg/server"
)

func TestServerAuthFlow(t *testing.T) {
	port := pickPort(t)
	cfg := server.Config{
		Host:        "127.0.0.1",
		Port:        port,
		AOFPath:     "",
		FsyncPolicy: persistence.FsyncNo,
		AuthEnabled: true,
		Password:    "pw",
	}
	srv := server.NewServer(cfg)

	go func() {
		if err := srv.Start(); err != nil {
			t.Logf("server error: %v", err)
		}
	}()

	conn := dialWithRetry(t, "127.0.0.1", port, false)
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := protocol.NewWriter(conn)

	// GET before AUTH should fail
	if err := writer.WriteCommand([]string{"GET", "k"}); err != nil {
		t.Fatalf("write GET: %v", err)
	}
	line, _ := reader.ReadString('\n')
	if got := line; got == "" || got[0] != '-' {
		t.Fatalf("expected error before auth, got %q", got)
	}

	// Wrong password
	_ = writer.WriteCommand([]string{"AUTH", "bad"})
	line, _ = reader.ReadString('\n')
	if got := line; got == "" || got[0] != '-' {
		t.Fatalf("expected auth error, got %q", got)
	}

	// Correct password
	_ = writer.WriteCommand([]string{"AUTH", "pw"})
	line, _ = reader.ReadString('\n')
	if want := "+OK\r\n"; line != want {
		t.Fatalf("auth ok expected %q got %q", want, line)
	}

	// Now GET should return nil
	_ = writer.WriteCommand([]string{"GET", "k"})
	line, _ = reader.ReadString('\n')
	if want := "$-1\r\n"; line != want {
		t.Fatalf("nil expected %q got %q", want, line)
	}
}

func TestServerTLS(t *testing.T) {
	certPath, keyPath := genSelfSignedCert(t)
	port := pickPort(t)
	cfg := server.Config{
		Host:        "127.0.0.1",
		Port:        port,
		AOFPath:     "",
		FsyncPolicy: persistence.FsyncNo,
		TLSCertPath: certPath,
		TLSKeyPath:  keyPath,
	}
	srv := server.NewServer(cfg)

	go func() {
		if err := srv.Start(); err != nil {
			t.Logf("server error: %v", err)
		}
	}()

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	var conn *tls.Conn
	var err error
	for i := 0; i < 20; i++ {
		conn, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("tls dial: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := protocol.NewWriter(conn)
	if err := writer.WriteCommand([]string{"PING"}); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	line, _ := reader.ReadString('\n')
	if len(line) == 0 || line[0] != '$' {
		t.Fatalf("expected bulk string, got %q", line)
	}
	var blen int
	if _, err := fmt.Sscanf(line, "$%d\r\n", &blen); err != nil {
		t.Fatalf("parse bulk len: %v", err)
	}
	data := make([]byte, blen+2)
	if _, err := reader.Read(data); err != nil {
		t.Fatalf("read bulk data: %v", err)
	}
	if want := "PONG\r\n"; string(data) != want {
		t.Fatalf("expected PONG got %q", data)
	}
}

// Helpers

func pickPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func dialWithRetry(t *testing.T, host string, port int, tlsMode bool) net.Conn {
	t.Helper()
	addr := fmt.Sprintf("%s:%d", host, port)
	var conn net.Conn
	var err error
	for i := 0; i < 20; i++ {
		if tlsMode {
			conn, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
		} else {
			conn, err = net.Dial("tcp", addr)
		}
		if err == nil {
			return conn
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("dial failed: %v", err)
	return nil
}

func genSelfSignedCert(t *testing.T) (string, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	dir := t.TempDir()
	certOut := filepath.Join(dir, "cert.pem")
	keyOut := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certOut, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyOut, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}), 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certOut, keyOut
}
