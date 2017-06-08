# remote_node_exporter
a agentless prometheus/node_exporter

**Bash**
```
pip install paramiko setproctitle
/usr/bin/env PORT=9101 SSH_HOST=192.168.1.1 SSH_USER=root SSH_PASS=123456 ./remote_node_exporter.py
```

**Systemd**
```
vi remote_node_exporter.service
systemctl enable $(pwd)/remote_node_exporter.service
systemctl start remote_node_exporter
```

**Docker**
```
docker run -it --rm --net=host -e "PORT=9101" -e "SSH_HOST=example.org" -e "SSH_USER=root" -e "SSH_PASS=123456" phuslu/remote_node_exporter
```
