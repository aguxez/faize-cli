//go:build darwin

package vm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Code-Hex/vz/v3"
	"github.com/faize-ai/faize/internal/artifacts"
	"github.com/faize-ai/faize/internal/guest"
	"github.com/faize-ai/faize/internal/session"
	"github.com/google/uuid"
	"golang.org/x/term"
)

func debugLog(format string, args ...interface{}) {
	if os.Getenv("FAIZE_DEBUG") == "1" {
		fmt.Printf("[DEBUG:VM] "+format+"\n", args...)
	}
}

// captureVZLogs captures recent macOS Virtualization.framework logs
func captureVZLogs() {
	debugLog("Capturing VZ Framework logs...")
	cmd := exec.Command("log", "show", "--predicate",
		"subsystem == 'com.apple.Virtualization'",
		"--last", "30s", "--style", "compact")
	output, err := cmd.CombinedOutput()
	if err != nil {
		debugLog("Failed to capture VZ logs: %v", err)
		return
	}
	if len(output) > 0 {
		debugLog("VZ Framework logs:\n%s", string(output))
	} else {
		debugLog("No VZ Framework logs found in last 30s")
	}
}

// validateKernelFile checks if the kernel is a valid ELF or ARM64 Image file
func validateKernelFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot open kernel: %w", err)
	}
	defer f.Close()

	// Read first 64 bytes for header detection
	header := make([]byte, 64)
	n, err := f.Read(header)
	if err != nil || n < 4 {
		return fmt.Errorf("cannot read kernel header: %w", err)
	}

	// Check ELF magic bytes: 0x7F 'E' 'L' 'F'
	if header[0] == 0x7F && header[1] == 'E' && header[2] == 'L' && header[3] == 'F' {
		debugLog("Kernel format: ELF (magic: %x)", header[:4])
		return nil
	}

	// Check ARM64 Linux Image format
	// ARM64 Image files start with executable code, and have "ARM\x64" at offset 56
	if n >= 60 && header[56] == 'A' && header[57] == 'R' && header[58] == 'M' && header[59] == 0x64 {
		debugLog("Kernel format: ARM64 Image (magic at 56: ARM\\x64)")
		return nil
	}

	// Also accept if file starts with ARM64 instruction (common for Image format)
	// The first instruction is typically a branch: 0x14xxxxxx or similar
	// Or NOP-like: 0xd503201f or similar (which includes 0x1f2003d5 little-endian)
	if header[3] == 0x14 || header[3] == 0xd5 {
		debugLog("Kernel format: ARM64 Image (starts with ARM64 instruction: %x)", header[:4])
		return nil
	}

	return fmt.Errorf("kernel is not a valid ELF or ARM64 Image file (header: %x)", header[:8])
}

// validateRootfs checks if the rootfs has valid ext4 superblock
func validateRootfs(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot open rootfs: %w", err)
	}
	defer f.Close()

	// ext4 superblock is at offset 1024, magic is at offset 0x38 (56) within superblock
	// Total offset: 1024 + 56 = 1080
	if _, err := f.Seek(1080, 0); err != nil {
		return fmt.Errorf("cannot seek to ext4 magic: %w", err)
	}

	magic := make([]byte, 2)
	if _, err := f.Read(magic); err != nil {
		return fmt.Errorf("cannot read ext4 magic: %w", err)
	}

	// ext4 magic is 0xEF53 (little-endian: 0x53 0xEF)
	if magic[0] != 0x53 || magic[1] != 0xEF {
		return fmt.Errorf("rootfs is not valid ext4 (magic: %x)", magic)
	}

	debugLog("Rootfs ext4 magic validated: %x", magic)
	return nil
}

// VZManager implements Manager using Apple's Virtualization.framework
type VZManager struct {
	sessions  *session.Store
	artifacts *artifacts.Manager
	vms       map[string]*vz.VirtualMachine
	consoles  map[string]*Console
	proxies   map[string]*ConsoleProxyServer
	mu        sync.RWMutex
}

// NewVZManager creates a new VZ-based VM manager
func NewVZManager() (*VZManager, error) {
	store, err := session.NewStore()
	if err != nil {
		return nil, fmt.Errorf("failed to create session store: %w", err)
	}

	artifactMgr, err := artifacts.NewManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create artifact manager: %w", err)
	}

	return &VZManager{
		sessions:  store,
		artifacts: artifactMgr,
		vms:       make(map[string]*vz.VirtualMachine),
		consoles:  make(map[string]*Console),
		proxies:   make(map[string]*ConsoleProxyServer),
	}, nil
}

