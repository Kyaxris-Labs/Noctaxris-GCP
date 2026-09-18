package cloudfunctions_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudfunctions"
)

func TestGenerateDownloadUrl(t *testing.T) {
	mux, _ := mountFunctions(t, nil)
	loc := cloudfunctions.DefaultLocation
	project := "noctaxris-gcp-local"
	base := "/v2/projects/" + project + "/locations/" + loc + "/functions"

	req := httptest.NewRequest(http.MethodPost, base+"?functionId=dl-fn", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/dl-fn:generateDownloadUrl", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("generateDownloadUrl status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["downloadUrl"] == nil {
		t.Fatalf("missing downloadUrl %#v", out)
	}
}
