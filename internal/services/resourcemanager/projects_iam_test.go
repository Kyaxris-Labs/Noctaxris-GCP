package resourcemanager_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCRMProjectsFoldersIAMAndTagDeletes(t *testing.T) {
	mux, _ := openCRM(t)
	project := "noctaxris-gcp-local"

	req := httptest.NewRequest(http.MethodGet, "/v3/projects", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list projects: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v3/projects/"+project, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get project: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v3/projects:search", bytes.NewReader([]byte(`{"query":"id:`+project+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search projects: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v3/projects/"+project+":getIamPolicy", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("project getIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/projects/"+project+":setIamPolicy",
		bytes.NewReader([]byte(`{"policy":{"etag":"ACAB","bindings":[{"role":"roles/viewer","members":["user:x@y.z"]}]}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("project setIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/projects/"+project+":testIamPermissions",
		bytes.NewReader([]byte(`{"permissions":["resourcemanager.projects.get"]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("project testIamPermissions: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+":getAncestry", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getAncestry: %d %s", rec.Code, rec.Body.String())
	}

	createFolder := []byte(`{"parent":"` + store.DefaultOrganizationName + `","displayName":"CRM Cov Folder"}`)
	req = httptest.NewRequest(http.MethodPost, "/v3/folders", bytes.NewReader(createFolder))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create folder: %d %s", rec.Code, rec.Body.String())
	}
	var folder map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &folder)
	fname, _ := folder["name"].(string)
	if fname == "" {
		t.Fatalf("folder=%#v", folder)
	}
	fid := fname[len("folders/"):]
	req = httptest.NewRequest(http.MethodGet, "/v3/folders/"+fid, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get folder: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, "/v3/folders/"+fid,
		bytes.NewReader([]byte(`{"displayName":"CRM Cov Folder 2"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch folder: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders/"+fid+":getIamPolicy", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder getIamPolicy: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/v3/folders/"+fid+":setIamPolicy",
		bytes.NewReader([]byte(`{"policy":{"etag":"ACAB","bindings":[{"role":"roles/viewer","members":["user:f@x.y"]}]}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("folder setIamPolicy: %d %s", rec.Code, rec.Body.String())
	}

	createKey := []byte(`{"parent":"` + store.DefaultOrganizationName + `","shortName":"covtag","description":"c"}`)
	req = httptest.NewRequest(http.MethodPost, "/v3/tagKeys", bytes.NewReader(createKey))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create tagKey: %d %s", rec.Code, rec.Body.String())
	}
	var tagKey map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tagKey)
	keyID := tagKey["name"].(string)[len("tagKeys/"):]
	bindBody := []byte(`{"parent":"projects/` + project + `","tagValueNamespacedName":"noctaxris-gcp-org/covtag/v1"}`)
	req = httptest.NewRequest(http.MethodPost, "/v3/tagBindings", bytes.NewReader(bindBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create binding: %d %s", rec.Code, rec.Body.String())
	}
	var binding map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &binding)
	bindID := binding["name"].(string)[len("tagBindings/"):]
	req = httptest.NewRequest(http.MethodGet, "/v3/tagBindings?parent=projects/"+project, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list bindings: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, "/v3/tagBindings/"+bindID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete binding: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, "/v3/tagKeys/"+keyID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete tagKey: %d %s", rec.Code, rec.Body.String())
	}
}
