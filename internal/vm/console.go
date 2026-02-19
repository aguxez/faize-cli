//go:build darwin

package vm

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/Code-Hex/vz/v3"
	"golang.org/x/term"
)

// Console manages VM serial console I/O
type Console struct {
	read   *os.File
	write  *os.File
	mu     sync.Mutex
	done   chan struct{}
	closed bool
}

// createConsole creates a console and its VZ serial port configuration
func createConsole() (*Console, *vz.VirtioConsoleDeviceSerialPortConfiguration, error) {
	// Create pipes for console I/O
	// Guest writes to readPipe, we read from it
	readPipe, guestWrite, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}

	// We write to writePipe, guest reads from it
	guestRead, writePipe, err := os.Pipe()
	if err != nil {
		_ = readPipe.Close()
		_ = guestWrite.Close()
		return nil, nil, err
	}

	// Create file handle attachment
	attachment, err := vz.NewFileHandleSerialPortAttachment(guestRead, guestWrite)
	if err != nil {
		_ = readPipe.Close()
		_ = guestWrite.Close()
		_ = guestRead.Close()
		_ = writePipe.Close()
		return nil, nil, err
	}

	// Create serial port configuration
	serialConfig, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(attachment)
	if err != nil {
		_ = readPipe.Close()
		_ = guestWrite.Close()
		_ = guestRead.Close()
		_ = writePipe.Close()
		return nil, nil, err
	}

	console := &Console{
		read:  readPipe,
		write: writePipe,
		done:  make(chan struct{}),
	}

	return console, serialConfig, nil
}

// Attach connects stdin/stdout to the console with proper terminal handling
// NOTE: This method does NOT hold the mutex during the blocking select.
// This allows Detach() to be called from another goroutine (e.g., VM state change handler)
// to signal shutdown by closing c.done.
func (c *Console) Attach(stdin io.Reader, stdout io.Writer) error {
	// Check if stdin is a terminal and set raw mode
	stdinFd := int(os.Stdin.Fd())
	if term.IsTerminal(stdinFd) {
		// Save current terminal state and set raw mode
		oldState, err := term.MakeRaw(stdinFd)
		if err != nil {
			return fmt.Errorf("failed to set raw mode: %w", err)
		}
		// Restore terminal on exit
		defer func() { _ = term.Restore(stdinFd, oldState) }()
	}

	// Create EscapeWriter for detecting user escape sequences
	escapeWriter := NewEscapeWriter(c.write, stdout)

	// Create error channel to capture copy errors
	errCh := make(chan error, 2)

	// Copy from console to stdout (VM -> host)
	go func() {
		_, err := io.Copy(stdout, c.read)
		errCh <- err
	}()

	// Copy from stdin to console (host -> VM) with escape detection
	go func() {
		_, err := io.Copy(escapeWriter, stdin)
		errCh <- err
	}()

	// Wait for done signal, error, or user escape
	select {
	case <-c.done:
		return nil
	case <-escapeWriter.DetachChan():
		return ErrUserDetach
	case err := <-errCh:
		return err
	}
}

// Detach disconnects the console
func (c *Console) Detach() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil // Already detached
	}
	c.closed = true

	close(c.done)
	_ = c.read.Close()
	_ = c.write.Close()

	return nil
}

// Close closes the console resources
func (c *Console) Close() error {
	return c.Detach()
}
