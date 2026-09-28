package containeranalysis

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestContainerAnalysisParseOccurrenceFilterKindAndResourceURL(t *testing.T) {
	f, err := parseOccurrenceFilter(`kind="ATTESTATION" AND resourceUrl="gcr.io/p/img@sha256:abc"`)
	if err != nil {
		t.Fatal(err)
	}
	ok := store.ContainerOccurrence{Kind: "ATTESTATION", ResourceURI: "gcr.io/p/img@sha256:abc"}
	badKind := store.ContainerOccurrence{Kind: "VULNERABILITY", ResourceURI: "gcr.io/p/img@sha256:abc"}
	badURI := store.ContainerOccurrence{Kind: "ATTESTATION", ResourceURI: "gcr.io/other"}
	if !f.match(ok) || f.match(badKind) || f.match(badURI) {
		t.Fatalf("filter match unexpected")
	}
}

func TestContainerAnalysisParseOccurrenceFilterSpacedAndParens(t *testing.T) {
	f, err := parseOccurrenceFilter(`( kind = "VULNERABILITY" ) AND ( resourceUri = "https://gcr.io/p/img" OR resourceUrl="other" )`)
	if err != nil {
		t.Fatal(err)
	}
	if !f.match(store.ContainerOccurrence{Kind: "VULNERABILITY", ResourceURI: "gcr.io/p/img"}) {
		t.Fatal("expected https filter to match bare stored URI")
	}
	if !f.match(store.ContainerOccurrence{Kind: "VULNERABILITY", ResourceURI: "https://other"}) {
		t.Fatal("expected OR branch")
	}
	if f.match(store.ContainerOccurrence{Kind: "ATTESTATION", ResourceURI: "gcr.io/p/img"}) {
		t.Fatal("kind should reject")
	}
}

func TestContainerAnalysisParseOccurrenceFilterHTTPSVsBare(t *testing.T) {
	f, err := parseOccurrenceFilter(`resourceUrl="https://us-docker.pkg.dev/p/apps/web:1"`)
	if err != nil {
		t.Fatal(err)
	}
	if !f.match(store.ContainerOccurrence{ResourceURI: "us-docker.pkg.dev/p/apps/web:1"}) {
		t.Fatal("https filter should match bare resourceUri")
	}
	f2, err := parseOccurrenceFilter(`resourceUri="us-docker.pkg.dev/p/apps/web:1"`)
	if err != nil {
		t.Fatal(err)
	}
	if !f2.match(store.ContainerOccurrence{ResourceURI: "https://us-docker.pkg.dev/p/apps/web:1"}) {
		t.Fatal("bare filter should match https resourceUri")
	}
}

func TestContainerAnalysisParseOccurrenceFilterHasPrefixAndNoteID(t *testing.T) {
	f, err := parseOccurrenceFilter(`has_prefix(resourceUrl,"https://gcr.io/p/") AND noteId="attestor"`)
	if err != nil {
		t.Fatal(err)
	}
	ok := store.ContainerOccurrence{
		ResourceURI: "gcr.io/p/img:1",
		NoteName:    "projects/p/notes/attestor",
	}
	if !f.match(ok) {
		t.Fatal("expected has_prefix + noteId match")
	}
	if f.match(store.ContainerOccurrence{ResourceURI: "gcr.io/other/img", NoteName: "projects/p/notes/attestor"}) {
		t.Fatal("prefix should reject")
	}
	if f.match(store.ContainerOccurrence{ResourceURI: "gcr.io/p/img", NoteName: "projects/p/notes/other"}) {
		t.Fatal("noteId should reject")
	}
}

func TestContainerAnalysisParseOccurrenceFilterSha256Hyphen(t *testing.T) {
	f, err := parseOccurrenceFilter(`resourceUrl="us-central1-docker.pkg.dev/p/r/app@sha256-deadbeef"`)
	if err != nil {
		t.Fatal(err)
	}
	if !f.match(store.ContainerOccurrence{ResourceURI: "us-central1-docker.pkg.dev/p/r/app@sha256:deadbeef"}) {
		t.Fatal("gcloud sha256- filter should match stored sha256: URI")
	}
	if !f.match(store.ContainerOccurrence{ResourceURI: "https://us-central1-docker.pkg.dev/p/r/app@sha256:deadbeef"}) {
		t.Fatal("hyphen filter should also match https stored URI")
	}
}

func TestContainerAnalysisParseOccurrenceFilterEmpty(t *testing.T) {
	f, err := parseOccurrenceFilter("  ")
	if err != nil {
		t.Fatal(err)
	}
	if !f.match(store.ContainerOccurrence{}) {
		t.Fatal("empty filter matches all")
	}
}

func TestContainerAnalysisBuildVulnerabilityCounts(t *testing.T) {
	list := []store.ContainerOccurrence{
		{
			Kind: "VULNERABILITY", ResourceURI: "img:1",
			BodyJSON: `{"vulnerability":{"severity":"HIGH","fixAvailable":true}}`,
		},
		{
			Kind: "VULNERABILITY", ResourceURI: "img:1",
			BodyJSON: `{"vulnerability":{"effectiveSeverity":"LOW","fixAvailable":false}}`,
		},
		{
			Kind: "ATTESTATION", ResourceURI: "img:1",
			BodyJSON: `{}`,
		},
		{
			Kind: "VULNERABILITY", ResourceURI: "img:2",
			BodyJSON: `{"vulnerability":{"severity":"CRITICAL","fixAvailable":true}}`,
		},
	}
	counts := buildVulnerabilityCounts(list)
	if len(counts) < 3 {
		t.Fatalf("counts=%#v", counts)
	}
	var sawHigh, sawLow, sawTotal1, sawCrit, sawTotal2 bool
	for _, c := range counts {
		uri, _ := c["resourceUri"].(string)
		sev, _ := c["severity"].(string)
		total, _ := c["totalCount"].(string)
		fixable, _ := c["fixableCount"].(string)
		switch {
		case uri == "img:1" && sev == "HIGH" && total == "1" && fixable == "1":
			sawHigh = true
		case uri == "img:1" && sev == "LOW" && total == "1" && fixable == "0":
			sawLow = true
		case uri == "img:1" && sev == "SEVERITY_UNSPECIFIED" && total == "2" && fixable == "1":
			sawTotal1 = true
		case uri == "img:2" && sev == "CRITICAL" && total == "1":
			sawCrit = true
		case uri == "img:2" && sev == "SEVERITY_UNSPECIFIED" && total == "1":
			sawTotal2 = true
		}
	}
	if !sawHigh || !sawLow || !sawTotal1 || !sawCrit || !sawTotal2 {
		t.Fatalf("missing rows: high=%v low=%v total1=%v crit=%v total2=%v counts=%#v",
			sawHigh, sawLow, sawTotal1, sawCrit, sawTotal2, counts)
	}
}
