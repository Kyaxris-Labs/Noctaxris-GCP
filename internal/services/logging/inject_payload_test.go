package logging

import (
	"encoding/json"
	"testing"
)

func TestBuildInjectProtoPayload(t *testing.T) {
	raw, err := buildInjectProtoPayload(injectEntryIn{
		ProtoPayload: json.RawMessage(`{"serviceName":"storage.googleapis.com"}`),
	})
	if err != nil || len(raw) == 0 {
		t.Fatalf("with proto: %v %s", err, raw)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["@type"] == nil {
		t.Fatal("missing @type")
	}
	if _, err := buildInjectProtoPayload(injectEntryIn{ProtoPayload: json.RawMessage(`not-json`)}); err == nil {
		t.Fatal("bad proto json")
	}
	if _, err := buildInjectProtoPayload(injectEntryIn{}); err == nil {
		t.Fatal("empty")
	}
	granted := true
	raw, err = buildInjectProtoPayload(injectEntryIn{
		ServiceName: "iam.googleapis.com", MethodName: "google.iam.v1.Foo",
		ResourceName: "projects/p", PrincipalEmail: "a@b.c",
		Permission: "iam.roles.get", Granted: &granted, StatusCode: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &m)
	if m["serviceName"] != "iam.googleapis.com" {
		t.Fatalf("%#v", m)
	}
	_ = wantsCloudAudit("projects/p/logs/cloudaudit.googleapis.com%2Factivity", "")
	_ = wantsCloudAudit("", "resource.type=gce AND cloudaudit.googleapis.com")
	_ = wantsCloudAudit("projects/p/logs/other", "nope")
}
