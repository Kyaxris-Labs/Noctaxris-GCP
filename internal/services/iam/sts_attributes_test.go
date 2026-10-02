package iam

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestResolveWIFSubjectMappingAndCondition(t *testing.T) {
	prov := store.WorkloadIdentityPoolProvider{
		AttributeMap:       `{"google.subject":"assertion.sub","attribute.email":"assertion.email"}`,
		AttributeCondition: `assertion.email.endsWith("@example.com")`,
	}
	assertion := map[string]any{"sub": "alice", "email": "alice@example.com"}
	got, err := resolveWIFSubject(prov, assertion, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if got != "alice" {
		t.Fatalf("subject=%q", got)
	}
	_, err = resolveWIFSubject(prov, map[string]any{"sub": "bob", "email": "bob@evil.com"}, "ignored")
	if err == nil {
		t.Fatal("expected attributeCondition deny")
	}
	_, err = resolveWIFSubject(store.WorkloadIdentityPoolProvider{
		AttributeMap: `{"attribute.email":"assertion.email"}`,
	}, assertion, "ignored")
	if err == nil {
		t.Fatal("expected missing google.subject")
	}
}

func TestResolveWIFSubjectEmptyMappingTheatre(t *testing.T) {
	got, err := resolveWIFSubject(store.WorkloadIdentityPoolProvider{}, map[string]any{}, "lab-theatre-sub")
	if err != nil {
		t.Fatal(err)
	}
	if got != "lab-theatre-sub" {
		t.Fatalf("got %q", got)
	}
}
