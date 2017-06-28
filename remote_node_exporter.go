// License:
//     MIT License, Copyright phuslu@hotmail.com
// Usage:
//     env PORT=9101 SSH_HOST=phus.lu SSH_USER=phuslu SSH_PASS=123456 ./remote_node_exporter
// TODO:
//     add ssh compression support

package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/crypto/ssh"
)

var (
	Port         = os.Getenv("PORT")
	SshHost      = os.Getenv("SSH_HOST")
	SshPort      = os.Getenv("SSH_PORT")
	SshUser      = os.Getenv("SSH_USER")
	SshPass      = os.Getenv("SSH_PASS")
	TextfilePath = os.Getenv("TEXTFILE_PATH")
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

var split func(string, int) []string = regexp.MustCompile(`\s+`).Split

type Client struct {
	Addr   string
	Config *ssh.ClientConfig

	client     *ssh.Client
	timeOffset time.Duration
	mu         sync.Mutex
}

func (c *Client) connect() error {
	c.client = nil

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		return nil
	}

	var err error
	c.client, err = ssh.Dial("tcp", c.Addr, c.Config)

	if err != nil {
		log.Printf("ssh.Dial(\"tcp\", %#v, ...) error: %+v\n", c.Addr, err)
		return err
	} else {
		log.Printf("ssh.Dial(\"tcp\", %#v, ...) ok\n", c.Addr)
	}

	session, err := c.client.NewSession()
	if err != nil {
		log.Printf("%v.NewSession() error: %+v, reconnecting...\n", c.client, err)
		return err
	}

	var b bytes.Buffer
	session.Stdout = &b

	session.Run("date +%z")
	s := strings.TrimSpace(b.String())
	if len(s) == 5 {
		log.Printf("%#v timezone is %#v\n", c.Addr, s)

		h, _ := strconv.Atoi(s[1:3])
		m, _ := strconv.Atoi(s[3:5])
		c.timeOffset = time.Duration((h*60+m)*60) * time.Second

		if s[0] == '-' {
			c.timeOffset = -c.timeOffset
		}
	}

	return err

}

func (c *Client) TimeOffset() time.Duration {
	return c.timeOffset
}

func (c *Client) Execute(cmd string) (string, error) {
	log.Printf("%T.Execute(%#v)\n", c, cmd)

	if c.client == nil {
		c.connect()
	}

	retry := 2
	for i := 0; i < retry; i += 1 {
		session, err := c.client.NewSession()
		if err != nil {
			if i < retry-1 {
				log.Printf("NewSession() error: %+v, reconnecting...\n", err)
				c.connect()
				continue
			}
			return "", err
		}
		defer session.Close()

		var b bytes.Buffer
		session.Stdout = &b

		err = session.Run(cmd)

		return b.String(), err
	}

	return "", nil
}

type ProcFile struct {
	Text     string
	Sep      string
	SkipRows int
}

func (pf ProcFile) sep() string {
	if pf.Sep != "" {
		return pf.Sep
	}
	return " "
}

func (pf ProcFile) Int() (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(pf.Text), 10, 64)
}

func (pf ProcFile) Float() (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(pf.Text), 64)
}

func (pf ProcFile) Strings() []string {
	sep := pf.Sep
	if sep == "" {
		sep = `\s+`
	}
	return regexp.MustCompile(sep).Split(strings.TrimSpace(pf.Text), -1)
}

func (pf ProcFile) KV() ([]string, map[string]string) {
	h := make([]string, 0)
	m := make(map[string]string)

	scanner := bufio.NewScanner(strings.NewReader(pf.Text))

	for i := 0; i < pf.SkipRows; i += 1 {
		if !scanner.Scan() {
			return h, m
		}
		h = append(h, scanner.Text())
	}

	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), pf.sep(), 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		m[key] = value
	}

	return h, m
}

