package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

const (
	serviceName = "azdo-tui"
	userName    = "pat"
)

// ErrNotFound is returned when a PAT is not found in the keyring
var ErrNotFound = errors.New("PAT not found in keyring")

// keyringProvider defines the interface for keyring operations
// This allows for mocking in tests
type keyringProvider interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

// systemKeyring is a wrapper around the go-keyring library
type systemKeyring struct{}

func (s *systemKeyring) Get(service, user string) (string, error) {
	secret, err := keyring.Get(service, user)
	if err != nil {
		if err == keyring.ErrNotFound {
			return "", ErrNotFound
		}
		return "", err
	}
	return secret, nil
}

func (s *systemKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (s *systemKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

// KeyringStore manages PAT storage in the system keyring.
type KeyringStore struct {
	provider       keyringProvider
	fallbackPath   string
	lastPATStorage string
}

// NewKeyringStore creates a new KeyringStore with the system keyring
func NewKeyringStore() *KeyringStore {
	return &KeyringStore{
		provider: &systemKeyring{},
	}
}

// CheckKeyring reports whether the system keyring is available. A missing
// credential is not an availability error.
func (k *KeyringStore) CheckKeyring() error {
	_, err := k.provider.Get(serviceName, userName)
	if err == nil || errors.Is(err, ErrNotFound) {
		return nil
	}
	return fmt.Errorf("system keyring unavailable: %w", err)
}

// SetPAT stores a Personal Access Token in the system keyring.
func (k *KeyringStore) SetPAT(token string) error {
	if token == "" {
		return errors.New("token cannot be empty")
	}

	if err := k.provider.Set(serviceName, userName, token); err != nil {
		return fmt.Errorf("failed to store PAT in system keyring: %w", err)
	}

	if err := k.removeLegacyFallbackPAT(); err != nil {
		return fmt.Errorf("PAT was stored in the system keyring but the legacy fallback file could not be removed: %w", err)
	}

	k.lastPATStorage = "system keyring"
	return nil
}

// GetPAT retrieves the Personal Access Token from the system keyring, or from
// AZDO_PAT for non-interactive environments.
func (k *KeyringStore) GetPAT() (string, error) {
	token, err := k.provider.Get(serviceName, userName)
	if err == nil {
		return token, nil
	}

	if envPAT := os.Getenv("AZDO_PAT"); envPAT != "" {
		return envPAT, nil
	}

	if errors.Is(err, ErrNotFound) {
		return "", ErrNotFound
	}
	return "", fmt.Errorf("failed to retrieve PAT from system keyring and AZDO_PAT is not set: %w", err)
}

// DeletePAT removes the Personal Access Token from the system keyring
func (k *KeyringStore) DeletePAT() error {
	keyringErr := k.provider.Delete(serviceName, userName)
	path, pathErr := k.getFallbackPath()
	if pathErr != nil {
		return pathErr
	}
	fileErr := os.Remove(path)
	if errors.Is(fileErr, os.ErrNotExist) {
		fileErr = nil
	}
	if keyringErr != nil && fileErr != nil {
		return fmt.Errorf("failed to delete PAT from keyring (%v) and fallback file: %w", keyringErr, fileErr)
	}
	if keyringErr != nil && !errors.Is(keyringErr, ErrNotFound) {
		return fmt.Errorf("failed to delete PAT from keyring: %w", keyringErr)
	}
	if fileErr != nil {
		return fmt.Errorf("failed to delete fallback PAT file: %w", fileErr)
	}
	return nil
}

// PATStorageDescription reports where the most recent SetPAT call stored its value.
func (k *KeyringStore) PATStorageDescription() string {
	if k.lastPATStorage == "" {
		return "unknown storage"
	}
	return k.lastPATStorage
}

func (k *KeyringStore) getFallbackPath() (string, error) {
	if k.fallbackPath != "" {
		return k.fallbackPath, nil
	}
	configPath, err := GetPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(configPath), "pat"), nil
}

func (k *KeyringStore) removeLegacyFallbackPAT() error {
	path, err := k.getFallbackPath()
	if err != nil {
		return fmt.Errorf("resolve legacy PAT file path: %w", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove legacy PAT file: %w", err)
	}
	return nil
}
