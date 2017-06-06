#!/usr/bin/env python
# coding:utf-8
# License:
#     MIT License, Copyright phuslu@hotmail.com
# Usage:
#     /usr/bin/env PORT=9101 SSH_HOST=192.168.2.1 SSH_USER=admin SSH_PASS=123456 ./remote_node_exporter.py


import BaseHTTPServer
import ctypes
import ctypes.util
import logging
import os
import paramiko
import re
import sys
import time

try:
    import setproctitle
except ImportError:
    print('Warnning: python-setproctitle is not installed')
    setproctitle = None

logging.basicConfig(format='%(asctime)s [%(levelname)s] process@%(process)s thread@%(thread)s %(filename)s@%(lineno)s - %(funcName)s(): %(message)s', level=logging.INFO)

ENV_PORT = os.environ.get('PORT')
ENV_SSH_HOST = os.environ.get('SSH_HOST')
ENV_SSH_PORT = os.environ.get('SSH_PORT')
ENV_SSH_USER = os.environ.get('SSH_USERNAME') or os.environ.get('SSH_USER')
ENV_SSH_PASS = os.environ.get('SSH_PASSWORD') or os.environ.get('SSH_PASS')
ENV_SSH_KEYFILE = os.environ.get('SSH_KEYFILE')
ENV_REMOTE_TEXTFILE_PATH = os.environ.get('REMOTE_TEXTFILE_PATH')


ssh_client = paramiko.SSHClient()

THIS_METRICS = {
    'timezone_offset': None,
    'name': '',
    'text': '',
}

REMOTE_TEXTFILE_PATH = (ENV_REMOTE_TEXTFILE_PATH or '').rstrip('/')

PREREAD_FILELIST = [
    '/etc/storage/system_time',
    '/proc/diskstats',
    '/proc/driver/rtc',
    '/proc/loadavg',
    '/proc/meminfo',
    '/proc/mounts',
    '/proc/net/arp',
    '/proc/net/dev',
    '/proc/net/netstat',
    '/proc/net/snmp',
    '/proc/net/sockstat',
    '/proc/stat',
    '/proc/sys/fs/file-nr',
    '/proc/sys/kernel/random/entropy_avail',
    '/proc/sys/net/netfilter/nf_conntrack_count',
    '/proc/sys/net/netfilter/nf_conntrack_max',
    '/proc/vmstat',
]

PREREAD_FILES = dict.fromkeys(PREREAD_FILELIST, '')


def do_connect():
    global ssh_client
    host = ENV_SSH_HOST
    port = int(ENV_SSH_PORT or '22')
    username = ENV_SSH_USER
    password = ENV_SSH_PASS
    keyfile = ENV_SSH_KEYFILE
    try:
        if ssh_client.get_transport() is not None:
            ssh_client.close()
        ssh_client = paramiko.SSHClient()
        ssh_client.set_missing_host_key_policy(paramiko.MissingHostKeyPolicy())
        ssh_client.connect(host, port=int(port), username=username, password=password, key_filename=keyfile, compress=True, timeout=8)
        ssh_client.get_transport().set_keepalive(60)
        THIS_METRICS['timezone_offset'] = None
    except (paramiko.SSHException, StandardError) as e:
        logging.error('do_connect(%r, %d, %r) error: %s', host, port, username, e)


def do_exec_command(cmd, redirect_stderr=False):
    SSH_COMMAND_TIMEOUT = 8
    MAX_RETRY = 3
    if redirect_stderr:
        cmd += ' 2>&1'
    for _ in range(MAX_RETRY):
        try:
            _, stdout, _ = ssh_client.exec_command(cmd, timeout=SSH_COMMAND_TIMEOUT)
            return stdout.read()
        except (paramiko.SSHException, StandardError) as e:
            logging.error('do_exec_command(%r) error: %s, reconnect', cmd, e)
            time.sleep(0.5)
            do_connect()
    return ''


