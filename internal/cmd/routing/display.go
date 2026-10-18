package routing

import (
	"encoding/json"
	"strings"

	"github.com/ikuaidev/ikuai-cli/internal/cliapp"
	"github.com/spf13/cobra"
)

// fiveTupleCreateCheck: a WAN-line rule needs --interface, a next-hop rule --nexthop.
func fiveTupleCreateCheck(cmd *cobra.Command) error {
	forwarding := "0"
	if f := cmd.Flags().Lookup("type"); f != nil && f.Changed {
		forwarding = strings.TrimSpace(f.Value.String())
	}
	if forwarding == "1" {
		return cliapp.RequireFlags(cmd, "nexthop")
	}
	return cliapp.RequireFlags(cmd, "interface")
}

// addAddressDisplay adds src/dst columns that show inverted matches as "!addr",
// which the iKuai web list hides (it shows the address only).
func addAddressDisplay(row map[string]interface{}) {
	row["src"] = addressText(row["src_addr"], number(row["src_addr_inv"]) == 1)
	row["dst"] = addressText(row["dst_addr"], number(row["dst_addr_inv"]) == 1)
}

func addressText(value interface{}, inverted bool) string {
	field, _ := value.(map[string]interface{})
	parts := custom(value)
	objects, _ := field["object"].([]interface{})
	for _, object := range objects {
		if ref, ok := object.(map[string]interface{}); ok {
			parts = append(parts, "@"+text(ref["gp_name"]))
		}
	}
	result := strings.Join(parts, ",")
	if result == "" {
		result = "any"
	}
	if inverted {
		return "!" + result
	}
	return result
}

// transformRows applies fn to every rule row (an object with "tagname") in a response.
func transformRows(raw json.RawMessage, fn func(map[string]interface{})) json.RawMessage {
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	var walk func(interface{})
	walk = func(node interface{}) {
		switch typed := node.(type) {
		case map[string]interface{}:
			if _, ok := typed["tagname"]; ok {
				fn(typed)
				return
			}
			for _, child := range typed {
				walk(child)
			}
		case []interface{}:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	out, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return out
}
