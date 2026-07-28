package store

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	masterKeySize = 32

	// EnvAllowMasterKeyInDataRoot permits a master key path under DATA_ROOT.
	EnvAllowMasterKeyInDataRoot = "NOCTAXRIS_GCP_ALLOW_MASTER_KEY_IN_DATA_ROOT"
)

// MasterKey is a 32-byte ChaCha20-Poly1305 key.
type MasterKey [masterKeySize]byte

// AllowMasterKeyInDataRoot reports whether master.key may live under DATA_ROOT.
func AllowMasterKeyInDataRoot() bool {
	v := strings.TrimSpace(os.Getenv(EnvAllowMasterKeyInDataRoot))
	return v == "1" || strings.EqualFold(v, "true")
}

// DefaultMasterKeyPath returns a path adjacent to but outside dataRoot.
// Example: dataRoot=/var/lib/noctaxris-gcp → /var/lib/noctaxris-gcp-secrets/master.key
func DefaultMasterKeyPath(dataRoot string) string {
	abs, err := filepath.Abs(dataRoot)
	if err != nil {
		abs = dataRoot
	}
	parent := filepath.Dir(abs)
	base := filepath.Base(abs)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "noctaxris-gcp"
	}
	return filepath.Join(parent, base+"-secrets", "master.key")
}

// MasterKeyPathUnderDataRoot reports whether keyPath resolves under dataRoot.
func MasterKeyPathUnderDataRoot(keyPath, dataRoot string) (bool, error) {
	absKey, err := filepath.Abs(keyPath)
	if err != nil {
		return false, fmt.Errorf("resolve master key path: %w", err)
	}
	absRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return false, fmt.Errorf("resolve data root: %w", err)
	}
	rel, err := filepath.Rel(absRoot, absKey)
	if err != nil {
		return false, fmt.Errorf("relate master key to data root: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}

// ResolveMasterKeyPath picks the master key file path.
func ResolveMasterKeyPath(configured, dataRoot string) (string, error) {
	path := strings.TrimSpace(configured)
	allow := AllowMasterKeyInDataRoot()
	if path == "" {
		if allow {
			path = filepath.Join(dataRoot, "master.key")
		} else {
			path = DefaultMasterKeyPath(dataRoot)
		}
	}
	under, err := MasterKeyPathUnderDataRoot(path, dataRoot)
	if err != nil {
		return "", err
	}
	if under && !allow {
		return "", fmt.Errorf("master key path %q is under NOCTAXRIS_GCP_DATA_ROOT %q; set NOCTAXRIS_GCP_MASTER_KEY_FILE outside the data root, or %s=1 to allow colocation",
			path, dataRoot, EnvAllowMasterKeyInDataRoot)
	}
	return path, nil
}

func ensureMasterKeyFileMode(path string) error {
	if runtime.GOOS == "windows" {
		_ = os.Chmod(path, 0o600)
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if mode&0o004 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("master key file %s is world-readable and chmod 0600 failed: %w", path, err)
		}
		info, err = os.Stat(path)
		if err != nil {
			return err
		}
		mode = info.Mode().Perm()
		if mode&0o004 != 0 {
			return fmt.Errorf("master key file %s remains world-readable after chmod 0600 (mode %04o)", path, mode)
		}
		return nil
	}
	if mode != 0o600 {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("master key file %s chmod 0600: %w", path, err)
		}
	}
	return nil
}

// LoadOrCreateMasterKey loads a 32-byte key from path or creates one.
func LoadOrCreateMasterKey(path string) (MasterKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return MasterKey{}, err
		}
		var key MasterKey
		if _, err := io.ReadFull(rand.Reader, key[:]); err != nil {
			return MasterKey{}, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return MasterKey{}, fmt.Errorf("create master key directory: %w", err)
		}
		if err := os.WriteFile(path, key[:], 0o600); err != nil {
			return MasterKey{}, err
		}
		if err := ensureMasterKeyFileMode(path); err != nil {
			return MasterKey{}, err
		}
		return key, nil
	}
	if err := ensureMasterKeyFileMode(path); err != nil {
		return MasterKey{}, err
	}
	if len(data) != masterKeySize {
		return MasterKey{}, fmt.Errorf("master key file %s: want %d bytes, got %d", path, masterKeySize, len(data))
	}
	var key MasterKey
	copy(key[:], data)
	return key, nil
}

// Seal encrypts plaintext with ChaCha20-Poly1305. Output is nonce || ciphertext+tag.
func Seal(key MasterKey, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key[:])
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := aead.Seal(nil, nonce, plaintext, nil)
	return append(nonce, sealed...), nil
}

// Unseal decrypts a blob produced by Seal.
func Unseal(key MasterKey, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key[:])
	if err != nil {
		return nil, err
	}
	nonceSize := aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce := ciphertext[:nonceSize]
	sealed := ciphertext[nonceSize:]
	return aead.Open(nil, nonce, sealed, nil)
}
