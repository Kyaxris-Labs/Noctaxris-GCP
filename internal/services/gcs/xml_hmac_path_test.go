package gcs

import "testing"

func TestGoog4HMACVerifyPaths(t *testing.T) {
	cases := []struct {
		name string
		host string
		path string
		want []string
	}{
		{
			name: "cloud host rewritten path",
			host: "storage.googleapis.com",
			path: "/storage/xml/bucket/obj",
			want: []string{"/bucket/obj", "/storage/xml/bucket/obj"},
		},
		{
			name: "cloud host with port 443",
			host: "storage.googleapis.com:443",
			path: "/storage/xml/bucket/obj",
			want: []string{"/bucket/obj", "/storage/xml/bucket/obj"},
		},
		{
			name: "cloud host xml root",
			host: "storage.googleapis.com",
			path: "/storage/xml",
			want: []string{"/", "/storage/xml"},
		},
		{
			name: "loopback unchanged",
			host: "127.0.0.1:4588",
			path: "/storage/xml/bucket/obj",
			want: []string{"/storage/xml/bucket/obj"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := goog4HMACVerifyPaths(tc.host, tc.path)
			if len(got) != len(tc.want) {
				t.Fatalf("paths=%v want=%v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("paths=%v want=%v", got, tc.want)
				}
			}
		})
	}
}

func TestGoog4HMACVerifyHost(t *testing.T) {
	if got := goog4HMACVerifyHost(""); got != "127.0.0.1:4588" {
		t.Fatalf("empty=%q", got)
	}
	if got := goog4HMACVerifyHost("storage.googleapis.com:443"); got != "storage.googleapis.com" {
		t.Fatalf("strip 443=%q", got)
	}
	if got := goog4HMACVerifyHost("127.0.0.1:4588"); got != "127.0.0.1:4588" {
		t.Fatalf("loopback=%q", got)
	}
}
