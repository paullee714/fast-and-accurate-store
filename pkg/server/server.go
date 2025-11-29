package server

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"fas/pkg/persistence"
	"fas/pkg/protocol"
	"fas/pkg/pubsub"
	"fas/pkg/store"
)

// Config holds the configuration for the server.
type Config struct {
	Host          string
	Port          int
	AOFPath       string // Path to the AOF file
	FsyncPolicy   persistence.FsyncPolicy
	MaxMemory     int64 // Max memory in bytes
	Eviction      store.EvictionPolicy
	AuthEnabled   bool
	Password      string // Optional password for AUTH
	TLSCertPath   string
	TLSKeyPath    string
	AllowedCIDR   []netip.Prefix
	RDBPath       string // Optional snapshot path; if present, load before AOF
	MaxClients    int    // 0 = unlimited
	MetricsPort   int    // optional http metrics port
	ReplicaOf     string // optional master address host:port (replica mode)
	ReplicaUseTLS bool
	ReplicaPass   string
	Slots         int // total slots (for future cluster; default 16384)
	StaticSlots   []SlotRange
}

// ValidateAndFill sets defaults and validates combinations.
func (c *Config) ValidateAndFill() error {
	if c.MaxMemory == 0 {
		c.MaxMemory = 1024 * 1024 * 1024
	}
	if c.Eviction == 0 {
		c.Eviction = store.EvictionAllKeysRandom
	}
	if c.Slots == 0 {
		c.Slots = 16384
	}
	if c.AuthEnabled && c.Password == "" {
		return fmt.Errorf("auth enabled but no password provided")
	}
	if (c.TLSCertPath == "") != (c.TLSKeyPath == "") {
		return fmt.Errorf("both tls-cert and tls-key must be provided together")
	}
	if c.ReplicaOf != "" && c.ReplicaUseTLS && c.TLSCertPath == "" && c.TLSKeyPath == "" {
		// replica TLS client is allowed even if server isn't serving TLS; no strict check
	}
	return nil
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

	bgSave struct {
		mu      sync.Mutex
		running bool
	}

	muClients sync.Mutex
	clients   int

	repMu      sync.Mutex
	replicas   map[int]*replicaClient
	replicaSeq int

	// replica mode client
	replicaMode bool
	masterAddr  string

	leaderMu   sync.RWMutex
	leaderAddr string

	selfAddr string

	slotCount int
	cluster   *ClusterState
}

// NewServer creates a new Server instance with the given configuration.
func NewServer(config Config) *Server {
	_ = config.ValidateAndFill()

	return &Server{
		config:      config,
		store:       store.New(config.MaxMemory, config.Eviction),
		pubsub:      pubsub.New(),
		replicas:    make(map[int]*replicaClient),
		replicaMode: config.ReplicaOf != "",
		masterAddr:  config.ReplicaOf,
		leaderAddr:  fmt.Sprintf("%s:%d", config.Host, config.Port),
		selfAddr:    fmt.Sprintf("%s:%d", config.Host, config.Port),
		slotCount:   config.Slots,
		cluster:     NewClusterState(config.Slots, fmt.Sprintf("%s:%d", config.Host, config.Port)),
	}
}

