FROM alpine:3.5

COPY /remote_node_exporter /

CMD ["/remote_node_exporter"]