// Create creates a new VM session
func (m *VZManager) Create(cfg *Config) (*session.Session, error) {
	// Ensure artifacts are downloaded
	debugLog("Ensuring artifacts...")
	if cfg.ClaudeMode {
		if err := m.artifacts.EnsureClaudeRootfs(); err != nil {
			return nil, fmt.Errorf("failed to ensure claude rootfs: %w", err)
		}
		if err := m.artifacts.EnsureToolchainDir(); err != nil {
			return nil, fmt.Errorf("failed to ensure toolchain dir: %w", err)
		}
		if cfg.CredentialsDir != "" {
			if err := m.artifacts.EnsureCredentialsDir(); err != nil {
				return nil, fmt.Errorf("failed to ensure credentials dir: %w", err)
			}
		}
	} else {
		if err := m.artifacts.EnsureArtifacts(); err != nil {
			return nil, fmt.Errorf("failed to ensure artifacts: %w", err)
		}
	}

	// Generate session ID
	id := uuid.New().String()[:8]
	debugLog("Session ID: %s", id)

	// Create bootstrap directory for init script
	bootstrapDir := filepath.Join(m.artifacts.SessionDir(id), "bootstrap")
	if err := os.MkdirAll(bootstrapDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create bootstrap directory: %w", err)
	}

	// Generate init script
	var initScript string
	if cfg.ClaudeMode {
		initScript = guest.GenerateClaudeInitScript(cfg.Mounts, cfg.ProjectDir, cfg.NetworkPolicy, cfg.CredentialsDir != "", cfg.ExtraDeps)
	} else {
		initScript = guest.GenerateInitScript(cfg.Mounts, cfg.ProjectDir)
	}

	// Write init script to bootstrap directory
	initScriptPath := filepath.Join(bootstrapDir, "init.sh")
	if err := os.WriteFile(initScriptPath, []byte(initScript), 0755); err != nil {
		return nil, fmt.Errorf("failed to write init script: %w", err)
	}

	// Write host time to bootstrap directory for guest clock sync
	hostTime := time.Now().Unix()
	hostTimePath := filepath.Join(bootstrapDir, "hosttime")
	if err := os.WriteFile(hostTimePath, []byte(fmt.Sprintf("%d", hostTime)), 0644); err != nil {
		return nil, fmt.Errorf("failed to write host time: %w", err)
	}

	// Write terminal size to bootstrap directory for guest terminal setup
	if term.IsTerminal(int(os.Stdout.Fd())) {
		width, height, err := term.GetSize(int(os.Stdout.Fd()))
		if err == nil && width > 0 && height > 0 {
			termSizePath := filepath.Join(bootstrapDir, "termsize")
			if err := os.WriteFile(termSizePath, []byte(fmt.Sprintf("%d %d", width, height)), 0644); err != nil {
				debugLog("Failed to write terminal size: %v", err)
			}
		}
	}

	// Write debug flag to bootstrap directory if debug mode is enabled
	if os.Getenv("FAIZE_DEBUG") == "1" {
		debugPath := filepath.Join(bootstrapDir, "debug")
		if err := os.WriteFile(debugPath, []byte("1"), 0644); err != nil {
			debugLog("Failed to write debug flag: %v", err)
		}
	}

	// Create bootstrap mount and prepend to mounts list
	bootstrapMount := session.VMMount{
		Source:   bootstrapDir,
		Target:   "/mnt/bootstrap",
		Tag:      "faize-bootstrap",
		ReadOnly: false,
	}
	allMounts := append([]session.VMMount{bootstrapMount}, cfg.Mounts...)

	// Add Claude mode specific mounts
	if cfg.ClaudeMode {
		// Add host-claude mount
		if cfg.HostClaudeDir != "" {
			claudeMount := session.VMMount{
				Source:   cfg.HostClaudeDir,
				Target:   "/mnt/host-claude",
				Tag:      "host-claude",
				ReadOnly: true,
			}
			allMounts = append(allMounts, claudeMount)
		}

		// Add toolchain mount
		if cfg.ToolchainDir != "" {
			toolchainMount := session.VMMount{
				Source:   cfg.ToolchainDir,
				Target:   "/opt/toolchain",
				Tag:      "toolchain",
				ReadOnly: false,
			}
			allMounts = append(allMounts, toolchainMount)
		}

		// Add credentials mount
		if cfg.CredentialsDir != "" {
			credentialsMount := session.VMMount{
				Source:   cfg.CredentialsDir,
				Target:   "/mnt/host-credentials",
				Tag:      "credentials",
				ReadOnly: false,
			}
			allMounts = append(allMounts, credentialsMount)
		}
	}

	// Create Linux boot loader
	kernelPath := m.artifacts.KernelPath()
	debugLog("Kernel path: %s", kernelPath)

	// Check kernel file
	if info, err := os.Stat(kernelPath); err != nil {
		debugLog("Kernel file error: %v", err)
	} else {
		debugLog("Kernel file size: %d bytes", info.Size())
	}

	cmdLine := "console=hvc0 root=/dev/vda ro rootwait init=/init"
	if os.Getenv("FAIZE_DEBUG") != "1" {
		cmdLine += " quiet loglevel=0"
	}
	debugLog("Kernel command line: %s", cmdLine)

	bootLoader, err := vz.NewLinuxBootLoader(
		kernelPath,
		vz.WithCommandLine(cmdLine),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create boot loader: %w", err)
	}
	debugLog("Boot loader created")

	// Create VM configuration
	memBytes := parseMemory(cfg.Memory)
	debugLog("VM config: CPUs=%d, Memory=%d bytes (%s)", cfg.CPUs, memBytes, cfg.Memory)

	vmConfig, err := vz.NewVirtualMachineConfiguration(
		bootLoader,
		uint(cfg.CPUs),
		memBytes,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM config: %w", err)
	}
	debugLog("VM configuration created")

	// IMPORTANT: Device configuration order matters for VZ framework
	// Configure entropy device FIRST (required by macOS 12+)
	debugLog("Configuring entropy device (first)...")
	entropyDevice, err := vz.NewVirtioEntropyDeviceConfiguration()
	if err != nil {
		return nil, fmt.Errorf("failed to create entropy device: %w", err)
	}
	vmConfig.SetEntropyDevicesVirtualMachineConfiguration([]*vz.VirtioEntropyDeviceConfiguration{entropyDevice})

	// Configure rootfs disk
	var rootfsPath string
	if cfg.ClaudeMode {
		rootfsPath = m.artifacts.ClaudeRootfsPath()
	} else {
		rootfsPath = m.artifacts.RootfsPath()
	}
	debugLog("Rootfs path: %s", rootfsPath)

	// Check rootfs file
	if info, err := os.Stat(rootfsPath); err != nil {
		debugLog("Rootfs file error: %v", err)
	} else {
		debugLog("Rootfs file size: %d bytes", info.Size())
	}
	// Use simpler disk attachment API for better macOS compatibility
	diskAttachment, err := vz.NewDiskImageStorageDeviceAttachment(
		rootfsPath,
		true, // read-only: ephemeral overlay provides writes
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create disk attachment: %w", err)
	}

	blockDevice, err := vz.NewVirtioBlockDeviceConfiguration(diskAttachment)
	if err != nil {
		return nil, fmt.Errorf("failed to create block device: %w", err)
	}
	vmConfig.SetStorageDevicesVirtualMachineConfiguration([]vz.StorageDeviceConfiguration{blockDevice})

	// Configure console/serial
	debugLog("Configuring serial console...")
	console, serialConfig, err := createConsole()
	if err != nil {
		return nil, fmt.Errorf("failed to create console: %w", err)
	}
	vmConfig.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{serialConfig})

	// Configure NAT network
	debugLog("Configuring NAT network...")
	natAttachment, err := vz.NewNATNetworkDeviceAttachment()
	if err != nil {
		return nil, fmt.Errorf("failed to create NAT attachment: %w", err)
	}
	networkDevice, err := vz.NewVirtioNetworkDeviceConfiguration(natAttachment)
	if err != nil {
		return nil, fmt.Errorf("failed to create network device: %w", err)
	}
	vmConfig.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{networkDevice})

	// Configure VirtioFS mounts (last - optional)
	debugLog("Configuring VirtioFS mounts...")
	fsDevices, err := createVirtioFSDevices(allMounts)
	if err != nil {
		return nil, fmt.Errorf("failed to create VirtioFS devices: %w", err)
	}
	vmConfig.SetDirectorySharingDevicesVirtualMachineConfiguration(fsDevices)

	// Validate configuration
	debugLog("Validating VM configuration...")
	valid, err := vmConfig.Validate()
	if err != nil {
		debugLog("Validation error: %v", err)
		return nil, fmt.Errorf("invalid VM configuration: %w", err)
	}
	if !valid {
		debugLog("Validation returned false")
		return nil, fmt.Errorf("VM configuration validation failed")
	}
	debugLog("VM configuration valid")

	// Create virtual machine
	debugLog("Creating virtual machine...")
	vm, err := vz.NewVirtualMachine(vmConfig)
	if err != nil {
		debugLog("VM creation error: %v", err)
		return nil, fmt.Errorf("failed to create virtual machine: %w", err)
	}
	debugLog("Virtual machine created")

	// Register VM state change handler to auto-detach console when VM stops
	go func() {
		for state := range vm.StateChangedNotify() {
			debugLog("VM state changed: %v", state)
			if state == vz.VirtualMachineStateStopped ||
				state == vz.VirtualMachineStateError {
				// Auto-detach console when VM stops to unblock Attach()
				m.mu.RLock()
				console := m.consoles[id]
				m.mu.RUnlock()
				if console != nil {
					debugLog("Auto-detaching console due to VM state: %v", state)
					console.Detach()
				}
			}
		}
	}()

	// Create session
	sess := &session.Session{
		ID:         id,
		ProjectDir: cfg.ProjectDir,
		Mounts:     cfg.Mounts,
		Network:    cfg.Network,
		CPUs:       cfg.CPUs,
		Memory:     cfg.Memory,
		Status:     "created",
		StartedAt:  time.Now(),
		ClaudeMode: cfg.ClaudeMode,
	}

	// Store VM and console
	m.mu.Lock()
	m.vms[id] = vm
	m.consoles[id] = console

	// Create and start console proxy server
	proxy, err := NewConsoleProxyServer(id, console)
	if err != nil {
		debugLog("Failed to create console proxy: %v", err)
	} else {
		if err := proxy.Start(); err != nil {
			debugLog("Failed to start console proxy: %v", err)
		} else {
			m.proxies[id] = proxy
			debugLog("Console proxy started at %s", proxy.SocketPath())
		}
	}

	m.mu.Unlock()

	// Persist session
	if err := m.sessions.Save(sess); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	return sess, nil
}