func (pf ProcFile) KVS() ([]string, map[string][]string) {
	h := make([]string, 0)
	m := make(map[string][]string)

	scanner := bufio.NewScanner(strings.NewReader(pf.Text))

	for i := 0; i < pf.SkipRows; i += 1 {
		if !scanner.Scan() {
			return h, m
		}
		h = append(h, scanner.Text())
	}

	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), pf.sep(), 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if v, ok := m[key]; ok {
			m[key] = append(v, value)
		} else {
			m[key] = []string{value}
		}
	}

	return h, m
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

	output, _ := m.Client.Execute(cmd)

	split := func(s string) map[string]string {
		m := make(map[string]string)
		var lastname string
		var b bytes.Buffer

		scanner := bufio.NewScanner(strings.NewReader(s))
		for scanner.Scan() {
			parts := strings.SplitN(scanner.Text(), ":", 2)
			if len(parts) != 2 {
				continue
			}

			filename := strings.TrimSpace(parts[0])
			line := parts[1]

			if filename != lastname {
				if lastname != "" {
					m[lastname] = b.String()
				}
				b.Reset()
				lastname = filename
			}

			b.WriteString(line)
			b.WriteString("\n")
		}
		m[lastname] = b.String()
		return m
	}

	m.preread = split(output)

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

	return m.Client.Execute("/bin/cat " + filename)
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

func (m *Metrics) PrintInt(labels string, value int64) {
	if labels != "" {
		m.body.WriteString(fmt.Sprintf("%s{%s} ", m.name, labels))
	} else {
		m.body.WriteString(fmt.Sprintf("%s ", m.name))
	}

	if value >= 1000000 {
		m.body.WriteString(fmt.Sprintf("%e\n", float64(value)))
	} else {
		m.body.WriteString(fmt.Sprintf("%d\n", value))
	}
}

func (m *Metrics) CollectTime() error {
	var nsec int64
	var t time.Time

	s, err := m.ReadFile("/proc/driver/rtc")

	if s != "" {
		_, kv := (ProcFile{Text: s, Sep: ":"}).KV()
		date := kv["rtc_date"] + " " + kv["rtc_time"]
		t, err = time.Parse("2006-01-02 15:04:05", date)
		nsec = t.Unix()
		nsec += int64(m.Client.TimeOffset() / time.Second)
	}

	if nsec == 0 {
		s, err = m.ReadFile("/etc/storage/system_time")
		nsec, err = (ProcFile{Text: s}).Int()
	}

	if nsec == 0 {
		s, err = m.Client.Execute("date +%s")
		nsec, err = (ProcFile{Text: s}).Int()
	}

	if nsec != 0 {
		m.PrintType("node_time", "counter", "System time in seconds since epoch (1970)")
		m.PrintInt("", nsec)
	}

	return err
}

func (m *Metrics) CollectLoadavg() error {
	s, err := m.ReadFile("/proc/loadavg")
	if err != nil {
		return err
	}

	parts := (ProcFile{Text: s}).Strings()
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

func (m *Metrics) CollectFilefd() error {
	s, err := m.ReadFile("/proc/sys/fs/file-nr")
	parts := (ProcFile{Text: s}).Strings()

	if len(parts) < 3 {
		return fmt.Errorf("Unknown file-nr %#v", s)
	}

	if allocated, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
		m.PrintType("node_filefd_allocated", "gauge", "File descriptor statistics: allocated")
		m.PrintInt("", allocated)
	}

	if maximum, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
		m.PrintType("node_filefd_maximum", "gauge", "File descriptor statistics: maximum")
		m.PrintInt("", maximum)
	}

	return err
}

func (m *Metrics) CollectNfConntrack() error {
	var s string
	var n int64
	var err error

	s, err = m.ReadFile("/proc/sys/net/netfilter/nf_conntrack_count")
	if s != "" {
		if n, err = (ProcFile{Text: s}).Int(); err == nil {
			m.PrintType("node_nf_conntrack_entries", "gauge", "Number of currently allocated flow entries for connection tracking")
			m.PrintInt("", n)
		}
	}

	s, err = m.ReadFile("/proc/sys/net/netfilter/nf_conntrack_max")
	if s != "" {
		if n, err = (ProcFile{Text: s}).Int(); err == nil {
			m.PrintType("node_nf_conntrack_entries_limit", "gauge", "Maximum size of connection tracking table")
			m.PrintInt("", n)
		}
	}

	return err
}

