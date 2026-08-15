package cloudfunctions

import "testing"

func TestStorageSourceFromConfigJSON(t *testing.T) {
	b, o, ok := storageSourceFromConfigJSON(`{"buildConfig":{"source":{"storageSource":{"bucket":"b","object":"o.zip"}}}}`)
	if !ok || b != "b" || o != "o.zip" {
		t.Fatalf("%q %q %v", b, o, ok)
	}
	if _, _, ok := storageSourceFromConfigJSON(`{}`); ok {
		t.Fatal("empty")
	}
	if _, _, ok := storageSourceFromConfigJSON(`not-json`); ok {
		t.Fatal("bad json")
	}
	_ = mustJSON(map[string]any{"a": 1})
}
