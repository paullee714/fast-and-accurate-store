//go:build !darwin && !linux

package server

import "fmt"

// StartEventLoop is a stub for unsupported systems.
func (s *Server) StartEventLoop() error {
	return fmt.Errorf("event loop is only supported on macOS (kqueue) and Linux (epoll) for now")
}
