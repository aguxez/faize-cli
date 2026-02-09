//go:build !darwin

package vm

import (
	"fmt"

	"github.com/faize-ai/faize/internal/session"
)

// VZManager is a stub for non-macOS platforms
type VZManager struct{}

// NewVZManager returns an error on non-macOS platforms
func NewVZManager() (*VZManager, error) {
	return nil, fmt.Errorf("Virtualization.framework is only available on macOS")
}

// Create is not implemented on non-macOS
func (m *VZManager) Create(cfg *Config) (*session.Session, error) {
	return nil, fmt.Errorf("VM support requires macOS")
}

// Start is not implemented on non-macOS
func (m *VZManager) Start(sess *session.Session) error {
	return fmt.Errorf("VM support requires macOS")
}

// Stop is not implemented on non-macOS
func (m *VZManager) Stop(id string) error {
	return fmt.Errorf("VM support requires macOS")
}

// List is not implemented on non-macOS
func (m *VZManager) List() ([]*session.Session, error) {
	return nil, fmt.Errorf("VM support requires macOS")
}

// Attach is not implemented on non-macOS
func (m *VZManager) Attach(id string) error {
	return fmt.Errorf("VM support requires macOS")
}
