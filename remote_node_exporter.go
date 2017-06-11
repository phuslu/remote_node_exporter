// License:
//     MIT License, Copyright phuslu@hotmail.com
// Usage:
//     env PORT=9101 SSH_HOST=192.168.2.1 SSH_USER=admin SSH_PASS=123456 ./remote_node_exporter

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

var (
	Port            = os.Getenv("PORT")
	SshHost         = os.Getenv("SSH_HOST")
	SshPort         = os.Getenv("SSH_PORT")
	SshUser         = os.Getenv("SSH_USER")
	SshPass         = os.Getenv("SSH_PASS")
	ExtraCollectors = os.Getenv("EXTRA_COLLECTORS")
	TextfilePath    = os.Getenv("TEXTFILE_PATH")
)

var PreReadFileList []string = []string{
	"/etc/storage/system_time",
	"/proc/diskstats",
	"/proc/driver/rtc",
	"/proc/loadavg",
	"/proc/meminfo",
	"/proc/mounts",
	"/proc/net/arp",
	"/proc/net/dev",
	"/proc/net/netstat",
	"/proc/net/snmp",
	"/proc/net/sockstat",
	"/proc/stat",
	"/proc/sys/fs/file-nr",
	"/proc/sys/kernel/random/entropy_avail",
	"/proc/sys/net/netfilter/nf_conntrack_count",
	"/proc/sys/net/netfilter/nf_conntrack_max",
	"/proc/vmstat",
}

type Client struct {
	Addr   string
	Config *ssh.ClientConfig

	client     *ssh.Client
	timeOffset time.Duration
	once       sync.Once
}

func (c *Client) execute(cmd string) (string, error) {
	c.once.Do(func() {
		if c.client == nil {
			var err error
			c.client, err = ssh.Dial("tcp", c.Addr, c.Config)
			if err != nil {
				log.Printf("ssh.Dial(%#v) error: %+v", c.Addr, err)
			}
		}
	})

	session, err := c.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var b bytes.Buffer
	session.Stdout = &b

	err = session.Run(cmd)

	return b.String(), err
}

type Metrics struct {
	Client *Client

	name    string
	body    bytes.Buffer
	preread map[string]string
}

func (m *Metrics) PreRead() error {
	m.preread = make(map[string]string)

	cmd := "/bin/fgrep \"\" " + strings.Join(PreReadFileList, " ")
	if TextfilePath != "" {
		cmd += " " + TextfilePath
	}

	output, _ := m.Client.execute(cmd)

	var b bytes.Buffer
	var lastname string = ""

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), ":", 2)
		if len(parts) != 2 {
			continue
		}

		filename := parts[0]
		line := parts[1]

		if filename != lastname {
			if lastname != "" {
				m.preread[lastname] = b.String()
			}
			b.Reset()
			lastname = filename
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	m.preread[lastname] = b.String()

	if err := scanner.Err(); err != nil {
		return err
	}

	for _, filename := range PreReadFileList {
		if _, ok := m.preread[filename]; !ok {
			m.preread[filename] = ""
		}
	}

	return nil
}

func (m *Metrics) ReadFile(filename string) (string, error) {
	s, ok := m.preread[filename]
	if ok {
		return s, nil
	}

	return m.Client.execute("/bin/cat " + filename)
}

func (m *Metrics) PrintType(name string, typ string, help string) {
	m.name = name
	if help != "" {
		m.body.WriteString(fmt.Sprintf("# HELP %s %s.\n", name, help))
	}
	m.body.WriteString(fmt.Sprintf("# TYPE %s %s\n", name, typ))
}

func (m *Metrics) PrintFloat(labels string, value float64) {
	if labels != "" {
		m.body.WriteString(fmt.Sprintf("%s{%s} ", m.name, labels))
	} else {
		m.body.WriteString(fmt.Sprintf("%s ", m.name))
	}

	if value >= 1000000 {
		m.body.WriteString(fmt.Sprintf("%e\n", value))
	} else {
		m.body.WriteString(fmt.Sprintf("%f\n", value))
	}
}

func (m *Metrics) PrintInt(labels string, value int) {
	if labels != "" {
		m.body.WriteString(fmt.Sprintf("%s{%s} ", m.name, labels))
	} else {
		m.body.WriteString(fmt.Sprintf("%s ", m.name))
	}

	if value >= 1000000 {
		m.body.WriteString(fmt.Sprintf("%e\n", value))
	} else {
		m.body.WriteString(fmt.Sprintf("%d\n", value))
	}
}

func (m *Metrics) CollectTime() error {
	_, err := m.ReadFile("/proc/driver/rtc")
	return err
}

func (m *Metrics) CollectLoadavg() error {
	s, err := m.ReadFile("/proc/loadavg")
	if err != nil {
		return err
	}

	parts := strings.Split(strings.TrimSpace(s), " ")
	if len(parts) < 3 {
		return fmt.Errorf("Unknown loadavg %#v", s)
	}

	v, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return err
	}

	m.PrintType("node_load1", "gauge", "1m load average")
	m.PrintFloat("", v)

	return nil
}

func (m *Metrics) CollectAll() (string, error) {
	var err error

	err = m.PreRead()
	if err != nil {
		log.Printf("%T.PreRead() error: %+v", m, err)
	}

	m.CollectLoadavg()

	return m.body.String(), nil
}

func main() {
	if SshPort == "" {
		SshPort = "22"
	}

	client := &Client{
		Addr: net.JoinHostPort(SshHost, SshPort),
		Config: &ssh.ClientConfig{
			User: SshUser,
			Auth: []ssh.AuthMethod{
				ssh.Password(SshPass),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
	}

	http.HandleFunc("/metrics", func(rw http.ResponseWriter, req *http.Request) {
		m := Metrics{
			Client: client,
		}

		s, err := m.CollectAll()
		if err != nil {
			http.Error(rw, err.Error(), http.StatusServiceUnavailable)
			return
		}

		io.WriteString(rw, s)
	})

	log.Fatal(http.ListenAndServe(":"+Port, nil))
}
