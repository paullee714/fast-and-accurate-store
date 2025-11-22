package server

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"strings"

	"fas/pkg/persistence"
	"fas/pkg/protocol"
	"fas/pkg/pubsub"
	"fas/pkg/store"
)

// Config holds the configuration for the server.
type Config struct {
	Host        string
	Port        int
	AOFPath     string // Path to the AOF file
	FsyncPolicy persistence.FsyncPolicy
	MaxMemory   int64 // Max memory in bytes
	AuthEnabled bool
	Password    string // Optional password for AUTH
	TLSCertPath string
	TLSKeyPath  string
	AllowedCIDR []netip.Prefix
}

// Server represents the TCP server instance.
type Server struct {
	config Config
	ln     net.Listener
	store  *store.Store
	pubsub *pubsub.PubSub
	aof    *persistence.AOF

	stats struct {
		expired int64
	}
}

// NewServer creates a new Server instance with the given configuration.
func NewServer(config Config) *Server {
	// Default to 1GB if not set
	if config.MaxMemory == 0 {
		config.MaxMemory = 1024 * 1024 * 1024
	}

	return &Server{
		config: config,
		store:  store.New(config.MaxMemory, store.EvictionAllKeysRandom),
		pubsub: pubsub.New(),
	}
}

// Start initializes the TCP listener and starts accepting connections.
// It blocks until the server stops or an error occurs.
func (s *Server) Start() error {
	// Start active expiration for store (only in multi-threaded mode)
	s.store.StartActiveExpiration()

	// Initialize AOF
	if s.config.AOFPath != "" {
		aof, err := s.initAOF()
		if err != nil {
			return err
		}
		s.aof = aof
		defer s.aof.Close()
	}

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	var ln net.Listener
	var err error
	if s.config.TLSCertPath != "" && s.config.TLSKeyPath != "" {
		cer, loadErr := tls.LoadX509KeyPair(s.config.TLSCertPath, s.config.TLSKeyPath)
		if loadErr != nil {
			return fmt.Errorf("failed to load TLS cert/key: %v", loadErr)
		}
		tlsCfg := &tls.Config{Certificates: []tls.Certificate{cer}}
		ln, err = tls.Listen("tcp", addr, tlsCfg)
		if err != nil {
			return err
		}
		log.Printf("Listening with TLS on %s", addr)
	} else {
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		log.Printf("Listening on %s", addr)
	}
	s.ln = ln

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

// initAOF opens the AOF file and replays its contents to restore state.
func (s *Server) initAOF() (*persistence.AOF, error) {
	aof, err := persistence.NewAOF(s.config.AOFPath, s.config.FsyncPolicy)
	if err != nil {
		return nil, fmt.Errorf("failed to open AOF file: %v", err)
	}

	log.Println("Restoring state from AOF...")
	if err := aof.ReadCommands(func(cmd *protocol.Command) {
		s.executeCommand(cmd, true) // replay mode (do not re-append)
	}); err != nil {
		aof.Close()
		return nil, fmt.Errorf("failed to restore from AOF: %v", err)
	}
	log.Println("State restored.")

	return aof, nil
}

// handleConnection manages a single client connection.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	log.Printf("New connection from %s", conn.RemoteAddr())

	reader := bufio.NewReader(conn)
	writer := protocol.NewWriter(conn)
	if s.config.AuthEnabled && s.config.Password == "" {
		writer.WriteError(fmt.Errorf("ERR auth enabled but password not set"))
		return
	}

	authenticated := !s.config.AuthEnabled

	for {
		cmd, err := protocol.ParseCommand(reader)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error parsing command: %v", err)
				writer.WriteError(err)
			}
			return
		}
		if cmd == nil {
			continue
		}

		// Allowlist check
		if len(s.config.AllowedCIDR) > 0 {
			addr, ok := conn.RemoteAddr().(*net.TCPAddr)
			if ok {
				ip, parseErr := netip.ParseAddr(addr.IP.String())
				if parseErr != nil || !s.isAllowed(ip) {
					writer.WriteError(fmt.Errorf("ERR connection not allowed"))
					return
				}
			}
		}

		// Authentication gate
		if !authenticated && strings.ToUpper(cmd.Name) != "AUTH" {
			writer.WriteError(fmt.Errorf("NOAUTH Authentication required"))
			continue
		}
		if strings.ToUpper(cmd.Name) == "AUTH" {
			if len(cmd.Args) != 1 {
				writer.WriteError(fmt.Errorf("ERR wrong number of arguments for 'auth' command"))
				continue
			}
			if cmd.Args[0] == s.config.Password {
				authenticated = true
				writer.WriteString("OK")
			} else {
				writer.WriteError(fmt.Errorf("ERR invalid password"))
			}
			continue
		}

		// Handle SUBSCRIBE command specially as it changes connection state
		if strings.ToUpper(cmd.Name) == "SUBSCRIBE" {
			if len(cmd.Args) < 1 {
				writer.WriteError(fmt.Errorf("wrong number of arguments for 'subscribe' command"))
				continue
			}
			s.handleSubscribe(conn, writer, cmd.Args)
			return // Connection is now dedicated to subscription or closed
		}

		response := s.executeCommand(cmd, false)

		// Determine response type based on content
		if strings.HasPrefix(response, "ERR ") {
			writer.WriteError(fmt.Errorf(strings.TrimPrefix(response, "ERR ")))
		} else if strings.HasPrefix(response, "(integer) ") {
			var n int
			fmt.Sscanf(response, "(integer) %d", &n)
			writer.WriteInteger(n)
		} else if response == "(nil)" {
			writer.WriteNull()
		} else {
			// Default to simple string for OK, or bulk string for values?
			// For now, let's stick to simple string for status, and bulk for data.
			// But executeCommand returns a string. We need to refine this.
			// To keep it simple for this refactor, let's treat "OK" as simple string, others as bulk.
			if response == "OK" {
				writer.WriteString(response)
			} else {
				writer.WriteBulkString(response)
			}
		}
	}
}

