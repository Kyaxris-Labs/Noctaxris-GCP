package store_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestValidateGCSBucketName(t *testing.T) {
	if err := store.ValidateGCSBucketName("safe-bucket"); err != nil {
		t.Fatalf("safe: %v", err)
	}
	for _, name := range []string{
		"",
		"ab",
		"../escape",
		"..",
		"foo/bar",
		`foo\bar`,
		"/tmp/abs",
		"UPPER-CASE",
		"has space",
	} {
		if err := store.ValidateGCSBucketName(name); err == nil {
			t.Fatalf("expected reject for %q", name)
		}
	}
}

func TestJoinUnderRootRejectsEscape(t *testing.T) {
	root := t.TempDir()
	ok, err := store.JoinUnderRoot(root, "gcs", "bucket-a")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ok, root) {
		t.Fatalf("joined=%s root=%s", ok, root)
	}
	if _, err := store.JoinUnderRoot(root, "gcs", "..", "outside"); err == nil {
		t.Fatal("expected escape reject")
	}
	if _, err := store.JoinUnderRoot(root, "gcs", filepath.Join("..", "outside")); err == nil {
		t.Fatal("expected join escape reject")
	}
}

func TestCreateBucketRejectsPathEscape(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	outsideMarker := filepath.Join(dir, "outside-marker")
	if err := os.MkdirAll(outsideMarker, 0o700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(outsideMarker, "keepme")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err = st.CreateBucket("../../outside-marker", "noctaxris-gcp-local", "US", "STANDARD")
	if err == nil {
		t.Fatal("expected invalid bucket name")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("outside tree must remain: %v", err)
	}

	b, created, err := st.CreateBucket("safe-bucket", "noctaxris-gcp-local", "US", "STANDARD")
	if err != nil || !created || b == nil {
		t.Fatalf("safe create: created=%v err=%v", created, err)
	}
	under := filepath.Join(dir, "data", "gcs", "safe-bucket")
	if stInfo, err := os.Stat(under); err != nil || !stInfo.IsDir() {
		t.Fatalf("expected dir under data root: %v", err)
	}
}
