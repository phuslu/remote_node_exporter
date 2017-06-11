FROM alpine:3.5

RUN apk add --no-cache openssl libssh python2 py2-pip py2-setproctitle && \
  pip install pyssh-ctypes && \
  wget -O /remote_node_exporter.py https://raw.githubusercontent.com/phuslu/remote_node_exporter/master/remote_node_exporter.py && \
  apk del openssl && \
  chmod +x /remote_node_exporter.py

CMD ["/remote_node_exporter.py"]

