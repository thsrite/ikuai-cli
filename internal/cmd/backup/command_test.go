package backup

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ikuaidev/ikuai-cli/internal/api"
	"github.com/ikuaidev/ikuai-cli/internal/cliapp"
	"github.com/ikuaidev/ikuai-cli/internal/output"
	"github.com/ikuaidev/ikuai-cli/internal/session"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func run(t *testing.T, getResponse string, args ...string) ([]string, []map[string]interface{}, string, error) {
	t.Helper()
	var out bytes.Buffer
	var requests []string
	var bodies []map[string]interface{}
	app := cliapp.New(&out, &out)
	app.Format = output.JSON
	app.Session = &session.Session{BaseURL: "https://router.local", Token: "t"}
	app.APIClient = api.NewWithHTTPClient(app.Session.BaseURL, "t", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Method+" "+req.URL.Path)
		body := map[string]interface{}{}
		if req.Body != nil {
			data, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(data, &body)
		}
		bodies = append(bodies, body)
		response := `{"code":0,"data":{}}`
		if req.Method == http.MethodGet {
			response = getResponse
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(response))}, nil
	})})
	cmd := New(app)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return requests, bodies, out.String(), err
}

func saved(t *testing.T) string {
	t.Helper()
	t.Setenv("IKUAI_CLI_BACKUP_DIR", t.TempDir())
	file, err := cliapp.SaveBackup("routing/static-routes", "3", json.RawMessage(
		`{"id":3,"tagname":"RT_3","dst_addr":"8.8.8.8","gateway":"10.0.0.4","dst_addr_int":134744072,"src_addr":{"custom":{},"object":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func TestRestoreReplacesAnExistingObject(t *testing.T) {
	file := saved(t)
	requests, bodies, _, err := run(t, `{"code":0,"data":{"data":[{"id":3,"tagname":"RT_3"}]}}`, "restore", file, "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(requests, "|") != "GET /api/v4.0/routing/static-routes/3|PUT /api/v4.0/routing/static-routes/3" {
		t.Fatalf("requests = %v", requests)
	}
	body := bodies[1]
	if _, hasID := body["id"]; hasID || body["dst_addr_int"] != nil || body["gateway"] != "10.0.0.4" {
		t.Fatalf("restore body = %v", body)
	}
	if src := body["src_addr"].(map[string]interface{}); len(src["custom"].([]interface{})) != 0 {
		t.Fatalf("empty address lists must be sent as []: %v", src)
	}
}

func TestRestoreRecreatesADeletedObject(t *testing.T) {
	file := saved(t)
	requests, _, out, err := run(t, `{"code":0,"data":{"data":[]}}`, "restore", file, "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(requests, "|") != "GET /api/v4.0/routing/static-routes/3|POST /api/v4.0/routing/static-routes" ||
		!strings.Contains(out, "creating it again") {
		t.Fatalf("requests = %v out = %s", requests, out)
	}
}

func TestRestoreNeverRecreatesOnAuthFailure(t *testing.T) {
	file := saved(t)
	requests, _, _, err := run(t, `{"code":3007,"message":"token invalid"}`, "restore", file, "--yes")
	if err == nil || len(requests) != 1 {
		t.Fatalf("err = %v requests = %v", err, requests)
	}
}

func TestRestoreNeedsYesWithoutATerminal(t *testing.T) {
	file := saved(t)
	requests, _, _, err := run(t, `{}`, "restore", file)
	if err == nil || !strings.Contains(err.Error(), "--yes") || len(requests) != 0 {
		t.Fatalf("err = %v requests = %v", err, requests)
	}
}
