package api

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/Songxwn/Auto-Deploy/internal/templates"
)

func loginClient(t *testing.T, tsURL string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	res, _ := doJSON(t, client, "POST", tsURL+"/api/login", map[string]string{"username": "admin", "password": "test-pass"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", res.StatusCode)
	}
	return client
}

func TestTemplateAPI(t *testing.T) {
	ts := newTestServer(t)
	client := loginClient(t, ts.URL)

	// Built-ins are seeded.
	req, _ := http.NewRequest("GET", ts.URL+"/api/templates", nil)
	res2, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", res2.StatusCode)
	}
	var arr []map[string]any
	if err := json.NewDecoder(res2.Body).Decode(&arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != len(templates.BuiltIn()) {
		t.Fatalf("built-in count = %d, want %d", len(arr), len(templates.BuiltIn()))
	}
	builtInID, _ := arr[0]["id"].(string)

	// Built-ins cannot be modified or deleted.
	res, _ := doJSON(t, client, "PUT", ts.URL+"/api/templates/"+builtInID, map[string]any{
		"name": "x", "os_type": "linux", "script": "echo x",
	})
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("update built-in status = %d, want 409", res.StatusCode)
	}
	res, _ = doJSON(t, client, "DELETE", ts.URL+"/api/templates/"+builtInID, nil)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("delete built-in status = %d, want 409", res.StatusCode)
	}

	// Validation errors.
	res, _ = doJSON(t, client, "POST", ts.URL+"/api/templates", map[string]any{"name": ""})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("validation status = %d", res.StatusCode)
	}
	res, _ = doJSON(t, client, "POST", ts.URL+"/api/templates", map[string]any{
		"name": "bad-os", "os_type": "macos", "script": "echo hi",
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("os validation status = %d", res.StatusCode)
	}

	// Create a custom template.
	res, data := doJSON(t, client, "POST", ts.URL+"/api/templates", map[string]any{
		"name": "my-tpl", "description": "d", "os_type": "linux", "category": "custom", "script": "echo hi",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d (%v)", res.StatusCode, data)
	}
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatal("no template id returned")
	}
	if bi, _ := data["built_in"].(bool); bi {
		t.Fatal("custom template marked built_in")
	}

	// Update it.
	res, _ = doJSON(t, client, "PUT", ts.URL+"/api/templates/"+id, map[string]any{
		"name": "renamed", "os_type": "linux", "category": "web", "script": "echo bye",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d", res.StatusCode)
	}
	res, data = doJSON(t, client, "GET", ts.URL+"/api/templates/"+id, nil)
	if res.StatusCode != http.StatusOK || data["name"] != "renamed" {
		t.Fatalf("get after update: %d %v", res.StatusCode, data)
	}

	// Delete it.
	res, _ = doJSON(t, client, "DELETE", ts.URL+"/api/templates/"+id, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", res.StatusCode)
	}
	res, _ = doJSON(t, client, "GET", ts.URL+"/api/templates/"+id, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("get deleted status = %d", res.StatusCode)
	}
}
