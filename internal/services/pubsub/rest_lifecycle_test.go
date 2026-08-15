package pubsub_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

func TestPubSubRESTFullLifecycle(t *testing.T) {
	mux, project := openPubSubREST(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})
	topicPath := "/v1/projects/" + project + "/topics/life-topic"
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

	rec := do(http.MethodPut, topicPath, `{"labels":{"env":"lab"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create topic: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPut, topicPath, `{}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate topic: %d", rec.Code)
	}
	rec = do(http.MethodGet, topicPath, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get topic: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPatch, topicPath, `{"labels":{"env":"prod"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch topic: %d %s", rec.Code, rec.Body.String())
	}
	var topic map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &topic)
	labels, _ := topic["labels"].(map[string]any)
	if labels["env"] != "prod" {
		t.Fatalf("labels=%#v", labels)
	}

	dlTopic := "/v1/projects/" + project + "/topics/dlq"
	rec = do(http.MethodPut, dlTopic, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create dlq: %d %s", rec.Code, rec.Body.String())
	}

	subPath := "/v1/projects/" + project + "/subscriptions/life-sub"
	subBody := `{
		"topic":"projects/` + project + `/topics/life-topic",
		"ackDeadlineSeconds":20,
		"labels":{"k":"v"},
		"filter":"attributes.k = \"v\"",
		"enableExactlyOnceDelivery":true,
		"deadLetterPolicy":{"deadLetterTopic":"projects/` + project + `/topics/dlq","maxDeliveryAttempts":5}
	}`
	rec = do(http.MethodPut, subPath, subBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("create sub: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPut, subPath, subBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate sub: %d", rec.Code)
	}
	rec = do(http.MethodGet, subPath, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get sub: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/v1/projects/"+project+"/subscriptions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list subs: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPatch, subPath, `{"ackDeadlineSeconds":30,"labels":{"k":"v2"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch sub: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, topicPath+":publish", `{"messages":[{"data":"aGVsbG8=","attributes":{"k":"v"}},{"data":"plain-text","attributes":{"k":"other"}}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, subPath+":pull", `{"maxMessages":10}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("pull: %d %s", rec.Code, rec.Body.String())
	}
	var pullResp struct {
		ReceivedMessages []struct {
			AckID   string `json:"ackId"`
			Message struct {
				Data string `json:"data"`
			} `json:"message"`
		} `json:"receivedMessages"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &pullResp)
	if len(pullResp.ReceivedMessages) < 1 {
		t.Fatalf("expected filtered messages, got %#v", pullResp)
	}
	ackID := pullResp.ReceivedMessages[0].AckID

	rec = do(http.MethodPost, subPath+":modifyAckDeadline", `{"ackIds":["`+ackID+`"],"ackDeadlineSeconds":60}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("modifyAckDeadline: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, subPath+":acknowledge", `{"ackIds":["`+ackID+`"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("ack: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, subPath+":modifyPushConfig", `{"pushConfig":{"pushEndpoint":""}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("modifyPushConfig clear: %d %s", rec.Code, rec.Body.String())
	}

	seekTime := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	rec = do(http.MethodPost, subPath+":seek", `{"time":"`+seekTime+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("seek: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, subPath+":seek", `{"snapshot":"projects/`+project+`/snapshots/x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("seek snapshot: %d", rec.Code)
	}
	rec = do(http.MethodPost, subPath+":seek", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("seek empty: %d", rec.Code)
	}
	rec = do(http.MethodPost, subPath+":unknown", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown sub method: %d", rec.Code)
	}
	rec = do(http.MethodPost, topicPath+":unknown", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown topic method: %d", rec.Code)
	}

	snapPath := "/v1/projects/" + project + "/snapshots/life-snap"
	rec = do(http.MethodPut, snapPath, `{"subscription":"projects/`+project+`/subscriptions/life-sub","labels":{"s":"1"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create snap: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPut, snapPath, `{"subscription":"projects/`+project+`/subscriptions/life-sub"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup snap: %d", rec.Code)
	}
	rec = do(http.MethodGet, snapPath, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get snap: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/v1/projects/"+project+"/snapshots", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list snaps: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, snapPath, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete snap: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, snapPath, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing snap: %d", rec.Code)
	}

	rec = do(http.MethodDelete, subPath, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete sub: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, subPath, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing sub: %d", rec.Code)
	}
	rec = do(http.MethodDelete, topicPath, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete topic: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, topicPath, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing topic: %d", rec.Code)
	}
	rec = do(http.MethodGet, topicPath, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing topic: %d", rec.Code)
	}
	rec = do(http.MethodGet, subPath, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing sub: %d", rec.Code)
	}
	rec = do(http.MethodGet, snapPath, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing snap: %d", rec.Code)
	}
}

func TestPubSubRESTUnauthenticatedAndNotFound(t *testing.T) {
	mux, project := openPubSubREST(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/topics", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("nil principal: %d", rec.Code)
	}

	mux2, project := openPubSubREST(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	})
	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/topics", nil)
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("false principal: %d", rec.Code)
	}

	mux3, project := openPubSubREST(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/topics/missing:publish",
		strings.NewReader(`{"messages":[{"data":"YQ=="}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux3.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("publish missing: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPut, "/v1/projects/"+project+"/subscriptions/orphan",
		strings.NewReader(`{"topic":"projects/`+project+`/topics/nope"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux3.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("sub missing topic: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPut, "/v1/projects/"+project+"/snapshots/s1",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux3.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("snap no subscription: %d", rec.Code)
	}
}
