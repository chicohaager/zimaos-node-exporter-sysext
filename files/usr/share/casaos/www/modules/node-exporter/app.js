'use strict';
// Prometheus Exporter tile. Helpers (api, esc, $, setStatus, describe, initTheme, zfw) come from common.js.

const API = '/node-exporter';
let zfwPresent = false;
let ports = [];

function exporterRow(e) {
  const on = e.active === 'active';
  const pill = on ? '<span class="pill pill-ok">running</span>' : `<span class="pill pill-off">${esc(e.active || 'stopped')}</span>`;
  const boot = e.boot ? '<span class="pill pill-accent">starts at boot</span>' : '';
  let what = '<span class="muted">not running</span>';
  if (on && e.error) what = `<span class="app-err">${esc(e.error)}</span>`;
  else if (on && e.name === 'node_exporter') what = `${esc(e.series)} CPU series in <span class="mono">/metrics</span>`;
  else if (on) what = `${esc(e.devices)} disk(s) reported`;
  return `<dt class="mono">${esc(e.name)}</dt><dd>${pill}${boot} port <span class="mono">${esc(e.port)}</span> · ${what}</dd>`;
}

function scrapeConfig(host, list) {
  const jobs = { node_exporter: 'zimaos-node', smartctl_exporter: 'zimaos-smartctl' };
  return 'scrape_configs:\n' + list.map((e) =>
    `  - job_name: ${jobs[e.name]}\n    static_configs:\n      - targets: ['${host}:${e.port}']`).join('\n') + '\n';
}

function render(d) {
  const list = d.exporters;
  ports = list.map((e) => e.port);
  const on = list.some((e) => e.active === 'active' || e.boot);
  const cfg = scrapeConfig(d.host, list);
  $('#main').innerHTML = `
  <section class="card">
    <div class="head">
      <h3>Exporters</h3>
      <div class="app-actions">
        ${on ? '<button class="btn-secondary" data-act="disable">Turn off</button><button class="btn-secondary" data-act="restart">Restart</button>'
          : '<button class="btn-primary" data-act="enable">Turn on</button>'}
      </div>
    </div>
    <dl class="kv">${list.map(exporterRow).join('')}</dl>
    <div class="zfw-line"></div>
  </section>
  <section class="card">
    <h2 class="section-title">Prometheus scrape config</h2>
    <p class="muted" style="font-size:12px">For <span class="mono">prometheus.yml</span>, with this host as your browser reaches it. Grafana: dashboard 1860 (Node Exporter Full) for the host metrics.</p>
    <pre class="cfg mono" id="cfg"></pre>
    <div class="app-actions"><button class="btn-secondary" data-act="copy">Copy</button></div>
  </section>`;
  $('#cfg').textContent = cfg;
}

async function zfwLine() {
  const el = document.querySelector('.zfw-line');
  if (!zfwPresent || !el) return;
  try {
    const open = [];
    for (const p of ports) if (await zfw.allows(p, 'tcp', 'lan')) open.push(p);
    el.innerHTML = open.length === ports.length
      ? `<span class="pill pill-ok">ZFW</span> TCP ${ports.join(' and ')} open for your LAN in the ZFW firewall.`
      : `<span class="pill pill-warn">ZFW</span> The ZFW firewall drops TCP ${ports.filter((p) => !open.includes(p)).join(' and ')}: Prometheus cannot scrape this host. <button class="btn-small btn-primary" data-act="zfw-open">Open for the LAN</button>`;
  } catch (e) {
    el.innerHTML = `<span class="pill pill-off">ZFW</span> <span class="muted">Could not read the ZFW rules: ${esc(describe(e))}</span>`;
  }
}

async function load() {
  render(await api(`${API}/status`));
  await zfwLine();
}

async function refresh() {
  try {
    await load();
    setStatus('');
  } catch (e) {
    $('#main').innerHTML = `<section class="card"><p class="app-err">${esc(describe(e))}</p></section>`;
    setStatus(describe(e), 'err');
  }
}

// The metrics ports open for the LAN only (rule ids mod-tcp-<port>, source = ZFW's LAN range);
// a ZFW failure is reported, the exporter action itself stands.
async function zfwOpen() {
  if (!zfwPresent) return null;
  try {
    for (const p of ports) await zfw.open(p, 'tcp', `Prometheus exporter ${p}/tcp`, 'Added by the Prometheus Exporter tile when the exporters were turned on', 'lan');
    return { note: `TCP ${ports.join(' and ')} opened for the LAN in ZFW` };
  } catch (e) {
    return { warning: `ZFW was not changed (${describe(e)}) — Prometheus cannot scrape this host until the ports are open` };
  }
}

async function zfwClose() {
  if (!zfwPresent) return null;
  try {
    let n = 0;
    for (const p of ports) if (await zfw.close(p, 'tcp')) n++;
    return n ? { note: `${n} ZFW rule(s) removed` } : null;
  } catch (e) {
    return { warning: `the ZFW rules could not be removed (${describe(e)})` };
  }
}

async function act(label, fn) {
  setStatus(`${label} …`);
  try {
    const r = await fn();
    setStatus(r && r.warning ? `${label}: done, but ${r.warning}` : `${label}: done${r && r.note ? ` — ${r.note}` : ''}.`, r && r.warning ? 'err' : 'ok');
    await load();
  } catch (e) {
    setStatus(`${label}: ${describe(e)}`, 'err');
  }
}

document.addEventListener('click', async (e) => {
  const btn = e.target.closest('button[data-act]');
  if (!btn) return;
  switch (btn.dataset.act) {
    case 'enable': await act('Turning the exporters on', async () => { await api(`${API}/action`, { method: 'POST', body: { action: 'enable' } }); return zfwOpen(); }); break;
    case 'disable': await act('Turning the exporters off', async () => { await api(`${API}/action`, { method: 'POST', body: { action: 'disable' } }); return zfwClose(); }); break;
    case 'restart': await act('Restarting the exporters', () => api(`${API}/action`, { method: 'POST', body: { action: 'restart' } })); break;
    case 'zfw-open': await act('Opening the ports for the LAN in ZFW', zfwOpen); break;
    case 'copy': await navigator.clipboard.writeText($('#cfg').textContent).catch(() => {}); btn.textContent = 'Copied'; break;
    default:
  }
});

initTheme('node-exporter.theme');
$('#refresh-btn').addEventListener('click', refresh);
(async () => { zfwPresent = await zfw.present(); refresh(); })();
