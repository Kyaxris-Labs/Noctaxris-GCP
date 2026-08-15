package pubsub_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

func TestPubSubPatchPushSeekErrors(t *testing.T) {
	mux, project := openPubSubREST(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})
	do := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, path, nil)
		} else {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec
	}
	topic := "/v1/projects/" + project + "/topics/patch-t"
	sub := "/v1/projects/" + project + "/subscriptions/patch-s"
	if rec := do(http.MethodPut, topic, `{}`); rec.Code != http.StatusOK {
		t.Fatalf("topic: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPut, sub, `{"topic":"projects/`+project+`/topics/patch-t"}`); rec.Code != http.StatusOK {
		t.Fatalf("sub: %d %s", rec.Code, rec.Body.String())
	}

	if rec := do(http.MethodPatch, topic, `not-json`); rec.Code == http.StatusOK {
		t.Fatal("bad topic patch")
	}
	if rec := do(http.MethodPatch, sub, `not-json`); rec.Code == http.StatusOK {
		t.Fatal("bad sub patch")
	}
	if rec := do(http.MethodPatch, sub, `{"filter":"!!!invalid"}`); rec.Code == http.StatusOK {
		t.Fatal("bad filter")
	}
	if rec := do(http.MethodPatch, "/v1/projects/"+project+"/subscriptions/missing", `{"ackDeadlineSeconds":10}`); rec.Code != http.StatusNotFound {
		t.Fatalf("missing patch: %d", rec.Code)
	}
	rec := do(http.MethodPatch, sub, `{"pushConfig":{"pushEndpoint":"http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/x","oidcToken":{"serviceAccountEmail":"sa@`+project+`.iam.gserviceaccount.com","audience":"aud"}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch push: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, sub+":modifyPushConfig", `not-json`); rec.Code == http.StatusOK {
		t.Fatal("bad modifyPush")
	}
	if rec := do(http.MethodPost, sub+":modifyPushConfig", `{"pushConfig":{"pushEndpoint":"http://169.254.169.254/"}}`); rec.Code == http.StatusOK {
		t.Fatal("private push endpoint should fail")
	}
	if rec := do(http.MethodPost, sub+":seek", `not-json`); rec.Code == http.StatusOK {
		t.Fatal("bad seek")
	}
	if rec := do(http.MethodPost, sub+":seek", `{"time":"not-a-time"}`); rec.Code == http.StatusOK {
		t.Fatal("bad seek time")
	}

	deny, _ := openPubSubREST(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodGet, topic, nil)
	rec = httptest.NewRecorder()
	deny.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("authz deny: %d", rec.Code)
	}
}
