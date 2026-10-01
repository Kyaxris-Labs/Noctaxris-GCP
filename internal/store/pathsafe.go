package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrInvalidGCSBucketName is returned when a bucket name is empty, not a single
// DNS-style label, or would escape the data-root object tree.
var ErrInvalidGCSBucketName = fmt.Errorf("invalid bucket name")

var gcsBucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$`)

// ValidateGCSBucketName checks DNS-style bucket naming (3-63 chars, lowercase)
// and rejects path separators, absolute names, and ".." segments.
func ValidateGCSBucketName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 3 || len(name) > 63 {
		return ErrInvalidGCSBucketName
	}
	if name != strings.ToLower(name) {
		return ErrInvalidGCSBucketName
	}
	if filepath.IsAbs(name) {
		return ErrInvalidGCSBucketName
	}
	if strings.ContainsAny(name, `/\`) {
		return ErrInvalidGCSBucketName
	}
	if strings.Contains(name, "..") {
		return ErrInvalidGCSBucketName
	}
	if !gcsBucketNamePattern.MatchString(name) {
		return ErrInvalidGCSBucketName
	}
	return nil
}

// JoinUnderRoot joins root with elem and ensures the cleaned absolute path
// stays under root (rejects ".." escape and absolute elem segments).
func JoinUnderRoot(root string, elem ...string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("root path required")
	}
	for _, e := range elem {
		if e == "" {
			continue
		}
		if filepath.IsAbs(e) {
			return "", fmt.Errorf("absolute path segment not allowed")
		}
		clean := filepath.Clean(e)
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path escapes root")
		}
		if strings.Contains(e, "..") {
			return "", fmt.Errorf("path escapes root")
		}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	joined := filepath.Join(append([]string{absRoot}, elem...)...)
	absJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("resolve joined path: %w", err)
	}
	rel, err := filepath.Rel(absRoot, absJoined)
	if err != nil {
		return "", fmt.Errorf("relate path to root: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root")
	}
	return absJoined, nil
}

// gcsBucketDir returns the on-disk directory for a validated bucket name.
func (s *Store) gcsBucketDir(name string) (string, error) {
	if err := ValidateGCSBucketName(name); err != nil {
		return "", err
	}
	return JoinUnderRoot(s.dataRoot, "gcs", name)
}

// ensureUnderGCSRoot reports whether absPath resolves under dataRoot/gcs.
func (s *Store) ensureUnderGCSRoot(absPath string) error {
	root, err := filepath.Abs(filepath.Join(s.dataRoot, "gcs"))
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(absPath)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes gcs root")
	}
	return nil
}

// mkdirGCSBucket creates the bucket directory under the data root.
func (s *Store) mkdirGCSBucket(name string) error {
	dir, err := s.gcsBucketDir(name)
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}
