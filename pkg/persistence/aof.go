package persistence

import (
	"bufio"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"fas/pkg/protocol"
)

// AOF (Append Only File) manages the persistence of commands.
type FsyncPolicy int

const (
	FsyncAlways FsyncPolicy = iota
	FsyncEverySec
	FsyncNo
)

type AOF struct {
	file   *os.File
	writer *bufio.Writer
	mu     sync.Mutex
	policy FsyncPolicy
	done   chan struct{}
}

// NewAOF creates a new AOF instance.
// It opens the specified file for writing (appending) and reading.
func NewAOF(path string, policy FsyncPolicy) (*AOF, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}

	aof := &AOF{
		file:   file,
		writer: bufio.NewWriter(file),
		policy: policy,
		done:   make(chan struct{}),
	}

	if policy == FsyncEverySec {
		go aof.backgroundSync()
	}

	return aof, nil
}

// Close closes the AOF file and stops background sync.
func (aof *AOF) Close() error {
	if aof.policy == FsyncEverySec {
		close(aof.done)
	}
	aof.mu.Lock()
	defer aof.mu.Unlock()

	if err := aof.writer.Flush(); err != nil {
		return err
	}
	return aof.file.Close()
}

// Write writes a command to the AOF file.
// The format is RESP.
func (aof *AOF) Write(cmd *protocol.Command) error {
	aof.mu.Lock()
	defer aof.mu.Unlock()

	// Reconstruct the command arguments including the name
	args := append([]string{cmd.Name}, cmd.Args...)

	writer := protocol.NewWriter(aof.writer)
	if err := writer.WriteCommand(args); err != nil {
		return err
	}

	// Always flush to OS buffer to reduce durability window
	if err := aof.writer.Flush(); err != nil {
		log.Printf("AOF flush error: %v", err)
		return err
	}

	if aof.policy == FsyncAlways {
		if err := aof.file.Sync(); err != nil {
			log.Printf("AOF sync error: %v", err)
			return err
		}
		return nil
	}

	// For EverySec and No, we just flush to buffer (bufio handles it),
	// but we might want to explicitly flush to OS buffer occasionally?
	// bufio flushes when full.
	// For EverySec, we need to ensure data reaches OS buffer so fsync works?
	// Actually bufio writes to fd. fsync syncs fd to disk.
	// So we need to Flush bufio before fsync.

	return nil
}

func (aof *AOF) backgroundSync() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			aof.mu.Lock()
			if err := aof.writer.Flush(); err != nil {
				log.Printf("AOF background flush error: %v", err)
			}
			if err := aof.file.Sync(); err != nil {
				log.Printf("AOF background sync error: %v", err)
			}
			aof.mu.Unlock()
		case <-aof.done:
			return
		}
	}
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
