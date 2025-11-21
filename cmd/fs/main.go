package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

const (
	DefaultHost = "localhost"
	DefaultPort = "6379"
)

func main() {
	addr := net.JoinHostPort(DefaultHost, DefaultPort)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Printf("Error connecting to server: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// If arguments are provided, execute single command
	if len(os.Args) > 1 {
		cmd := strings.Join(os.Args[1:], " ")
		sendCommand(conn, cmd)
		return
	}

	// Interactive mode
	fmt.Println("fs-cli v0.1.0")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("fs> ")
		if !scanner.Scan() {
			break
		}
		cmd := strings.TrimSpace(scanner.Text())
		if cmd == "" {
			continue
		}
		if cmd == "exit" || cmd == "quit" {
			break
		}
		sendCommand(conn, cmd)
	}
}

func sendCommand(conn net.Conn, cmd string) {
	_, err := fmt.Fprintf(conn, "%s\n", cmd)
	if err != nil {
		fmt.Printf("Error sending command: %v\n", err)
		return
	}

	// Read response
	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		return
	}
	fmt.Print(response)
}
