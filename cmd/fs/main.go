package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"fas/pkg/protocol"
)

func main() {
	host := flag.String("host", "127.0.0.1", "Server host")
	port := flag.String("port", "6379", "Server port")
	useTLS := flag.Bool("tls", false, "Enable TLS (insecure, skips verify)")
	flag.Parse()

	addr := net.JoinHostPort(*host, *port)
	var conn net.Conn
	var err error
	if *useTLS {
		conn, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	} else {
		conn, err = net.DialTimeout("tcp", addr, 5*time.Second)
	}
	if err != nil {
		fmt.Printf("Error connecting to server: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	args := flag.Args()
	// If arguments are provided, execute single command
	if len(args) > 0 {
		sendCommand(conn, args)
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
	// Optional: pre-auth if env FAS_PASSWORD is set
	if pass := os.Getenv("FAS_PASSWORD"); pass != "" {
		_ = writer.WriteCommand([]string{"AUTH", pass})
		reader := bufio.NewReader(conn)
		readAndPrintResponse(reader)
	}

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
			data := make([]byte, length+2)
			if _, err := reader.Read(data); err != nil {
				fmt.Printf("Error reading bulk data: %v\n", err)
				os.Exit(1)
			}
			fmt.Print(string(data))
		}
	}
}
