package logging_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoggingListFollowsKeysetToken(t *testing.T) {
	mux := setupLogging(t)
	const project = "noctaxris-gcp-local"
	write := `{
	  "entries": [
	    {"logName":"projects/` + project + `/logs/requests","textPayload":"a","insertId":"a","timestamp":"2026-08-01T00:00:01Z"},
	    {"logName":"projects/` + project + `/logs/requests","textPayload":"b","insertId":"b","timestamp":"2026-08-01T00:00:02Z"},
	    {"logName":"projects/` + project + `/logs/requests","textPayload":"c","insertId":"c","timestamp":"2026-08-01T00:00:03Z"}
	  ]
	}`
	req := httptest.NewRequest(http.MethodPost, "/v2/entries:write", bytes.NewReader([]byte(write)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("write status=%d body=%s", rec.Code, rec.Body.String())
	}

	var got []string
	token := ""
	for page := 0; page < 4; page++ {
		body := `{"resourceNames":["projects/` + project + `"],"pageSize":1,"filter":"logName=\"projects/` + project + `/logs/requests\"","pageToken":"` + token + `"}`
		req = httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(body)))
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Entries []struct {
				InsertID string `json:"insertId"`
			} `json:"entries"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Entries) == 0 {
			if resp.NextPageToken != "" {
				t.Fatalf("empty page kept a token %q", resp.NextPageToken)
			}
			token = ""
			break
		}
		if len(resp.Entries) != 1 {
			t.Fatalf("page %d entries %#v", page, resp.Entries)
		}
		got = append(got, resp.Entries[0].InsertID)
		token = resp.NextPageToken
		if token == "" {
			break
		}
	}
	if strings.Join(got, ",") != "a,b,c" || token != "" {
		t.Fatalf("pages %v token %q", got, token)
	}
}

func TestLoggingListDescendingOrder(t *testing.T) {
	mux := setupLogging(t)
	const project = "noctaxris-gcp-local"
	write := `{
	  "entries": [
	    {"logName":"projects/` + project + `/logs/requests","textPayload":"a","insertId":"a","timestamp":"2026-08-01T00:00:01Z"},
	    {"logName":"projects/` + project + `/logs/requests","textPayload":"b","insertId":"b","timestamp":"2026-08-01T00:00:02Z"},
	    {"logName":"projects/` + project + `/logs/requests","textPayload":"c","insertId":"c","timestamp":"2026-08-01T00:00:03Z"}
	  ]
	}`
	req := httptest.NewRequest(http.MethodPost, "/v2/entries:write", bytes.NewReader([]byte(write)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("write status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list",
		bytes.NewReader([]byte(`{"resourceNames":["projects/`+project+`"],"orderBy":"nope","pageSize":1}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("invalid orderBy")
	}

	var got []string
	token := ""
	for page := 0; page < 4; page++ {
		body := `{"resourceNames":["projects/` + project + `"],"pageSize":1,"orderBy":"timestamp desc","filter":"logName=\"projects/` + project + `/logs/requests\"","pageToken":"` + token + `"}`
		req = httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(body)))
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Entries []struct {
				InsertID string `json:"insertId"`
			} `json:"entries"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Entries) == 0 {
			token = ""
			break
		}
		got = append(got, resp.Entries[0].InsertID)
		token = resp.NextPageToken
		if token == "" {
			break
		}
	}
	if strings.Join(got, ",") != "c,b,a" || token != "" {
		t.Fatalf("desc pages %v token %q", got, token)
	}
}