func (m *Metrics) CollectMemory() error {
	s, err := m.ReadFile("/proc/meminfo")
	s = strings.Replace(strings.Replace(s, "(", "_", -1), ")", "", -1)

	_, kv := (ProcFile{Text: s, Sep: ":"}).KV()

	for key, value := range kv {
		parts := split(value, -1)
		if len(parts) == 0 {
			continue
		}

		size, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}

		if len(parts) == 2 {
			size *= 1024
		}

		m.PrintType(fmt.Sprintf("node_memory_%s", key), "gauge", "")
		m.PrintInt("", size)
	}

	return err
}

func (m *Metrics) CollectNetstat() error {
	var s1, s2 string
	var err error

	s1, err = m.ReadFile("/proc/net/netstat")
	s2, err = m.ReadFile("/proc/net/snmp")
	_, kv := (ProcFile{Text: (s1 + s2), Sep: ":"}).KVS()

	for key, values := range kv {
		if len(values) != 2 {
			continue
		}

		v1 := split(values[0], -1)
		v2 := split(values[1], -1)

		for i, v := range v1 {
			n, err := strconv.ParseInt(v2[i], 10, 64)
			if err != nil {
				continue
			}
			m.PrintType(fmt.Sprintf("node_netstat_%s_%s", key, v), "gauge", "")
			m.PrintInt("", n)
		}
	}

	return err
}

func (m *Metrics) CollectSockstat() error {
	s, err := m.ReadFile("/proc/net/sockstat")
	_, kv := (ProcFile{Text: s, Sep: ":"}).KV()

	for key, value := range kv {
		vs := split(value, -1)
		for i := 0; i < len(vs)-1; i += 2 {
			k := vs[i]
			n, err := strconv.ParseInt(vs[i+1], 10, 64)
			if err != nil {
				continue
			}
			m.PrintType(fmt.Sprintf("node_sockstat_%s_%s", key, k), "gauge", "")
			m.PrintInt("", n)
		}
	}

	return err
}

func (m *Metrics) CollectVmstat() error {
	s, err := m.ReadFile("/proc/vmstat")
	_, kv := (ProcFile{Text: s}).KV()

	for key, value := range kv {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			continue
		}
		m.PrintType(fmt.Sprintf("node_vmstat_%s", key), "gauge", "")
		m.PrintInt("", n)
	}

	return err
}

var CPUModes []string = []string{
	"user",
	"nice",
	"system",
	"idle",
	"iowait",
	"irq",
	"softirq",
	"steal",
	"guest",
	"guest_nice",
}

func (m *Metrics) CollectStat() error {
	s, err := m.ReadFile("/proc/stat")
	_, kv := (ProcFile{Text: s}).KV()

	if v, ok := kv["btime"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			m.PrintType("node_boot_time", "gauge", "Node boot time, in unixtime")
			m.PrintInt("", n)
		}
	}

	if v, ok := kv["ctxt"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			m.PrintType("node_context_switches", "counter", "Total number of context switches")
			m.PrintInt("", n)
		}
	}

	if v, ok := kv["processes"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			m.PrintType("node_forks", "counter", "Total number of forks")
			m.PrintInt("", n)
		}
	}

	if v, ok := kv["intr"]; ok {
		vs := split(v, -1)
		if n, err := strconv.ParseInt(vs[0], 10, 64); err == nil {
			m.PrintType("node_intr", "counter", "Total number of interrupts serviced")
			m.PrintInt("", n)
		}
	}

	if v, ok := kv["procs_blocked"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			m.PrintType("node_procs_blocked", "gauge", "Number of processes blocked waiting for I/O to complete")
			m.PrintInt("", n)
		}
	}

	if v, ok := kv["procs_running"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			m.PrintType("node_procs_running", "gauge", "Number of processes in runnable state")
			m.PrintInt("", n)
		}
	}

	m.PrintType("node_cpu", "counter", "Seconds the cpus spent in each mode")
	for key, value := range kv {
		if key == "cpu" || !strings.HasPrefix(key, "cpu") {
			continue
		}

		vs := split(value, -1)
		for i, mode := range CPUModes {
			if i == len(vs) {
				break
			}
			if n, err := strconv.ParseInt(vs[i], 10, 64); err == nil {
				m.PrintInt(fmt.Sprintf("cpu=\"%s\",mode=\"%s\"", key, mode), n/100)
			}
		}
	}

	return err
}