// Start boots the VM
func (m *VZManager) Start(sess *session.Session) error {
	debugLog("Starting VM for session %s...", sess.ID)

	m.mu.RLock()
	vm, ok := m.vms[sess.ID]
	m.mu.RUnlock()

	if !ok {
		debugLog("VM not found in map")
		return fmt.Errorf("VM not found: %s", sess.ID)
	}

	// Pre-start validation
	debugLog("Running pre-start validation...")
	if err := validateKernelFile(m.artifacts.KernelPath()); err != nil {
		return fmt.Errorf("kernel validation failed: %w", err)
	}

	// Validate the correct rootfs based on mode
	rootfsToValidate := m.artifacts.RootfsPath()
	if sess.ClaudeMode {
		rootfsToValidate = m.artifacts.ClaudeRootfsPath()
	}
	if err := validateRootfs(rootfsToValidate); err != nil {
		return fmt.Errorf("rootfs validation failed: %w", err)
	}

	debugLog("Calling vm.Start()...")
	if err := vm.Start(); err != nil {
		debugLog("vm.Start() error: %v", err)
		// Capture VZ framework logs for diagnostics
		captureVZLogs()
		return fmt.Errorf("failed to start VM: %w", err)
	}
	debugLog("vm.Start() succeeded")

	// Update session status
	sess.Status = "running"
	if err := m.sessions.Save(sess); err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	return nil
}

