package cloudsql

import "testing"

func TestSafeMySQLHost(t *testing.T) {
	if !safeMySQLHost("") || !safeMySQLHost("%") || !safeMySQLHost("localhost") || !safeMySQLHost("db-1.example.com") {
		t.Fatal("expected allow")
	}
	if safeMySQLHost("bad;host") || safeMySQLHost("a b") || safeMySQLHost("x@y") {
		t.Fatal("expected deny")
	}
}
