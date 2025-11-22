package persistence

import (
	"bufio"
	"io"
	"os"
	"sync"

	"fas/pkg/protocol"
)

// AOF (Append Only File) manages the persistence of commands.
type AOF struct {
	file *os.File
	mu   sync.Mutex
}

// NewAOF creates a new AOF instance.
// It opens the specified file for writing (appending) and reading.
func NewAOF(path string) (*AOF, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}

	return &AOF{
		file: file,
	}, nil
}

// Close closes the AOF file.
func (aof *AOF) Close() error {
	aof.mu.Lock()
	defer aof.mu.Unlock()
	return aof.file.Close()
}

// Write writes a command to the AOF file.
// The format is RESP.
func (aof *AOF) Write(cmd *protocol.Command) error {
	aof.mu.Lock()
	defer aof.mu.Unlock()

	// Reconstruct the command arguments including the name
	args := append([]string{cmd.Name}, cmd.Args...)

	writer := protocol.NewWriter(aof.file)
	return writer.WriteCommand(args)
}

// ReadCommands reads all commands from the AOF file.
// This is used during server startup to restore the state.
func (aof *AOF) ReadCommands(callback func(*protocol.Command)) error {
	aof.mu.Lock()
	defer aof.mu.Unlock()

	// Seek to the beginning of the file
	_, err := aof.file.Seek(0, 0)
	if err != nil {
		return err
	}

	reader := bufio.NewReader(aof.file)
	for {
		cmd, err := protocol.ParseCommand(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if cmd != nil {
			callback(cmd)
		}
	}

	return nil
}
