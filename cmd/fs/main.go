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

	// Read response
	reader := bufio.NewReader(conn)
	// We can use a simplified reader here or just read lines for now since we are in CLI.
	// Ideally CLI should also parse RESP.
	// For now, let's just print what we get to verify RESP format is working.
	// Or better, implement a simple RESP reader for CLI.

	// Let's just read everything until we get a full response.
	// Since we don't have a full RESP parser in CLI yet (we only have it in server package),
	// we can reuse protocol.ParseCommand? No, ParseCommand returns *Command, but response is not a Command.
	// Response is +OK, $len\r\ndata, etc.

	// For this task, let's just read line by line and print.
	// The user will see raw RESP output which confirms the protocol change.
	// "OK" -> "+OK"
	// "val" -> "$3\r\nval\r\n"

	text, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		return
	}
	fmt.Print(text)

	// If it's a bulk string ($...), read the data line too
	if strings.HasPrefix(text, "$") {
		var length int
		fmt.Sscanf(text, "$%d", &length)
		if length != -1 {
			data, _ := reader.ReadString('\n')
			fmt.Print(data)
		}
	}
}
