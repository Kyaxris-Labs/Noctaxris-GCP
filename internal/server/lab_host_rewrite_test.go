package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRewriteLabHostPath(t *testing.T) {
	cases := []struct {
		name string
		host string
		path string
		want string
	}{
		{
			name: "iamcredentials prefix",
			host: "iamcredentials.googleapis.com",
			path: "/v1/projects/-/serviceAccounts/sa:generateAccessToken",
			want: "/iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/sa:generateAccessToken",
		},
		{
			name: "iamcredentials already prefixed",
			host: "iamcredentials.googleapis.com",
			path: "/iamcredentials.googleapis.com/v1/x",
			want: "/iamcredentials.googleapis.com/v1/x",
		},
		{
			name: "storage xml",
			host: "storage.googleapis.com",
			path: "/bucket/obj",
			want: "/storage/xml/bucket/obj",
		},
		{
			name: "storage json left alone",
			host: "storage.googleapis.com",
			path: "/storage/v1/b",
			want: "/storage/v1/b",
		},
		{
			name: "oauth2 empty path",
			host: "oauth2.googleapis.com",
			path: "/",
			want: "/token",
		},
		{
			name: "oauth2 token unchanged",
			host: "oauth2.googleapis.com",
			path: "/token",
			want: "/token",
		},
		{
			name: "accounts oauth2 alias",
			host: "accounts.google.com",
			path: "/o/oauth2/token",
			want: "/oauth2/token",
		},
		{
			name: "accounts other path unchanged",
			host: "accounts.google.com",
			path: "/o/oauth2/auth",
			want: "/o/oauth2/auth",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			req.Host = tc.host
			rewriteLabHostPath(req)
			if req.URL.Path != tc.want {
				t.Fatalf("path=%q want=%q", req.URL.Path, tc.want)
			}
		})
	}
}