def do_preread():
    cmd = '/bin/fgrep "" ' + ' '.join(PREREAD_FILELIST)
    if REMOTE_TEXTFILE_PATH:
        cmd += ' %s/*.prom' % REMOTE_TEXTFILE_PATH
    output = do_exec_command(cmd)
    lines = output.splitlines(True)
    PREREAD_FILES.clear()
    for line in lines:
        name, value = line.split(':', 1)
        PREREAD_FILES[name] = PREREAD_FILES.get(name, '') + value


def read_file(filename):
    if filename in PREREAD_FILELIST:
        return PREREAD_FILES.get(filename, '')
    else:
        return do_exec_command('/bin/cat ' + filename)


def print_metric_type(metric, mtype, mhelp=''):
    THIS_METRICS['name'] = metric
    if mhelp:
        THIS_METRICS['text'] += '# HELP %s %s.\n' % (metric, mhelp)
    THIS_METRICS['text'] += '# TYPE %s %s\n' % (metric, mtype)


def print_metric(labels, value):
    assert isinstance(value, (int, float))
    if value >= 1000000:
        value = '%e' % value
    else:
        value = str(value)
    if labels:
        THIS_METRICS['text'] += '%s{%s} %s\n' % (THIS_METRICS['name'], labels, value)
    else:
        THIS_METRICS['text'] += '%s %s\n' % (THIS_METRICS['name'], value)


def collect_time():
    rtc = read_file('/proc/driver/rtc').strip()
    system_time = read_file('/etc/storage/system_time').strip()
    if rtc:
        info = dict(re.split(r'\s*:\s*', line, maxsplit=1) for line in rtc.splitlines())
        ts = time.mktime(time.strptime('%(rtc_date)s %(rtc_time)s' % info, '%Y-%m-%d %H:%M:%S'))
        if THIS_METRICS['timezone_offset'] is None:
            timezone = do_exec_command('date +%z').strip() or '+0000'
            timezone_offset = int('%s%d' %(timezone[0], int(timezone[1:3], 10) * 60 + int(timezone[-2:])))
            THIS_METRICS['timezone_offset'] = timezone_offset
        ts += THIS_METRICS['timezone_offset'] * 60
    elif system_time:
        ts = float(system_time)
    else:
        ts = float(do_exec_command('date +%s').strip() or '0')
    print_metric_type('node_time', 'counter', 'System time in seconds since epoch (1970)')
    print_metric(None, ts)


def collect_loadavg():
    loadavg = read_file('/proc/loadavg').strip().split()
    print_metric_type('node_load1', 'gauge', '1m load average')
    print_metric(None, float(loadavg[0]))


def collect_filefd():
    file_nr = read_file('/proc/sys/fs/file-nr').split()
    print_metric_type('node_filefd_allocated', 'gauge', 'File descriptor statistics: allocated')
    print_metric(None, int(file_nr[0]))
    print_metric_type('node_filefd_maximum', 'gauge', 'File descriptor statistics: maximum')
    print_metric(None, int(file_nr[2]))


def collect_nf_conntrack():
    nf_conntrack_count = read_file('/proc/sys/net/netfilter/nf_conntrack_count').strip()
    nf_conntrack_max = read_file('/proc/sys/net/netfilter/nf_conntrack_max').strip()
    if nf_conntrack_count:
        print_metric_type('node_nf_conntrack_entries', 'gauge', 'Number of currently allocated flow entries for connection tracking')
        print_metric(None, int(nf_conntrack_count))
    if nf_conntrack_max:
        print_metric_type('node_nf_conntrack_entries_limit', 'gauge', 'Maximum size of connection tracking table')
        print_metric(None, int(nf_conntrack_max))


def collect_memory():
    meminfo = read_file('/proc/meminfo')
    meminfo = meminfo.replace(')', '').replace(':', '').replace('(', '_')
    meminfo = meminfo.splitlines()
    for mi in meminfo:
        mia = mi.split()
        print_metric_type('node_memory_%s' % mia[0], 'gauge')
        if len(mia) == 3:
            print_metric(None, int(mia[1]) * 1024)
        else:
            print_metric(None, int(mia[1]))


