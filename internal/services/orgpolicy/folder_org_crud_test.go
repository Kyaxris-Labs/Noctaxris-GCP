package orgpolicy_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestOrgPolicyFolderAndOrgCRUD(t *testing.T) {
	h := open(t)
	folder, created, err := h.store.CreateFolder(store.Folder{
		FolderID: "lab-folder", DisplayName: "Lab Folder", Parent: store.DefaultOrganizationName,
	})
	if err != nil || !created {
		t.Fatalf("create folder: created=%v err=%v", created, err)
	}
	folderID := folder.FolderID
	if folderID == "" {
		folderID = "lab-folder"
	}
	constraint := store.ConstraintDisableServiceAccountKeyCreation

	req := httptest.NewRequest(http.MethodGet, "/v2/folders/"+folderID+"/constraints", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder constraints: %d %s", rec.Code, rec.Body.String())
	}

	body := `{"name":"folders/` + folderID + `/policies/` + constraint + `","spec":{"rules":[{"enforce":true}]}}`
	req = httptest.NewRequest(http.MethodPost, "/v2/folders/"+folderID+"/policies?constraint="+constraint, bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/folders/"+folderID+"/policies", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder list: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/folders/"+folderID+"/policies/"+constraint, nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder get: %d %s", rec.Code, rec.Body.String())
	}

	patch := `{"spec":{"rules":[{"enforce":false}]}}`
	req = httptest.NewRequest(http.MethodPatch, "/v2/folders/"+folderID+"/policies/"+constraint, bytes.NewReader([]byte(patch)))
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder patch: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/folders/"+folderID+"/policies/"+constraint+":getEffectivePolicy", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder effective: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v2/folders/"+folderID+"/policies/"+constraint, nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder delete: %d %s", rec.Code, rec.Body.String())
	}

	org := store.DefaultOrganizationID
	orgConstraint := store.ConstraintStoragePublicAccessPrevention
	body = `{"name":"organizations/` + org + `/policies/` + orgConstraint + `","spec":{"rules":[{"enforce":true}]}}`
	req = httptest.NewRequest(http.MethodPost, "/v2/organizations/"+org+"/policies?constraint="+orgConstraint, bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("org create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/organizations/"+org+"/policies", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("org list: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Policies []map[string]any `json:"policies"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Policies) < 1 {
		t.Fatalf("org policies empty: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/organizations/"+org+"/policies/"+orgConstraint, nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("org get: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/v2/organizations/"+org+"/policies/"+orgConstraint, bytes.NewReader([]byte(patch)))
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("org patch: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/projects/noctaxris-gcp-local/constraints", nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("project constraints: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v2/organizations/"+org+"/policies/"+orgConstraint, nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("org delete: %d %s", rec.Code, rec.Body.String())
	}

	project := "noctaxris-gcp-local"
	body = `{"name":"projects/` + project + `/policies/` + constraint + `","spec":{"rules":[{"enforce":true}]}}`
	req = httptest.NewRequest(http.MethodPost, "/v2/projects/"+project+"/policies?constraint="+constraint, bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("project create: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v2/projects/"+project+"/policies/"+constraint, nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("project get: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, "/v2/projects/"+project+"/policies/"+constraint, bytes.NewReader([]byte(patch)))
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("project patch: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, "/v2/projects/"+project+"/policies/"+constraint, nil)
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("project delete: %d %s", rec.Code, rec.Body.String())
	}
}
