package webui

import (
	"net/http"
	"strings"
	"testing"
)

func TestPickPathRequestValidation(t *testing.T) {
	_, ts := testServer(t)
	cases := []struct {
		name, method, token, body string
		want                      int
	}{
		{"token required", "POST", "", "{\"kind\":\"folder\"}", http.StatusUnauthorized},
		{"availability", "GET", testToken, "", http.StatusOK},
		{"invalid JSON", "POST", testToken, "{", http.StatusBadRequest},
		{"unknown kind", "POST", testToken, "{\"kind\":\"invalid\"}", http.StatusBadRequest},
		{"unknown field", "POST", testToken, "{\"kind\":\"folder\",\"unexpected\":1}", http.StatusBadRequest},
		{"too long", "POST", testToken, "{\"kind\":\"folder\",\"initial\":\"" + strings.Repeat("a", 4097) + "\"}", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, data := do(t, ts, tc.method, "/api/pick-path", tc.token, tc.body)
			if res.StatusCode != tc.want {
				t.Errorf("status=%d, want %d: %s", res.StatusCode, tc.want, data)
			}
		})
	}
}

func TestPathFieldsHavePickers(t *testing.T) {
	_, ts := testServer(t)
	res, data := do(t, ts, "GET", "/", "", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("index status=%d", res.StatusCode)
	}
	for _, id := range []string{
		"export-output", "export-recipients-file", "inspect-path", "inspect-identity",
		"import-path", "import-identity", "import-project", "import-clone",
	} {
		if !strings.Contains(string(data), "data-pick-target=\""+id+"\"") {
			t.Errorf("missing picker for %s", id)
		}
		if !strings.Contains(string(data), "id=\""+id+"\" type=\"text\"") {
			t.Errorf("manual input missing for %s", id)
		}
	}
}
