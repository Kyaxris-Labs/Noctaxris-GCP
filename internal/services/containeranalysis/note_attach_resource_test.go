package containeranalysis

import "testing"

func TestNoteAttachResourceBareID(t *testing.T) {
	got := noteAttachResource("lab-note", "proj-a")
	want := "projects/proj-a/notes/lab-note"
	if got != want {
		t.Fatalf("bare id: got %q want %q", got, want)
	}
	full := "projects/other/notes/n1"
	if noteAttachResource(full, "proj-a") != full {
		t.Fatalf("full name rewritten: %q", noteAttachResource(full, "proj-a"))
	}
	if noteAttachResource("", "proj-a") != "" {
		t.Fatal("empty noteName must stay empty")
	}
	if noteAttachResource("bare", "") != "bare" {
		t.Fatalf("empty project: %q", noteAttachResource("bare", ""))
	}
}
