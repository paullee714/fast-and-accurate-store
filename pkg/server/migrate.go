package server

import (
	"bufio"
	"fmt"
	"net"
	"time"

	"fas/pkg/protocol"
)

// migrateKey moves a single key to a target node using SET (string only).
// TTL is not preserved (store only supports strings).
func (s *Server) migrateKey(key string, target string) error {
	val, err := s.store.Get(key)
	if err != nil {
		return err
	}

	conn, err := net.DialTimeout("tcp", target, 1*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	w := protocol.NewWriter(conn)
	if err := w.WriteCommand([]string{"SET", key, val}); err != nil {
		return err
	}
	r := bufio.NewReader(conn)
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	if len(line) > 0 && line[0] == '-' {
		return fmt.Errorf("target error: %s", line)
	}

	s.store.Delete(key)
	return nil
}
