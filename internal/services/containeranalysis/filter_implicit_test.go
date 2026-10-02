package containeranalysis

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestLooksLikePrimaryStartAndParseQuotedBare(t *testing.T) {
	if looksLikePrimaryStart("") || looksLikePrimaryStart("   ") {
		t.Fatal("empty")
	}
	if !looksLikePrimaryStart("(kind=\"A\")") {
		t.Fatal("paren")
	}
	if !looksLikePrimaryStart("kind=\"ATTESTATION\"") {
		t.Fatal("kind=")
	}
	if !looksLikePrimaryStart("resourceUrl = \"x\"") {
		t.Fatal("resourceUrl")
	}
	if !looksLikePrimaryStart("has_prefix(resourceUrl,\"x\")") {
		t.Fatal("has_prefix")
	}
	if looksLikePrimaryStart("kind") || looksLikePrimaryStart("AND kind=\"A\"") {
		t.Fatal("should reject incomplete or keyword")
	}

	f, err := parseOccurrenceFilter(`kind="VULNERABILITY" resourceUri=bare-img`)
	if err != nil {
		t.Fatal(err)
	}
	if !f.match(store.ContainerOccurrence{Kind: "VULNERABILITY", ResourceURI: "bare-img"}) {
		t.Fatal("bare value match")
	}
	if f.match(store.ContainerOccurrence{Kind: "ATTESTATION", ResourceURI: "bare-img"}) {
		t.Fatal("kind reject")
	}

	_, err = parseOccurrenceFilter(`kind="unterminated`)
	if err == nil {
		t.Fatal("expected unterminated quote")
	}
}
