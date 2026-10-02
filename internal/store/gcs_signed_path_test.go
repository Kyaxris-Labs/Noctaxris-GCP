package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestIsV4SignedURLPath(t *testing.T) {
	cases := []struct {
		path string
		ok   bool
	}{
		{"/storage/v1/b/lab/o/hello.txt", true},
		{"/storage/v1/b/lab/o/dir/file.txt", true},
		{"/upload/storage/v1/b/lab/o", true},
		{"/storage/v1/b/lab/iam", false},
		{"/storage/v1/b/lab/o/hello.txt/iam", false},
		{"/storage/v1/b", false},
		{"/storage/v1/b/lab", false},
		{"/v1/projects/p/topics", false},
	}
	for _, tc := range cases {
		if got := store.IsV4SignedURLPath(tc.path); got != tc.ok {
			t.Fatalf("path %q got %v want %v", tc.path, got, tc.ok)
		}
	}
}
