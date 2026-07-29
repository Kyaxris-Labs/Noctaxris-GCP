package eventarc_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/eventarc"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func mountEventarc(t *testing.T, principal func(*http.Request) (authn.Principal, bool)) (*http.ServeMux, *eventarc.Service) {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
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
	if principal == nil {
		principal = func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: root, IsRoot: true}, true
		}
	}
	mux := http.NewServeMux()
	svc := &eventarc.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, principal)
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/triggers", restlab.Wrap(principal, svc.CreateTriggerHTTP))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/triggers/{trigger}", restlab.Wrap(principal, func(w http.ResponseWriter, r *http.Request, p authn.Principal) {
		if !svc.GetTriggerHTTP(w, r, p) {
			http.NotFound(w, r)
		}
	}))
	return mux, svc
}

func TestEventarcTriggerCreateAndChannel(t *testing.T) {
	mux, _ := mountEventarc(t, nil)
	project := "noctaxris-gcp-local"
	loc := "us-central1"
	trigBase := "/v1/projects/" + project + "/locations/" + loc + "/triggers"
	catcher := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/eventarc-unit"
	body := `{"eventFilters":[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}],"destination":{"httpEndpoint":{"uri":"` + catcher + `"}}}`
	req := httptest.NewRequest(http.MethodPost, trigBase+"?triggerId=lab", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create trigger status=%d body=%s", rec.Code, rec.Body.String())
	}

	chBase := "/v1/projects/" + project + "/locations/" + loc + "/channels"
	req = httptest.NewRequest(http.MethodPost, chBase+"?channelId=ch1", bytes.NewReader([]byte(`{"provider":"custom"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create channel status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEventarcHTTPEndpointSSRFFailClosed(t *testing.T) {
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")

	mux, _ := mountEventarc(t, nil)
	base := "/v1/projects/noctaxris-gcp-local/locations/us-central1/triggers"
	blocked := []string{
		`{"eventFilters":[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}],"destination":{"httpEndpoint":{"uri":"http://metadata.google.internal/computeMetadata/v1/"}}}`,
		`{"eventFilters":[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}],"destination":{"httpEndpoint":{"uri":"https://example.com/hook"}}}`,
		`{"eventFilters":[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}],"destination":{"httpEndpoint":{"uri":"http://169.254.169.254/latest"}}}`,
	}
	for i, body := range blocked {
		req := httptest.NewRequest(http.MethodPost, base+"?triggerId=ssrf"+strconv.Itoa(i), bytes.NewReader([]byte(body)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("trigger %d: expected 400, got %d body=%s", i, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "http egress") {
			t.Fatalf("trigger %d: body=%s", i, rec.Body.String())
		}
	}
}

func TestEventarcAuthzDenyNonRoot(t *testing.T) {
	mux, _ := mountEventarc(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	base := "/v1/projects/noctaxris-gcp-local/locations/us-central1/channels"
	req := httptest.NewRequest(http.MethodGet, base, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
