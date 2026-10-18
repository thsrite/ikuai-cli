package webapi

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestEncodeMatchesWebUIForms(t *testing.T) {
	// Same vector ConfigFlow verified against the 4.x web UI.
	got := Encode(map[string]interface{}{
		"comment":  " cf:Unraid SS\nnext ",
		"gateway":  "10.0.0.4",
		"prio":     1.0,
		"dst_addr": map[string]interface{}{"custom": []interface{}{"a b"}, "object": []interface{}{}},
		"x":        `100% "a";$b`,
	})
	want := map[string]interface{}{
		"comment":  "cf:Unraid%20SS%0Anext",
		"gateway":  "10.0.0.4",
		"prio":     1.0,
		"dst_addr": map[string]interface{}{"custom": []interface{}{"a%20b"}, "object": []interface{}{}},
		"x":        "100%25%20%22a%22%3B%24b",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Encode() = %#v\nwant %#v", got, want)
	}
	back := Decode(got).(map[string]interface{})
	if back["x"] != `100% "a";$b` || back["comment"] != "cf:Unraid SS\nnext" {
		t.Fatalf("Decode() round trip = %#v", back)
	}
}

type fakeRouter struct {
	t       *testing.T
	calls   []map[string]interface{}
	respond func(call map[string]interface{}) map[string]interface{}
}

func (f *fakeRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch r.URL.Path {
	case "/Action/login":
		sum := md5.Sum([]byte("pw"))
		if body["username"] != "admin" || body["passwd"] != hex.EncodeToString(sum[:]) ||
			body["pass"] != base64.StdEncoding.EncodeToString([]byte("salt_11pw")) {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"Result": 30000, "message": "bad"})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "sess_key", Value: "ok"})
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"Result": 10000})
	case "/Action/call":
		if c, err := r.Cookie("sess_key"); err != nil || c.Value != "ok" {
			f.t.Fatalf("call without session cookie")
		}
		f.calls = append(f.calls, body)
		_ = json.NewEncoder(w).Encode(f.respond(body))
	}
}

func TestLoginAndPaginatedList(t *testing.T) {
	router := &fakeRouter{t: t, respond: func(call map[string]interface{}) map[string]interface{} {
		param := call["param"].(map[string]interface{})
		rows := []interface{}{}
		if param["limit"] == "0,1000" {
			for i := 0; i < 1000; i++ {
				rows = append(rows, map[string]interface{}{"id": float64(i), "comment": "a%20b"})
			}
		} else {
			rows = append(rows, map[string]interface{}{"id": float64(1000), "comment": "tail"})
		}
		return map[string]interface{}{"code": 0, "results": map[string]interface{}{"total": 1001, "data": rows}}
	}}
	server := httptest.NewServer(router)
	defer server.Close()

	client := New(server.URL, "admin", "pw")
	rows, err := client.List("static_rt")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(rows) != 1001 || rows[0]["comment"] != "a b" || len(router.calls) != 2 {
		t.Fatalf("rows=%d first=%v calls=%d", len(rows), rows[0], len(router.calls))
	}
}

func TestCallErrorsExplainCommonCodes(t *testing.T) {
	router := &fakeRouter{t: t, respond: func(map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"code": 3001, "message": "invalid action"}
	}}
	server := httptest.NewServer(router)
	defer server.Close()

	_, err := New(server.URL, "admin", "pw").Call("static_rt_table", "del", map[string]interface{}{"id": 1})
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != 3001 || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("error = %v", err)
	}
}

func TestWriteParamsAreEncoded(t *testing.T) {
	router := &fakeRouter{t: t, respond: func(map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"code": 0, "results": map[string]interface{}{}}
	}}
	server := httptest.NewServer(router)
	defer server.Close()

	if _, err := New(server.URL, "admin", "pw").Call("dnat", "edit", map[string]interface{}{"comment": "Unraid SS"}); err != nil {
		t.Fatal(err)
	}
	if got := router.calls[0]["param"].(map[string]interface{})["comment"]; got != "Unraid%20SS" {
		t.Fatalf("comment sent as %v", got)
	}
}

func TestFromEnvRequiresCredentials(t *testing.T) {
	t.Setenv(envUser, "")
	t.Setenv(envPassword, "")
	if _, err := FromEnv("http://10.0.0.1"); err == nil || !strings.Contains(err.Error(), envUser) {
		t.Fatalf("FromEnv() error = %v", err)
	}
}
