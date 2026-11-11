package sdk_test

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var standaloneAccessTokenJSON = regexp.MustCompile(`"accessToken"\s*:\s*"[^"]*"`)

func TestStandaloneProjectSDK(t *testing.T) {
	ep := requireReady(t)
	root := requireToken(t)
	home := projectID()

	name, seeded := seedStandaloneProject(t, ep, root)
	if seeded.AccessToken == "" || seeded.AccessToken == root {
		t.Fatal("minted access token is empty or matches the root token")
	}
	if rootSA := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT")); rootSA != "" && strings.EqualFold(seeded.ServiceAccountEmail, rootSA) {
		t.Fatalf("serviceAccountEmail=%q matches the root service account", seeded.ServiceAccountEmail)
	}

	status, body := doJSON(t, http.MethodGet, ep+"/v3/projects/"+seeded.ProjectID, seeded.AccessToken, nil)
	if status != http.StatusOK {
		t.Fatalf("get standalone project status=%d body=%s", status, redactStandalone(body, root, seeded.AccessToken))
	}
	var own map[string]any
	if err := json.Unmarshal(body, &own); err != nil {
		t.Fatalf("decode standalone project: %v body=%s", err, redactStandalone(body, root, seeded.AccessToken))
	}
	if got, _ := own["projectId"].(string); got != seeded.ProjectID {
		t.Fatalf("projectId=%q want %q", got, seeded.ProjectID)
	}

	status, body = doJSON(t, http.MethodGet, ep+"/v3/projects/"+home, seeded.AccessToken, nil)
	if status != http.StatusForbidden {
		t.Fatalf("get default project status=%d body=%s", status, redactStandalone(body, root, seeded.AccessToken))
	}

	status, body = doJSON(t, http.MethodGet, ep+"/v3/projects?parent=projects/"+seeded.ProjectID, seeded.AccessToken, nil)
	if status != http.StatusOK {
		t.Fatalf("list projects status=%d body=%s", status, redactStandalone(body, root, seeded.AccessToken))
	}
	if strings.Contains(string(body), home) {
		t.Fatal("project list included the default project id")
	}

	again := postStandaloneSeed(t, ep, root, name)
	if again.AccessToken != seeded.AccessToken {
		t.Fatal("repeat seed returned a different access token")
	}
}

func seedStandaloneProject(t *testing.T, ep, root string) (string, standaloneSeed) {
	t.Helper()
	var lastStatus int
	var lastBody []byte
	for attempt := 0; attempt < 4; attempt++ {
		name := standaloneSeedName(attempt)
		if !validStandaloneSDKProjectID(name) {
			t.Fatalf("seed name %q is not a valid project id", name)
		}
		status, body := doJSON(t, http.MethodPost, ep+"/_noctaxris-gcp/lab/projects:seedStandalone", root, map[string]string{
			"name": name,
		})
		if status == http.StatusNotFound {
			t.Skip("NOCTAXRIS_GCP_LAB_STANDALONE is off")
		}
		if status == http.StatusOK {
			seeded := decodeStandaloneSeed(t, body, root)
			if seeded.ProjectID != name {
				t.Fatalf("projectId=%q want %q", seeded.ProjectID, name)
			}
			if seeded.ServiceAccountEmail == "" || seeded.AccessToken == "" {
				t.Fatal("seed response missing serviceAccountEmail or accessToken")
			}
			return name, seeded
		}
		lastStatus, lastBody = status, body
		if status != http.StatusConflict {
			break
		}
	}
	t.Fatalf("seed status=%d body=%s", lastStatus, redactStandalone(lastBody, root))
	return "", standaloneSeed{}
}

func postStandaloneSeed(t *testing.T, ep, root, name string) standaloneSeed {
	t.Helper()
	status, body := doJSON(t, http.MethodPost, ep+"/_noctaxris-gcp/lab/projects:seedStandalone", root, map[string]string{
		"name": name,
	})
	if status == http.StatusNotFound {
		t.Skip("NOCTAXRIS_GCP_LAB_STANDALONE is off")
	}
	if status != http.StatusOK {
		t.Fatalf("repeat seed status=%d body=%s", status, redactStandalone(body, root))
	}
	seeded := decodeStandaloneSeed(t, body, root)
	if seeded.AccessToken == "" {
		t.Fatal("repeat seed response missing accessToken")
	}
	return seeded
}

type standaloneSeed struct {
	ProjectID           string `json:"projectId"`
	ServiceAccountEmail string `json:"serviceAccountEmail"`
	AccessToken         string `json:"accessToken"`
}

func decodeStandaloneSeed(t *testing.T, body []byte, root string) standaloneSeed {
	t.Helper()
	var out standaloneSeed
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode seed: %v body=%s", err, redactStandalone(body, root))
	}
	return out
}

func standaloneSeedName(attempt int) string {
	const base = "standalone-sdk-go"
	if attempt == 0 {
		return base
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 16)
	if len(suffix) > 4 {
		suffix = suffix[len(suffix)-4:]
	}
	if attempt > 1 {
		suffix += strconv.Itoa(attempt)
	}
	return base + "-" + suffix
}

func validStandaloneSDKProjectID(id string) bool {
	if id == "" {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

func redactStandalone(body []byte, secrets ...string) string {
	text := string(body)
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, "[redacted]")
	}
	return standaloneAccessTokenJSON.ReplaceAllString(text, `"accessToken":"[redacted]"`)
}
