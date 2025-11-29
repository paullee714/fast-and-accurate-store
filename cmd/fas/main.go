package main

import (
	"flag"
	"log"
	"net/netip"
	"strings"

	"fas/pkg/persistence"
	"fas/pkg/server"
)

func main() {
	host := flag.String("host", "localhost", "Host to listen on")
	port := flag.Int("port", 6379, "Port to listen on")
	aofPath := flag.String("aof", "fas.aof", "Path to AOF file")
	fsync := flag.String("fsync", "everysec", "Fsync policy: always, everysec, no")
	useEventLoop := flag.Bool("eventloop", false, "Use single-threaded event loop (macOS/Linux only)")
	maxMemory := flag.Int64("maxmemory", 1024*1024*1024, "Max memory in bytes (default 1GB)")
	eviction := flag.String("maxmemory-policy", "allkeys-random", "Eviction policy: noeject, allkeys-random, volatile-random, allkeys-lru, allkeys-lfu")
	authEnabled := flag.Bool("auth", false, "Require AUTH for clients")
	password := flag.String("requirepass", "", "Password for AUTH (required if -auth is true)")
	tlsCert := flag.String("tls-cert", "", "Path to TLS certificate (PEM)")
	tlsKey := flag.String("tls-key", "", "Path to TLS private key (PEM)")
	allowCIDR := flag.String("allow-cidr", "", "Comma-separated CIDR list to allow (optional)")
	rdbPath := flag.String("rdb", "", "Path to snapshot (RDB) file (optional)")
	maxClients := flag.Int("maxclients", 0, "Maximum concurrent clients (0 = unlimited)")
	configFile := flag.String("config", "", "Path to config file (key=value)")
	metricsPort := flag.Int("metrics-port", 0, "Expose Prometheus-style metrics on this port (0=disabled)")
	seed := flag.String("seed", "", "Seed master address (host:port) to auto-join as replica")
	seedTLS := flag.Bool("seed-tls", false, "Use TLS to connect to seed (insecure)")
	seedPass := flag.String("seed-pass", "", "Password for seed AUTH (if seed requires auth)")
	slotRanges := flag.String("cluster-slot-ranges", "", "Static slot ranges mapping (e.g., 0-5000:1.1.1.1:6379,5001-16383:2.2.2.2:6379)")
	flag.Parse()

	log.Println("FAS: Fast and Accurate System v0.1.0")
	log.Println("Starting server...")

	var policy persistence.FsyncPolicy
	switch strings.ToLower(*fsync) {
	case "always":
		policy = persistence.FsyncAlways
	case "everysec":
		policy = persistence.FsyncEverySec
	case "no":
		policy = persistence.FsyncNo
	default:
		log.Fatalf("Invalid fsync policy: %s. Must be one of: always, everysec, no", *fsync)
	}

	evictPolicy, err := server.ParseEvictionPolicy(*eviction)
	if err != nil {
		log.Fatalf("Invalid maxmemory-policy: %v", err)
	}

	// Configure the server
	config := server.Config{
		Host:          *host,
		Port:          *port,
		AOFPath:       *aofPath,
		FsyncPolicy:   policy,
		MaxMemory:     *maxMemory,
		Eviction:      evictPolicy,
		AuthEnabled:   *authEnabled,
		Password:      *password,
		TLSCertPath:   *tlsCert,
		TLSKeyPath:    *tlsKey,
		RDBPath:       *rdbPath,
		MaxClients:    *maxClients,
		MetricsPort:   *metricsPort,
		ReplicaOf:     *seed,
		ReplicaUseTLS: *seedTLS,
		ReplicaPass:   *seedPass,
	}

	if *allowCIDR != "" {
		for _, part := range strings.Split(*allowCIDR, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			prefix, err := netip.ParsePrefix(part)
			if err != nil {
				log.Fatalf("Invalid CIDR %s: %v", part, err)
			}
			config.AllowedCIDR = append(config.AllowedCIDR, prefix)
		}
	}

	// Apply config file overrides if provided
	if *configFile != "" {
		if err := server.LoadConfigFile(*configFile, &config); err != nil {
			log.Fatalf("Failed to load config file: %v", err)
		}
	}

	// Slot ranges override if provided
	if *slotRanges != "" {
		ranges, err := server.ParseSlotRanges(*slotRanges)
		if err != nil {
			log.Fatalf("Invalid cluster-slot-ranges: %v", err)
		}
		config.StaticSlots = ranges
	}

	// Initialize and start the server
	if err := config.ValidateAndFill(); err != nil {
		log.Fatalf("Invalid config: %v", err)
	}
	srv := server.NewServer(config)

	if *useEventLoop {
		log.Println("Using Single Threaded Event Loop (Netpoll)")
		if err := srv.StartEventLoop(); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	} else {
		log.Println("Using Standard Goroutine-per-Connection")
		if err := srv.Start(); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}
}