func (s *Server) isAllowed(ip netip.Addr) bool {
	if len(s.config.AllowedCIDR) == 0 {
		return true
	}
	for _, p := range s.config.AllowedCIDR {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// handleSubscribe handles the subscription loop for a client.
// It blocks until the client disconnects.
func (s *Server) handleSubscribe(conn net.Conn, writer *protocol.Writer, channels []string) {
	// We only support single channel subscription for now based on previous implementation
	// But args can be multiple. Let's just take the first one for now or loop.
	// The previous implementation took one channel.

	if len(channels) == 0 {
		writer.WriteError(fmt.Errorf("wrong number of arguments for 'subscribe' command"))
		return
	}

	channelName := channels[0]
	ch := s.pubsub.Subscribe(channelName)
	defer s.pubsub.Unsubscribe(channelName, ch)

	// Send subscription confirmation RESP array
	writer.WriteCommand([]string{"subscribe", channelName, "1"})

	// Block and forward messages
	for msg := range ch {
		// RESP message array: ["message", channel, payload]
		err := writer.WriteCommand([]string{"message", channelName, msg})
		if err != nil {
			return // Client disconnected
		}
	}
}

// executeCommand executes a single command and returns the response string.
// replay: if true, indicates we are replaying from AOF (so don't write to AOF again)
func (s *Server) executeCommand(cmd *protocol.Command, replay bool) string {
	switch cmd.Name {
	case "SET":
		if len(cmd.Args) != 2 {
			return "ERR wrong number of arguments for 'set' command"
		}
		s.store.Set(cmd.Args[0], cmd.Args[1], 0)

		// Persist to AOF if not replaying
		if !replay && s.aof != nil {
			s.aof.Write(cmd)
		}

		return "OK"
	case "GET":
		if len(cmd.Args) != 1 {
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
		if len(cmd.Args) != 2 {
			return "ERR wrong number of arguments for 'publish' command"
		}
		count := s.pubsub.Publish(cmd.Args[0], cmd.Args[1])
		return fmt.Sprintf("(integer) %d", count)
	case "PING":
		return "PONG"
	case "INFO":
		stats := s.store.Stats()
		info := fmt.Sprintf("keys:%d\nkeys_with_ttl:%d\nexpired:%d\nmemory_used:%d\nmax_memory:%d\n",
			stats.Keys, stats.TTLKeys, stats.Expired, stats.MemoryUsed, stats.MaxMemory)
		return info
	default:
		return fmt.Sprintf("ERR unknown command '%s'", cmd.Name)
	}
}
