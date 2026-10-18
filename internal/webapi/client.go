// Package webapi talks to the iKuai 4.x web management interface
// (/Action/login, /Action/call), the API the router's own web UI uses.
//
// The local REST API (/api/v4.0) does not expose everything the UI can: the
// kernel routing table (当前路由表) and virtual machines, for example. Commands
// that need those fall back to this client. It authenticates with the admin
// account, so credentials are only read from the environment and never saved.
package webapi

import (
	"bytes"
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"time"
	"unicode"
)

const (
	envUser     = "IKUAI_CLI_WEB_USER"
	envPassword = "IKUAI_CLI_WEB_PASSWORD"
	pageSize    = 1000
)

// Error is a failed /Action/call (non-zero code).
type Error struct {
	Func, Action string
	Code         int
	Message      string
}

func (e *Error) Error() string {
	hint := ""
	switch e.Code {
	case 1003:
		hint = " (access denied: admin account required, or the function does not exist)"
	case 2007:
		hint = " (unknown TYPE for this function)"
	case 3001:
		hint = " (action not supported, e.g. a read-only function)"
	}
	return fmt.Sprintf("iKuai %s %s failed: code %d %s%s", e.Func, e.Action, e.Code, e.Message, hint)
}

// Client is a logged-in web management session.
type Client struct {
	BaseURL  string
	Username string
	Password string
	HTTP     *http.Client
	loggedIn bool
}

// FromEnv builds a client for baseURL using IKUAI_CLI_WEB_USER/IKUAI_CLI_WEB_PASSWORD.
func FromEnv(baseURL string) (*Client, error) {
	user, password := strings.TrimSpace(os.Getenv(envUser)), os.Getenv(envPassword)
	if baseURL == "" {
		return nil, fmt.Errorf("no router URL configured. Run: ikuai-cli auth set-url https://192.168.1.1")
	}
	if user == "" || password == "" {
		return nil, fmt.Errorf("this command uses the router's web management API: set %s and %s (admin account; never saved to disk)", envUser, envPassword)
	}
	return New(baseURL, user, password), nil
}

// New creates a client. Routers usually serve a self-signed certificate.
func New(baseURL, user, password string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		Username: user,
		Password: password,
		HTTP: &http.Client{
			Timeout: 15 * time.Second,
			Jar:     jar,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // local router, self-signed
			},
		},
	}
}

func (c *Client) post(path string, payload interface{}) (map[string]interface{}, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Post(c.BaseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cannot reach iKuai web management: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iKuai web management returned HTTP %d", resp.StatusCode)
	}
	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("iKuai web management did not return JSON: check the router URL")
	}
	return data, nil
}

// Login authenticates once per client: md5 password plus base64("salt_11"+password).
func (c *Client) Login() error {
	if c.loggedIn {
		return nil
	}
	sum := md5.Sum([]byte(c.Password))
	data, err := c.post("/Action/login", map[string]string{
		"username": c.Username,
		"passwd":   hex.EncodeToString(sum[:]),
		"pass":     base64.StdEncoding.EncodeToString([]byte("salt_11" + c.Password)),
	})
	if err != nil {
		return err
	}
	if number(data["Result"]) != 10000 && number(data["code"]) != 0 {
		return fmt.Errorf("iKuai web login failed: %v", data["message"])
	}
	c.loggedIn = true
	return nil
}

// Call runs func_name/action. Write params are encoded like the web UI forms;
// the returned results are decoded.
func (c *Client) Call(fn, action string, param map[string]interface{}) (map[string]interface{}, error) {
	if err := c.Login(); err != nil {
		return nil, err
	}
	if action != "show" {
		param = Encode(param).(map[string]interface{})
	}
	data, err := c.post("/Action/call", map[string]interface{}{"func_name": fn, "action": action, "param": param})
	if err != nil {
		return nil, err
	}
	if _, legacy := data["Data"]; legacy {
		return nil, fmt.Errorf("iKuai 3.x response detected; only 4.x is supported")
	}
	if code := number(data["code"]); code != 0 {
		message, _ := data["message"].(string)
		return nil, &Error{Func: fn, Action: action, Code: code, Message: message}
	}
	results, _ := data["results"].(map[string]interface{})
	if results == nil {
		results = map[string]interface{}{}
	}
	return Decode(results).(map[string]interface{}), nil
}

// List reads every row of a total/data function, failing on a truncated read.
func (c *Client) List(fn string) ([]map[string]interface{}, error) {
	var rows []map[string]interface{}
	for offset := 0; ; offset += pageSize {
		results, err := c.Call(fn, "show", map[string]interface{}{
			"TYPE": "total,data", "limit": fmt.Sprintf("%d,%d", offset, pageSize),
		})
		if err != nil {
			return nil, err
		}
		page, _ := results["data"].([]interface{})
		for _, item := range page {
			if row, ok := item.(map[string]interface{}); ok {
				rows = append(rows, row)
			}
		}
		total := number(results["total"])
		if len(page) < pageSize || len(rows) >= total {
			if total > 0 && len(rows) != total {
				return nil, fmt.Errorf("iKuai %s list changed while reading (%d of %d rows), retry", fn, len(rows), total)
			}
			return rows, nil
		}
	}
}

func number(value interface{}) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	}
	return -1
}

// The web UI escapes these after '%' (first) and whitespace (-> %20).
var escapes = [][2]string{{`"`, "%22"}, {"`", "%60"}, {"$", "%24"}, {"&", "%26"}, {";", "%3B"},
	{`\`, "%5C"}, {"|", "%7C"}, {"'", "%27"}}

// Encode applies the web UI form encoding to every string in a value.
func Encode(value interface{}) interface{} { return encode(value, "") }

func encode(value interface{}, key string) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for k, v := range typed {
			out[k] = encode(v, k)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, v := range typed {
			out[i] = encode(v, "")
		}
		return out
	case string:
		text := typed
		if key == "comment" {
			text = strings.TrimSpace(text)
		}
		text = strings.ReplaceAll(text, "%", "%25")
		if key == "comment" {
			text = strings.NewReplacer("\r\n", "%0A", "\n", "%0A", "\r", "%0A").Replace(text)
		}
		// Every whitespace character becomes %20, one for one.
		text = strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return ' '
			}
			return r
		}, text)
		text = strings.ReplaceAll(text, " ", "%20")
		for _, pair := range escapes {
			text = strings.ReplaceAll(text, pair[0], pair[1])
		}
		return text
	}
	return value
}

// Decode reverses Encode for values read back from the router.
func Decode(value interface{}) interface{} { return decode(value, "") }

func decode(value interface{}, key string) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for k, v := range typed {
			out[k] = decode(v, k)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, v := range typed {
			out[i] = decode(v, "")
		}
		return out
	case string:
		text := typed
		if key == "comment" {
			text = strings.NewReplacer("%0A", "\n", "%0a", "\n").Replace(text)
		}
		text = strings.ReplaceAll(text, "%20", " ")
		for _, pair := range escapes {
			text = strings.ReplaceAll(text, pair[1], pair[0])
			text = strings.ReplaceAll(text, strings.ToLower(pair[1]), pair[0])
		}
		return strings.NewReplacer("%2F", "/", "%2f", "/", "%25", "%").Replace(text)
	}
	return value
}
