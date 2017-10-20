# remote_node_exporter
a agentless prometheus/node_exporter

### Usage

    env PORT=9101 SSH_HOST=192.168.2.1 SSH_USER=root SSH_PASS=123456 ./remote_node_exporter
### Howto integrate to prometheus/grafana
1. Download prometheus
```
mkdir prometheus
sudo mv prometheus /opt/
cd /opt/prometheus

curl -L https://github.com/prometheus/prometheus/releases/download/v1.7.1/prometheus-1.7.1.linux-amd64.tar.gz | tar xvzp --strip-components=1
curl -L https://github.com/prometheus/blackbox_exporter/releases/download/v0.8.1/blackbox_exporter-0.8.1.linux-amd64.tar.gz | tar xvzp --strip-components=1
curl -L https://github.com/phuslu/remote_node_exporter/releases/download/v0.8.0/remote_node_exporter-0.8.0.linux-amd64.tar.gz | tar xvzp --strip-components=1

```
2. Configure prometheus.yml
```yaml
global:
  scrape_interval:     15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'node_exporter'
    scheme: 'http'
    static_configs:
      - targets: ['192.168.2.2:10001']
        labels:
          instance: phus.lu
  - job_name: 'blackbox'
    metrics_path: /probe
    params:
      module: [http_2xx]
    static_configs:
      - targets:
        - https://phus.lu
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: 127.0.0.1:9115
  - job_name: 'ping'
    metrics_path: /probe
    params:
      module: [icmp]
    static_configs:
      - targets:
        - phus.lu
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: 127.0.0.1:9115
```
3. Create systemd services
```
cat <<EOF >prometheus.service
[Unit]
Description=prometheus

[Service]
WorkingDirectory=/opt/prometheus
ExecStart=/opt/prometheus/prometheus --config.file=/opt/prometheus/prometheus.yml
ExecReload=/bin/kill -HUP \$MAINPID
Restart=always
LimitNOFILE=100000
LimitNPROC=100000

[Install]
WantedBy=multi-user.target
EOF

cat <<EOF >prometheus-blackbox-exporter.service
[Unit]
Description=prometheus blackbox exporter

[Service]
ExecStart=/opt/prometheus/blackbox_exporter --config.file=/opt/prometheus/blackbox.yml
Restart=always
LimitNOFILE=100000
LimitNPROC=100000

[Install]
WantedBy=multi-user.target
EOF

cat <<EOF >prometheus-remote-node-exporter.service
[Unit]
Description=prometheus remote node exporter

[Service]
ExecStart=/opt/prometheus/remote_node_exporter --config.file=/opt/prometheus/remote_node_exporter.yml
Restart=always
LimitNOFILE=100000
LimitNPROC=100000

[Install]
WantedBy=multi-user.target
EOF

```
4. start monitoring services
```
sudo systemctl enable $(pwd)/*.service
sudo systemctl start prometheus-blackbox-exporter
sudo systemctl start prometheus-remote-node-exporter
sudo systemctl start prometheus
```
5. Install grafana
```
mkdir grafana
sudo mv grafana /opt/
cd /opt/grafana

curl -L https://s3-us-west-2.amazonaws.com/grafana-releases/release/grafana-4.5.1.linux-x64.tar.gz | tar xvzp --strip-components=1

cat <<EOF >grafana.service
[Unit]
Description=grafana

[Service]
WorkingDirectory=/opt/grafana
ExecStart=/opt/grafana/bin/grafana-server
ExecReload=/bin/kill -HUP \$MAINPID
Restart=always
LimitNOFILE=100000
LimitNPROC=100000

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl enable $(pwd)/grafana.service
sudo systemctl start grafana.service

```
6. Impport to grafana dashboard
  - Visit http://<your_ip>:9090 to verify prometheus api
  - Import http://<your_ip>:9090 as datasource to grafana server
  - Import [grafana_dashboard.json](https://raw.githubusercontent.com/phuslu/remote_node_exporter/master/grafana_dashboard.json) as dashboard to grafana server

