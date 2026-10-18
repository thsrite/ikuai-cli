package cliapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const backupDirEnv = "IKUAI_CLI_BACKUP_DIR"

// Backup is the router object as it was before a change or delete.
type Backup struct {
	SavedAt string          `json:"saved_at"`
	APIPath string          `json:"api_path"` // e.g. routing/static-routes
	ID      string          `json:"id"`
	Record  json.RawMessage `json:"record"`
	File    string          `json:"file,omitempty"`
}

// BackupDir is $IKUAI_CLI_BACKUP_DIR or ~/.ikuai-cli/backups.
func BackupDir() string {
	if dir := strings.TrimSpace(os.Getenv(backupDirEnv)); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ikuai-cli", "backups")
}

// SaveBackup writes the current record of apiPath/id and returns the file path.
func SaveBackup(apiPath, id string, record json.RawMessage) (string, error) {
	dir := BackupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	now := time.Now()
	name := fmt.Sprintf("%s_%s_%s.json", now.Format("20060102-150405.000"),
		strings.NewReplacer("/", "-", "\\", "-").Replace(strings.Trim(apiPath, "/")), id)
	data, err := json.MarshalIndent(Backup{SavedAt: now.Format(time.RFC3339), APIPath: strings.Trim(apiPath, "/"), ID: id, Record: record}, "", "  ")
	if err != nil {
		return "", err
	}
	file := filepath.Join(dir, name)
	return file, os.WriteFile(file, data, 0o600)
}

// LoadBackup reads one backup file.
func LoadBackup(file string) (*Backup, error) {
	data, err := os.ReadFile(file) //nolint:gosec // reading the backup file the user named is the purpose
	if err != nil {
		return nil, err
	}
	var backup Backup
	if err := json.Unmarshal(data, &backup); err != nil || backup.APIPath == "" || len(backup.Record) == 0 {
		return nil, &ValidationError{Message: "not an ikuai-cli backup file: " + file}
	}
	backup.File = file
	return &backup, nil
}

// ListBackups returns backups, newest first.
func ListBackups() ([]Backup, error) {
	matches, err := filepath.Glob(filepath.Join(BackupDir(), "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	backups := []Backup{}
	for _, file := range matches {
		if backup, err := LoadBackup(file); err == nil {
			backup.Record = nil
			backups = append(backups, *backup)
		}
	}
	return backups, nil
}

// RecordFromGet finds the object (it has a "tagname") in a get-by-ID response.
func RecordFromGet(raw json.RawMessage) (map[string]interface{}, bool) {
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, false
	}
	return findRecord(value)
}

func findRecord(value interface{}) (map[string]interface{}, bool) {
	switch typed := value.(type) {
	case map[string]interface{}:
		if _, ok := typed["tagname"]; ok {
			return typed, true
		}
		for _, key := range []string{"data", "results"} {
			if nested, ok := typed[key]; ok {
				if found, ok := findRecord(nested); ok {
					return found, true
				}
			}
		}
	case []interface{}:
		if len(typed) > 0 {
			return findRecord(typed[0])
		}
	}
	return nil, false
}

// InputBody turns a read record into a write body: drop id and *_int, and
// send empty address lists as [] (the router reads them back as {}).
func InputBody(record map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{}
	for k, v := range record {
		if k == "id" || strings.HasSuffix(k, "_int") {
			continue
		}
		body[k] = v
	}
	normalizeEmpty(body)
	return body
}

func normalizeEmpty(value interface{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for k, child := range typed {
			if m, ok := child.(map[string]interface{}); ok && len(m) == 0 && (k == "custom" || k == "object") {
				typed[k] = []interface{}{}
				continue
			}
			normalizeEmpty(child)
		}
	case []interface{}:
		for _, child := range typed {
			normalizeEmpty(child)
		}
	}
}

// Mismatches lists fields of sent that the read-back record does not hold.
// Scalars compare by text (the router may return 1 for "1"); empty address
// lists compare equal whether {} or [].
func Mismatches(sent, got map[string]interface{}) []string {
	var out []string
	for key, want := range sent {
		have, ok := got[key]
		if !ok || !sameValue(want, have) {
			out = append(out, fmt.Sprintf("%s: sent %s, router has %s", key, compact(want), compact(have)))
		}
	}
	sort.Strings(out)
	return out
}

func sameValue(a, b interface{}) bool {
	switch typedA := a.(type) {
	case map[string]interface{}, []interface{}:
		copyA, copyB := roundTrip(typedA), roundTrip(b)
		normalizeEmpty(copyA)
		normalizeEmpty(copyB)
		return compact(copyA) == compact(copyB)
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func roundTrip(value interface{}) interface{} {
	data, _ := json.Marshal(value)
	var out interface{}
	_ = json.Unmarshal(data, &out)
	if m, ok := out.(map[string]interface{}); ok {
		wrapped := map[string]interface{}{"v": m}
		normalizeEmpty(wrapped)
		return wrapped["v"]
	}
	return out
}

func compact(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}
