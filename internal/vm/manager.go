package vm

import (
	"errors"

	"github.com/faize-ai/faize/internal/session"
)

// ErrVMNotImplemented is returned when VM operations are called before Phase 2 implementation
var ErrVMNotImplemented = errors.New("VM support not yet implemented - coming in Phase 2")

// ErrUserDetach is returned when the user requests to detach from the console
var ErrUserDetach = errors.New("user requested detach")

type Manager interface {
	Create(cfg *Config) (*session.Session, error)
	Start(sess *session.Session) error
	Stop(id string) error
	List() ([]*session.Session, error)
	Attach(id string) error
	WaitForVMStop(id string) <-chan struct{}
}

type StubManager struct{}

func NewStubManager() *StubManager {
	return &StubManager{}
}

func (m *StubManager) Create(cfg *Config) (*session.Session, error) {
	return nil, ErrVMNotImplemented
}

func (m *StubManager) Start(sess *session.Session) error {
	return ErrVMNotImplemented
}

func (m *StubManager) Stop(id string) error {
	return ErrVMNotImplemented
}

func (m *StubManager) List() ([]*session.Session, error) {
	return []*session.Session{}, nil // Return empty list
}

func (m *StubManager) Attach(id string) error {
	return ErrVMNotImplemented
}

func (m *StubManager) WaitForVMStop(id string) <-chan struct{} {
	ch := make(chan struct{})
	close(ch) // Immediately returns for stub
	return ch
}