func (m *Metrics) CollectNetdev() error {
	s, err := m.ReadFile("/proc/net/dev")
	hs, kv := (ProcFile{Text: s, Sep: ":", SkipRows: 2}).KV()

	if len(hs) != 2 {
		return nil
	}

	faces := strings.Split(hs[1], "|")
	rfaces := split(strings.TrimSpace(faces[1]), -1)
	tfaces := split(strings.TrimSpace(faces[2]), -1)

	metrics := make(map[string][]string)
	for key, value := range kv {
		metrics[key] = split(value, -1)
	}

	for i := 0; i < len(rfaces)+len(tfaces); i += 1 {
		var inter, face string
		if i < len(rfaces) {
			inter = "receive"
			face = rfaces[i]
		} else {
			inter = "transmit"
			face = tfaces[i-len(rfaces)]
		}

		m.PrintType(fmt.Sprintf("node_network_%s_%s", inter, face), "gauge", "")

		for key, values := range metrics {
			n, err := strconv.ParseInt(values[i], 10, 64)
			if err != nil {
				continue
			}
			m.PrintInt(fmt.Sprintf("device=\"%s\"", key), n)
		}
	}

	return err
}

func (m *Metrics) CollectArp() error {
	s, err := m.ReadFile("/proc/net/arp")
	if err != nil {
		return err
	}

	_, kv := (ProcFile{Text: s, SkipRows: 1}).KV()

	devices := make(map[string]int64)
	for _, value := range kv {
		vs := split(value, -1)
		dev := vs[len(vs)-1]
		if n, ok := devices[dev]; !ok {
			devices[dev] = 1
		} else {
			devices[dev] = n + 1
		}
	}

	m.PrintType("node_arp_entries", "gauge", "ARP entries by device")
	for key, value := range devices {
		m.PrintInt(fmt.Sprintf("device=\"%s\"", key), value)
	}

	return err
}

func (m *Metrics) CollectEntropy() error {
	s, err := m.ReadFile("/proc/sys/kernel/random/entropy_avail")
	if err != nil {
		return err
	}

	n, err := (ProcFile{Text: s}).Int()
	if err != nil {
		return err
	}

	m.PrintType("node_entropy_available_bits", "gauge", "Bits of available entropy")
	m.PrintInt("", n)

	return err
}

var DiskStatsMode []string = []string{
	"reads_completed",
	"reads_merged",
	"sectors_read",
	"read_time_ms",
	"writes_completed",
	"writes_merged",
	"sectors_written",
	"write_time_ms",
	"io_now",
	"io_time_ms",
	"io_time_weighted",
}

func (m *Metrics) CollectDiskstats() error {
	s, err := m.ReadFile("/proc/diskstats")
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(strings.NewReader(s))

	devices := make(map[string][]string)
	for scanner.Scan() {
		parts := split(strings.TrimSpace(scanner.Text()), -1)
		if len(parts) < 2 {
			continue
		}

		dev := parts[2]
		values := parts[3:14]

		devices[dev] = values
	}

	for i, mode := range DiskStatsMode {
		m.PrintType(fmt.Sprintf("node_disk_%s", mode), "gauge", "")
		for dev, values := range devices {
			n, err := strconv.ParseInt(values[i], 10, 64)
			if err != nil {
				continue
			}
			m.PrintInt(fmt.Sprintf("device=\"%s\"", dev), n)
		}
	}

	return err
}

// https://github.com/prometheus/node_exporter/blob/master/collector/filesystem_linux.go
const (
	defIgnoredMountPoints = "^/(sys|proc|dev)($|/)"
	defIgnoredFSTypes     = "^(sys|proc|auto)fs$"
)

type FilesystemInfo struct {
	MountPoint string
	FSType     string
	Device     string
	Size       int64
	Used       int64
	Avail      int64
}

