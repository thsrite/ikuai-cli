package routing

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ikuaidev/ikuai-cli/internal/api"
	"github.com/ikuaidev/ikuai-cli/internal/cliapp"
	"github.com/ikuaidev/ikuai-cli/internal/output"
	"github.com/ikuaidev/ikuai-cli/internal/session"
)

func addr(values ...string) map[string]interface{} {
	custom := []interface{}{}
	for _, v := range values {
		custom = append(custom, v)
	}
	return map[string]interface{}{"custom": custom, "object": map[string]interface{}{}}
}

func TestDiagnoseFindsLeftoversAndLoops(t *testing.T) {
	static := []map[string]interface{}{
		{"dst_addr": "10.0.1.0", "netmask": "255.255.255.0", "gateway": "10.0.0.8", "enabled": "yes"},
		{"dst_addr": "9.9.9.9", "netmask": "255.255.255.255", "gateway": "10.0.0.4", "enabled": "no"},
	}
	active := []map[string]interface{}{
		{"dst_addr": "0.0.0.0", "netmask": "0.0.0.0", "gateway": "221.3.46.1", "prio": 0.0},
		{"dst_addr": "10.0.1.0", "netmask": "255.255.255.0", "gateway": "10.0.0.8", "prio": 1.0},
		{"dst_addr": "8.8.8.8", "netmask": "255.255.255.255", "gateway": "10.0.0.4", "prio": 20.0},
	}
	policies := []map[string]interface{}{
		{"tagname": "good", "enabled": "yes", "type": 1.0, "nexthop": "10.0.0.4", "src_addr_inv": 1.0,
			"src_addr": addr("10.0.0.4"), "dst_addr": addr("198.18.0.0/16")},
		{"tagname": "PT_1", "enabled": "yes", "type": 1.0, "nexthop": "10.0.0.6", "src_addr_inv": 0.0,
			"src_addr": addr(), "dst_addr": addr("91.108.0.0/16")},
		{"tagname": "off", "enabled": "no", "type": 1.0, "nexthop": "10.0.0.9", "src_addr": addr()},
	}
	findings := Diagnose(static, active, policies)
	kinds := map[string]Finding{}
	for _, f := range findings {
		kinds[f.Kind+" "+f.Dst] = f
	}
	if len(findings) != 3 {
		t.Fatalf("findings = %+v", findings)
	}
	if f := kinds["stale-kernel-route 8.8.8.8/32"]; f.Severity != "error" || !strings.Contains(f.Detail, "Reboot") {
		t.Fatalf("stale route finding = %+v", f)
	}
	if f := kinds["static-to-lan-host 10.0.1.0/24"]; f.Severity != "warning" || f.Gateway != "10.0.0.8" {
		t.Fatalf("static-to-lan-host finding = %+v", f)
	}
	if f := kinds["policy-loop 91.108.0.0/16"]; f.Gateway != "10.0.0.6" || !strings.Contains(f.Detail, "PT_1") {
		t.Fatalf("policy-loop finding = %+v", f)
	}
	if findings[0].Severity != "error" {
		t.Fatalf("errors should come first: %+v", findings)
	}
}

type recorder struct {
	mu       sync.Mutex
	requests []string
	bodies   []map[string]interface{}
	respond  func(req *http.Request) string
}

func (r *recorder) app(t *testing.T, out *bytes.Buffer) *cliapp.Runtime {
	app := cliapp.New(out, out)
	app.Format = output.JSON
	app.Session = &session.Session{BaseURL: "https://router.local", Token: "t"}
	app.APIClient = api.NewWithHTTPClient(app.Session.BaseURL, app.Session.Token, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.requests = append(r.requests, req.Method+" "+req.URL.Path)
			body := map[string]interface{}{}
			if req.Body != nil {
				data, _ := io.ReadAll(req.Body)
				_ = json.Unmarshal(data, &body)
			}
			r.bodies = append(r.bodies, body)
			return jsonResponse(r.respond(req)), nil
		}),
	})
	return app
}

const policyRecord = `{"code":0,"data":{"data":[{"id":7,"tagname":"cf_pr_1","type":1,"nexthop":"10.0.0.4","prio":31,"enabled":"yes",
"src_addr":{"custom":["10.0.0.4"],"object":{}},"src_addr_inv":1,"dst_addr":{"custom":["198.18.0.0/16"],"object":{}},"comment":"cf-mihomo"}]}}`

