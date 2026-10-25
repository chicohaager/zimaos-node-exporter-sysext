package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Real exporter output, shortened: node_exporter 1.12.1 and smartctl_exporter 0.15.0 series.
const nodeMetrics = `# HELP node_cpu_seconds_total Seconds the CPUs spent in each mode.
# TYPE node_cpu_seconds_total counter
node_cpu_seconds_total{cpu="0",mode="idle"} 1234.5
node_cpu_seconds_total{cpu="0",mode="user"} 12.5
node_cpu_seconds_total{cpu="1",mode="idle"} 1200
node_load1 0.12
`

const smartMetrics = `# HELP smartctl_device Device info
smartctl_device{device="sda",interface="scsi",model_name="X"} 1
smartctl_device{device="sdb",interface="scsi",model_name="Y"} 1
smartctl_device{device="sdc",interface="sat",model_name="Z"} 1
smartctl_device_temperature{device="sdc",temperature_type="current"} 31
`

type fake struct {
	calls  [][]string
	active map[string]string
}

func setup(t *testing.T, metrics map[int]string) *fake {
	t.Helper()
	f := &fake{active: map[string]string{}}
	unitDir = t.TempDir()
	ports := map[int]string{}
	for port, body := range metrics {
		body := body
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		t.Cleanup(srv.Close)
		ports[port] = srv.URL + "/metrics"
	}
	metricsAt = func(port int) string {
		if u, ok := ports[port]; ok {
			return u
		}
		return "http://127.0.0.1:1/metrics" // nothing listens there
	}
	run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		f.calls = append(f.calls, append([]string{name}, args...))
		if name == "systemctl" && args[0] == "is-active" {
			if s, ok := f.active[args[1]]; ok {
				return []byte(s + "\n"), nil
			}
			return []byte("inactive\n"), nil
		}
		return nil, nil
	}
	return f
}

func call(t *testing.T, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, &buf)
	req.Host = "192.0.2.10"
	api(func(h http.Handler) http.Handler { return h }).ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

func states(t *testing.T, out map[string]any) map[string]map[string]any {
	t.Helper()
	m := map[string]map[string]any{}
	for _, e := range out["exporters"].([]any) {
		s := e.(map[string]any)
		m[s["name"].(string)] = s
	}
	return m
}

func TestStatusCountsSeriesAndDisks(t *testing.T) {
	f := setup(t, map[int]string{9100: nodeMetrics, 9633: smartMetrics})
	f.active["node-exporter.service"] = "active"
	f.active["smartctl-exporter.service"] = "active"
	if err := os.WriteFile(filepath.Join(unitDir, "node-exporter-watchdog.timer"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	res, out := call(t, "GET", "/node-exporter/status", nil)
	if res.Code != 200 || out["host"] != "192.0.2.10" {
		t.Fatalf("status: %d %v", res.Code, out)
	}
	s := states(t, out)
	if n := s["node_exporter"]; n["series"].(float64) != 3 || n["boot"] != true || n["port"].(float64) != 9100 {
		t.Fatalf("node_exporter: %v", n)
	}
	if m := s["smartctl_exporter"]; m["series"].(float64) != 3 || m["devices"].(float64) != 3 || m["boot"] != false {
		t.Fatalf("smartctl_exporter: %v", m)
	}
}

// A unit that says active but answers nothing is reported, not counted as working.
func TestActiveButSilentIsAnError(t *testing.T) {
	f := setup(t, map[int]string{})
	f.active["node-exporter.service"] = "active"
	_, out := call(t, "GET", "/node-exporter/status", nil)
	n := states(t, out)["node_exporter"]
	if n["series"].(float64) != 0 || !strings.Contains(n["error"].(string), "no answer on") {
		t.Fatalf("silent exporter: %v", n)
	}
	if s := states(t, out)["smartctl_exporter"]; s["active"] != "inactive" || s["error"] != nil {
		t.Fatalf("stopped exporter must not be scraped: %v", s)
	}
}

func TestActions(t *testing.T) {
	f := setup(t, nil)
	for action, want := range map[string]string{
		"enable":  "node-exporter-zima enable",
		"disable": "node-exporter-zima disable",
		"restart": "systemctl restart node-exporter.service smartctl-exporter.service",
	} {
		f.calls = nil
		if res, out := call(t, "POST", "/node-exporter/action", map[string]string{"action": action}); res.Code != 200 {
			t.Fatalf("%s: %d %v", action, res.Code, out)
		}
		if got := strings.Join(f.calls[len(f.calls)-1], " "); got != want {
			t.Errorf("%s ran %q, want %q", action, got, want)
		}
	}
	if res, _ := call(t, "POST", "/node-exporter/action", map[string]string{"action": "rm -rf"}); res.Code != 400 {
		t.Fatalf("unknown action: %d", res.Code)
	}
	if res, _ := call(t, "GET", "/node-exporter/action", nil); res.Code != 405 {
		t.Fatalf("GET action: %d", res.Code)
	}
}

func TestHostOfDropsThePort(t *testing.T) {
	for in, want := range map[string]string{"192.0.2.10:8080": "192.0.2.10", "zima.local": "zima.local", "[2001:db8::1]:80": "2001:db8::1"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = in
		if got := hostOf(r); got != want {
			t.Errorf("hostOf(%s) = %s, want %s", in, got, want)
		}
	}
}
