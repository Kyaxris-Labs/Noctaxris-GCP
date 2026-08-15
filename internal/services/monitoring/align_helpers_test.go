package monitoring

import (
	"encoding/json"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestAlignPointsAndToFloat(t *testing.T) {
	if toFloat(float64(1.5)) != 1.5 {
		t.Fatal("float64")
	}
	if toFloat("2.5") != 2.5 {
		t.Fatal("string")
	}
	if toFloat(json.Number("3")) != 3 {
		t.Fatal("json.Number")
	}
	if toFloat(true) == 0 && toFloat(int(4)) == 0 {
		// fmt.Sprint fallback for bool may be 0; int via default
		_ = toFloat(4)
	}
	_ = toFloat(int64(7))

	pts := []store.TimeSeriesPoint{
		{ValueJSON: `{"doubleValue":1}`, EndTime: "t1"},
		{ValueJSON: `{"int64Value":"3"}`, EndTime: "t2"},
		{ValueJSON: `not-json`, EndTime: "t3"},
	}
	if got := alignPoints(pts, ""); len(got) != 3 {
		t.Fatalf("ALIGN_NONE: %d", len(got))
	}
	if got := alignPoints(nil, "ALIGN_SUM"); len(got) != 0 {
		t.Fatal("empty")
	}
	sum := alignPoints(pts, "ALIGN_SUM")
	if len(sum) != 1 {
		t.Fatalf("sum len=%d", len(sum))
	}
	mean := alignPoints(pts, "ALIGN_MEAN")
	if len(mean) != 1 {
		t.Fatalf("mean len=%d", len(mean))
	}
	max := alignPoints(pts, "ALIGN_MAX")
	if len(max) != 1 {
		t.Fatalf("max len=%d", len(max))
	}
	min := alignPoints(pts, "ALIGN_MIN")
	if len(min) != 1 {
		t.Fatalf("min len=%d", len(min))
	}
	bad := alignPoints([]store.TimeSeriesPoint{{ValueJSON: `{}`}}, "ALIGN_SUM")
	if len(bad) != 1 {
		t.Fatalf("no vals fallback: %d", len(bad))
	}
	if got := alignPoints(pts, "ALIGN_UNKNOWN"); len(got) != 3 {
		t.Fatal("default")
	}
	if parseMetricTypeFilter(`metric.type="custom.googleapis.com/x"`) != "custom.googleapis.com/x" {
		t.Fatal("parse filter")
	}
	if parseMetricTypeFilter("nope") != "" {
		t.Fatal("empty filter")
	}
}
