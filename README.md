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

curl -L https://github.com/prometheus/prometheus/releases/download/v2.3.2/prometheus-2.3.2.linux-amd64.tar.gz | tar xvzp --strip-components=1
curl -L https://github.com/phuslu/remote_node_exporter/releases/download/v0.12.0/remote_node_exporter-0.12.0.linux-amd64.tar.gz | tar xvzp --strip-components=1

```
2. Configure prometheus.yml
```yaml
global:
  scrape_interval:     15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'remote_node_exporter'
    scheme: 'http'
    static_configs:
      - targets: ['192.168.2.2:10001']
        labels:
          instance: lab.phus.lu
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

cat <<EOF >remote-node-exporter.service
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
sudo systemctl start remote-node-exporter
sudo systemctl start prometheus
```
5. Install grafana
```
mkdir grafana
sudo mv grafana /opt/
cd /opt/grafana

curl -L https://s3-us-west-2.amazonaws.com/grafana-releases/release/grafana-5.2.2.linux-x64.tar.gz | tar xvzp --strip-components=1

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

