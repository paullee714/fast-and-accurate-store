package main

import (
	"flag"
	"log"
	"strings"

	"fas/pkg/persistence"
	"fas/pkg/server"
)

func main() {
	host := flag.String("host", "localhost", "Host to listen on")
	port := flag.Int("port", 6379, "Port to listen on")
	aofPath := flag.String("aof", "fas.aof", "Path to AOF file")
	fsync := flag.String("fsync", "everysec", "Fsync policy: always, everysec, no")
	useEventLoop := flag.Bool("eventloop", false, "Use single-threaded event loop (macOS only)")
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

	// Configure the server
	config := server.Config{
		Host:        *host,
		Port:        *port,
		AOFPath:     *aofPath,
		FsyncPolicy: policy,
	}

	// Initialize and start the server
	srv := server.NewServer(config)

	var err error
	if *useEventLoop {
		log.Println("Using Single Threaded Event Loop (Netpoll)")
		err = srv.StartEventLoop()
	} else {
		log.Println("Using Standard Goroutine-per-Connection")
		err = srv.Start()
	}

	if err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