def collect_netstat():
    netstat = (read_file('/proc/net/netstat') + read_file('/proc/net/snmp')).splitlines()
    for i in range(0, len(netstat), 2):
        prefix, keystr = netstat[i].split(': ', 1)
        prefix, valuestr = netstat[i+1].split(': ', 1)
        keys = keystr.split()
        values = valuestr.split()
        for ii, ss in enumerate(keys):
            print_metric_type('node_netstat_%s_%s' % (prefix, ss), 'gauge')
            print_metric(None, int(values[ii]))


def collect_sockstat():
    sockstat = read_file('/proc/net/sockstat').splitlines()
    for line in sockstat:
        prefix, statline = line.split(':')
        prefix = prefix.strip()
        for stat, count in re.findall(r'(\w+) (\d+)', statline):
            print_metric_type('node_sockstat_%s_%s' % (prefix, stat), 'gauge')
            print_metric(None, int(count))


def collect_vmstat():
    vmstat = read_file('/proc/vmstat').splitlines()
    for vm in vmstat:
        vma = vm.split()
        print_metric_type('node_vmstat_%s' % vma[0], 'gauge')
        print_metric(None, int(vma[1]))


def collect_stat():
    cpu_mode = 'user nice system idle iowait irq softirq steal guest guest_nice'.split()
    stat = read_file('/proc/stat')
    print_metric_type('node_boot_time', 'gauge', 'Node boot time, in unixtime')
    print_metric(None, int(re.search(r'btime ([0-9]+)', stat).group(1)))
    print_metric_type('node_context_switches', 'counter', 'Total number of context switches')
    print_metric(None, int(re.search(r'ctxt ([0-9]+)', stat).group(1)))
    print_metric_type('node_forks', 'counter', 'Total number of forks')
    print_metric(None, int(re.search(r'processes ([0-9]+)', stat).group(1)))
    print_metric_type('node_intr', 'counter', 'Total number of interrupts serviced')
    print_metric(None, int(re.search(r'intr ([0-9]+)', stat).group(1)))
    print_metric_type('node_procs_blocked', 'gauge', 'Number of processes blocked waiting for I/O to complete')
    print_metric(None, int(re.search(r'procs_blocked ([0-9]+)', stat).group(1)))
    print_metric_type('node_procs_running', 'gauge', 'Number of processes in runnable state')
    print_metric(None, int(re.search(r'procs_running ([0-9]+)', stat).group(1)))
    print_metric_type('node_cpu', 'counter', 'Seconds the cpus spent in each mode')
    cpulines = re.findall(r'(?m)cpu\d+ .+', stat)
    for i, line in enumerate(cpulines):
        cpu = line.split()[1:]
        for ii, mode in enumerate(cpu_mode):
            print_metric('cpu="cpu%d",mode="%s"' % (i, mode), int(cpu[ii]) / 100)


def collect_netdev():
    netdevstat = read_file('/proc/net/dev').splitlines()
    faces = netdevstat[1].replace('|', ' ').split()[1:]
    devices = []
    statss = []
    for i in range(2, len(netdevstat)):
        stats = netdevstat[i].replace('|', ' ').replace(':', ' ').split()
        if len(stats) > 0:
            devices.append(stats[0])
            statss.append(stats[1:])
    for i in range(len(statss[0])):
        inter = 'receive' if 2*i < len(statss[0]) else 'transmit'
        print_metric_type('node_network_%s_%s' % (inter, faces[i]), 'gauge')
        for ii, value in enumerate(devices):
            print_metric('device="%s"' % value, int(statss[ii][i]))


def collect_arp():
    arp = read_file('/proc/net/arp').splitlines()
    arp.pop(0)
    offset = -1
    devices = {}
    for line in arp:
        device = line.strip().split()[offset]
        devices[device] = devices.get(device, 0) + 1
    print_metric_type('node_arp_entries', 'gauge', 'ARP entries by device')
    for device, count in devices.items():
        print_metric('device="%s"' % device, count)