func TestUpdateBacksUpThenWarnsWhenTheRouterIgnoresAField(t *testing.T) {
	t.Setenv("IKUAI_CLI_BACKUP_DIR", t.TempDir())
	var out bytes.Buffer
	rec := &recorder{respond: func(req *http.Request) string {
		if req.Method == http.MethodPut {
			return `{"code":0,"data":{}}`
		}
		return policyRecord // the router still reports prio 31
	}}
	app := rec.app(t, &out)
	cmd := New(app)
	cmd.SetArgs([]string{"stream", "five-tuple", "update", "7", "--priority", "0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if want := []string{"GET /api/v4.0/routing/five-tuple-rules/7", "GET /api/v4.0/routing/five-tuple-rules/7",
		"PUT /api/v4.0/routing/five-tuple-rules/7", "GET /api/v4.0/routing/five-tuple-rules/7"}; strings.Join(rec.requests, "|") != strings.Join(want, "|") {
		t.Fatalf("requests = %v", rec.requests)
	}
	put := rec.bodies[2]
	if put["prio"] != 0.0 || put["nexthop"] != "10.0.0.4" || put["src_addr_inv"] != 1.0 {
		t.Fatalf("PUT body lost unchanged fields: %v", put)
	}
	files, _ := filepath.Glob(filepath.Join(os.Getenv("IKUAI_CLI_BACKUP_DIR"), "*five-tuple-rules_7.json"))
	if len(files) != 1 {
		t.Fatalf("backup files = %v", files)
	}
	backup, err := cliapp.LoadBackup(files[0])
	if err != nil || backup.APIPath != "routing/five-tuple-rules" || backup.ID != "7" {
		t.Fatalf("backup = %+v, %v", backup, err)
	}
	if !strings.Contains(out.String(), "warning: readback prio: sent 0, router has 31") {
		t.Fatalf("missing readback warning:\n%s", out.String())
	}
}

func TestDryRunNeitherBacksUpNorReadsBack(t *testing.T) {
	t.Setenv("IKUAI_CLI_BACKUP_DIR", t.TempDir())
	var out bytes.Buffer
	rec := &recorder{respond: func(*http.Request) string { return policyRecord }}
	app := rec.app(t, &out)
	app.APIClient.DryRun = true
	cmd := New(app)
	cmd.SetArgs([]string{"stream", "five-tuple", "toggle", "7", "--enabled", "no"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if files, _ := filepath.Glob(filepath.Join(os.Getenv("IKUAI_CLI_BACKUP_DIR"), "*")); len(files) != 0 {
		t.Fatalf("dry run wrote backups: %v", files)
	}
}

func TestDeleteBacksUpFirst(t *testing.T) {
	t.Setenv("IKUAI_CLI_BACKUP_DIR", t.TempDir())
	var out bytes.Buffer
	rec := &recorder{respond: func(req *http.Request) string {
		if req.Method == http.MethodDelete {
			return `{"code":0,"data":{}}`
		}
		return `{"code":0,"data":{"data":[{"id":3,"tagname":"RT_3","dst_addr":"8.8.8.8","gateway":"10.0.0.4"}]}}`
	}}
	cmd := New(rec.app(t, &out))
	cmd.SetArgs([]string{"static", "delete", "3", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.Join(rec.requests, "|") != "GET /api/v4.0/routing/static-routes/3|DELETE /api/v4.0/routing/static-routes/3" {
		t.Fatalf("requests = %v", rec.requests)
	}
	if files, _ := filepath.Glob(filepath.Join(os.Getenv("IKUAI_CLI_BACKUP_DIR"), "*static-routes_3.json")); len(files) != 1 {
		t.Fatalf("backup files = %v", files)
	}
}

func TestNextHopRuleNeedsNexthopNotInterface(t *testing.T) {
	var out bytes.Buffer
	rec := &recorder{respond: func(*http.Request) string { return `{"code":0,"data":{"id":9}}` }}
	app := rec.app(t, &out)

	cmd := New(app)
	cmd.SetArgs([]string{"stream", "five-tuple", "create", "--name", "cf", "--type", "1"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--nexthop") {
		t.Fatalf("missing --nexthop error = %v", err)
	}

	cmd = New(app)
	cmd.SetArgs([]string{"stream", "five-tuple", "create", "--name", "cf", "--type", "1", "--nexthop", "10.0.0.4",
		"--src-addr", "10.0.0.4", "--src-addr-inv", "1", "--dst-addr", "198.18.0.0/16", "--priority", "0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	body := rec.bodies[len(rec.bodies)-1]
	if body["type"] != 1.0 || body["nexthop"] != "10.0.0.4" || body["interface"] != "" || body["prio"] != 0.0 || body["src_addr_inv"] != 1.0 {
		t.Fatalf("create body = %v", body)
	}

	cmd = New(app)
	cmd.SetArgs([]string{"stream", "five-tuple", "create", "--name", "wan"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--interface") {
		t.Fatalf("WAN rule without --interface error = %v", err)
	}
}

func TestFiveTupleListShowsInvertedSources(t *testing.T) {
	var out bytes.Buffer
	rec := &recorder{respond: func(*http.Request) string { return policyRecord }}
	cmd := New(rec.app(t, &out))
	cmd.SetArgs([]string{"stream", "five-tuple", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), `"src":"!10.0.0.4"`) || !strings.Contains(out.String(), `"dst":"198.18.0.0/16"`) {
		t.Fatalf("list output = %s", out.String())
	}
}
