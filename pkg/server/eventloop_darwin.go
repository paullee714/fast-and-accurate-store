//go:build darwin

package server

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"syscall"

	"fas/pkg/protocol"
)

// EventLoop manages the single-threaded event loop using kqueue.
type EventLoop struct {
	kq          int
	server      *Server
	connections map[int]*ClientState
}

// ClientState holds the state for a connected client.
type ClientState struct {
	fd     int
	buffer bytes.Buffer
}

// StartEventLoop starts the kqueue-based event loop.
func (s *Server) StartEventLoop() error {
	// Create kqueue
	kq, err := syscall.Kqueue()
	if err != nil {
		return fmt.Errorf("failed to create kqueue: %v", err)
	}

	// Create listener socket
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return fmt.Errorf("failed to create socket: %v", err)
	}

	// Set non-blocking
	if err := syscall.SetNonblock(fd, true); err != nil {
		return fmt.Errorf("failed to set non-blocking: %v", err)
	}

	// Bind
	sa := &syscall.SockaddrInet4{Port: s.config.Port}
	if s.config.Host == "localhost" || s.config.Host == "127.0.0.1" {
		sa.Addr = [4]byte{127, 0, 0, 1}
	} else {
		sa.Addr = [4]byte{0, 0, 0, 0}
	}

	if err := syscall.Bind(fd, sa); err != nil {
		return fmt.Errorf("failed to bind: %v", err)
	}

	if err := syscall.Listen(fd, 128); err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	log.Printf("Event Loop Server listening on %s:%d (fd: %d)", s.config.Host, s.config.Port, fd)

	// Register listener with kqueue
	// EVFILT_READ, EV_ADD | EV_ENABLE
	change := syscall.Kevent_t{
		Ident:  uint64(fd),
		Filter: syscall.EVFILT_READ,
		Flags:  syscall.EV_ADD | syscall.EV_ENABLE,
	}

	if _, err := syscall.Kevent(kq, []syscall.Kevent_t{change}, nil, nil); err != nil {
		return fmt.Errorf("failed to register listener: %v", err)
	}

	el := &EventLoop{
		kq:          kq,
		server:      s,
		connections: make(map[int]*ClientState),
	}

	// Event loop
	events := make([]syscall.Kevent_t, 1024)
	for {
		// Wait for events
		n, err := syscall.Kevent(kq, nil, events, nil)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return fmt.Errorf("kevent error: %v", err)
		}

		for i := 0; i < n; i++ {
			event := events[i]
			ident := int(event.Ident)

			if ident == fd {
				// Listener event: Accept new connection
				el.accept(fd)
			} else {
				// Client event: Read data
				if event.Flags&syscall.EV_EOF != 0 {
					el.close(ident)
				} else if event.Filter == syscall.EVFILT_READ {
					el.read(ident)
				}
			}
		}
	}
}

func (el *EventLoop) accept(serverFd int) {
	for {
		nfd, _, err := syscall.Accept(serverFd)
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				break
			}
			log.Printf("Accept error: %v", err)
			break
		}

		if err := syscall.SetNonblock(nfd, true); err != nil {
			log.Printf("SetNonblock error: %v", err)
			syscall.Close(nfd)
			continue
		}

		// Register new client
		change := syscall.Kevent_t{
			Ident:  uint64(nfd),
			Filter: syscall.EVFILT_READ,
			Flags:  syscall.EV_ADD | syscall.EV_ENABLE,
		}

		if _, err := syscall.Kevent(el.kq, []syscall.Kevent_t{change}, nil, nil); err != nil {
			log.Printf("Kevent register error: %v", err)
			syscall.Close(nfd)
			continue
		}

		el.connections[nfd] = &ClientState{fd: nfd}
		log.Printf("New connection (fd: %d)", nfd)
	}
}

func (el *EventLoop) read(fd int) {
	client, ok := el.connections[fd]
	if !ok {
		return
	}

	buf := make([]byte, 4096)
	n, err := syscall.Read(fd, buf)
	if err != nil {
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return
		}
		log.Printf("Read error fd %d: %v", fd, err)
		el.close(fd)
		return
	}

	if n == 0 {
		el.close(fd)
		return
	}

	// Append to buffer
	client.buffer.Write(buf[:n])

	// Try to parse commands
	for {
		if client.buffer.Len() == 0 {
			break
		}

		cmd, consumed, err := protocol.ParseCommandFromBytes(client.buffer.Bytes())
		if err != nil {
			log.Printf("Parse error: %v", err)
			el.close(fd)
			return
		}

		if cmd == nil {
			// Incomplete command, wait for more data
			break
		}

		// Advance buffer
		client.buffer.Next(consumed)

		// Execute command
		// We need a way to write back response.
		// For now, synchronous write to non-blocking socket (might block/fail).
		// Ideally we should buffer response and handle EAGAIN.
		// MVP: Just write and hope it fits in kernel buffer.

		response := el.server.executeCommand(cmd, false)

		// Format response (simplified)
		var respData string
		if strings.HasPrefix(response, "ERR ") {
			respData = fmt.Sprintf("-ERR %s\r\n", strings.TrimPrefix(response, "ERR "))
		} else if strings.HasPrefix(response, "(integer) ") {
			var n int
			fmt.Sscanf(response, "(integer) %d", &n)
			respData = fmt.Sprintf(":%d\r\n", n)
		} else if response == "OK" {
			respData = "+OK\r\n"
		} else {
			respData = fmt.Sprintf("$%d\r\n%s\r\n", len(response), response)
		}

		_, err = syscall.Write(fd, []byte(respData))
		if err != nil {
			log.Printf("Write error: %v", err)
			el.close(fd)
			return
		}
	}
}

func (el *EventLoop) close(fd int) {
	syscall.Close(fd)
	delete(el.connections, fd)
	log.Printf("Connection closed (fd: %d)", fd)
}
