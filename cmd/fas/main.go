package main

import (
	"log"

	"fas/pkg/server"
)

func main() {
	log.Println("FAS: Fast and Accurate System v0.1.0")
	log.Println("Starting server...")

	// Configure the server
	cfg := server.Config{
		Host:    "0.0.0.0",
		Port:    6379,
		AOFPath: "fas.aof", // Enable persistence
	}

	// Initialize and start the server
	srv := server.NewServer(cfg)
	if err := srv.Start(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
