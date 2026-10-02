package celutil_test

import (
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/celutil"
)

func TestEvalRequestTimeComparisons(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	before := `request.time < timestamp("2026-10-03T13:00:00Z")`
	after := `request.time > timestamp("2026-10-03T11:00:00Z")`
	combined := before + " && " + after
	if !celutil.EvalRequestTime(before, now) {
		t.Fatal("expected before deadline to allow")
	}
	if !celutil.EvalRequestTime(after, now) {
		t.Fatal("expected after start to allow")
	}
	if !celutil.EvalRequestTime(combined, now) {
		t.Fatal("expected combined window to allow")
	}
	if celutil.EvalRequestTime(`request.time < timestamp("2026-10-03T11:00:00Z")`, now) {
		t.Fatal("past deadline must deny")
	}
}

func TestEvalRequestTimeFailClosed(t *testing.T) {
	now := time.Now().UTC()
	if !celutil.EvalRequestTime("", now) {
		t.Fatal("empty expression should allow")
	}
	if celutil.EvalRequestTime("not a cel expression (((", now) {
		t.Fatal("bad expression must deny")
	}
	if celutil.EvalRequestTime(`request.missing == true`, now) {
		t.Fatal("unknown field must deny")
	}
	if celutil.EvalRequestTime(`"not-a-bool"`, now) {
		t.Fatal("non-bool result must deny")
	}
}

func TestEvalAttributeMappingRequiresSubject(t *testing.T) {
	assertion := map[string]any{"sub": "alice", "email": "a@example.com"}
	out, err := celutil.EvalAttributeMapping(map[string]string{
		"google.subject":  "assertion.sub",
		"attribute.email": "assertion.email",
	}, assertion)
	if err != nil {
		t.Fatal(err)
	}
	if out["google.subject"] != "alice" || out["attribute.email"] != "a@example.com" {
		t.Fatalf("got %#v", out)
	}
	if _, err := celutil.EvalAttributeMapping(map[string]string{
		"attribute.email": "assertion.email",
	}, assertion); err == nil {
		t.Fatal("expected missing google.subject error")
	}
	if _, err := celutil.EvalAttributeMapping(nil, assertion); err == nil {
		t.Fatal("expected empty mapping error")
	}
}

func TestEvalAttributeCondition(t *testing.T) {
	assertion := map[string]any{"email": "ok@example.com", "hd": "evil.com"}
	ok, err := celutil.EvalAttributeCondition(`assertion.email == "ok@example.com"`, assertion)
	if err != nil || !ok {
		t.Fatalf("expected allow: ok=%v err=%v", ok, err)
	}
	ok, err = celutil.EvalAttributeCondition(`assertion.hd == "example.com"`, assertion)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected condition deny")
	}
	ok, err = celutil.EvalAttributeCondition("", assertion)
	if err != nil || !ok {
		t.Fatalf("empty condition must allow: ok=%v err=%v", ok, err)
	}
}

func TestEvalArmorMatchEmptyAndPath(t *testing.T) {
	if !celutil.EvalArmorMatch("", nil) {
		t.Fatal("empty must match")
	}
	attrs := map[string]any{
		"path": "/ok",
		"host": "example.com",
		"headers": map[string]any{
			"user-agent": "lab",
		},
	}
	if !celutil.EvalArmorMatch(`request.path == "/ok"`, attrs) {
		t.Fatal("expected path match")
	}
	if !celutil.EvalArmorMatch(`request.host == "example.com"`, attrs) {
		t.Fatal("expected host match")
	}
	if !celutil.EvalArmorMatch(`request.headers["user-agent"] == "lab"`, attrs) {
		t.Fatal("expected header match")
	}
	if celutil.EvalArmorMatch(`request.path == "/deny"`, attrs) {
		t.Fatal("expected non-match")
	}
	if celutil.EvalArmorMatch(`request.missing == true`, attrs) {
		t.Fatal("eval error must be non-match")
	}
}
