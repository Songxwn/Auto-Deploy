package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Songxwn/Auto-Deploy/internal/config"
	"github.com/Songxwn/Auto-Deploy/internal/engine"
	"github.com/Songxwn/Auto-Deploy/internal/secure"
	"github.com/Songxwn/Auto-Deploy/internal/store"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := config.Default()
	cfg.Secret = "test-secret"
	cfg.Username = "admin"
	cfg.Password = "test-pass"
	box := secure.NewBox(cfg.Secret)
	eng := engine.New(st, box, nil, 2, 0)
	srv := New(cfg, st, box, eng, "test")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func doJSON(t *testing.T, client *http.Client, method, url string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var reqBody []byte
	if body != nil {
		reqBody, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var data map[string]any
	_ = json.NewDecoder(res.Body).Decode(&data)
	return res, data
}

func TestHealthzPublic(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func TestAuthRequired(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/api/hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.StatusCode)
	}
}

func TestLoginAndHostCRUD(t *testing.T) {
	ts := newTestServer(t)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// Wrong password.
	res, _ := doJSON(t, client, "POST", ts.URL+"/api/login", map[string]string{"username": "admin", "password": "wrong"})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d", res.StatusCode)
	}

	// Correct login sets a cookie.
	res, _ = doJSON(t, client, "POST", ts.URL+"/api/login", map[string]string{"username": "admin", "password": "test-pass"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", res.StatusCode)
	}

	// Create host.
	res, data := doJSON(t, client, "POST", ts.URL+"/api/hosts", map[string]any{
		"name": "web-01", "addr": "10.0.0.1", "port": 22, "user": "root",
		"auth_type": "password", "secret": "super-secret", "os_type": "linux",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d (%v)", res.StatusCode, data)
	}
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatal("no host id returned")
	}
	if _, leaked := data["secret"]; leaked {
		t.Fatal("secret leaked in response")
	}

	// List hosts.
	req, _ := http.NewRequest("GET", ts.URL+"/api/hosts", nil)
	res2, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := new(bytes.Buffer)
	_, _ = body.ReadFrom(res2.Body)
	res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", res2.StatusCode)
	}
	if strings.Contains(body.String(), "super-secret") {
		t.Fatal("plaintext secret leaked in list")
	}
	if !strings.Contains(body.String(), "web-01") {
		t.Fatal("host missing in list")
	}

	// Validation error.
	res, _ = doJSON(t, client, "POST", ts.URL+"/api/hosts", map[string]any{"name": ""})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("validation status = %d", res.StatusCode)
	}

	// Delete.
	res, _ = doJSON(t, client, "DELETE", ts.URL+"/api/hosts/"+id, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", res.StatusCode)
	}
	res, _ = doJSON(t, client, "GET", ts.URL+"/api/hosts/"+id, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("get deleted status = %d", res.StatusCode)
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/hosts", nil)
	req.Header.Set("Authorization", "Bearer YWRtaW58OTk5OTk5OTk5OQ.fake")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.StatusCode)
	}
}

func TestStaticUI(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	body := new(bytes.Buffer)
	_, _ = body.ReadFrom(res.Body)
	if !strings.Contains(body.String(), "Auto-Deploy") {
		t.Fatal("index.html not served")
	}
}
