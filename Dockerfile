FROM alpine:3.5

RUN apk add --no-cache curl python2 py2-pip py2-paramiko py2-setproctitle && \
  curl https://raw.githubusercontent.com/phuslu/remote_node_exporter/master/remote_node_exporter.py > /remote_node_exporter.py

CMD ["/usr/bin/python2.7", "/remote_node_exporter.py"]

