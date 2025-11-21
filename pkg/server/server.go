package server

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"strings"

	"fas/pkg/protocol"
	"fas/pkg/pubsub"
	"fas/pkg/store"
)

// Config holds the configuration for the server.
type Config struct {
	Host string
	Port int
}

// Server represents the TCP server instance.
type Server struct {
	config Config
	ln     net.Listener
	store  *store.Store
	pubsub *pubsub.PubSub
}

// NewServer creates a new Server instance with the given configuration.
func NewServer(config Config) *Server {
	return &Server{
		config: config,
		store:  store.New(),
		pubsub: pubsub.New(),
	}
}

// Start initializes the TCP listener and starts accepting connections.
// It blocks until the server stops or an error occurs.
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.ln = ln
	log.Printf("Listening on %s", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

// handleConnection manages a single client connection.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	log.Printf("New connection from %s", conn.RemoteAddr())

	reader := bufio.NewReader(conn)

	for {
		cmd, err := protocol.ParseCommand(reader)
		if err != nil {
			if err != io.EOF {
				log.Printf("Read error: %v", err)
			}
			return
		}
		if cmd == nil {
			continue
		}

		// Handle SUBSCRIBE specially as it changes connection state
		if cmd.Name == "SUBSCRIBE" {
			s.handleSubscribe(conn, cmd)
			return // Connection is now dedicated to subscription or closed
		}

		response := s.executeCommand(cmd)
		conn.Write([]byte(response + "\n"))
	}
}

// handleSubscribe handles the subscription loop for a client.
// It blocks until the client disconnects.
func (s *Server) handleSubscribe(conn net.Conn, cmd *protocol.Command) {
	if len(cmd.Args) < 1 {
		conn.Write([]byte("ERR wrong number of arguments for 'subscribe' command\n"))
		return
	}

	channelName := cmd.Args[0]
	ch := s.pubsub.Subscribe(channelName)
	conn.Write([]byte(fmt.Sprintf("Subscribed to %s\n", channelName)))

	// Block and forward messages
	for msg := range ch {
		_, err := conn.Write([]byte(msg + "\n"))
		if err != nil {
			return // Client disconnected
		}
	}
}

// executeCommand executes a single command and returns the response string.
func (s *Server) executeCommand(cmd *protocol.Command) string {
	switch cmd.Name {
	case "SET":
		if len(cmd.Args) < 2 {
			return "ERR wrong number of arguments for 'set' command"
		}
		s.store.Set(cmd.Args[0], strings.Join(cmd.Args[1:], " "), 0)
		return "OK"
	case "GET":
		if len(cmd.Args) < 1 {
			return "ERR wrong number of arguments for 'get' command"
		}
		val, err := s.store.Get(cmd.Args[0])
		if err != nil {
			if err == store.ErrNotFound {
				return "(nil)"
			}
			return fmt.Sprintf("ERR %v", err)
		}
		return val
	case "PUBLISH":
		if len(cmd.Args) < 2 {
			return "ERR wrong number of arguments for 'publish' command"
		}
		count := s.pubsub.Publish(cmd.Args[0], strings.Join(cmd.Args[1:], " "))
		return fmt.Sprintf("(integer) %d", count)
	default:
		return fmt.Sprintf("ERR unknown command '%s'", cmd.Name)
	}
}
