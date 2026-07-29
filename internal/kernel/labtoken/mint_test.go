package labtoken_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestMintRegistersAccessToken(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	email := "runner@noctaxris-gcp-local.iam.gserviceaccount.com"
	token, expire, err := labtoken.Mint(st, email, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "ngsa_") {
		t.Fatalf("token=%q", token)
	}
	if expire.Before(time.Now().UTC()) {
		t.Fatalf("expire in past: %v", expire)
	}
	got, ok, err := st.LookupAccessToken(authn.HashToken(token), time.Now().UTC())
	if err != nil || !ok || got != email {
		t.Fatalf("lookup: email=%q ok=%v err=%v", got, ok, err)
	}
}

func TestMintRequiresEmail(t *testing.T) {
	_, _, err := labtoken.Mint(nil, "", time.Hour)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDefaultComputeSAEmail(t *testing.T) {
	got := labtoken.DefaultComputeSAEmail("noctaxris-gcp-local")
	want := "noctaxris-gcp-local-compute@developer.gserviceaccount.com"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if labtoken.DefaultComputeSAEmail("") != "" {
		t.Fatal("empty project should yield empty email")
	}
}
