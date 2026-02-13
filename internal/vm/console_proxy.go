//go:build darwin

package vm

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// ConsoleProxyServer manages a Unix socket proxy for VM console access.
// Only one client can be attached at a time. The proxy uses a single reader
// goroutine that broadcasts console output to the current client, avoiding
// the issue of orphaned io.Copy goroutines competing for console data.
type ConsoleProxyServer struct {
	socketPath string
	listener   net.Listener
	console    *Console
	mu         sync.Mutex
	done       chan struct{}
	wg         sync.WaitGroup

	// Current client connection (nil if no client attached)
	currentClient net.Conn
	clientMu      sync.RWMutex
}

// NewConsoleProxyServer creates a new console proxy server
func NewConsoleProxyServer(sessionID string, console *Console) (*ConsoleProxyServer, error) {
	// Create socket directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	socketDir := filepath.Join(homeDir, ".faize", "sessions")
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %w", err)
	}

	socketPath := filepath.Join(socketDir, fmt.Sprintf("%s.sock", sessionID))

	// Remove existing socket file if present
	os.Remove(socketPath)

	return &ConsoleProxyServer{
		socketPath: socketPath,
		console:    console,
		done:       make(chan struct{}),
	}, nil
}

// Start begins accepting connections on the Unix socket
func (s *ConsoleProxyServer) Start() error {
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to create Unix socket listener: %w", err)
	}

	s.listener = listener
	debugLog("Console proxy listening on %s", s.socketPath)

	// Start the single console reader that broadcasts to current client
	s.wg.Add(1)
	go s.consoleReaderLoop()

	// Start console EOF monitor
	s.wg.Add(1)
	go s.monitorConsoleEOF()

	// Accept connections
	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// consoleReaderLoop is the single goroutine that reads from the console
// and writes to the current client. This ensures only one reader exists
// for the console pipe, preventing data loss when clients disconnect/reconnect.
func (s *ConsoleProxyServer) consoleReaderLoop() {
	defer s.wg.Done()

	buf := make([]byte, 4096)
	for {
		select {
		case <-s.done:
			return
		default:
		}

		// Read from console (this blocks until data available or EOF)
		n, err := s.console.read.Read(buf)
		if err != nil {
			if err != io.EOF {
				debugLog("Console read error: %v", err)
			}
			return
		}

		if n > 0 {
			// Write to current client if one is connected
			s.clientMu.RLock()
			client := s.currentClient
			s.clientMu.RUnlock()

			if client != nil {
				_, writeErr := client.Write(buf[:n])
				if writeErr != nil {
					// Client disconnected, will be cleaned up by handleClient
					debugLog("Client write error (client likely disconnected): %v", writeErr)
				}
			}
			// If no client connected, data is discarded (expected behavior for detached state)
		}
	}
}

// acceptLoop accepts new client connections
func (s *ConsoleProxyServer) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				// Normal shutdown
				return
			default:
				debugLog("Accept error: %v", err)
				continue
			}
		}

		// Check if we already have a client
		s.clientMu.Lock()
		if s.currentClient != nil {
			// Reject the connection - already have an active client
			conn.Write([]byte("ERROR: session already attached\n"))
			conn.Close()
			s.clientMu.Unlock()
			debugLog("Rejected connection - session already attached")
			continue
		}

		// Accept this client
		s.currentClient = conn
		s.clientMu.Unlock()

		debugLog("Client connected to console proxy")

		// Handle this client's input (client -> console direction)
		s.wg.Add(1)
		go s.handleClientInput(conn)
	}
}

// handleClientInput handles input from a connected client to the console.
// The console -> client direction is handled by consoleReaderLoop.
func (s *ConsoleProxyServer) handleClientInput(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		// Clear current client
		s.clientMu.Lock()
		if s.currentClient == conn {
			s.currentClient = nil
		}
		s.clientMu.Unlock()

		conn.Close()
		debugLog("Client disconnected from console proxy")
	}()

	// Copy client input to console (client -> VM)
	// This will return when:
	// 1. Client disconnects (EOF)
	// 2. Console pipe closes (VM stopped)
	// 3. Connection is closed externally
	_, err := io.Copy(s.console.write, conn)
	if err != nil && err != io.EOF {
		debugLog("Client input relay error: %v", err)
	}
}

// monitorConsoleEOF watches for console EOF (VM stopped) and shuts down the proxy
func (s *ConsoleProxyServer) monitorConsoleEOF() {
	defer s.wg.Done()

	// Wait for console to be detached/closed
	<-s.console.done

	debugLog("Console EOF detected - shutting down proxy")
	s.Stop()
}

// Stop closes all connections and removes the socket file
func (s *ConsoleProxyServer) Stop() error {
	s.mu.Lock()

	// Check if already stopped
	select {
	case <-s.done:
		s.mu.Unlock()
		return nil
	default:
	}

	// Signal shutdown
	close(s.done)

	// Close listener
	if s.listener != nil {
		s.listener.Close()
	}

	// Close current client if any
	s.clientMu.Lock()
	if s.currentClient != nil {
		s.currentClient.Close()
		s.currentClient = nil
	}
	s.clientMu.Unlock()

	s.mu.Unlock()

	// Wait for all goroutines to finish
	s.wg.Wait()

	// Remove socket file
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		debugLog("Failed to remove socket file: %v", err)
	}

	debugLog("Console proxy stopped")
	return nil
}

// SocketPath returns the Unix socket path
func (s *ConsoleProxyServer) SocketPath() string {
	return s.socketPath
}
