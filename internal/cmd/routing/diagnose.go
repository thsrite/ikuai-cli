package routing

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/ikuaidev/ikuai-cli/internal/cliapp"
	"github.com/ikuaidev/ikuai-cli/internal/webapi"
	"github.com/spf13/cobra"
)

// Finding is one routing problem found by diagnose.
type Finding struct {
	Severity string `json:"severity"` // error | warning
	Kind     string `json:"kind"`
	Dst      string `json:"dst"`
	Gateway  string `json:"gateway"`
	Detail   string `json:"detail"`
}

func diagnoseCmd(app *cliapp.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "diagnose",
		Short: "Find kernel leftover routes and routing loops (web management API)",
		Long: `Compare static routes with the kernel routing table (当前路由表) and check rules that hand
traffic to a LAN host:

  stale-kernel-route  the kernel forwards with a route no static route explains. iKuai can
                      keep one after a static route is edited or deleted; it cannot be removed
                      through the UI or API and beats 端口分流. Reboot the router to clear it.
  static-to-lan-host  a static route's next hop is a LAN host. Static routes match the
                      destination only, so that host's own traffic to the destination is sent
                      back to it and can loop. Use a 端口分流 rule that excludes the host.
  policy-loop         a next-hop 端口分流 rule does not exclude its own next hop as a source.

Needs IKUAI_CLI_WEB_USER / IKUAI_CLI_WEB_PASSWORD (the REST API has no routing table).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.RequireURL(); err != nil {
				return err
			}
			client, err := webapi.FromEnv(app.Session.BaseURL)
			if err != nil {
				return &cliapp.ValidationError{Message: err.Error()}
			}
			static, err := client.List("static_rt")
			if err != nil {
				return err
			}
			active, err := client.Call("static_rt_table", "show", map[string]interface{}{"TYPE": "data"})
			if err != nil {
				return err
			}
			policies, err := client.List("stream_ipport")
			if err != nil {
				return err
			}
			findings := Diagnose(static, rows(active["data"]), policies)
			app.DefaultColumns = []string{"severity", "kind", "dst", "gateway", "detail"}
			app.PrintJSON(findings)
			return nil
		},
	}
}

func rows(value interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	items, _ := value.([]interface{})
	for _, item := range items {
		if row, ok := item.(map[string]interface{}); ok {
			out = append(out, row)
		}
	}
	return out
}

// Diagnose inspects static routes, the kernel table and 端口分流 rules.
func Diagnose(static, active, policies []map[string]interface{}) []Finding {
	findings := []Finding{}
	configured := map[string]bool{}
	for _, route := range static {
		dst := cidr(route["dst_addr"], route["netmask"])
		gateway := text(route["gateway"])
		if text(route["enabled"]) != "yes" || dst == "" {
			continue
		}
		configured[dst+" "+gateway] = true
		if lanHost(gateway) {
			findings = append(findings, Finding{"warning", "static-to-lan-host", dst, gateway,
				fmt.Sprintf("if %s itself reaches %s through the router, its traffic is sent back to it and loops; a 端口分流 rule excluding %s avoids that", gateway, dst, gateway)})
		}
	}
	for _, route := range active {
		dst := cidr(route["dst_addr"], route["netmask"])
		gateway := text(route["gateway"])
		if dst == "" || dst == "0.0.0.0/0" || !lanHost(gateway) || configured[dst+" "+gateway] {
			continue
		}
		findings = append(findings, Finding{"error", "stale-kernel-route", dst, gateway,
			fmt.Sprintf("kernel route via %s (prio %v) has no static route; it beats 端口分流 and cannot be deleted through the UI or API. Reboot the router to clear it", gateway, route["prio"])})
	}
	for _, rule := range policies {
		nexthop := text(rule["nexthop"])
		if text(rule["enabled"]) != "yes" || number(rule["type"]) != 1 || nexthop == "" {
			continue
		}
		excluded := number(rule["src_addr_inv"]) == 1 && contains(custom(rule["src_addr"]), nexthop)
		if !excluded {
			findings = append(findings, Finding{"warning", "policy-loop", strings.Join(custom(rule["dst_addr"]), ","), nexthop,
				fmt.Sprintf("rule %v does not exclude its next hop %s as a source (set --src-addr %s --src-addr-inv 1)", rule["tagname"], nexthop, nexthop)})
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Severity < findings[j].Severity })
	return findings
}

func cidr(addr, mask interface{}) string {
	ip := net.ParseIP(text(addr)).To4()
	maskIP := net.ParseIP(strings.Fields(text(mask) + " ")[0]).To4()
	if ip == nil || maskIP == nil {
		return ""
	}
	ones, _ := net.IPv4Mask(maskIP[0], maskIP[1], maskIP[2], maskIP[3]).Size()
	return fmt.Sprintf("%s/%d", ip.Mask(net.IPv4Mask(maskIP[0], maskIP[1], maskIP[2], maskIP[3])), ones)
}

// lanHost: a next hop inside a private range, i.e. a device on the LAN side.
func lanHost(gateway string) bool {
	ip := net.ParseIP(gateway)
	return ip != nil && ip.IsPrivate()
}

func text(value interface{}) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func number(value interface{}) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		var n int
		_, _ = fmt.Sscan(typed, &n)
		return n
	}
	return -1
}

// custom returns the literal addresses of an iKuai address field.
func custom(value interface{}) []string {
	field, _ := value.(map[string]interface{})
	items, _ := field["custom"].([]interface{})
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, text(item))
	}
	return out
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
