FROM golang
WORKDIR /go/src/github.com/phuslu/remote_node_exporter
RUN go get -d -v golang.org/x/crypto/ssh
RUN go get -d -v github.com/go-yaml/yaml
COPY remote_node_exporter.go .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o prometheus-remote-node-exporter .

FROM scratch
COPY --from=0 /go/src/github.com/phuslu/remote_node_exporter/prometheus-remote-node-exporter /usr/bin/
CMD ["/usr/bin/prometheus-remote-node-exporter"]  

