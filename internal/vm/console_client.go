//go:build darwin

package vm

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

const escapeHelp = "\r\nSupported escape sequences:\r\n  ~.  Disconnect from session (VM keeps running)\r\n  ~~  Send literal ~ character\r\n  ~?  Show this help\r\n"

// EscapeWriter wraps an io.Writer to detect SSH-style escape sequences.
// Detects ~. (detach), ~~ (literal ~), ~? (help) when ~ follows a newline.
//
// EscapeWriter is not safe for concurrent use from multiple goroutines.
// It expects sequential Write() calls from a single source (stdin).
type EscapeWriter struct {
	w             io.Writer     // underlying writer to forward bytes to
	afterNewline  bool          // true if last byte was newline or at start
	pendingTilde  bool          // true if we saw ~ and waiting for next char
	detachCh      chan struct{} // closed when ~. detected
	stdout        io.Writer     // for printing help message
}

// NewEscapeWriter creates a new EscapeWriter that wraps w
func NewEscapeWriter(w io.Writer, stdout io.Writer) *EscapeWriter {
	return &EscapeWriter{
		w:            w,
		afterNewline: true, // treat start as after newline
		detachCh:     make(chan struct{}),
		stdout:       stdout,
	}
}

// Write processes input bytes and detects escape sequences
func (e *EscapeWriter) Write(p []byte) (n int, err error) {
	for _, b := range p {
		// Check for newline characters
		if b == 0x0a || b == 0x0d {
			if e.pendingTilde {
				// Write the pending tilde before the newline
				if _, err := e.w.Write([]byte{'~'}); err != nil {
					return len(p), err
				}
				e.pendingTilde = false
			}
			if _, err := e.w.Write([]byte{b}); err != nil {
				return len(p), err
			}
			e.afterNewline = true
			continue
		}

		// Detect tilde after newline
		if e.afterNewline && b == 0x7e {
			e.pendingTilde = true
			e.afterNewline = false
			continue
		}

		// Process pending tilde
		if e.pendingTilde {
			e.pendingTilde = false
			switch b {
			case 0x2e: // '.' - detach
				close(e.detachCh)
				return len(p), nil
			case 0x7e: // '~' - literal tilde
				if _, err := e.w.Write([]byte{'~'}); err != nil {
					return len(p), err
				}
			case 0x3f: // '?' - help
				if _, err := e.stdout.Write([]byte(escapeHelp)); err != nil {
					return len(p), err
				}
			default: // any other byte - write pending tilde + this byte
				if _, err := e.w.Write([]byte{'~', b}); err != nil {
					return len(p), err
				}
			}
			e.afterNewline = false
			continue
		}

		// Normal byte - write it
		if _, err := e.w.Write([]byte{b}); err != nil {
			return len(p), err
		}
		e.afterNewline = false
	}

	return len(p), nil
}

// DetachChan returns a channel that is closed when ~. is detected
func (e *EscapeWriter) DetachChan() chan struct{} {
	return e.detachCh
}

// ConsoleClient manages connection to a VM console via Unix socket
type ConsoleClient struct {
	conn         net.Conn
	termsizePath string
	clipboardDir string
}

// SetTermsizePath sets the path to the termsize file used for propagating
// terminal resize events to the VM guest via VirtioFS.
func (c *ConsoleClient) SetTermsizePath(path string) {
	c.termsizePath = path
}

// SetClipboardDir sets the path to the clipboard directory used for syncing
// host clipboard contents to the VM guest via VirtioFS.
func (c *ConsoleClient) SetClipboardDir(path string) {
	c.clipboardDir = path
}

// NewConsoleClient connects to a VM console Unix socket
func NewConsoleClient(socketPath string) (*ConsoleClient, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to console socket: %w", err)
	}

	return &ConsoleClient{
		conn: conn,
	}, nil
}

// Attach connects stdin/stdout to the console socket with proper terminal handling
func (c *ConsoleClient) Attach(stdin io.Reader, stdout io.Writer) error {
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

	// Start terminal resize handler to propagate SIGWINCH to guest
	if c.termsizePath != "" && term.IsTerminal(stdinFd) {
		sigwinch := make(chan os.Signal, 1)
		signal.Notify(sigwinch, syscall.SIGWINCH)
		go func() {
			for range sigwinch {
				w, h, err := term.GetSize(stdinFd)
				if err == nil && w > 0 && h > 0 {
					os.WriteFile(c.termsizePath, []byte(fmt.Sprintf("%d %d", w, h)), 0644)
				}
			}
		}()
		defer signal.Stop(sigwinch)
	}

	// Check for immediate error response from proxy (e.g., "already attached")
	// Set short deadline for initial check
	c.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	initialBuf := make([]byte, 64)
	n, err := c.conn.Read(initialBuf)
	c.conn.SetReadDeadline(time.Time{}) // Clear deadline

	if n > 0 {
		msg := string(initialBuf[:n])
		if strings.HasPrefix(msg, "ERROR:") {
			return fmt.Errorf("%s", strings.TrimSpace(msg))
		}
		// Not an error - it's VM output, write it through
		stdout.Write(initialBuf[:n])
	}
	// If err is timeout, that's expected - no immediate data, proceed normally
	if err != nil && !os.IsTimeout(err) && err != io.EOF {
		return fmt.Errorf("failed to read from console: %w", err)
	}

	// Create escape writer for detecting ~. sequence
	escapeWriter := NewEscapeWriter(c.conn, stdout)

	// Create error channel to capture copy errors
	errCh := make(chan error, 2)

	// Copy from socket to stdout (VM -> host)
	go func() {
		_, err := io.Copy(stdout, c.conn)
		errCh <- err
	}()

	// Copy from stdin to socket (host -> VM) with clipboard sync and escape detection
	go func() {
		var stdinWriter io.Writer = escapeWriter
		if c.clipboardDir != "" {
			stdinWriter = NewClipboardWriter(escapeWriter, c.clipboardDir)
		}
		_, err := io.Copy(stdinWriter, stdin)
		errCh <- err
	}()

	// Wait for escape sequence or error
	select {
	case <-escapeWriter.DetachChan():
		return ErrUserDetach
	case err := <-errCh:
		return err
	}
}

// Close closes the console socket connection
func (c *ConsoleClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
