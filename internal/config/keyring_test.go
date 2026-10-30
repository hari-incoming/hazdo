package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockKeyring implements the keyringProvider interface for testing
type mockKeyring struct {
	store map[string]string
	err   error
}

func newMockKeyring() *mockKeyring {
	return &mockKeyring{
		store: make(map[string]string),
	}
}

func newTestKeyringStore(t *testing.T, provider keyringProvider) *KeyringStore {
	t.Helper()
	return &KeyringStore{
		provider:     provider,
		fallbackPath: filepath.Join(t.TempDir(), "azdo-pat"),
	}
}

func (m *mockKeyring) Get(service, user string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	key := service + ":" + user
	val, ok := m.store[key]
	if !ok {
		return "", ErrNotFound
	}
	return val, nil
}

func (m *mockKeyring) Set(service, user, password string) error {
	if m.err != nil {
		return m.err
	}
	key := service + ":" + user
	m.store[key] = password
	return nil
}

func (m *mockKeyring) Delete(service, user string) error {
	if m.err != nil {
		return m.err
	}
	key := service + ":" + user
	delete(m.store, key)
	return nil
}

func TestSetPAT(t *testing.T) {
	mock := newMockKeyring()
	ks := newTestKeyringStore(t, mock)

	err := ks.SetPAT("test-pat-token")
	if err != nil {
		t.Fatalf("SetPAT() failed: %v", err)
	}

	// Verify it was stored
	stored, err := mock.Get(serviceName, userName)
	if err != nil {
		t.Fatalf("Failed to verify stored PAT: %v", err)
	}

	if stored != "test-pat-token" {
		t.Errorf("Expected stored PAT to be 'test-pat-token', got %s", stored)
	}
}

func TestGetPAT_Success(t *testing.T) {
	mock := newMockKeyring()
	ks := newTestKeyringStore(t, mock)

	// Store a PAT first
	mock.Set(serviceName, userName, "my-secret-token")

	// Retrieve it
	pat, err := ks.GetPAT()
	if err != nil {
		t.Fatalf("GetPAT() failed: %v", err)
	}

	if pat != "my-secret-token" {
		t.Errorf("Expected PAT to be 'my-secret-token', got %s", pat)
	}
}

func TestGetPAT_NotFound(t *testing.T) {
	mock := newMockKeyring()
	ks := newTestKeyringStore(t, mock)
	t.Setenv("AZDO_PAT", "")

	// Try to get PAT when none exists
	pat, err := ks.GetPAT()
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}

	if pat != "" {
		t.Errorf("Expected empty PAT, got %s", pat)
	}
}

func TestDeletePAT(t *testing.T) {
	mock := newMockKeyring()
	ks := newTestKeyringStore(t, mock)

	// Store a PAT first
	ks.SetPAT("token-to-delete")
	if err := os.WriteFile(ks.fallbackPath, []byte("legacy-pat"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Delete it
	err := ks.DeletePAT()
	if err != nil {
		t.Fatalf("DeletePAT() failed: %v", err)
	}

	// Verify it's deleted
	_, err = mock.Get(serviceName, userName)
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound after deletion, got %v", err)
	}
	if _, err := os.Stat(ks.fallbackPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("legacy PAT file still exists after deletion: %v", err)
	}
}

func TestSetPAT_KeyringUnavailableReturnsError(t *testing.T) {
	mock := newMockKeyring()
	mock.err = errors.New("keyring access denied")
	ks := newTestKeyringStore(t, mock)

	if err := ks.SetPAT("test-token"); err == nil || !strings.Contains(err.Error(), "system keyring") {
		t.Fatalf("SetPAT() error = %v, want system keyring error", err)
	}
	if _, err := os.Stat(ks.fallbackPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("SetPAT() created a fallback file: %v", err)
	}
}

func TestSetPAT_RemovesLegacyFallbackFile(t *testing.T) {
	mock := newMockKeyring()
	ks := newTestKeyringStore(t, mock)
	if err := os.WriteFile(ks.fallbackPath, []byte("legacy-pat"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ks.SetPAT("new-pat"); err != nil {
		t.Fatalf("SetPAT() failed: %v", err)
	}
	if _, err := os.Stat(ks.fallbackPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("legacy PAT file still exists after keyring save: %v", err)
	}
}

func TestSetPAT_EmptyToken(t *testing.T) {
	mock := newMockKeyring()
	ks := newTestKeyringStore(t, mock)

	err := ks.SetPAT("")
	if err == nil {
		t.Error("Expected SetPAT to fail with empty token")
	}
}

func TestGetPAT_FallbackToEnvVar(t *testing.T) {
	// Set environment variable
	envPAT := "env-pat-token-123"
	t.Setenv("AZDO_PAT", envPAT)

	// Create keyring that will fail
	mock := newMockKeyring()
	mock.err = errors.New("keyring unavailable")
	ks := newTestKeyringStore(t, mock)

	// Should fall back to environment variable
	pat, err := ks.GetPAT()
	if err != nil {
		t.Fatalf("GetPAT() should succeed with env fallback, got error: %v", err)
	}

	if pat != envPAT {
		t.Errorf("Expected PAT from env var '%s', got '%s'", envPAT, pat)
	}
}

func TestGetPAT_ErrorWhenNoFallback(t *testing.T) {
	// Ensure AZDO_PAT is not set
	t.Setenv("AZDO_PAT", "")

	// Create keyring that will fail
	mock := newMockKeyring()
	mock.err = errors.New("keyring unavailable")
	ks := newTestKeyringStore(t, mock)

	// Should return error with helpful message
	_, err := ks.GetPAT()
	if err == nil {
		t.Error("Expected GetPAT to fail when keyring unavailable and no env var set")
	}

	// Check error message mentions both issues
	errMsg := err.Error()
	if !strings.Contains(errMsg, "keyring") {
		t.Errorf("Error message should mention keyring: %s", errMsg)
	}
	if !strings.Contains(errMsg, "AZDO_PAT") {
		t.Errorf("Error message should mention AZDO_PAT env var: %s", errMsg)
	}
}

func TestGetPAT_KeyringUnavailableDoesNotReadLegacyFile(t *testing.T) {
	t.Setenv("AZDO_PAT", "")
	mock := newMockKeyring()
	mock.err = errors.New("keyring unavailable")
	ks := newTestKeyringStore(t, mock)
	if err := os.WriteFile(ks.fallbackPath, []byte("legacy-pat"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ks.GetPAT(); err == nil || !strings.Contains(err.Error(), "keyring") {
		t.Fatalf("GetPAT() error = %v, want keyring error", err)
	}
}

func TestGetPAT_KeyringHasNoEntryDoesNotReadLegacyFile(t *testing.T) {
	t.Setenv("AZDO_PAT", "")
	mock := newMockKeyring()
	ks := newTestKeyringStore(t, mock)
	if err := os.WriteFile(ks.fallbackPath, []byte("legacy-pat"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ks.GetPAT(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPAT() error = %v, want ErrNotFound", err)
	}
}

func TestCheckKeyring(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{name: "available without credential"},
		{name: "unavailable", err: errors.New("keyring daemon unavailable"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockKeyring()
			mock.err = tt.err
			ks := newTestKeyringStore(t, mock)

			err := ks.CheckKeyring()
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckKeyring() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
