package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	"fas/pkg/protocol"
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
		sendCommand(conn, os.Args[1:])
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
		cmdLine := strings.TrimSpace(scanner.Text())
		if cmdLine == "" {
			continue
		}
		if cmdLine == "exit" || cmdLine == "quit" {
			break
		}
		args := strings.Fields(cmdLine)
		sendCommand(conn, args)
	}
}

func sendCommand(conn net.Conn, args []string) {
	writer := protocol.NewWriter(conn)
	err := writer.WriteCommand(args)
	if err != nil {
		fmt.Printf("Error sending command: %v\n", err)
		return
	}

	reader := bufio.NewReader(conn)

	// If command is SUBSCRIBE, enter continuous loop
	if len(args) > 0 && strings.ToUpper(args[0]) == "SUBSCRIBE" {
		fmt.Println("Reading messages... (Press Ctrl+C to quit)")
		for {
			readAndPrintResponse(reader)
		}
	} else {
		// Single response for other commands
		readAndPrintResponse(reader)
	}
}

func readAndPrintResponse(reader *bufio.Reader) {
	text, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		os.Exit(1) // Exit on connection error
	}
	fmt.Print(text)

	// If it's a bulk string ($...), read the data line too
	if strings.HasPrefix(text, "$") {
		var length int
		if _, err := fmt.Sscanf(text, "$%d", &length); err != nil {
			fmt.Printf("Error parsing bulk string length: %v\n", err)
			os.Exit(1)
		}
		if length != -1 {
			data, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Error reading bulk data: %v\n", err)
				os.Exit(1)
			}
			fmt.Print(data)
		}
	}
}
