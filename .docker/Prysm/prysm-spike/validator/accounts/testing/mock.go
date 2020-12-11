package mock

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/prysmaticlabs/prysm/validator/keymanager"
)

// Wallet contains an in-memory, simulated wallet implementation.
type Wallet struct {
	InnerAccountsDir  string
	Directories       []string
	Files             map[string]map[string][]byte
	EncryptedSeedFile []byte
	AccountPasswords  map[string]string
	WalletPassword    string
	UnlockAccounts    bool
	lock              sync.RWMutex
}

// AccountNames --
func (m *Wallet) AccountNames() ([]string, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	names := make([]string, 0)
	for name := range m.AccountPasswords {
		names = append(names, name)
	}
	return names, nil
}

// AccountsDir --
func (m *Wallet) AccountsDir() string {
	return m.InnerAccountsDir
}

// Exists --
func (m *Wallet) Exists() (bool, error) {
	return len(m.Directories) > 0, nil
}

// Password --
func (m *Wallet) Password() string {
	return m.WalletPassword
}

// WriteFileAtPath --
func (m *Wallet) WriteFileAtPath(_ context.Context, pathName, fileName string, data []byte) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	if m.Files[pathName] == nil {
		m.Files[pathName] = make(map[string][]byte)
	}
	m.Files[pathName][fileName] = data
	return nil
}

// ReadFileAtPath --
func (m *Wallet) ReadFileAtPath(_ context.Context, pathName, fileName string) ([]byte, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	for f, v := range m.Files[pathName] {
		if strings.Contains(fileName, f) {
			return v, nil
		}
	}
	return nil, errors.New("no files found")
}

// InitializeKeymanager --
func (m *Wallet) InitializeKeymanager(_ context.Context) (keymanager.IKeymanager, error) {
	return nil, nil
}