// Stop stops a running VM
func (m *VZManager) Stop(id string) error {
	m.mu.Lock()
	vm, ok := m.vms[id]
	if !ok {
		m.mu.Unlock()
		// Try to load from store and update status
		sess, err := m.sessions.Load(id)
		if err != nil {
			return fmt.Errorf("session not found: %s", id)
		}
		sess.Status = "stopped"
		return m.sessions.Save(sess)
	}

	delete(m.vms, id)
	delete(m.consoles, id)

	// Stop and remove proxy
	if proxy, ok := m.proxies[id]; ok {
		proxy.Stop()
		delete(m.proxies, id)
	}

	m.mu.Unlock()

	// Check if VM is already stopped
	if vm.State() == vz.VirtualMachineStateStopped || vm.State() == vz.VirtualMachineStateError {
		// VM already stopped, just update session status
		sess, err := m.sessions.Load(id)
		if err == nil {
			sess.Status = "stopped"
			m.sessions.Save(sess)
		}
		return nil
	}

	// Request stop
	if vm.CanRequestStop() {
		if _, err := vm.RequestStop(); err != nil {
			// Force stop if graceful fails
			if err := vm.Stop(); err != nil {
				// Ignore "already stopped" race condition errors
				if !strings.Contains(err.Error(), "Invalid virtual machine state transition") {
					return fmt.Errorf("failed to stop VM: %w", err)
				}
			}
		}
	} else {
		if err := vm.Stop(); err != nil {
			// Ignore "already stopped" race condition errors
			if !strings.Contains(err.Error(), "Invalid virtual machine state transition") {
				return fmt.Errorf("failed to stop VM: %w", err)
			}
		}
	}

	// Update session status
	sess, err := m.sessions.Load(id)
	if err == nil {
		sess.Status = "stopped"
		m.sessions.Save(sess)
	}

	return nil
}

