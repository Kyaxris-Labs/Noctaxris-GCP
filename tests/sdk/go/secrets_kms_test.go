package sdk_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func TestSecretManagerCreateAccessSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()
	secretID := uniqueID("sdk-secret")
	base := ep + "/v1/projects/" + project + "/secrets/" + secretID

	status, body := doJSON(t, http.MethodPost, ep+"/v1/projects/"+project+"/secrets?secretId="+url.QueryEscape(secretID), token, map[string]any{
		"replication": map[string]any{"automatic": map[string]any{}},
	})
	if status != http.StatusOK {
		t.Fatalf("create secret status=%d body=%s", status, body)
	}
	t.Cleanup(func() {
		_, _, _ = doJSONErr(http.MethodDelete, base, token, nil)
	})

	payload := base64.StdEncoding.EncodeToString([]byte("sdk-smoke"))
	status, body = doJSON(t, http.MethodPost, base+":addVersion", token, map[string]any{
		"payload": map[string]any{"data": payload},
	})
	if status != http.StatusOK {
		t.Fatalf("addVersion status=%d body=%s", status, body)
	}

	status, body = doJSON(t, http.MethodGet, base+"/versions/latest:access", token, nil)
	if status != http.StatusOK {
		t.Fatalf("access secret status=%d body=%s", status, body)
	}
	var accessed map[string]any
	if err := json.Unmarshal(body, &accessed); err != nil {
		t.Fatalf("decode access: %v body=%s", err, body)
	}
	payloadObj, _ := accessed["payload"].(map[string]any)
	gotB64, _ := payloadObj["data"].(string)
	got, err := base64.StdEncoding.DecodeString(gotB64)
	if err != nil {
		t.Fatalf("decode payload data: %v body=%s", err, body)
	}
	if string(got) != "sdk-smoke" {
		t.Fatalf("payload=%q want sdk-smoke body=%s", got, body)
	}
}

func TestListKMSKeyRingsSmoke(t *testing.T) {
	ep := requireReady(t)
	token := requireToken(t)
	project := projectID()

	path := ep + "/v1/projects/" + project + "/locations/global/keyRings"
	status, body := doJSON(t, http.MethodGet, path, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list KMS keyRings status=%d body=%s", status, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode KMS keyRings: %v body=%s", err, body)
	}
	if _, ok := parsed["keyRings"]; !ok {
		t.Fatalf("missing keyRings field body=%s", body)
	}
}
