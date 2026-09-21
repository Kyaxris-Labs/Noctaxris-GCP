package cloudbuild

import "testing"

func TestParseBuildServiceAccountEmail(t *testing.T) {
	const project = "noctaxris-gcp-local"
	cases := []struct {
		name string
		json string
		proj string
		want string
	}{
		{
			name: "explicit email",
			json: `{"serviceAccount":"builder@noctaxris-gcp-local.iam.gserviceaccount.com"}`,
			proj: project,
			want: "builder@noctaxris-gcp-local.iam.gserviceaccount.com",
		},
		{
			name: "resource name",
			json: `{"serviceAccount":"projects/noctaxris-gcp-local/serviceAccounts/ci-runner@noctaxris-gcp-local.iam.gserviceaccount.com"}`,
			proj: project,
			want: "ci-runner@noctaxris-gcp-local.iam.gserviceaccount.com",
		},
		{
			name: "empty uses default compute SA",
			json: `{"steps":[]}`,
			proj: project,
			want: "noctaxris-gcp-local-compute@developer.gserviceaccount.com",
		},
		{
			name: "whitespace serviceAccount uses default",
			json: `{"serviceAccount":"  "}`,
			proj: project,
			want: "noctaxris-gcp-local-compute@developer.gserviceaccount.com",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBuildServiceAccountEmail(tc.json, tc.proj)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
