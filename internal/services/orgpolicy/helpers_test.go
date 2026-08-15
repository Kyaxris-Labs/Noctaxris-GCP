package orgpolicy

import "testing"

func TestConstraintFromName(t *testing.T) {
	if got := constraintFromName("projects/p/policies/iam.disableServiceAccountKeyCreation"); got == "" {
		t.Fatal("empty")
	}
	if got := constraintFromName("iam.disableServiceAccountKeyCreation"); got == "" {
		t.Fatal("raw")
	}
	_ = constraintFromName("  ")
	name, action := splitColonAction("foo:bar")
	if name != "foo" || action != "bar" {
		t.Fatalf("%q %q", name, action)
	}
	name, action = splitColonAction("plain")
	if name != "plain" || action != "" {
		t.Fatalf("%q %q", name, action)
	}
	_ = labConstraintDescription("iam.disableServiceAccountKeyCreation")
	_ = labConstraintDescription("storage.publicAccessPrevention")
	_ = labConstraintDescription("unknown")
}
