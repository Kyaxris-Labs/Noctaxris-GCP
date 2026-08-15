package eventarc_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/eventarc"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func mountEventarcFull(t *testing.T, principal func(*http.Request) (authn.Principal, bool)) (*http.ServeMux, *eventarc.Service) {
	t.Helper()
	mux, svc := mountEventarc(t, principal)
	if principal == nil {
		principal = func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
		}
	}
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/triggers", restlab.Wrap(principal, svc.ListTriggersHTTP))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/triggers/{trigger}", restlab.Wrap(principal, func(w http.ResponseWriter, r *http.Request, p authn.Principal) {
		if !svc.DeleteTriggerHTTP(w, r, p) {
			http.NotFound(w, r)
		}
	}))
	return mux, svc
}

func TestEventarcChannelAndTriggerCRUD(t *testing.T) {
	mux, svc := mountEventarcFull(t, nil)
	project := "noctaxris-gcp-local"
	loc := "us-central1"
	trigBase := "/v1/projects/" + project + "/locations/" + loc + "/triggers"
	chBase := "/v1/projects/" + project + "/locations/" + loc + "/channels"

	body := `{"eventFilters":[{"attribute":"type","value":"google.cloud.storage.object.v1.finalized"}],"destination":{"cloudRunService":{"service":"svc"}},"serviceAccount":"sa@` + project + `.iam.gserviceaccount.com","channel":"projects/` + project + `/locations/` + loc + `/channels/ch1"}`
	req := httptest.NewRequest(http.MethodPost, trigBase+"?triggerId=crud-trig", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create trigger: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, trigBase+"?triggerId=crud-trig", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup trigger: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, trigBase+"/crud-trig", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get trigger: %d %s", rec.Code, rec.Body.String())
	}
	var trig map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &trig)
	if trig["name"] == nil {
		t.Fatalf("trigger=%#v", trig)
	}
	_ = eventarc.TriggerResourceJSON(&store.EventarcTrigger{
		Name: "projects/" + project + "/locations/" + loc + "/triggers/crud-trig",
		FiltersJSON: `[]`, DestinationJSON: `{}`, TransportJSON: `{}`,
	})

	req = httptest.NewRequest(http.MethodGet, trigBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list triggers: %d %s", rec.Code, rec.Body.String())
	}

	root := authn.Principal{Email: "root@" + project + ".iam.gserviceaccount.com", IsRoot: true}
	if !svc.MayListTriggers(root, project) {
		t.Fatal("root should list triggers")
	}
	if svc.MayListTriggers(authn.Principal{Email: "nobody@example.com"}, project) {
		t.Fatal("non-root should not list")
	}
	if !eventarc.LooksLikeEventarcTrigger(map[string]any{"eventFilters": []any{}}) {
		t.Fatal("LooksLikeEventarcTrigger filters")
	}
	if !eventarc.LooksLikeEventarcTrigger(map[string]any{"destination": map[string]any{}}) {
		t.Fatal("LooksLikeEventarcTrigger destination")
	}
	if eventarc.LooksLikeEventarcTrigger(map[string]any{"name": "x"}) {
		t.Fatal("LooksLikeEventarcTrigger false")
	}
	if eventarc.LooksLikeEventarcTrigger(nil) {
		t.Fatal("LooksLikeEventarcTrigger nil")
	}

	req = httptest.NewRequest(http.MethodPost, chBase+"?channelId=ch-crud", bytes.NewReader([]byte(`{"provider":"custom","pubsubTopic":"projects/`+project+`/topics/t"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create channel: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, chBase+"?channelId=ch-crud", bytes.NewReader([]byte(`{"provider":"custom"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup channel: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, chBase+"/ch-crud", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get channel: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, chBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list channels: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, trigBase+"/crud-trig", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete trigger: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, trigBase+"/crud-trig", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing trigger: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, trigBase+"/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing trigger: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, chBase+"/ch-crud", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete channel: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, chBase+"/ch-crud", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing channel: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, chBase+"/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing channel: %d", rec.Code)
	}
}

func TestEventarcValidationErrors(t *testing.T) {
	mux, _ := mountEventarcFull(t, nil)
	base := "/v1/projects/noctaxris-gcp-local/locations/us-central1/triggers"
	cases := []struct {
		q    string
		body string
	}{
		{"", `{"eventFilters":[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}]}`},
		{"?triggerId=badtype", `{"eventFilters":[{"attribute":"type","value":"unsupported.event"}],"destination":{}}`},
		{"?triggerId=notype", `{"eventFilters":[{"attribute":"source","value":"x"}],"destination":{}}`},
		{"?triggerId=badjson", `{`},
	}
	for i, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, base+tc.q, bytes.NewReader([]byte(tc.body)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("case %d: expected 400, got %d body=%s", i, rec.Code, rec.Body.String())
		}
	}
	chBase := "/v1/projects/noctaxris-gcp-local/locations/us-central1/channels"
	req := httptest.NewRequest(http.MethodPost, chBase, bytes.NewReader([]byte(`{"provider":"custom"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("channel without id: %d", rec.Code)
	}
}
