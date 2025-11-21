package protocol

import (
	"bufio"
	"strings"
)

// Command represents a parsed command from the client.
type Command struct {
	Name string   // The command name (e.g., "SET", "GET")
	Args []string // The arguments for the command
}

// ParseCommand reads a line from the reader and parses it into a Command.
// It returns nil, nil if the line is empty.
func ParseCommand(reader *bufio.Reader) (*Command, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil // Empty line, ignore
	}

	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil, nil
	}

	return &Command{
		Name: strings.ToUpper(parts[0]),
		Args: parts[1:],
	}, nil
}
