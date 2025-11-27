//go:build linux

package server

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"syscall"

	"fas/pkg/protocol"
)

// EventLoop manages the single-threaded event loop using epoll.
type EventLoop struct {
	epfd        int
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

// StartEventLoop starts the epoll-based event loop (Linux).
func (s *Server) StartEventLoop() error {
	if s.config.TLSCertPath != "" || s.config.TLSKeyPath != "" {
		return fmt.Errorf("TLS is not supported in event loop mode; use standard mode instead")
	}
	if len(s.config.AllowedCIDR) > 0 {
		return fmt.Errorf("CIDR allowlist is not supported in event loop mode; use standard mode")
	}
	if s.config.AuthEnabled && s.config.Password == "" {
		return fmt.Errorf("auth enabled but no password provided")
	}

	// Parity with goroutine-per-connection path: start active expiration and restore data.
	s.store.StartActiveExpiration()
	if err := s.restoreData(); err != nil {
		return err
	}
	s.startMetrics()
	if s.replicaMode {
		go s.startReplicaClient()
	}

	epfd, err := syscall.EpollCreate1(0)
	if err != nil {
		return fmt.Errorf("failed to create epoll: %v", err)
	}

	// Create listener socket
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return fmt.Errorf("failed to create socket: %v", err)
	}

	if err := syscall.SetNonblock(fd, true); err != nil {
		return fmt.Errorf("failed to set non-blocking: %v", err)
	}

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

	// Register listener
	ev := &syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(fd)}
	if err := syscall.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, fd, ev); err != nil {
		return fmt.Errorf("epoll ctl add listener: %v", err)
	}

	el := &EventLoop{
		epfd:        epfd,
		server:      s,
		connections: make(map[int]*ClientState),
	}

	events := make([]syscall.EpollEvent, 1024)
	log.Printf("Event Loop Server (epoll) listening on %s:%d (fd: %d)", s.config.Host, s.config.Port, fd)

	for {
		n, err := syscall.EpollWait(epfd, events, -1)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return fmt.Errorf("epoll wait error: %v", err)
		}
		for i := 0; i < n; i++ {
			e := events[i]
			ident := int(e.Fd)
			if ident == fd {
				el.accept(fd)
			} else {
				if e.Events&(syscall.EPOLLHUP|syscall.EPOLLERR) != 0 {
					el.close(ident)
					continue
				}
				if e.Events&syscall.EPOLLIN != 0 {
					el.read(ident)
				}
				if e.Events&syscall.EPOLLOUT != 0 {
					el.write(ident)
				}
			}
		}
	}
}

func (el *EventLoop) accept(serverFd int) {
	for {
		nfd, sa, err := syscall.Accept(serverFd)
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				return
			}
			log.Printf("Accept error: %v", err)
			return
		}
		if err := syscall.SetNonblock(nfd, true); err != nil {
			log.Printf("SetNonblock error: %v", err)
			syscall.Close(nfd)
			continue
		}

		ev := &syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(nfd)}
		if err := syscall.EpollCtl(el.epfd, syscall.EPOLL_CTL_ADD, nfd, ev); err != nil {
			log.Printf("EpollCtl add error: %v", err)
			syscall.Close(nfd)
			continue
		}

		authed := !el.server.config.AuthEnabled
		el.connections[nfd] = &ClientState{fd: nfd, authed: authed}
		log.Printf("New connection (fd: %d, from: %v)", nfd, sa)
	}
}

func (el *EventLoop) read(fd int) {
	client, ok := el.connections[fd]
	if !ok {
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := syscall.Read(fd, buf)
		if n > 0 {
			client.buffer.Write(buf[:n])
		}
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				break
			}
			log.Printf("Read error fd %d: %v", fd, err)
			el.close(fd)
			return
		}
		if n == 0 {
			el.close(fd)
			return
		}
	}

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
			break
		}
		client.buffer.Next(consumed)

		// Auth gate
		if el.server.config.AuthEnabled && !client.authed {
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

		resp := el.server.executeCommand(cmd, false)
		var respData string
		if strings.HasPrefix(resp, "ERR ") {
			respData = fmt.Sprintf("-ERR %s\r\n", strings.TrimPrefix(resp, "ERR "))
		} else if strings.HasPrefix(resp, "(integer) ") {
			var n int
			fmt.Sscanf(resp, "(integer) %d", &n)
			respData = fmt.Sprintf(":%d\r\n", n)
		} else if resp == "(nil)" {
			respData = "$-1\r\n"
		} else if resp == "OK" {
			respData = "+OK\r\n"
		} else {
			respData = fmt.Sprintf("$%d\r\n%s\r\n", len(resp), resp)
		}
		el.queueWrite(client, []byte(respData))
	}
}

func (el *EventLoop) write(fd int) {
	client, ok := el.connections[fd]
	if !ok {
		return
	}
	for client.writeBuffer.Len() > 0 {
		data := client.writeBuffer.Bytes()
		n, err := syscall.Write(fd, data)
		if n > 0 {
			client.writeBuffer.Next(n)
		}
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				el.enableWrite(fd)
				return
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

func (el *EventLoop) queueWrite(client *ClientState, data []byte) {
	if _, err := client.writeBuffer.Write(data); err != nil {
		log.Printf("write buffer error fd %d: %v", client.fd, err)
		el.close(client.fd)
		return
	}
	el.enableWrite(client.fd)
}

func (el *EventLoop) enableWrite(fd int) {
	client, ok := el.connections[fd]
	if !ok || client.writeEnabled {
		return
	}
	ev := &syscall.EpollEvent{Events: syscall.EPOLLIN | syscall.EPOLLOUT, Fd: int32(fd)}
	if err := syscall.EpollCtl(el.epfd, syscall.EPOLL_CTL_MOD, fd, ev); err != nil {
		log.Printf("EpollCtl mod enable write fd %d: %v", fd, err)
		return
	}
	client.writeEnabled = true
}

func (el *EventLoop) disableWrite(fd int) {
	client, ok := el.connections[fd]
	if !ok || !client.writeEnabled {
		return
	}
	ev := &syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(fd)}
	if err := syscall.EpollCtl(el.epfd, syscall.EPOLL_CTL_MOD, fd, ev); err != nil {
		log.Printf("EpollCtl mod disable write fd %d: %v", fd, err)
		return
	}
	client.writeEnabled = false
}

func (el *EventLoop) close(fd int) {
	syscall.EpollCtl(el.epfd, syscall.EPOLL_CTL_DEL, fd, nil)
	syscall.Close(fd)
	delete(el.connections, fd)
	log.Printf("Connection closed (fd: %d)", fd)
}
