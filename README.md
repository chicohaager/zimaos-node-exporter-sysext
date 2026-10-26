# Prometheus Exporter for ZimaOS

**Version 1.0.0** · ZimaOS 1.8.0 (amd64) · sysext module `node-exporter`

Host and disk metrics of a ZimaOS host for [Prometheus](https://prometheus.io) or Grafana, as a native ZimaOS module — no container:

- [`node_exporter`](https://github.com/prometheus/node_exporter) **1.12.1** on port **9100**: CPU, memory, disks, file systems, network;
- [`smartctl_exporter`](https://github.com/prometheus-community/smartctl_exporter) **0.15.0** on port **9633**: SMART values of every disk, read with ZimaOS' own `/usr/sbin/smartctl` (7.5 on ZimaOS 1.8.0);
- both static binaries from the upstream releases, checked against pinned sha256;
- `node-exporter-zima` — starts both now and at every boot;
- the **Prometheus Exporter** tile on the dashboard (`node-exporter-web`).

## The tile

**Turn on** starts both exporters, also at every boot, and shows what they deliver: the CPU series of `node_exporter` and the disks `smartctl_exporter` reports. **With ZFW installed, turning on opens TCP 9100 and 9633 for your LAN only** (rules `mod-tcp-9100`, `mod-tcp-9633`, source = the LAN range ZFW knows); **Turn off** stops both and removes exactly these rules. The tile prints the scrape config for `prometheus.yml` with the host name your browser used:

```yaml
scrape_configs:
  - job_name: zimaos-node
    static_configs:
      - targets: ['<host>:9100']
  - job_name: zimaos-smartctl
    static_configs:
      - targets: ['<host>:9633']
```

For Grafana, dashboard 1860 (*Node Exporter Full*) shows the host metrics and dashboard 22604
(*SMARTctl Exporter Dashboard*) the disks (tested 2026-10-25 with Prometheus 3.15.1 and Grafana 13.2.4
against a ZimaBoard 2: 3 disks, temperatures, power-on time, data written). *Root FS* stays N/A: the
ZimaOS root is a read-only squashfs, which node_exporter leaves out by default; your data is on `/DATA`.

The exporters answer anyone who reaches the ports, without a login, as upstream does. ZFW limits that to the LAN; without ZFW every device that reaches the host can read the metrics.

## Install (on the ZimaOS host)

With the [Module Manager](https://github.com/chicohaager/modmanager) (once this module is in its catalog): tab **Catalog**, **Prometheus Exporter**, **Install**. By hand:

```sh
cd /tmp
curl -fsSLO https://github.com/chicohaager/zimaos-node-exporter-sysext/releases/latest/download/node-exporter-amd64.raw
curl -fsSLO https://github.com/chicohaager/zimaos-node-exporter-sysext/releases/latest/download/node-exporter-amd64.raw.sha256
sha256sum -c node-exporter-amd64.raw.sha256
mv node-exporter-amd64.raw node-exporter.raw      # zpkg takes the module name from the file name
sudo zpkg install /tmp/node-exporter.raw
```

## Command line

```sh
sudo node-exporter-zima enable     # both exporters on, also at boot
sudo node-exporter-zima disable
node-exporter-zima status
curl -s http://127.0.0.1:9100/metrics | head
```

## Build and test

```sh
sh build.sh amd64              # downloads both releases and checks their pinned sha256; needs Go, node, ../lintux-modkit
sudo sh tools/host-test.sh     # on the host after installing; `cleanup` turns the exporters off again
```

## License

MIT for this repository. The tile icon is the Prometheus logo (`documentation/images/prometheus-logo.svg` of [prometheus/prometheus](https://github.com/prometheus/prometheus), commit a83f715, Apache-2.0) on the tile background. node_exporter and smartctl_exporter are Apache-2.0 (`/usr/share/doc/node-exporter/`).
