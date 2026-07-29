package pubsub_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/pubsub"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func openPubSubREST(t *testing.T, principal func(*http.Request) (authn.Principal, bool)) (*http.ServeMux, string) {
	t.Helper()
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
	project := "noctaxris-gcp-local"
	rootSA := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, rootSA); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &pubsub.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.RegisterREST(mux, principal)
	return mux, project
}

func TestPubSubRESTCreatePublishListHappy(t *testing.T) {
	mux, project := openPubSubREST(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})
	topicPath := "/v1/projects/" + project + "/topics/lab-happy"
	req := httptest.NewRequest(http.MethodPut, topicPath, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create topic status=%d body=%s", rec.Code, rec.Body.String())
	}

	pub := httptest.NewRequest(http.MethodPost, topicPath+":publish",
		strings.NewReader(`{"messages":[{"data":"aGVsbG8="}]}`))
	pub.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, pub)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pubResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &pubResp); err != nil {
		t.Fatal(err)
	}
	ids, _ := pubResp["messageIds"].([]any)
	if len(ids) != 1 {
		t.Fatalf("messageIds=%#v", pubResp)
	}

	list := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/topics", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, list)
	if rec.Code != http.StatusOK {
		t.Fatalf("list topics status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	topics, _ := listResp["topics"].([]any)
	if len(topics) < 1 {
		t.Fatalf("topics=%#v", listResp)
	}
}

func TestPubSubRESTAuthzDenyNonRoot(t *testing.T) {
	mux, project := openPubSubREST(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodPut, "/v1/projects/"+project+"/topics/deny-topic", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
