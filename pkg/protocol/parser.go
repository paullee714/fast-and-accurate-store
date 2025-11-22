package protocol

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const (
	maxArgs        = 1024
	maxBulkLength  = 512 * 1024 // 512KB per bulk
	maxInlineBytes = 8 * 1024   // 8KB inline line
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
	if argc > maxArgs {
		return nil, fmt.Errorf("too many arguments")
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
		if length > maxBulkLength {
			return nil, fmt.Errorf("bulk string too large")
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

// ParseCommandFromBytes parses a command from a byte slice.
// It returns the command, the number of bytes consumed, and an error.
// If the buffer does not contain a full command, it returns nil, 0, nil.
func ParseCommandFromBytes(data []byte) (*Command, int, error) {
	if len(data) == 0 {
		return nil, 0, nil
	}

	switch data[0] {
	case '*':
		return parseRESPArray(data)
	default:
		return parseInline(data)
	}
}

func parseInline(data []byte) (*Command, int, error) {
	idx := indexCRLF(data)
	if idx < 0 {
		return nil, 0, nil
	}
	if idx > maxInlineBytes {
		return nil, 0, fmt.Errorf("inline command too long")
	}
	line := strings.TrimSpace(string(data[:idx]))
	if line == "" {
		return nil, idx + 2, nil
	}
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil, idx + 2, nil
	}
	return &Command{
		Name: strings.ToUpper(parts[0]),
		Args: parts[1:],
	}, idx + 2, nil
}

func parseRESPArray(data []byte) (*Command, int, error) {
	idx := indexCRLF(data)
	if idx < 0 {
		return nil, 0, nil
	}
	var count int
	if _, err := fmt.Sscanf(string(data[1:idx]), "%d", &count); err != nil {
		return nil, 0, fmt.Errorf("invalid array start: %v", err)
	}
	if count > maxArgs {
		return nil, 0, fmt.Errorf("too many arguments")
	}
	pos := idx + 2
	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		if pos >= len(data) || data[pos] != '$' {
			return nil, 0, nil
		}
		lenEnd := indexCRLF(data[pos:])
		if lenEnd < 0 {
			return nil, 0, nil
		}
		var blen int
		if _, err := fmt.Sscanf(string(data[pos+1:pos+lenEnd]), "%d", &blen); err != nil {
			return nil, 0, fmt.Errorf("invalid bulk length: %v", err)
		}
		if blen > maxBulkLength {
			return nil, 0, fmt.Errorf("bulk string too large")
		}
		pos += lenEnd + 2
		if blen == -1 {
			args = append(args, "")
			continue
		}
		if pos+blen+2 > len(data) {
			return nil, 0, nil
		}
		args = append(args, string(data[pos:pos+blen]))
		pos += blen + 2
	}
	if len(args) == 0 {
		return nil, pos, nil
	}
	return &Command{
		Name: strings.ToUpper(args[0]),
		Args: args[1:],
	}, pos, nil
}

func indexCRLF(b []byte) int {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == '\r' && b[i+1] == '\n' {
			return i
		}
	}
	return -1
}
