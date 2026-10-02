package store_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestGOOG4HMACSignVerifyAndAccessID(t *testing.T) {
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	accessID := "GOOG1LABACCESS"
	secret := "0123456789abcdef0123456789abcdef"
	host := "storage.googleapis.com"
	path := "/lab-bucket/obj.txt"
	q := url.Values{"versions": []string{"true"}}

	auth, googDate := store.SignGOOG4HMACHeader("GET", host, path, accessID, secret, now, q)
	if auth == "" || googDate == "" {
		t.Fatalf("auth=%q googDate=%q", auth, googDate)
	}
	if got := store.GOOG4AccessID(auth); got != accessID {
		t.Fatalf("accessID=%q", got)
	}
	if store.GOOG4AccessID("Bearer xyz") != "" {
		t.Fatal("expected empty access id for non-GOOG4")
	}

	if err := store.VerifyGOOG4HMACHeader("GET", host, path, auth, googDate, secret, now, q); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := store.VerifyGOOG4HMACHeader("PUT", host, path, auth, googDate, secret, now, q); err == nil {
		t.Fatal("expected method mismatch")
	}
	if err := store.VerifyGOOG4HMACHeader("GET", host, path, "GOOG4-HMAC-SHA256 Credential=x", googDate, secret, now, q); err == nil {
		t.Fatal("expected incomplete auth failure")
	}
	if err := store.VerifyGOOG4HMACHeader("GET", host, path, auth, "", secret, now, q); err == nil {
		t.Fatal("expected missing date failure")
	}
	if err := store.VerifyGOOG4HMACHeader("GET", host, path, auth, "not-a-date", secret, now, q); err == nil {
		t.Fatal("expected invalid date failure")
	}
	expired := now.Add(30 * time.Minute)
	if err := store.VerifyGOOG4HMACHeader("GET", host, path, auth, googDate, secret, expired, q); err == nil {
		t.Fatal("expected expiry failure")
	}
	tooEarly := now.Add(-30 * time.Minute)
	if err := store.VerifyGOOG4HMACHeader("GET", host, path, auth, googDate, secret, tooEarly, q); err == nil {
		t.Fatal("expected not-yet-valid failure")
	}
	if err := store.VerifyGOOG4HMACHeader("GET", host, path, auth, googDate, "wrong-secret", now, q); err == nil {
		t.Fatal("expected signature mismatch")
	}

	// Zero now uses wall clock inside sign/verify; sign+verify with shared wall time should pass.
	auth2, date2 := store.SignGOOG4HMACHeader("GET", host, path, accessID, secret, time.Time{}, nil)
	if err := store.VerifyGOOG4HMACHeader("GET", host, path, auth2, date2, secret, time.Time{}, nil); err != nil {
		t.Fatalf("zero-now verify: %v", err)
	}
}

func TestListObjectGenerationsPrefixAndAll(t *testing.T) {
	st := openTestStore(t)
	if _, created, err := st.CreateBucket("gen-bucket", "noctaxris-gcp-local", "US", "STANDARD"); err != nil || !created {
		t.Fatalf("bucket created=%v err=%v", created, err)
	}
	if _, err := st.PutObjectBytes("gen-bucket", "a/one.txt", "text/plain", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("gen-bucket", "a/one.txt", "text/plain", []byte("v2")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("gen-bucket", "b/two.txt", "text/plain", []byte("other")); err != nil {
		t.Fatal(err)
	}

	all, err := st.ListObjectGenerations("gen-bucket", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 3 {
		t.Fatalf("all generations=%#v", all)
	}
	pref, err := st.ListObjectGenerations("gen-bucket", "a/")
	if err != nil {
		t.Fatal(err)
	}
	if len(pref) != 2 {
		t.Fatalf("prefix generations=%#v", pref)
	}
	for _, o := range pref {
		if o.Name != "a/one.txt" {
			t.Fatalf("unexpected object %#v", o)
		}
	}
	empty, err := st.ListObjectGenerations("gen-bucket", "zzz/")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty=%#v err=%v", empty, err)
	}
}
