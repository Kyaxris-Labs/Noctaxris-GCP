package containeranalysis_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/containeranalysis"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCreateOccurrenceRequiresNoteName(t *testing.T) {
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
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&containeranalysis.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/occurrences?occurrenceId=no-note",
		bytes.NewReader([]byte(`{"resourceUri":"img:1","kind":"ATTESTATION"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty noteName status=%d body=%s", rec.Code, rec.Body.String())
	}
}
