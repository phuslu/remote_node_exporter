# remote_node_exporter
a agentless promtheus/node_exporter via ssh

### Usage
```
/usr/bin/env PORT=9101 SSH_HOST=192.168.1.1 SSH_USER=root SSH_PASS=123456 ./remote_node_exporter.py
```
or
```
systemctl enable $(pwd)/remote_node_exporter.service
systemctl start remote_node_exporter
```

### Docker
```
docker run -it --rm -p 9101:9101 -e "SSH_HOST=phus.lu" -e "SSH_USER=phuslu" -e "SSH_PASS=123456" phuslu/remote_node_exporter
```
or
```
docker run -d --restart always --log-opt max-size=10m --log-opt max-file=2 -p 9101:9101 -e "SSH_HOST=phus.lu" -e "SSH_USER=phuslu" -e "SSH_PASS=123456" phuslu/remote_node_exporter
```
