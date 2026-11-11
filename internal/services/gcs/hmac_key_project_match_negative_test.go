package gcs_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	gcshandler "github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/gcs"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestHMACKeyProjectMatchNegative(t *testing.T) {
	const (
		projectQ        = "quartz-owned"
		projectP        = "pebble-other"
		projectPrefix   = "quartz"
		projectLonger   = "quartz-owned-extra"
		projectCase     = "Quartz-Owned"
		projectContains = "north-quartz-south"
	)
	email := "hmac-admin@" + projectQ + ".iam.gserviceaccount.com"
	projects := []string{projectQ, projectP, projectPrefix, projectLonger, projectCase, projectContains}
	mux, st, dir := openHMACProjectMux(t, email, projects)

	keyQ := mustCreateHMACKey(t, st, projectQ, email)
	keyP := mustCreateHMACKey(t, st, projectP, email)
	keyPrefix := mustCreateHMACKey(t, st, projectPrefix, email)
	keyLonger := mustCreateHMACKey(t, st, projectLonger, email)
	keyContains := mustCreateHMACKey(t, st, projectContains, email)
	// CreateHMACKey rejects an empty project. The row is rewritten in place so a
	// DELETE that skipped the project match would remove it.
	keyEmpty := mustCreateHMACKey(t, st, projectQ, email)
	setHMACKeyProject(t, dir, keyEmpty.AccessID, "")

	t.Run("other project", func(t *testing.T) {
		requireHMACKeyNotFound(t, mux, projectP, keyQ.AccessID)
		requireStoredHMACSecret(t, st, keyQ.AccessID, projectQ, keyQ.Secret)
		requireHMACKeyVisible(t, mux, projectQ, keyQ)
		requireHMACKeyVisible(t, mux, projectP, keyP)
		requireListedHMAC(t, mux, projectP, keyP.AccessID, keyQ.AccessID)
		requireListedHMAC(t, mux, projectQ, keyQ.AccessID, keyP.AccessID)
	})

	t.Run("empty project id", func(t *testing.T) {
		for _, project := range []string{projectP, projectQ, projectPrefix, projectCase} {
			requireHMACKeyNotFound(t, mux, project, keyEmpty.AccessID)
			requireHMACKeyNotFound(t, mux, project, store.LabGCSHMACAccessID)
		}
		requireStoredHMACSecret(t, st, keyEmpty.AccessID, "", keyEmpty.Secret)
		lab, ok, err := st.GetHMACKey(store.LabGCSHMACAccessID)
		if err != nil || !ok || lab.ProjectID != "" || lab.Secret != store.LabGCSHMACSecret {
			t.Fatalf("lab key after delete ok=%v err=%v got=%#v", ok, err, lab)
		}
		requireListedHMAC(t, mux, projectQ, keyQ.AccessID, keyEmpty.AccessID)
	})

	t.Run("url project prefix", func(t *testing.T) {
		requireHMACKeyNotFound(t, mux, projectPrefix, keyQ.AccessID)
		requireHMACKeyNotFound(t, mux, projectQ, keyPrefix.AccessID)
		requireHMACKeyNotFound(t, mux, projectLonger, keyQ.AccessID)
		requireHMACKeyNotFound(t, mux, projectPrefix, keyLonger.AccessID)
		requireHMACKeyNotFound(t, mux, projectCase, keyQ.AccessID)
		requireHMACKeyNotFound(t, mux, projectPrefix, keyContains.AccessID)
		requireStoredHMACSecret(t, st, keyQ.AccessID, projectQ, keyQ.Secret)
		requireStoredHMACSecret(t, st, keyPrefix.AccessID, projectPrefix, keyPrefix.Secret)
		requireStoredHMACSecret(t, st, keyLonger.AccessID, projectLonger, keyLonger.Secret)
		requireStoredHMACSecret(t, st, keyContains.AccessID, projectContains, keyContains.Secret)
		requireHMACKeyVisible(t, mux, projectPrefix, keyPrefix)
		requireListedHMAC(t, mux, projectPrefix, keyPrefix.AccessID, keyQ.AccessID, keyContains.AccessID, keyLonger.AccessID)
	})
}

