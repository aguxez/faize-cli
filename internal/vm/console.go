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
	read  *os.File
	write *os.File
	mu    sync.Mutex
	done  chan struct{}
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
		readPipe.Close()
		guestWrite.Close()
		return nil, nil, err
	}

	// Create file handle attachment
	attachment, err := vz.NewFileHandleSerialPortAttachment(guestRead, guestWrite)
	if err != nil {
		readPipe.Close()
		guestWrite.Close()
		guestRead.Close()
		writePipe.Close()
		return nil, nil, err
	}

	// Create serial port configuration
	serialConfig, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(attachment)
	if err != nil {
		readPipe.Close()
		guestWrite.Close()
		guestRead.Close()
		writePipe.Close()
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
func (c *Console) Attach(stdin io.Reader, stdout io.Writer) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if stdin is a terminal and set raw mode
	stdinFd := int(os.Stdin.Fd())
	if term.IsTerminal(stdinFd) {
		// Save current terminal state and set raw mode
		oldState, err := term.MakeRaw(stdinFd)
		if err != nil {
			return fmt.Errorf("failed to set raw mode: %w", err)
		}
		// Restore terminal on exit
		defer term.Restore(stdinFd, oldState)
	}

	// Create error channel to capture copy errors
	errCh := make(chan error, 2)

	// Copy from console to stdout (VM -> host)
	go func() {
		_, err := io.Copy(stdout, c.read)
		errCh <- err
	}()

	// Copy from stdin to console (host -> VM)
	go func() {
		_, err := io.Copy(c.write, stdin)
		errCh <- err
	}()

	// Wait for done signal or error
	select {
	case <-c.done:
		return nil
	case err := <-errCh:
		return err
	}
}

// Detach disconnects the console
func (c *Console) Detach() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	close(c.done)
	c.read.Close()
	c.write.Close()

	return nil
}

// Close closes the console resources
func (c *Console) Close() error {
	return c.Detach()
}
