package protocol

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Command represents a parsed command from the client.
type Command struct {
	Name string   // The command name (e.g., "SET", "GET")
	Args []string // The arguments for the command
}

// ParseCommand reads a RESP-encoded command from the reader.
// It expects a RESP Array of Bulk Strings.
func ParseCommand(reader *bufio.Reader) (*Command, error) {
	// Read the first byte to check type
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}

	// Check for Array start
	if line[0] != '*' {
		// Fallback to inline command (space separated) for backward compatibility or simple telnet usage
		parts := strings.Fields(line)
		if len(parts) == 0 {
			return nil, nil
		}
		return &Command{
			Name: strings.ToUpper(parts[0]),
			Args: parts[1:],
		}, nil
	}

	// Parse array length
	var argc int
	_, err = fmt.Sscanf(line, "*%d", &argc)
	if err != nil {
		return nil, fmt.Errorf("invalid array length: %v", err)
	}

	args := make([]string, 0, argc)
	for i := 0; i < argc; i++ {
		// Read bulk string header
		line, err = reader.ReadString('\n')
		if err != nil {
			return nil, err
		}

		var length int
		_, err = fmt.Sscanf(strings.TrimSpace(line), "$%d", &length)
		if err != nil {
			return nil, fmt.Errorf("invalid bulk string length: %v", err)
		}

		if length == -1 {
			args = append(args, "") // Handle null bulk string as empty
			continue
		}

		// Read data
		data := make([]byte, length+2) // +2 for \r\n
		_, err = io.ReadFull(reader, data)
		if err != nil {
			return nil, err
		}

		// Verify \r\n
		if data[length] != '\r' || data[length+1] != '\n' {
			return nil, fmt.Errorf("invalid bulk string termination")
		}

		args = append(args, string(data[:length]))
	}

	if len(args) == 0 {
		return nil, nil
	}

	return &Command{
		Name: strings.ToUpper(args[0]),
		Args: args[1:],
	}, nil
}
