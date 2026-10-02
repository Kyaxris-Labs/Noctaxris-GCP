package iam

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestOrderSAKeysForJWTAndKeyID(t *testing.T) {
	if saKeyIDFromName("projects/p/serviceAccounts/a@b/keys/abc") != "abc" {
		t.Fatal("key id parse failed")
	}
	if saKeyIDFromName("no-keys-here") != "" {
		t.Fatal("expected empty key id")
	}

	keys := []store.ServiceAccountKey{
		{Name: "projects/p/serviceAccounts/a/keys/second"},
		{Name: "projects/p/serviceAccounts/a/keys/first"},
	}
	ordered := orderSAKeysForJWT(keys, "first")
	if len(ordered) != 2 || saKeyIDFromName(ordered[0].Name) != "first" {
		t.Fatalf("ordered=%#v", ordered)
	}
	same := orderSAKeysForJWT(keys[:1], "first")
	if len(same) != 1 {
		t.Fatalf("single key=%#v", same)
	}
	unchanged := orderSAKeysForJWT(keys, "")
	if len(unchanged) != 2 || saKeyIDFromName(unchanged[0].Name) != "second" {
		t.Fatalf("empty prefer=%#v", unchanged)
	}
}