func (m *Metrics) CollectFilesystem() error {
	s, err := m.ReadFile("/proc/mounts")
	if err != nil {
		return err
	}

	mountpoints := make(map[string]FilesystemInfo)

	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		parts := split(strings.TrimSpace(scanner.Text()), -1)
		device, mountpoint, fstype := parts[0], parts[1], parts[1]

		if regexp.MustCompile(defIgnoredMountPoints).MatchString(mountpoint) {
			continue
		}
		if regexp.MustCompile(defIgnoredFSTypes).MatchString(fstype) {
			continue
		}

		mountpoints[mountpoint] = FilesystemInfo{
			MountPoint: mountpoint,
			FSType:     fstype,
			Device:     device,
		}
	}

	s, err = m.Client.Execute("df")
	if err != nil {
		return err
	}

	scanner = bufio.NewScanner(strings.NewReader(s))
	scanner.Scan()
	for scanner.Scan() {
		parts := split(strings.TrimSpace(scanner.Text()), -1)
		size, used, avail, mountpoint := parts[1], parts[2], parts[3], parts[5]

		fi, ok := mountpoints[mountpoint]
		if !ok {
			continue
		}

		if n, err := strconv.ParseInt(size, 10, 64); err == nil {
			fi.Size = n * 1024
		}
		if n, err := strconv.ParseInt(used, 10, 64); err == nil {
			fi.Used = n * 1024
		}
		if n, err := strconv.ParseInt(avail, 10, 64); err == nil {
			fi.Avail = n * 1024
		}

		mountpoints[mountpoint] = fi
	}

	m.PrintType("node_filesystem_size", "gauge", "Filesystem size in bytes")
	for _, fi := range mountpoints {
		m.PrintInt(fmt.Sprintf("device=\"%s\",fstype=\"%s\",mountpoint=\"%s\"", fi.Device, fi.FSType, fi.MountPoint), fi.Size)
	}

	m.PrintType("node_filesystem_free", "gauge", "Filesystem free space in bytes")
	for _, fi := range mountpoints {
		m.PrintInt(fmt.Sprintf("device=\"%s\",fstype=\"%s\",mountpoint=\"%s\"", fi.Device, fi.FSType, fi.MountPoint), fi.Size-fi.Used)
	}

	m.PrintType("node_filesystem_avail", "gauge", "Filesystem space available to non-root users in bytes")
	for _, fi := range mountpoints {
		m.PrintInt(fmt.Sprintf("device=\"%s\",fstype=\"%s\",mountpoint=\"%s\"", fi.Device, fi.FSType, fi.MountPoint), fi.Avail)
	}

	return nil
}

func (m *Metrics) CollectTextfile() error {
	return nil
}

func (m *Metrics) CollectAll() (string, error) {
	var err error

	err = m.PreRead()
	if err != nil {
		log.Printf("%T.PreRead() error: %+v\n", m, err)
	}

	m.CollectTime()
	m.CollectLoadavg()
	m.CollectFilefd()
	m.CollectNfConntrack()
	m.CollectMemory()
	m.CollectNetstat()
	m.CollectSockstat()
	m.CollectVmstat()
	m.CollectStat()
	m.CollectNetdev()
	m.CollectArp()
	m.CollectEntropy()
	m.CollectDiskstats()
	m.CollectFilesystem()
	m.CollectTextfile()

	return m.body.String(), nil
}

func SetProcessName(name string) error {
	argv0str := (*reflect.StringHeader)(unsafe.Pointer(&os.Args[0]))
	argv0 := (*[1 << 30]byte)(unsafe.Pointer(argv0str.Data))[:len(name)+1]

	n := copy(argv0, name+string(0))
	if n < len(argv0) {
		argv0[n] = 0
	}

	return nil
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
			Timeout:         8 * time.Second,
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

		if strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") {
			rw.Header().Set("Content-Encoding", "gzip")
			rw.WriteHeader(http.StatusOK)
			w := gzip.NewWriter(rw)
			io.WriteString(w, s)
			w.Close()
		} else {
			io.WriteString(rw, s)
		}
	})

	http.HandleFunc("/", func(rw http.ResponseWriter, req *http.Request) {
		io.WriteString(rw, `<html>
			<head><title>Node Exporter</title></head>
			<body>
			<h1>Node Exporter</h1>
			<p><a href="/metrics">Metrics</a></p>
			</body>
			</html>`)
	})

	if runtime.GOOS == "linux" {
		SetProcessName(fmt.Sprintf("remote_node_exporter: [%s:%s]", SshUser, SshHost))
	}

	log.Fatal(http.ListenAndServe(":"+Port, nil))
}
