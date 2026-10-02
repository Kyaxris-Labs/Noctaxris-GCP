package store_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestUpdateSubscriptionFieldsAndDeadLetter(t *testing.T) {
	st := openTestStore(t)
	const project = "noctaxris-gcp-local"
	topic := "projects/" + project + "/topics/upd-t"
	dlq := "projects/" + project + "/topics/upd-dlq"
	subName := "projects/" + project + "/subscriptions/upd-s"
	if _, created, err := st.CreateTopic(topic, project); err != nil || !created {
		t.Fatal(err)
	}
	if _, created, err := st.CreateTopic(dlq, project); err != nil || !created {
		t.Fatal(err)
	}
	if _, created, err := st.CreateSubscription(subName, topic, project, 10); err != nil || !created {
		t.Fatal(err)
	}

	ack := 0
	push := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/push"
	labels := map[string]string{"env": "lab"}
	filter := `attributes.x="1"`
	eos := true
	oidc := &store.PubSubOIDCToken{ServiceAccountEmail: "push@" + project + ".iam.gserviceaccount.com", Audience: "aud"}
	dl := &store.PubSubDeadLetterPolicy{DeadLetterTopic: dlq, MaxDeliveryAttempts: 0}
	updated, err := st.UpdateSubscription(subName, &ack, &push, &labels, &filter, dl, &eos, oidc)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AckDeadlineSeconds != 10 {
		t.Fatalf("ack zero should default to 10, got %d", updated.AckDeadlineSeconds)
	}
	if updated.PushEndpoint != push || updated.Filter != filter || !updated.EnableExactlyOnceDelivery {
		t.Fatalf("updated=%#v", updated)
	}
	if updated.DeadLetterTopic != dlq || updated.MaxDeliveryAttempts != 5 {
		t.Fatalf("dlq=%#v", updated)
	}
	if updated.OidcServiceAccountEmail == "" || updated.OidcAudience != "aud" {
		t.Fatalf("oidc=%#v", updated)
	}

	badDL := &store.PubSubDeadLetterPolicy{DeadLetterTopic: "projects/" + project + "/topics/missing", MaxDeliveryAttempts: 5}
	if _, err := st.UpdateSubscription(subName, nil, nil, nil, nil, badDL, nil, nil); err == nil {
		t.Fatal("expected missing dead letter topic error")
	}
	badAttempts := &store.PubSubDeadLetterPolicy{DeadLetterTopic: dlq, MaxDeliveryAttempts: 2}
	if _, err := st.UpdateSubscription(subName, nil, nil, nil, nil, badAttempts, nil, nil); err == nil {
		t.Fatal("expected maxDeliveryAttempts range error")
	}
	badFilter := `not-a-filter`
	if _, err := st.UpdateSubscription(subName, nil, nil, nil, &badFilter, nil, nil, nil); err == nil {
		t.Fatal("expected invalid filter error")
	}
	clearDL := &store.PubSubDeadLetterPolicy{}
	cleared, err := st.UpdateSubscription(subName, nil, nil, nil, nil, clearDL, nil, nil)
	if err != nil || cleared.DeadLetterTopic != "" {
		t.Fatalf("clear dlq err=%v got=%#v", err, cleared)
	}
	if _, err := st.UpdateSubscription(subName+"-missing", nil, nil, nil, nil, nil, nil, nil); err == nil {
		t.Fatal("expected missing subscription error")
	}
}
