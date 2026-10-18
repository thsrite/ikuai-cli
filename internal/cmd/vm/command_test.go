package vm

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ikuaidev/ikuai-cli/internal/cliapp"
	"github.com/ikuaidev/ikuai-cli/internal/output"
	"github.com/ikuaidev/ikuai-cli/internal/session"
)

func router(t *testing.T, actions *[]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path == "/Action/login" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"Result": 10000})
			return
		}
		if body["action"] != "show" {
			*actions = append(*actions, body["action"].(string)+" "+string(must(json.Marshal(body["param"]))))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "results": map[string]interface{}{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "results": map[string]interface{}{
			"total": 2, "data": []interface{}{
				map[string]interface{}{"id": 3, "name": "mihomo", "status": 1, "cpu_cores": 2, "mem_size": 512, "auto_start": 1},
				map[string]interface{}{"id": 7, "name": "msm", "status": 0, "cpu_cores": 1, "mem_size": 512, "auto_start": 0},
			}}})
	}))
}

func must(data []byte, err error) []byte { return data }

func run(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	t.Setenv("IKUAI_CLI_WEB_USER", "admin")
	t.Setenv("IKUAI_CLI_WEB_PASSWORD", "pw")
	var out bytes.Buffer
	app := cliapp.New(&out, &out)
	app.Format = output.JSON
	app.Session = &session.Session{BaseURL: server.URL}
	cmd := New(app)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestListShowsState(t *testing.T) {
	var actions []string
	server := router(t, &actions)
	defer server.Close()
	out, err := run(t, server, "list")
	if err != nil || !strings.Contains(out, `"state":"running"`) || !strings.Contains(out, `"state":"stopped"`) {
		t.Fatalf("out = %s err = %v", out, err)
	}
}

func TestStartByName(t *testing.T) {
	var actions []string
	server := router(t, &actions)
	defer server.Close()
	if _, err := run(t, server, "start", "msm"); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0] != `up {"id":7}` {
		t.Fatalf("actions = %v", actions)
	}
	if out, _ := run(t, server, "start", "mihomo"); !strings.Contains(out, "already running") || len(actions) != 1 {
		t.Fatalf("starting a running VM should do nothing: %s %v", out, actions)
	}
}

func TestStopNeedsConfirmation(t *testing.T) {
	var actions []string
	server := router(t, &actions)
	defer server.Close()
	if _, err := run(t, server, "stop", "mihomo"); err == nil || len(actions) != 0 {
		t.Fatalf("stop without --yes: err = %v actions = %v", err, actions)
	}
	if _, err := run(t, server, "stop", "mihomo", "--yes"); err != nil || actions[0] != `down {"id":3}` {
		t.Fatalf("err = %v actions = %v", err, actions)
	}
	if _, err := run(t, server, "start", "nope"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown VM error = %v", err)
	}
}

func TestNeedsWebCredentials(t *testing.T) {
	var out bytes.Buffer
	t.Setenv("IKUAI_CLI_WEB_USER", "")
	app := cliapp.New(&out, &out)
	app.Session = &session.Session{BaseURL: "http://10.0.0.1"}
	cmd := New(app)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "IKUAI_CLI_WEB_USER") {
		t.Fatalf("err = %v", err)
	}
}
