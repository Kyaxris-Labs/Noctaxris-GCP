package spanner

import "testing"

func TestEmptyResultSet(t *testing.T) {
	rs := emptyResultSet()
	if rs == nil {
		t.Fatal("nil")
	}
	rs2 := resultSetWithRows([]string{"a", "b"}, [][]string{{"1", "2"}, {"3", "4"}})
	if rs2 == nil {
		t.Fatal("rows")
	}
}
