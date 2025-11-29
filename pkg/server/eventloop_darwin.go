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
	fd           int
	buffer       bytes.Buffer
	writeBuffer  bytes.Buffer
	writeEnabled bool
	authed       bool
}

// StartEventLoop starts the kqueue-based event loop.
func (s *Server) StartEventLoop() error {
	if s.config.TLSCertPath != "" || s.config.TLSKeyPath != "" {
		return fmt.Errorf("TLS is not supported in event loop mode; use standard mode instead")
	}
	if s.config.ReplicaUseTLS {
		return fmt.Errorf("Replica TLS is not supported in event loop mode")
	}
	if s.config.AuthEnabled && s.config.Password == "" {
		return fmt.Errorf("auth enabled but no password provided")
	}
	if len(s.config.AllowedCIDR) > 0 {
		return fmt.Errorf("CIDR allowlist is not supported in event loop mode; use standard mode")
	}

	// Parity with goroutine-per-connection path: start active expiration.
	s.store.StartActiveExpiration()

	// Restore RDB/AOF if configured.
	if err := s.restoreData(); err != nil {
		return err
	}
	s.startMetrics()
	if s.replicaMode {
		go s.startReplicaClient()
	}

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
				} else if event.Filter == syscall.EVFILT_WRITE {
					el.write(ident)
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

		el.connections[nfd] = &ClientState{fd: nfd, authed: el.server.config.Password == ""}
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

		// Authentication gate
		if el.server.config.Password != "" && !client.authed {
			if strings.ToUpper(cmd.Name) != "AUTH" {
				el.queueWrite(client, []byte("-NOAUTH Authentication required\r\n"))
				continue
			}
			if len(cmd.Args) != 1 {
				el.queueWrite(client, []byte("-ERR wrong number of arguments for 'auth' command\r\n"))
				continue
			}
			if cmd.Args[0] == el.server.config.Password {
				client.authed = true
				el.queueWrite(client, []byte("+OK\r\n"))
			} else {
				el.queueWrite(client, []byte("-ERR invalid password\r\n"))
			}
			continue
		}

		// Execute command and buffer response for async write.
		response := el.server.executeCommand(cmd, false)

		// Format response (simplified)
		var respData string
		if strings.HasPrefix(response, "ERR ") {
			respData = fmt.Sprintf("-ERR %s\r\n", strings.TrimPrefix(response, "ERR "))
		} else if strings.HasPrefix(response, "(integer) ") {
			var n int
			fmt.Sscanf(response, "(integer) %d", &n)
			respData = fmt.Sprintf(":%d\r\n", n)
		} else if response == "(nil)" {
			respData = "$-1\r\n"
		} else if response == "OK" {
			respData = "+OK\r\n"
		} else {
			respData = fmt.Sprintf("$%d\r\n%s\r\n", len(response), response)
		}

		el.queueWrite(client, []byte(respData))
	}
}

func (el *EventLoop) close(fd int) {
	syscall.Close(fd)
	delete(el.connections, fd)
	log.Printf("Connection closed (fd: %d)", fd)
}

// queueWrite appends data to the client's write buffer and enables EVFILT_WRITE.
func (el *EventLoop) queueWrite(client *ClientState, data []byte) {
	if _, err := client.writeBuffer.Write(data); err != nil {
		log.Printf("Write buffer error fd %d: %v", client.fd, err)
		el.close(client.fd)
		return
	}
	el.enableWrite(client.fd)
}

// write flushes the client's write buffer on EVFILT_WRITE.
func (el *EventLoop) write(fd int) {
	client, ok := el.connections[fd]
	if !ok {
		return
	}

	for client.writeBuffer.Len() > 0 {
		buf := client.writeBuffer.Bytes()
		n, err := syscall.Write(fd, buf)
		if n > 0 {
			client.writeBuffer.Next(n)
		}

		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				return // try again later
			}
			log.Printf("Write error fd %d: %v", fd, err)
			el.close(fd)
			return
		}

		if n == 0 {
			break
		}
	}

	if client.writeBuffer.Len() == 0 {
		el.disableWrite(fd)
	}
}

func (el *EventLoop) enableWrite(fd int) {
	client, ok := el.connections[fd]
	if !ok || client.writeEnabled {
		return
	}

	change := syscall.Kevent_t{
		Ident:  uint64(fd),
		Filter: syscall.EVFILT_WRITE,
		Flags:  syscall.EV_ADD | syscall.EV_ENABLE,
	}
	if _, err := syscall.Kevent(el.kq, []syscall.Kevent_t{change}, nil, nil); err != nil {
		log.Printf("Failed to enable write fd %d: %v", fd, err)
		return
	}
	client.writeEnabled = true
}

func (el *EventLoop) disableWrite(fd int) {
	client, ok := el.connections[fd]
	if !ok || !client.writeEnabled {
		return
	}

	change := syscall.Kevent_t{
		Ident:  uint64(fd),
		Filter: syscall.EVFILT_WRITE,
		Flags:  syscall.EV_DELETE,
	}
	if _, err := syscall.Kevent(el.kq, []syscall.Kevent_t{change}, nil, nil); err != nil {
		log.Printf("Failed to disable write fd %d: %v", fd, err)
	}
	client.writeEnabled = false
}
