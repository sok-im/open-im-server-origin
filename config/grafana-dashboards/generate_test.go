package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func asPanelMaps(v any) []map[string]any {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var panels []map[string]any
	if err := json.Unmarshal(raw, &panels); err != nil {
		return nil
	}
	return panels
}

func collectExprs(panels []map[string]any) []string {
	var out []string
	for _, p := range panels {
		targets, _ := p["targets"].([]any)
		if targets == nil {
			if tmaps, ok := p["targets"].([]map[string]any); ok {
				for _, t := range tmaps {
					if expr, ok := t["expr"].(string); ok {
						out = append(out, expr)
					}
				}
				continue
			}
		}
		for _, t := range targets {
			tm, _ := t.(map[string]any)
			if tm == nil {
				continue
			}
			if expr, ok := tm["expr"].(string); ok {
				out = append(out, expr)
			}
		}
	}
	return out
}

func mustContain(t *testing.T, exprs []string, substr string) {
	t.Helper()
	for _, e := range exprs {
		if strings.Contains(e, substr) {
			return
		}
	}
	t.Fatalf("expected expr containing %q, got %#v", substr, exprs)
}

func mustNotContain(t *testing.T, exprs []string, substr string) {
	t.Helper()
	for _, e := range exprs {
		if strings.Contains(e, substr) {
			t.Fatalf("did not expect expr containing %q, found %q", substr, e)
		}
	}
}

func TestBuildDomainDashboard_AuthHasAPIAndRPC(t *testing.T) {
	d := Domain{ID: "auth", Title: "Auth", APIModule: "auth", RPCName: "auth"}
	dash := buildDomainDashboard(d)
	if dash["uid"] != "openim-domain-auth" {
		t.Fatalf("uid=%v", dash["uid"])
	}
	if dash["title"] != "OpenIM Domain / Auth" {
		t.Fatalf("title=%v", dash["title"])
	}
	exprs := collectExprs(asPanelMaps(dash["panels"]))
	mustContain(t, exprs, `api_count{module="auth"`)
	mustContain(t, exprs, `rpc_count{name="auth"`)
}

func TestBuildDomainDashboard_ObjectAPIOnly(t *testing.T) {
	d := Domain{ID: "object", Title: "Object", APIModule: "object", RPCName: ""}
	dash := buildDomainDashboard(d)
	exprs := collectExprs(asPanelMaps(dash["panels"]))
	mustContain(t, exprs, `module="object"`)
	mustNotContain(t, exprs, `rpc_count`)
}

func TestBuildDomainDashboard_CaptchaRPCOnly(t *testing.T) {
	d := Domain{ID: "captcha", Title: "Captcha", APIModule: "", RPCName: "captcha"}
	dash := buildDomainDashboard(d)
	exprs := collectExprs(asPanelMaps(dash["panels"]))
	mustContain(t, exprs, `rpc_count{name="captcha"`)
	mustNotContain(t, exprs, `api_count`)
}

func TestLoadModulesCounts(t *testing.T) {
	path := filepath.Join("modules.yaml")
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join("config", "grafana-dashboards", "modules.yaml")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skip("modules.yaml not found")
	}
	m, err := loadModules(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Domains) != 16 {
		t.Fatalf("domains=%d want 16", len(m.Domains))
	}
	if len(m.Services) != 19 {
		t.Fatalf("services=%d want 19", len(m.Services))
	}
}

func TestBuildServiceDashboard_Gateway(t *testing.T) {
	s := Service{ID: "msggateway", Title: "MsgGateway", Job: "openimserver-openim-msggateway", Kind: "gateway"}
	dash := buildServiceDashboard(s)
	if dash["uid"] != "openim-svc-msggateway" {
		t.Fatalf("uid=%v", dash["uid"])
	}
	exprs := collectExprs(asPanelMaps(dash["panels"]))
	mustContain(t, exprs, `up{job="openimserver-openim-msggateway"}`)
	mustContain(t, exprs, `online_user_num`)
	mustContain(t, exprs, `ws_connection_num`)
	mustContain(t, exprs, `ws_conn_rejected_total`)
}

func TestBuildServiceDashboard_MsgExtras(t *testing.T) {
	s := Service{ID: "rpc-msg", Title: "rpc-msg", Job: "openimserver-openim-rpc-msg", Kind: "rpc", Extras: []string{"msg_process"}}
	exprs := collectExprs(asPanelMaps(buildServiceDashboard(s)["panels"]))
	mustContain(t, exprs, `single_chat_msg_process_success_total`)
	mustContain(t, exprs, `rpc_count{job="openimserver-openim-rpc-msg"`)
}

func TestBuildServiceDashboard_TransferUsesSum(t *testing.T) {
	s := Service{ID: "msgtransfer", Title: "MsgTransfer", Job: "openimserver-openim-msgtransfer", Kind: "transfer"}
	exprs := collectExprs(asPanelMaps(buildServiceDashboard(s)["panels"]))
	mustContain(t, exprs, `sum(rate(msg_insert_redis_success_total`)
}

func TestStatHasReduceOptions(t *testing.T) {
	s := Service{ID: "api", Title: "API", Job: "openimserver-openim-api", Kind: "api"}
	panels := asPanelMaps(buildServiceDashboard(s)["panels"])
	found := false
	for _, p := range panels {
		if p["type"] != "stat" {
			continue
		}
		found = true
		opts, _ := p["options"].(map[string]any)
		if opts == nil {
			t.Fatal("stat missing options")
		}
		ro, _ := opts["reduceOptions"].(map[string]any)
		if ro == nil {
			t.Fatal("stat missing reduceOptions")
		}
	}
	if !found {
		t.Fatal("expected at least one stat panel")
	}
}
