package iam

import (
	"strings"
	"testing"
)

func TestNormalizeWIFAudience(t *testing.T) {
	provider := "projects/p/locations/global/workloadIdentityPools/pool/providers/oidc"
	if got := normalizeWIFAudience("//iam.googleapis.com/" + provider); got != provider {
		t.Fatalf("normalize prefix: got %q want %q", got, provider)
	}
	if got := normalizeWIFAudience("  " + provider + "  "); got != provider {
		t.Fatalf("normalize trim: got %q", got)
	}
}

func TestLabSubjectFromToken(t *testing.T) {
	if got := labSubjectFromToken("lab-sub"); got != "lab-sub" {
		t.Fatalf("got %q", got)
	}
	if got := labSubjectFromToken("bad chars!@#"); got != "bad-chars---" {
		t.Fatalf("sanitize got %q", got)
	}
	long := strings.Repeat("a", 80)
	got := labSubjectFromToken(long)
	if len(got) != 64 {
		t.Fatalf("truncate len=%d", len(got))
	}
	if got := labSubjectFromToken("   "); got != "anonymous" {
		t.Fatalf("empty-ish got %q", got)
	}
}