func openHMACProjectMux(t *testing.T, email string, projects []string) (*http.ServeMux, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: projects[0], Email: email, UniqueID: "hmac-admin-quartz", DisplayName: "hmac-admin",
	}); err != nil {
		t.Fatal(err)
	}
	for _, project := range projects {
		if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
			Bindings: []authz.Binding{{
				Role:    "roles/storage.hmacKeyAdmin",
				Members: []string{"serviceAccount:" + email},
			}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	h := &gcshandler.Handler{
		Store:          st,
		Authz:          &authz.Evaluator{Policies: st},
		DefaultProject: "noctaxris-gcp-local",
		Principal: func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: email, IsRoot: false}, true
		},
	}
	h.Register(mux)
	return mux, st, dir
}

func mustCreateHMACKey(t *testing.T, st *store.Store, project, email string) *store.HMACKey {
	t.Helper()
	k, err := st.CreateHMACKey(project, email)
	if err != nil {
		t.Fatal(err)
	}
	if k.ProjectID != project || k.Secret == "" {
		t.Fatalf("created=%#v", k)
	}
	return k
}

func setHMACKeyProject(t *testing.T, dataRoot, accessID, project string) {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(filepath.Join(dataRoot, "state.db")) + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	res, err := db.Exec(`UPDATE gcs_hmac_keys SET project_id = ? WHERE access_id = ?`, project, accessID)
	if err != nil {
		t.Fatal(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("updated %d rows for %s", n, accessID)
	}
}

func requireHMACKeyNotFound(t *testing.T, mux http.Handler, project, accessID string) {
	t.Helper()
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		rec := serveHMACKey(mux, method, project, accessID)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s project=%s status=%d body=%s", method, project, rec.Code, rec.Body.String())
		}
		var body struct {
			Error struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s project=%s body=%s err=%v", method, project, rec.Body.String(), err)
		}
		if body.Error.Status != "NOT_FOUND" || body.Error.Message != "hmac key not found" {
			t.Fatalf("%s project=%s body=%s", method, project, rec.Body.String())
		}
	}
}

func requireHMACKeyVisible(t *testing.T, mux http.Handler, project string, k *store.HMACKey) {
	t.Helper()
	rec := serveHMACKey(mux, http.MethodGet, project, k.AccessID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get project=%s status=%d body=%s", project, rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["accessId"] != k.AccessID || got["projectId"] != project {
		t.Fatalf("got=%#v", got)
	}
	if _, hasSecret := got["secret"]; hasSecret {
		t.Fatalf("get must hide secret: %#v", got)
	}
}

func requireStoredHMACSecret(t *testing.T, st *store.Store, accessID, project, secret string) {
	t.Helper()
	got, ok, err := st.GetHMACKey(accessID)
	if err != nil || !ok {
		t.Fatalf("key %s present=%v err=%v", accessID, ok, err)
	}
	if got.ProjectID != project || got.AccessID != accessID || got.Secret != secret {
		t.Fatalf("stored=%#v", got)
	}
}

func requireListedHMAC(t *testing.T, mux http.Handler, project, want string, absent ...string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/storage/v1/projects/"+project+"/hmacKeys", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list project=%s status=%d body=%s", project, rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			AccessID string `json:"accessId"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	found := false
	seen := map[string]bool{}
	for _, item := range body.Items {
		seen[item.AccessID] = true
		if item.AccessID == want {
			found = true
		}
	}
	if want != "" && !found {
		t.Fatalf("list project=%s missing %s items=%v", project, want, seen)
	}
	for _, id := range absent {
		if seen[id] {
			t.Fatalf("list project=%s leaked %s", project, id)
		}
	}
}