// List returns all sessions
func (m *VZManager) List() ([]*session.Session, error) {
	return m.sessions.List()
}

// Attach connects to the VM console
// Always uses socket-based attach via proxy to ensure consistent behavior
// between initial attach (faize claude start) and reattach (faize claude attach).
// This prevents the bug where in-memory io.Copy goroutines would continue
// consuming console output after detach, starving subsequent proxy clients.
func (m *VZManager) Attach(id string) error {
	socketPath := m.GetProxySocketPath(id)

	// Wait briefly for proxy to be ready (relevant for fresh start)
	for range 10 {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if _, err := os.Stat(socketPath); err != nil {
		return fmt.Errorf("console not found for session: %s (VM may have stopped)", id)
	}

	client, err := NewConsoleClient(socketPath)
	if err != nil {
		// Connection failed - socket is stale (process crashed)
		// Clean up the orphaned socket file
		os.Remove(socketPath)

		// Update session status to stopped
		if sess, loadErr := m.sessions.Load(id); loadErr == nil {
			sess.Status = "stopped"
			m.sessions.Save(sess)
		}

		return fmt.Errorf("session %s is no longer running (cleaned up stale socket)", id)
	}
	defer client.Close()

	// Set up terminal resize propagation via VirtioFS termsize file
	termsizePath := filepath.Join(m.artifacts.SessionDir(id), "bootstrap", "termsize")
	client.SetTermsizePath(termsizePath)

	// Write current terminal size immediately (handles reattach from different-sized terminal)
	if term.IsTerminal(int(os.Stdout.Fd())) {
		if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 && h > 0 {
			os.WriteFile(termsizePath, []byte(fmt.Sprintf("%d %d", w, h)), 0644)
		}
	}

	return client.Attach(os.Stdin, os.Stdout)
}

// GetProxySocketPath returns the socket path for a session's proxy
func (m *VZManager) GetProxySocketPath(id string) string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".faize", "sessions", fmt.Sprintf("%s.sock", id))
}

// WaitForVMStop blocks until the VM stops or an error occurs
func (m *VZManager) WaitForVMStop(id string) <-chan struct{} {
	done := make(chan struct{})

	go func() {
		defer close(done)

		m.mu.RLock()
		vm, ok := m.vms[id]
		m.mu.RUnlock()

		if !ok {
			return
		}

		// Wait for VM to stop
		for state := range vm.StateChangedNotify() {
			if state == vz.VirtualMachineStateStopped || state == vz.VirtualMachineStateError {
				return
			}
		}
	}()

	return done
}

// parseMemory converts memory string like "4GB" to bytes
func parseMemory(mem string) uint64 {
	var size uint64
	var unit string
	fmt.Sscanf(mem, "%d%s", &size, &unit)

	switch unit {
	case "GB", "G":
		return size * 1024 * 1024 * 1024
	case "MB", "M":
		return size * 1024 * 1024
	default:
		return 4 * 1024 * 1024 * 1024 // Default 4GB
	}
}
