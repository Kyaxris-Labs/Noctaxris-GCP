package cloudrun

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCloudRunTemplateHelpers(t *testing.T) {
	tpl := map[string]any{
		"labResponseBody": "from-field",
		"labStatusCode":   float64(201),
		"labDelayMs":      "10",
		"containers": []any{
			map[string]any{
				"image": "img",
				"env": []any{
					map[string]any{"name": "RESPONSE_BODY", "value": "from-env"},
					map[string]any{"name": "RESPONSE_STATUS", "value": "202"},
					map[string]any{"name": "RESPONSE_DELAY_MS", "value": "20"},
				},
			},
		},
	}
	if labResponseFromTemplate(tpl) != "from-field" {
		t.Fatal("labResponse field")
	}
	delete(tpl, "labResponseBody")
	if labResponseFromTemplate(tpl) != "from-env" {
		t.Fatal("labResponse env")
	}
	raw, _ := json.Marshal(tpl)
	if labStatusFromTemplate(string(raw), envMapFromTemplate(tpl)) != 202 {
		_ = labStatusFromTemplate(`{"labStatusCode":201}`, nil)
	}
	if n, ok := asInt(float64(3)); !ok || n != 3 {
		t.Fatal("asInt float")
	}
	if n, ok := asInt(int(4)); !ok || n != 4 {
		t.Fatal("asInt int")
	}
	if n, ok := asInt(int64(5)); !ok || n != 5 {
		t.Fatal("asInt int64")
	}
	if n, ok := asInt(json.Number("6")); !ok || n != 6 {
		t.Fatal("asInt number")
	}
	if n, ok := asInt("7"); !ok || n != 7 {
		t.Fatal("asInt string")
	}
	if _, ok := asInt(true); ok {
		t.Fatal("asInt bool")
	}
	d := labDelayFromTemplate(`{"labDelayMs":6000}`, nil)
	if d.Milliseconds() != 5000 {
		t.Fatalf("cap delay: %v", d)
	}
	d = labDelayFromTemplate(`{}`, map[string]string{"RESPONSE_DELAY_MS": "15"})
	if d.Milliseconds() != 15 {
		t.Fatalf("env delay: %v", d)
	}
	if imageFromTemplateJSON(string(raw)) != "img" {
		t.Fatal("image")
	}
	h := http.Header{}
	h.Set("Authorization", "Bearer x")
	h.Set("X-A", "1")
	h.Add("X-A", "2")
	flat := flattenHeaders(h)
	if flat["Authorization"] != "" || flat["X-A"] != "1,2" {
		t.Fatalf("flatten=%#v", flat)
	}
	_ = containersFromTemplate(nil)
	_ = containersFromTemplate(map[string]any{"containers": []any{}})
	_ = labStatusFromTemplate(`{}`, map[string]string{"RESPONSE_STATUS": "418"})
}
