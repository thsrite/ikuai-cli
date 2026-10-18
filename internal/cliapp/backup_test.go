package cliapp

import "testing"

func TestMismatchesIgnoreRepresentationDifferences(t *testing.T) {
	sent := map[string]interface{}{
		"prio": int64(0), "enabled": "yes",
		"src_addr": map[string]interface{}{"custom": []interface{}{"10.0.0.4"}, "object": []interface{}{}},
	}
	got := map[string]interface{}{
		"prio": 0.0, "enabled": "yes",
		"src_addr": map[string]interface{}{"custom": []interface{}{"10.0.0.4"}, "object": map[string]interface{}{}},
	}
	if m := Mismatches(sent, got); len(m) != 0 {
		t.Fatalf("unexpected mismatches: %v", m)
	}
	got["prio"] = 31.0
	if m := Mismatches(sent, got); len(m) != 1 || m[0] != "prio: sent 0, router has 31" {
		t.Fatalf("mismatches = %v", m)
	}
}
