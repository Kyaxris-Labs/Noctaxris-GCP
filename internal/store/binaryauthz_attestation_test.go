package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestBinaryAuthzEnforcedRequiresAttestationNote(t *testing.T) {
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
	const project = "noctaxris-gcp-local"
	image := "us-docker.pkg.dev/lib/app:1"
	if err := st.PutBinaryAuthzPolicy(project, "ENFORCED_BLOCK_AND_AUDIT_LOG", `{}`); err != nil {
		t.Fatal(err)
	}
	// Occurrence without noteName must not admit.
	if err := st.PutContainerOccurrence(store.ContainerOccurrence{
		Name: "projects/" + project + "/occurrences/empty-note", ProjectID: project, OccurrenceID: "empty-note",
		ResourceURI: image, Kind: "ATTESTATION", NoteName: "",
	}); err != nil {
		t.Fatal(err)
	}
	allow, err := st.BinaryAuthzAllows(project, image)
	if err != nil || allow {
		t.Fatalf("empty noteName must deny: allow=%v err=%v", allow, err)
	}
	// Non-attestation kind must not admit.
	if err := st.PutContainerOccurrence(store.ContainerOccurrence{
		Name: "projects/" + project + "/occurrences/vuln", ProjectID: project, OccurrenceID: "vuln",
		ResourceURI: image, Kind: "VULNERABILITY", NoteName: "projects/" + project + "/notes/v",
	}); err != nil {
		t.Fatal(err)
	}
	allow, err = st.BinaryAuthzAllows(project, image)
	if err != nil || allow {
		t.Fatalf("non-attestation must deny: allow=%v err=%v", allow, err)
	}
	if err := st.PutContainerOccurrence(store.ContainerOccurrence{
		Name: "projects/" + project + "/occurrences/ok", ProjectID: project, OccurrenceID: "ok",
		ResourceURI: image, Kind: "ATTESTATION", NoteName: "projects/" + project + "/notes/attestor",
	}); err != nil {
		t.Fatal(err)
	}
	allow, err = st.BinaryAuthzAllows(project, image)
	if err != nil || !allow {
		t.Fatalf("attestation with note must allow: allow=%v err=%v", allow, err)
	}
}

func TestBinaryAuthzEnforcedRequireAttestationsBy(t *testing.T) {
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
	const project = "noctaxris-gcp-local"
	image := "us-docker.pkg.dev/lib/app:required-notes"
	required := "projects/" + project + "/attestors/prod"
	body := `{"defaultAdmissionRule":{"requireAttestationsBy":["` + required + `"]}}`
	if err := st.PutBinaryAuthzPolicy(project, "ENFORCED_BLOCK_AND_AUDIT_LOG", body); err != nil {
		t.Fatal(err)
	}
	if err := st.PutContainerOccurrence(store.ContainerOccurrence{
		Name: "projects/" + project + "/occurrences/wrong-note", ProjectID: project, OccurrenceID: "wrong-note",
		ResourceURI: image, Kind: "ATTESTATION", NoteName: "projects/" + project + "/notes/other",
	}); err != nil {
		t.Fatal(err)
	}
	allow, err := st.BinaryAuthzAllows(project, image)
	if err != nil || allow {
		t.Fatalf("mismatched note must deny: allow=%v err=%v", allow, err)
	}
	if err := st.PutContainerOccurrence(store.ContainerOccurrence{
		Name: "projects/" + project + "/occurrences/right-note", ProjectID: project, OccurrenceID: "right-note",
		ResourceURI: image, Kind: "ATTESTATION", NoteName: required,
	}); err != nil {
		t.Fatal(err)
	}
	allow, err = st.BinaryAuthzAllows(project, image)
	if err != nil || !allow {
		t.Fatalf("required note must allow: allow=%v err=%v", allow, err)
	}
}
