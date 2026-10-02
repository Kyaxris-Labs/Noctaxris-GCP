package resourcemanager_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCRMFolderMoveErrorsGetPatchDelete(t *testing.T) {
	mux, _ := openCRM(t)
	org := store.DefaultOrganizationName

	create := []byte(`{"parent":"` + org + `","displayName":"MoveSrc"}`)
	req := httptest.NewRequest(http.MethodPost, "/v3/folders", bytes.NewReader(create))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var folder map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)
	id := folder["name"].(string)[len("folders/"):]

	req = httptest.NewRequest(http.MethodGet, "/v3/folders/"+id, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get folder status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v3/folders/missing-folder-xyz", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPatch, "/v3/folders/"+id+"?updateMask=displayName",
		bytes.NewReader([]byte(`{"displayName":"Moved Name"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v3/folders/"+id+":move",
		bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("move bad json")
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders/"+id+":move",
		bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("move missing destination")
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders/"+id+":move",
		bytes.NewReader([]byte(`{"destinationParent":"projects/noctaxris-gcp-local"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("move bad destination type")
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders/"+id+":move",
		bytes.NewReader([]byte(`{"destinationParent":"organizations/no-such-org"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("move missing org status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders/"+id+":move",
		bytes.NewReader([]byte(`{"destinationParent":"folders/no-such-dest"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("move missing folder dest status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders/"+id+":move",
		bytes.NewReader([]byte(`{"destinationParent":"`+org+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("move to org status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v3/folders/"+id, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, "/v3/folders/missing-folder-xyz", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v3/folders/"+id+":undelete", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("undelete status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders/missing-folder-xyz:undelete", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("undelete missing status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v3/organizations/"+store.DefaultOrganizationID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get org status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v3/organizations/no-such-org", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing org status=%d", rec.Code)
	}
}

func TestCRMTagKeysAndBindingsCRUD(t *testing.T) {
	mux, _ := openCRM(t)
	org := store.DefaultOrganizationName

	req := httptest.NewRequest(http.MethodPost, "/v3/tagKeys",
		bytes.NewReader([]byte(`{"parent":"`+org+`","shortName":"env","description":"lab"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create tag key status=%d body=%s", rec.Code, rec.Body.String())
	}
	var key map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &key)
	keyName, _ := key["name"].(string)
	if keyName == "" {
		t.Fatalf("key=%#v", key)
	}
	keyID := keyName[len("tagKeys/"):]

	req = httptest.NewRequest(http.MethodPost, "/v3/tagKeys",
		bytes.NewReader([]byte(`{"parent":"`+org+`","shortName":"env"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup tag key status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/tagKeys", bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad json tag key")
	}

	req = httptest.NewRequest(http.MethodGet, "/v3/tagKeys?parent="+org, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tag keys status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/v3/tagKeys/"+keyID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get tag key status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v3/tagKeys/missing-key", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing tag key status=%d", rec.Code)
	}

	bindBody := `{"parent":"projects/noctaxris-gcp-local","tagValueNamespacedName":"` + org + `/env/prod"}`
	req = httptest.NewRequest(http.MethodPost, "/v3/tagBindings", bytes.NewReader([]byte(bindBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create binding status=%d body=%s", rec.Code, rec.Body.String())
	}
	var binding map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &binding)
	bindName, _ := binding["name"].(string)
	if bindName == "" {
		t.Fatalf("binding=%#v", binding)
	}
	bindID := bindName[len("tagBindings/"):]

	req = httptest.NewRequest(http.MethodPost, "/v3/tagBindings",
		bytes.NewReader([]byte(`{"parent":"","tagValue":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("binding missing parent")
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/tagBindings",
		bytes.NewReader([]byte(`{"parent":"projects/noctaxris-gcp-local"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("binding missing tag value")
	}

	req = httptest.NewRequest(http.MethodGet, "/v3/tagBindings?parent=projects/noctaxris-gcp-local", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list bindings status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/v3/tagBindings/"+bindID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get binding status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v3/tagBindings/missing-bind", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing binding status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v3/tagBindings/"+bindID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete binding status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, "/v3/tagBindings/missing-bind", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing binding status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v3/tagKeys/"+keyID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete tag key status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, "/v3/tagKeys/missing-key", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing tag key status=%d", rec.Code)
	}
}
