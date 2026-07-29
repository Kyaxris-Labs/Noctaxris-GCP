package pubsub

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func openTestStore(t *testing.T) *store.Store {
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
	return st
}

func TestValidatePushEndpointFailClosed(t *testing.T) {
	t.Setenv(httpegress.EnvHTTPEgress, "1")
	t.Setenv(httpegress.EnvHTTPAllowlist, "http://169.254.169.254/latest,http://metadata.google.internal/computeMetadata/v1/")

	blocked := []string{
		"http://169.254.169.254/latest",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://10.0.0.1/hook",
		"https://example.com/push",
		"",
	}
	for _, ep := range blocked {
		if ep == "" {
			if err := validatePushEndpoint(ep); err != nil {
				t.Fatalf("empty endpoint: %v", err)
			}
			continue
		}
		err := validatePushEndpoint(ep)
		if err == nil {
			t.Fatalf("expected block for %q", ep)
		}
		if !errors.Is(err, httpegress.ErrNotAllowed) {
			t.Fatalf("endpoint %q: want ErrNotAllowed, got %v", ep, err)
		}
	}

	allowed := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/push-oidc"
	if err := validatePushEndpoint(allowed); err != nil {
		t.Fatalf("lab catcher should be allowed: %v", err)
	}
}

func TestDeliverPushOIDCCatcher(t *testing.T) {
	st := openTestStore(t)
	store.ClearHTTPCatcher()
	t.Cleanup(store.ClearHTTPCatcher)

	project := "noctaxris-gcp-local"
	topic := "projects/" + project + "/topics/push-oidc"
	sub := "projects/" + project + "/subscriptions/push-oidc"
	catcher := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/push-oidc-unit"
	saEmail := "push-sa@" + project + ".iam.gserviceaccount.com"
	audience := "https://aud.example/push"

	if _, created, err := st.CreateTopic(topic, project); err != nil || !created {
		t.Fatalf("topic: created=%v err=%v", created, err)
	}
	if _, created, err := st.CreateSubscriptionFull(sub, topic, project, 10, catcher, nil, "", "", 0, false, saEmail, audience); err != nil || !created {
		t.Fatalf("sub: created=%v err=%v", created, err)
	}

	svc := &Service{Store: st}
	_, copies, err := st.PublishFanout(topic, []byte("oidc-body"), map[string]string{"k": "v"})
	if err != nil || len(copies) != 1 {
		t.Fatalf("fanout: copies=%d err=%v", len(copies), err)
	}
	svc.deliverPush(copies)

	caught := store.ListHTTPCatcher()
	if len(caught) == 0 {
		t.Fatal("expected lab catcher payload")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(caught[len(caught)-1]), &payload); err != nil {
		t.Fatalf("catcher json: %v body=%s", err, caught[len(caught)-1])
	}
	authz, _ := payload["authorization"].(string)
	if !strings.HasPrefix(authz, "Bearer ") {
		t.Fatalf("authorization=%#v", payload["authorization"])
	}
	jwt := strings.TrimPrefix(authz, "Bearer ")
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 || parts[2] != "" {
		t.Fatalf("jwt shape: %q", jwt)
	}
	claimsRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimsRaw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["aud"] != audience || claims["email"] != saEmail || claims["sub"] != saEmail {
		t.Fatalf("claims=%#v", claims)
	}

	msgs, err := st.Pull(sub, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("push catcher should ack; still leased: %#v", msgs)
	}
}

func TestOidcFromPushConfig(t *testing.T) {
	email, aud := oidcFromPushConfig(nil)
	if email != "" || aud != "" {
		t.Fatalf("nil config: email=%q aud=%q", email, aud)
	}
}

func TestDeliverPushDeadLetterAfterMaxAttempts(t *testing.T) {
	st := openTestStore(t)
	project := "noctaxris-gcp-local"
	topic := "projects/" + project + "/topics/push-dlq-main"
	dlTopic := "projects/" + project + "/topics/push-dlq"
	sub := "projects/" + project + "/subscriptions/push-dlq-sub"
	dlSub := "projects/" + project + "/subscriptions/push-dlq-reader"
	// Lab-local non-catcher URL so deliverPush uses HTTPClient (not catcher short-circuit).
	failURL := "http://127.0.0.1:4588/lab-push-fail"

	if _, created, err := st.CreateTopic(topic, project); err != nil || !created {
		t.Fatalf("topic: created=%v err=%v", created, err)
	}
	if _, created, err := st.CreateTopic(dlTopic, project); err != nil || !created {
		t.Fatalf("dl topic: created=%v err=%v", created, err)
	}
	if _, created, err := st.CreateSubscriptionFull(sub, topic, project, 10, failURL, nil, "", dlTopic, 5, false, "", ""); err != nil || !created {
		t.Fatalf("sub: created=%v err=%v", created, err)
	}
	if _, created, err := st.CreateSubscription(dlSub, dlTopic, project, 10); err != nil || !created {
		t.Fatalf("dl sub: created=%v err=%v", created, err)
	}

	svc := &Service{
		Store: st,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("fail")),
				Header:     make(http.Header),
			}, nil
		})},
	}

	_, copies, err := st.PublishFanout(topic, []byte("poison-push"), map[string]string{"k": "v"})
	if err != nil || len(copies) != 1 {
		t.Fatalf("fanout: copies=%d err=%v", len(copies), err)
	}
	for i := 0; i < 5; i++ {
		svc.deliverPush(copies)
	}

	still, err := st.Pull(sub, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(still) != 0 {
		t.Fatalf("expected source message dead-lettered, still have %#v", still)
	}
	dlMsgs, err := st.Pull(dlSub, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dlMsgs) != 1 || string(dlMsgs[0].Data) != "poison-push" {
		t.Fatalf("dl msgs = %#v", dlMsgs)
	}
	attrs := map[string]string{}
	if dlMsgs[0].AttributesJSON != "" {
		_ = json.Unmarshal([]byte(dlMsgs[0].AttributesJSON), &attrs)
	}
	if attrs["CloudPubSubDeadLetterSourceSubscription"] != sub {
		t.Fatalf("dl attrs = %#v", attrs)
	}
}
