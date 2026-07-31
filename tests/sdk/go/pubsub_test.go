package sdk_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPubSubCreateAndListTopicsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	topicID := uniqueID("sdk-topic")
	topicPath := ep + "/v1/projects/" + project + "/topics/" + topicID

	status, body := doJSON(t, http.MethodPut, topicPath, token, map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("create topic status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, topicPath, token, nil)
	})

	status, body = doJSON(t, http.MethodGet, ep+"/v1/projects/"+project+"/topics", token, nil)
	if status != http.StatusOK {
		t.Fatalf("list topics status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode topics: %v body=%s", err, body)
	}
	if _, ok := parsed["topics"]; !ok {
		t.Fatalf("missing topics field body=%s", body)
	}
}

func TestPubSubOIDCPushSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	topicID := uniqueID("sdk-oidc-topic")
	subID := uniqueID("sdk-oidc-sub")
	topicPath := ep + "/v1/projects/" + project + "/topics/" + topicID
	subPath := ep + "/v1/projects/" + project + "/subscriptions/" + subID
	catcher := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/sdk-oidc-push"
	saEmail := "push-sa@" + project + ".iam.gserviceaccount.com"
	audience := "https://example.com/sdk-oidc"

	status, body := doJSON(t, http.MethodPut, topicPath, token, map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("create topic status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, subPath, token, nil)
		_, _, _ = doJSONErr(http.MethodDelete, topicPath, token, nil)
	})

	status, body = doJSON(t, http.MethodPut, subPath, token, map[string]any{
		"topic":              "projects/" + project + "/topics/" + topicID,
		"ackDeadlineSeconds": 10,
		"pushConfig": map[string]any{
			"pushEndpoint": catcher,
			"oidcToken": map[string]any{
				"serviceAccountEmail": saEmail,
				"audience":            audience,
			},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("create subscription status=%d body=%s", status, body)
	}
	var created map[string]any
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode subscription: %v body=%s", err, body)
	}
	pc, _ := created["pushConfig"].(map[string]any)
	oidc, _ := pc["oidcToken"].(map[string]any)
	if pc["pushEndpoint"] != catcher || oidc["serviceAccountEmail"] != saEmail || oidc["audience"] != audience {
		t.Fatalf("oidcToken round-trip failed: %#v", created["pushConfig"])
	}

	payload := base64.StdEncoding.EncodeToString([]byte("sdk-oidc-ping"))
	status, body = doJSON(t, http.MethodPost, topicPath+":publish", token, map[string]any{
		"messages": []any{map[string]any{"data": payload}},
	})
	if status != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", status, body)
	}

	t.Run("catcherAuthorization", func(t *testing.T) {
		status, dump, err := doJSONErr(http.MethodGet, ep+"/_noctaxris-gcp/http-catcher", token, nil)
		if err != nil || status != http.StatusOK {
			t.Skipf("lab catcher dump unavailable (status=%d err=%v); soft-skip Authorization assert (unit TestPubSubOIDCPushCatcher covers Bearer JWT)", status, err)
		}
		var parsed map[string]any
		if err := json.Unmarshal(dump, &parsed); err != nil {
			t.Fatalf("decode catcher dump: %v body=%s", err, dump)
		}
		deliveries, _ := parsed["deliveries"].([]any)
		if len(deliveries) == 0 {
			t.Skip("catcher dump empty; soft-skip Authorization assert")
		}
		found := false
		for i := len(deliveries) - 1; i >= 0; i-- {
			raw, _ := deliveries[i].(string)
			var payload map[string]any
			if err := json.Unmarshal([]byte(raw), &payload); err != nil {
				continue
			}
			authz, _ := payload["authorization"].(string)
			if strings.HasPrefix(authz, "Bearer ") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected Bearer authorization in catcher dump body=%s", dump)
		}
	})
}
