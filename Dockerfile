FROM alpine:3.5

RUN apk add --no-cache openssl python2 py2-pip py2-paramiko py2-setproctitle && \
  wget -O /remote_node_exporter.py https://raw.githubusercontent.com/phuslu/remote_node_exporter/python/remote_node_exporter.py && \
  apk del openssl && \
  chmod +x /remote_node_exporter.py

CMD ["/remote_node_exporter.py"]