def collect_entropy():
    entropy_avail = read_file('/proc/sys/kernel/random/entropy_avail').strip()
    print_metric_type('node_entropy_available_bits', 'gauge', 'Bits of available entropy')
    print_metric(None, int(entropy_avail))


def collect_diskstats():
    suffixs = [
        'reads_completed',
        'reads_merged',
        'sectors_read',
        'read_time_ms',
        'writes_completed',
        'writes_merged',
        'sectors_written',
        'write_time_ms',
        'io_now',
        'io_time_ms',
        'io_time_weighted',
    ]
    diskstats = read_file('/proc/diskstats').splitlines()
    devices = {}
    for line in diskstats:
        values = line.split()[2:14]
        device = values.pop(0)
        devices[device] = values
    for i, suffix in enumerate(suffixs):
        print_metric_type('node_disk_%s' % suffix, 'gauge')
        for device, values in devices.items():
            print_metric('device="%s"' % device, int(values[i]))


def collect_filesystem():
    suffixs = 'avail free size'.split()
    ignore_mountpoints = '/sys /dev /proc'.split()
    ignore_fstypes = 'autofs procfs sysfs'.split()
    mountinfo = read_file('/proc/mounts').strip().splitlines()
    df = do_exec_command('df').splitlines()
    if not df:
        return
    df.pop(0)
    mountpoints = {}
    for line in mountinfo:
        device, mountpoint, fstype = line.split()[:3]
        if mountpoint in ignore_mountpoints or any(mountpoint.startswith(x+'/') for x in ignore_mountpoints):
            continue
        if fstype in ignore_fstypes:
            continue
        mountpoints[mountpoint] = dict(mountpoint=mountpoint, fstype=fstype, device=device)
    for line in df:
        device, size, used, avail, _, mountpoint = line.split()[:6]
        if mountpoint in mountpoints:
            mountpoints[mountpoint].update(dict(size=int(size)*1024, free=(int(size)-int(used))*1024, avail=int(avail)*1024))
    for suffix in suffixs:
        print_metric_type('node_filesystem_%s' % suffix, 'gauge')
        for mountpoint, info in mountpoints.items():
            print_metric('device="%(device)s",fstype="%(fstype)s",mountpoint="%(mountpoint)s"' % info, int(info.get(suffix, 0)))


def collect_textfile():
    if not REMOTE_TEXTFILE_PATH:
        return
    for path, text in PREREAD_FILES.items():
        if path.startswith(REMOTE_TEXTFILE_PATH + '/'):
            THIS_METRICS['text'] += text.strip('\n') + '\n'


def collect_all():
    THIS_METRICS['text'] = ''
    if ssh_client.get_transport() is None:
        do_connect()
    do_preread()
    collect_time()
    collect_loadavg()
    collect_stat()
    collect_vmstat()
    collect_memory()
    collect_filefd()
    collect_nf_conntrack()
    collect_netstat()
    collect_sockstat()
    collect_netdev()
    collect_diskstats()
    collect_textfile()
    collect_arp()
    collect_entropy()
    # collect_filesystem()
    return THIS_METRICS['text']


class MetricsHandler(BaseHTTPServer.BaseHTTPRequestHandler):
    def do_GET(self):
        body = collect_all()
        self.send_response(200, 'OK')
        self.end_headers()
        self.wfile.write(body)
        self.wfile.close()


def main():
    port = int(ENV_PORT or '9101')
    if setproctitle:
        setproctitle.setproctitle('remote_node_exporter: listen :%d [%s@%s]' % (port, ENV_SSH_USER, ENV_SSH_HOST))
    logging.info('Serving HTTP on 0.0.0.0 port %d ...', port)
    BaseHTTPServer.HTTPServer(('', port), MetricsHandler).serve_forever()


if __name__ == '__main__':
    main()