// Start initializes the TCP listener and starts accepting connections.
// It blocks until the server stops or an error occurs.
func (s *Server) Start() error {
	// Start active expiration for store (only in multi-threaded mode)
	s.store.StartActiveExpiration()

	if err := s.restoreData(); err != nil {
		return err
	}
	s.startMetrics()
	if len(s.config.StaticSlots) > 0 {
		s.cluster.SetRanges(s.config.StaticSlots)
	}

	if s.replicaMode {
		go s.startReplicaClient()
	}
	go s.startLeaderHeartbeat()

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
		if s.config.MaxClients > 0 {
			s.muClients.Lock()
			if s.clients >= s.config.MaxClients {
				s.muClients.Unlock()
				conn.Close()
				log.Printf("Rejecting connection: max clients reached (%d)", s.config.MaxClients)
				continue
			}
			s.clients++
			s.muClients.Unlock()
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

func (s *Server) restoreData() error {
	// Try RDB first if configured and exists
	if s.config.RDBPath != "" {
		if _, err := os.Stat(s.config.RDBPath); err == nil {
			log.Printf("Loading snapshot from %s", s.config.RDBPath)
			entries, err := persistence.LoadSnapshot(s.config.RDBPath)
			if err != nil {
				return fmt.Errorf("failed to load snapshot: %v", err)
			}
			s.store.RestoreSnapshot(entries)
			log.Println("Snapshot loaded.")
		}
	}

	if s.config.AOFPath != "" {
		aof, err := s.initAOF()
		if err != nil {
			return err
		}
		s.aof = aof
		// Do not defer close here; Start manages lifecycle
	}
	return nil
}

func (s *Server) saveSnapshot() error {
	if s.config.RDBPath == "" {
		return fmt.Errorf("snapshot path not configured")
	}
	entries := s.store.SnapshotEntries()
	if err := persistence.SaveSnapshot(s.config.RDBPath, entries); err != nil {
		return err
	}
	return nil
}

func (s *Server) startBGSAVE() bool {
	s.bgSave.mu.Lock()
	if s.bgSave.running {
		s.bgSave.mu.Unlock()
		return false
	}
	s.bgSave.running = true
	s.bgSave.mu.Unlock()

	go func() {
		defer func() {
			s.bgSave.mu.Lock()
			s.bgSave.running = false
			s.bgSave.mu.Unlock()
		}()
		if err := s.saveSnapshot(); err != nil {
			log.Printf("BGSAVE failed: %v", err)
		} else {
			log.Printf("BGSAVE completed: %s", s.config.RDBPath)
		}
	}()
	return true
}

// Store exposes underlying store (for tests/embedding).
func (s *Server) Store() *store.Store {
	return s.store
}

// SaveSnapshot triggers a synchronous snapshot (used in tests).
func (s *Server) SaveSnapshot() error {
	return s.saveSnapshot()
}

// handleConnection manages a single client connection.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	log.Printf("New connection from %s", conn.RemoteAddr())
	if s.config.MaxClients > 0 {
		defer func() {
			s.muClients.Lock()
			s.clients--
			s.muClients.Unlock()
		}()
	}

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

		if strings.ToUpper(cmd.Name) == "REPL" {
			s.handleReplicaConnection(conn, writer)
			return
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

func (s *Server) currentClients() int {
	s.muClients.Lock()
	defer s.muClients.Unlock()
	return s.clients
}

func (s *Server) currentLeader() string {
	s.leaderMu.RLock()
	defer s.leaderMu.RUnlock()
	return s.leaderAddr
}

func (s *Server) isLeader() bool {
	return s.currentLeader() == s.selfAddr
}

func (s *Server) startMetrics() {
	if s.config.MetricsPort == 0 {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		stats := s.store.Stats()
		clients := s.currentClients()
		leader := s.currentLeader()
		fmt.Fprintf(w, "fas_keys %d\n", stats.Keys)
		fmt.Fprintf(w, "fas_keys_with_ttl %d\n", stats.TTLKeys)
		fmt.Fprintf(w, "fas_expired_total %d\n", stats.Expired)
		fmt.Fprintf(w, "fas_memory_used_bytes %d\n", stats.MemoryUsed)
		fmt.Fprintf(w, "fas_max_memory_bytes %d\n", stats.MaxMemory)
		fmt.Fprintf(w, "fas_clients %d\n", clients)
		fmt.Fprintf(w, "fas_leader_info 1\n")
		fmt.Fprintf(w, "fas_leader_address %q\n", leader)
	})
	addr := fmt.Sprintf(":%d", s.config.MetricsPort)
	go func() {
		srv := &http.Server{Addr: addr, Handler: mux}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("metrics server error: %v", err)
		}
	}()
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

type replicaClient struct {
	ch chan *protocol.Command
}

// handleReplicaConnection sends a full snapshot then streams future write commands.
func (s *Server) handleReplicaConnection(conn net.Conn, w *protocol.Writer) {
	log.Printf("Replica connected from %s", conn.RemoteAddr())

	// Send snapshot as SET commands
	for _, e := range s.store.SnapshotEntries() {
		cmd := []string{"SET", e.Key, e.Value}
		if err := w.WriteCommand(cmd); err != nil {
			log.Printf("Replica snapshot write error: %v", err)
			return
		}
	}

	// Register for streaming
	rc := &replicaClient{ch: make(chan *protocol.Command, 1024)}
	s.repMu.Lock()
	s.replicaSeq++
	id := s.replicaSeq
	s.replicas[id] = rc
	s.repMu.Unlock()

	// Ensure write events flush
	go func() {
		for cmd := range rc.ch {
			if err := w.WriteCommand(append([]string{cmd.Name}, cmd.Args...)); err != nil {
				log.Printf("Replica stream error: %v", err)
				break
			}
		}
		conn.Close()
	}()

	// Block until connection closes
	buf := make([]byte, 1)
	conn.Read(buf)

	s.repMu.Lock()
	delete(s.replicas, id)
	close(rc.ch)
	s.repMu.Unlock()
}

func (s *Server) broadcastToReplicas(cmd *protocol.Command) {
	if cmd == nil {
		return
	}
	if cmd.Name != "SET" && cmd.Name != "PUBLISH" {
		return
	}
	s.repMu.Lock()
	defer s.repMu.Unlock()
	for _, r := range s.replicas {
		select {
		case r.ch <- cmd:
		default:
			// drop if replica is slow
		}
	}
}

// startReplicaClient runs in replica mode: connects to master and applies incoming commands.
func (s *Server) startReplicaClient() {
	for {
		var conn net.Conn
		var err error
		if s.config.ReplicaUseTLS {
			conn, err = tls.Dial("tcp", s.masterAddr, &tls.Config{InsecureSkipVerify: true})
		} else {
			conn, err = net.Dial("tcp", s.masterAddr)
		}
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		reader := bufio.NewReader(conn)
		writer := protocol.NewWriter(conn)

		// AUTH if needed
		if s.config.ReplicaPass != "" {
			if err := writer.WriteCommand([]string{"AUTH", s.config.ReplicaPass}); err != nil {
				conn.Close()
				time.Sleep(1 * time.Second)
				continue
			}
			// consume reply
			if _, err := reader.ReadString('\n'); err != nil {
				conn.Close()
				time.Sleep(1 * time.Second)
				continue
			}
		}

		// Request replication stream
		if err := writer.WriteCommand([]string{"REPL"}); err != nil {
			conn.Close()
			time.Sleep(1 * time.Second)
			continue
		}

		// Update leader
		s.leaderMu.Lock()
		s.leaderAddr = s.masterAddr
		s.leaderMu.Unlock()

		for {
			cmd, err := protocol.ParseCommand(reader)
			if err != nil {
				if err != io.EOF {
					log.Printf("replica parse error: %v", err)
				}
				conn.Close()
				break
			}
			if cmd == nil {
				continue
			}
			s.executeCommand(cmd, true)
		}
		time.Sleep(1 * time.Second)
	}
}

// executeCommand executes a single command and returns the response string.
// replay: if true, indicates we are replaying from AOF (so don't write to AOF again)
func (s *Server) executeCommand(cmd *protocol.Command, replay bool) string {
	writeCmd := false
	defer func() {
		if writeCmd && !replay {
			s.broadcastToReplicas(cmd)
		}
	}()

	// Slot redirection if not leader for the key slot
	if !s.isLeader() && !replay {
		slot := 0
		if len(cmd.Args) > 0 {
			slot = keySlot(cmd.Args[0], s.slotCount)
		}
		target := s.cluster.Lookup(slot)
		if target != "" && target != s.selfAddr {
			return fmt.Sprintf("MOVED %d %s", slot, target)
		}
	}

	// Enforce slot ownership: if this node is not owner for the key slot, redirect
	if len(cmd.Args) > 0 {
		slot := keySlot(cmd.Args[0], s.slotCount)
		target := s.cluster.Lookup(slot)
		if target != "" && target != s.selfAddr {
			return fmt.Sprintf("MOVED %d %s", slot, target)
		}
	}

	switch cmd.Name {
	case "SET":
		if len(cmd.Args) != 2 {
			return "ERR wrong number of arguments for 'set' command"
		}
		if s.replicaMode && !replay {
			return "ERR READONLY replica"
		}
		writeCmd = true
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
		if s.replicaMode && !replay {
			return "ERR READONLY replica"
		}
		writeCmd = true
		count := s.pubsub.Publish(cmd.Args[0], cmd.Args[1])
		return fmt.Sprintf("(integer) %d", count)
	case "PING":
		return "PONG"
	case "INFO":
		stats := s.store.Stats()
		info := fmt.Sprintf("keys:%d\nkeys_with_ttl:%d\nexpired:%d\nmemory_used:%d\nmax_memory:%d\nleader:%s\n",
			stats.Keys, stats.TTLKeys, stats.Expired, stats.MemoryUsed, stats.MaxMemory, s.currentLeader())
		return info
	case "CLUSTER":
		if len(cmd.Args) == 0 {
			return "ERR wrong number of arguments for 'cluster' command"
		}
		switch strings.ToUpper(cmd.Args[0]) {
		case "INFO":
			return s.clusterInfo()
		case "NODES":
			return s.clusterNodes()
		case "SLOTS":
			return s.clusterSlots()
		default:
			return fmt.Sprintf("ERR unknown subcommand '%s'", cmd.Args[0])
		}
	case "SAVE":
		if s.config.RDBPath == "" {
			return "ERR snapshot path not configured"
		}
		if err := s.saveSnapshot(); err != nil {
			return fmt.Sprintf("ERR %v", err)
		}
		return "OK"
	case "BGSAVE":
		if s.config.RDBPath == "" {
			return "ERR snapshot path not configured"
		}
		started := s.startBGSAVE()
		if !started {
			return "ERR BGSAVE already in progress"
		}
		return "Background saving started"
	case "MIGRATE":
		if len(cmd.Args) < 2 {
			return "ERR wrong number of arguments for 'migrate' command"
		}
		key := cmd.Args[0]
		target := cmd.Args[1]
		if err := s.migrateKey(key, target); err != nil {
			return fmt.Sprintf("ERR %v", err)
		}
		return "OK"
	default:
		return fmt.Sprintf("ERR unknown command '%s'", cmd.Name)
	}

	if !replay {
		s.broadcastToReplicas(cmd)
	}
	return ""
}

// Simple leader heartbeat; in this phase we only announce self and detect missing leader to self-elect.
func (s *Server) startLeaderHeartbeat() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		// If we are replica and masterAddr unreachable, self-elect to allow writes (single slot model)
		if s.replicaMode {
			conn, err := net.DialTimeout("tcp", s.masterAddr, 500*time.Millisecond)
			if err != nil {
				log.Printf("leader unreachable, self-electing")
				s.leaderMu.Lock()
				s.leaderAddr = fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
				s.leaderMu.Unlock()
				s.replicaMode = false
			} else {
				conn.Close()
			}
		}
	}
}

// Cluster introspection (single leader, single slot range for now)
func (s *Server) clusterInfo() string {
	state := "ok"
	return fmt.Sprintf("cluster_state:%s\ncluster_slots_assigned:%d\ncluster_slots_ok:%d\nleader:%s\n",
		state, s.slotCount, s.slotCount, s.currentLeader())
}

func (s *Server) clusterNodes() string {
	leader := s.currentLeader()
	return fmt.Sprintf("%s master - 0 0 connected\n", leader)
}

func (s *Server) clusterSlots() string {
	return s.cluster.SlotsInfo()
}
