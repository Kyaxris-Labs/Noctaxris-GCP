package cloudfunctions_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudfunctions"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func mountFunctions(t *testing.T, principal func(*http.Request) (authn.Principal, bool)) (*http.ServeMux, *store.Store) {
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
	svc := &cloudfunctions.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, principal)
	return mux, st
}

func TestCloudFunctionsCRUDAndInvokeStub(t *testing.T) {
	mux, _ := mountFunctions(t, nil)
	loc := cloudfunctions.DefaultLocation
	project := "noctaxris-gcp-local"
	base := "/v2/projects/" + project + "/locations/" + loc + "/functions"

	body := `{"labResponse":{"answer":42}}`
	req := httptest.NewRequest(http.MethodPost, base+"?functionId=fn1", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var fn map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fn); err != nil {
		t.Fatal(err)
	}
	if fn["state"] != "ACTIVE" {
		t.Fatalf("state=%v", fn["state"])
	}

	req = httptest.NewRequest(http.MethodPost, base+"/fn1:invoke", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`42`)) {
		t.Fatalf("invoke stub body=%s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/fn1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudFunctionsAuthzDenyNonRoot(t *testing.T) {
	mux, _ := mountFunctions(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	base := "/v2/projects/noctaxris-gcp-local/locations/" + cloudfunctions.DefaultLocation + "/functions"
	req := httptest.NewRequest(http.MethodGet, base, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudFunctionsInvokeFunctionInvokerBinding(t *testing.T) {
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
	invoker := "fn-invoker@example.com"
	cur := authn.Principal{Email: root, IsRoot: true}
	mux := http.NewServeMux()
	svc := &cloudfunctions.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return cur, true
	})
	loc := cloudfunctions.DefaultLocation
	base := "/v2/projects/" + project + "/locations/" + loc + "/functions"
	body := `{"labResponse":{"answer":1}}`
	req := httptest.NewRequest(http.MethodPost, base+"?functionId=bound", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create bound status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, base+"?functionId=unbound", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create unbound status=%d body=%s", rec.Code, rec.Body.String())
	}
	pol := `{"policy":{"bindings":[{"role":"roles/cloudfunctions.invoker","members":["serviceAccount:` + invoker + `"]}],"etag":"ACAB"}}`
	req = httptest.NewRequest(http.MethodPost, base+"/bound:setIamPolicy", bytes.NewReader([]byte(pol)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIam status=%d body=%s", rec.Code, rec.Body.String())
	}

	cur = authn.Principal{Email: invoker, IsRoot: false}
	req = httptest.NewRequest(http.MethodPost, base+"/bound:invoke", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoker with binding: status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, base+"/unbound:invoke", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("invoker without binding: expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	cur = authn.Principal{Email: root, IsRoot: true}
	req = httptest.NewRequest(http.MethodPost, base+"/unbound:invoke", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("root invoke unbound: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudFunctionsCreateWiresEventarcAndPubSubInvoke(t *testing.T) {
	mux, st := mountFunctions(t, nil)
	store.ClearCloudFunctionInvokes()
	loc := cloudfunctions.DefaultLocation
	project := "noctaxris-gcp-local"
	base := "/v2/projects/" + project + "/locations/" + loc + "/functions"
	topic := "projects/" + project + "/topics/fn-events"
	body := `{
		"labResponse":{"from":"event"},
		"eventTrigger":{
			"eventType":"google.cloud.pubsub.topic.v1.messagePublished",
			"pubsubTopic":"` + topic + `"
		}
	}`
	req := httptest.NewRequest(http.MethodPost, base+"?functionId=evt-fn", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var fn map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fn); err != nil {
		t.Fatal(err)
	}
	et, _ := fn["eventTrigger"].(map[string]any)
	if et == nil {
		t.Fatalf("expected eventTrigger echo: %s", rec.Body.String())
	}
	wantTrigger := "projects/" + project + "/locations/" + loc + "/triggers/function-evt-fn"
	if et["trigger"] != wantTrigger {
		t.Fatalf("trigger=%v want %s", et["trigger"], wantTrigger)
	}

	trig, ok, err := st.GetEventarcTrigger(wantTrigger)
	if err != nil || !ok {
		t.Fatalf("eventarc trigger missing: ok=%v err=%v", ok, err)
	}
	if !bytes.Contains([]byte(trig.DestinationJSON), []byte("cloudFunction")) {
		t.Fatalf("destination=%s", trig.DestinationJSON)
	}
	if !bytes.Contains([]byte(trig.TransportJSON), []byte(topic)) {
		t.Fatalf("transport=%s", trig.TransportJSON)
	}

	st.DeliverEventarcForPubSub(topic, []byte("hello-fn"), map[string]string{"k": "v"})
	invokes := store.ListCloudFunctionInvokes()
	fnName := "projects/" + project + "/locations/" + loc + "/functions/evt-fn"
	found := false
	for _, inv := range invokes {
		if inv.Function == fnName && (bytes.Contains([]byte(inv.Body), []byte("hello-fn")) ||
			bytes.Contains([]byte(inv.Body), []byte("messagePublished"))) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected function invoke record; got %#v", invokes)
	}
}

func TestCloudFunctionsEventarcGCSFinalizeInvoke(t *testing.T) {
	mux, st := mountFunctions(t, nil)
	store.ClearCloudFunctionInvokes()
	loc := cloudfunctions.DefaultLocation
	project := "noctaxris-gcp-local"
	base := "/v2/projects/" + project + "/locations/" + loc + "/functions"
	bucket := "evt-bucket"
	body := `{
		"labResponse":{"from":"gcs"},
		"eventTrigger":{
			"eventType":"google.cloud.storage.object.v1.finalized",
			"eventFilters":[{"attribute":"bucket","value":"` + bucket + `"}]
		}
	}`
	req := httptest.NewRequest(http.MethodPost, base+"?functionId=gcs-fn", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	st.DeliverEventarcForGCSFinalize(bucket, "obj.txt", 1, 4, "text/plain")
	invokes := store.ListCloudFunctionInvokes()
	fnName := "projects/" + project + "/locations/" + loc + "/functions/gcs-fn"
	found := false
	for _, inv := range invokes {
		if inv.Function == fnName && bytes.Contains([]byte(inv.Body), []byte("finalized")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected GCS finalize invoke; got %#v", invokes)
	}
}

func TestEventarcCloudFunctionDestinationObjectShape(t *testing.T) {
	_, st := mountFunctions(t, nil)
	store.ClearCloudFunctionInvokes()
	project := "noctaxris-gcp-local"
	loc := cloudfunctions.DefaultLocation
	fnName := "projects/" + project + "/locations/" + loc + "/functions/obj-fn"
	created, err := st.CreateCloudFunction(store.CloudFunction{
		Name: fnName, ProjectID: project, Location: loc, FunctionID: "obj-fn",
		LabResponseJSON: `{"ok":true}`,
	})
	if err != nil || !created {
		t.Fatalf("create fn: created=%v err=%v", created, err)
	}
	dest := `{"cloudFunction":{"service":"obj-fn","region":"` + loc + `"}}`
	_, created, err = st.CreateEventarcTrigger(store.EventarcTrigger{
		ProjectID: project, Location: loc, TriggerID: "obj-trig",
		FiltersJSON:     `[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}]`,
		DestinationJSON: dest,
		TransportJSON:   `{"pubsub":{"topic":"projects/` + project + `/topics/t"}}`,
	})
	if err != nil || !created {
		t.Fatalf("create trigger: created=%v err=%v", created, err)
	}
	st.DeliverEventarcForPubSub("projects/"+project+"/topics/t", []byte("via-obj"), nil)
	invokes := store.ListCloudFunctionInvokes()
	if len(invokes) == 0 || invokes[0].Function != fnName {
		t.Fatalf("expected invoke to %s; got %#v", fnName, invokes)
	}
}
