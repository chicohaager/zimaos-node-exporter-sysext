#!/bin/sh
# Run on a ZimaOS host as root after `zpkg install node-exporter.raw`.
#   sudo sh host-test.sh           turn both exporters on, check what they deliver, leave them running (reboot test)
#   sudo sh host-test.sh cleanup   turn them off again — unless they ran before the test
set -u
# pass/fail and the cleanup after a failure or abort: lintux-modkit tools/hosttest-lib.sh, copied next to this file
. "$(dirname "$0")/hosttest-lib.sh"
HT_LIST=/etc/lintux-hosttest/node-exporter.list

# Only what this run turned on is turned off: exporters that ran before stay on (after a failure,
# an abort and with `cleanup`). The state before the run is kept in $WAS.
# (27.10.2026: `cleanup` turned off exporters that had run since the boot.)
WAS=/etc/lintux-hosttest/node-exporter.was
exporter_cleanup() {
  if [ "$(cat "$WAS" 2>/dev/null)" = active ]; then echo "exporters ran before the test: left on"
  else node-exporter-zima disable; fi
  rm -f "$WAS"
  echo "cleanup done: exporters $(systemctl is-active node-exporter.service), $(ls /etc/systemd/system/*exporter-watchdog.* 2>/dev/null | wc -l) watchdog units"
}
if [ "${1:-}" = cleanup ]; then
  exporter_cleanup
  rm -f "$HT_LIST"
  exit 0
fi

v=$(node_exporter --version 2>&1 | head -n 1); case "$v" in *"version 1.12.1"*) pass "node_exporter: $v" ;; *) fail "node_exporter version: $v" ;; esac
v=$(smartctl_exporter --version 2>&1 | head -n 1); case "$v" in *"version 0.15.0"*) pass "smartctl_exporter: $v" ;; *) fail "smartctl_exporter version: $v" ;; esac
a=$(systemctl is-active node-exporter-web.service); [ "$a" = active ] && pass "node-exporter-web active" || fail "node-exporter-web: $a"

mkdir -p "$(dirname "$WAS")"; [ -e "$WAS" ] || systemctl is-active node-exporter.service > "$WAS"
ht_init "$HT_LIST"; ht_made call exporter_cleanup   # from here on a failure or abort runs exporter_cleanup
node-exporter-zima enable >/dev/null 2>&1 || fail "node-exporter-zima enable"
for i in 1 2 3 4 5 6 7 8 9 10; do
  curl -fs -m 3 -o /dev/null http://127.0.0.1:9100/metrics && curl -fs -m 20 -o /dev/null http://127.0.0.1:9633/metrics && break
  sleep 2
done
n=$(curl -fs -m 10 http://127.0.0.1:9100/metrics | grep -c '^node_cpu_seconds_total{')
[ "$n" -gt 0 ] && pass "node_exporter: $n CPU series" || fail "node_exporter: no node_cpu_seconds_total"
f=$(curl -fs -m 10 http://127.0.0.1:9100/metrics | grep -c '^node_filesystem_size_bytes{.*mountpoint="/DATA"')
[ "$f" -gt 0 ] && pass "node_exporter sees /DATA" || fail "node_exporter: no file system /DATA"
# The disks smartctl_exporter reports must be the ones smartctl itself finds.
want=$(smartctl --scan | awk '{print $1}' | sed 's#/dev/##' | sort -u | tr '\n' ' ')
got=$(curl -fs -m 60 http://127.0.0.1:9633/metrics | grep '^smartctl_device{' | sed 's/.*device="\([^"]*\)".*/\1/' | sort -u | tr '\n' ' ')
[ -n "$want" ] && [ "$want" = "$got" ] && pass "smartctl_exporter: disks $got" || fail "smartctl_exporter disks: smartctl --scan '$want', exporter '$got'"
for u in node-exporter smartctl-exporter; do
  [ -f "/etc/systemd/system/$u-watchdog.timer" ] && [ "$(systemctl is-enabled "$u-watchdog.timer")" = enabled ] && pass "$u: boot watchdog" || fail "$u: no boot watchdog"
done
h=$(curl -fs -m 5 http://127.0.0.1/node-exporter/health); case "$h" in *'"version":"1.0.0"'*) pass "tile health $h" ;; *) fail "tile health: $h" ;; esac

echo "node-exporter host-test: $ok OK, $bad FAIL"
ht_done
