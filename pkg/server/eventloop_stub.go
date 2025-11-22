//go:build !darwin

package server

import "fmt"

// StartEventLoop is a stub for non-darwin systems.
func (s *Server) StartEventLoop() error {
	return fmt.Errorf("event loop is only supported on macOS (darwin) for now")
}
