### create directory
```
mkdir prometheus
sudo mv prometheus /opt/
cd /opt/prometheus
```
### download prometheus
```
curl -L https://github.com/prometheus/prometheus/releases/download/v1.7.1/prometheus-1.7.1.linux-amd64.tar.gz | tar xvzp --strip-components=1
curl -L https://github.com/prometheus/blackbox_exporter/releases/download/v0.8.1/blackbox_exporter-0.8.1.linux-amd64.tar.gz | tar xvzp --strip-components=1
curl -L https://github.com/phuslu/remote_node_exporter/releases/download/v0.1.0/remote_node_exporter-0.1.0.linux-amd64.tar.gz | tar xvzp --strip-components=1
```
### configure prometheus
- prometheus.yaml
```yaml
global:
  scrape_interval:     15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'node_exporter'
    scheme: 'http'
    static_configs:
      - targets: ['192.168.2.2:9100']
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
        replacement: $(hostname -i):9115
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
        replacement: $(hostname -i):9115
```
- prometheus.service
```systemd
[Unit]
Description=prometheus
[Service]
ExecStart=/opt/prometheus/prometheus --config.file=/opt/prometheus/prometheus.yml
Restart=always
[Install]
WantedBy=multi-user.target
```
- prometheus-blackbox-exporter.service
```systemd
[Unit]
Description=prometheus blackbox exporter
[Service]
ExecStart=/opt/prometheus/blackbox_exporter --config.file=/opt/prometheus/blackbox.yml
Restart=always
[Install]
WantedBy=multi-user.target
```
- prometheus-remote-node-exporter.service
```systemd
[Unit]
Description=prometheus remote node exporter
[Service]
ExecStart=/opt/prometheus/remote_node_exporter --config.file=/opt/prometheus/remote_node_exporter.yml
Restart=always
[Install]
WantedBy=multi-user.target
```

# start monitoring
```
sudo systemctl enable $(pwd)/*.service
sudo systemctl start prometheus-blackbox-exporter
sudo systemctl start prometheus-remote-node-exporter
sudo systemctl start prometheus
```
