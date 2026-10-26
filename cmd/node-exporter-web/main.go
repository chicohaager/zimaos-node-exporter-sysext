// Command node-exporter-web serves the Prometheus Exporter tile: turn
// node_exporter (:9100) and smartctl_exporter (:9633) on and off (on = also at
// every boot, node-exporter-zima), show what they deliver and the scrape config
// for Prometheus. The ZFW rule for the two ports is set by the page itself.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chicohaager/lintux-modkit/httpx"
	"github.com/chicohaager/lintux-modkit/webmod"
)

// version is set at build time from VERSION (-ldflags -X main.version=...).
var version = "dev"

// Exporter is one of the two programs the tile switches.
type Exporter struct {
	Name string `json:"name"`
	Unit string `json:"unit"`
	Port int    `json:"port"`
	// Metrics: the series that show the exporter does its job, counted from /metrics.
	Probe string `json:"-"`
}

// Paths and commands; tests point them elsewhere.
var (
	unitDir   = "/etc/systemd/system"
	metricsAt = func(port int) string { return fmt.Sprintf("http://127.0.0.1:%d/metrics", port) }
	run       = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
	exporters = []Exporter{
		{Name: "node_exporter", Unit: "node-exporter.service", Port: 9100, Probe: "node_cpu_seconds_total"},
		{Name: "smartctl_exporter", Unit: "smartctl-exporter.service", Port: 9633, Probe: "smartctl_device"},
	}
)

var mu sync.Mutex

func main() {
	webmod.Run(webmod.Options{
		Name: "node-exporter", Service: "node-exporter-web", Binary: "/usr/bin/node-exporter-web",
		Route: "/node-exporter", Version: version, API: api,
		// The exporters keep the old binaries when zpkg replaces the module (it restarts
		// only node-exporter-web): webmod restarts them then (lintux-modkit stale).
		Restart: []string{exporters[0].Unit, exporters[1].Unit},
	})
}

func api(verify func(http.Handler) http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/node-exporter/status", verify(http.HandlerFunc(statusHandler)))
	mux.Handle("/node-exporter/action", verify(http.HandlerFunc(actionHandler)))
	return mux
}

// State is what the tile shows per exporter.
type State struct {
	Exporter
	Active  string `json:"active"`
	Boot    bool   `json:"boot"`
	Series  int    `json:"series"`  // lines of Probe in /metrics; 0 when not running
	Devices int    `json:"devices"` // smartctl_exporter: disks it reports
	Error   string `json:"error,omitempty"`
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.MethodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	out := []State{}
	for _, e := range exporters {
		s := State{Exporter: e}
		a, _ := run(ctx, "systemctl", "is-active", e.Unit)
		s.Active = strings.TrimSpace(string(a))
		base := strings.ReplaceAll(strings.TrimSuffix(e.Unit, ".service"), "@", "-")
		_, err := os.Stat(filepath.Join(unitDir, base+"-watchdog.timer"))
		s.Boot = err == nil
		if s.Active == "active" {
			s.Series, s.Devices, err = scrape(ctx, metricsAt(e.Port), e.Probe)
			if err != nil {
				s.Error = err.Error()
			}
		}
		out = append(out, s)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"exporters": out, "host": hostOf(r)})
}

// scrape counts the probe series in an exporter's /metrics and, for the SMART
// exporter, the distinct devices (label device="…") it reports.
func scrape(ctx context.Context, url, probe string) (int, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("no answer on %s", url)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("%s answered HTTP %d", url, resp.StatusCode)
	}
	series := 0
	devices := map[string]bool{}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 32<<20))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, probe+"{") && !strings.HasPrefix(line, probe+" ") {
			continue
		}
		series++
		if i := strings.Index(line, `device="`); i >= 0 {
			rest := line[i+8:]
			if j := strings.IndexByte(rest, '"'); j > 0 {
				devices[rest[:j]] = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		return 0, 0, err
	}
	return series, len(devices), nil
}

// hostOf is the host name the browser used for the tile: the address
// Prometheus reaches this host by, for the scrape config.
func hostOf(r *http.Request) string {
	h := r.Host
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return h
}

func actionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.MethodNotAllowed(w)
		return
	}
	var in struct {
		Action string `json:"action"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	var cmd []string
	switch in.Action {
	case "enable", "disable":
		cmd = []string{"node-exporter-zima", in.Action}
	case "restart":
		cmd = []string{"systemctl", "restart", exporters[0].Unit, exporters[1].Unit}
	default:
		httpx.WriteError(w, http.StatusBadRequest, "bad_action", "action: enable, disable or restart")
		return
	}
	mu.Lock()
	defer mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if out, err := run(ctx, cmd[0], cmd[1:]...); err != nil {
		httpx.WriteError(w, http.StatusBadGateway, "failed", strings.TrimSpace(string(out)))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
