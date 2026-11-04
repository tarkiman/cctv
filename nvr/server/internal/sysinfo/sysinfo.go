// Package sysinfo reads host CPU, memory, load and disk figures from /proc and statfs.
package sysinfo

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type Stats struct {
	CPUPercent float64    `json:"cpu_percent"` // whole host, 0-100
	CPUCores   int        `json:"cpu_cores"`
	Load       [3]float64 `json:"load"`
	MemTotal   uint64     `json:"mem_total"`
	MemAvail   uint64     `json:"mem_available"`
	SwapTotal  uint64     `json:"swap_total"`
	SwapFree   uint64     `json:"swap_free"`
	DiskTotal  uint64     `json:"disk_total"`
	DiskFree   uint64     `json:"disk_free"`
	UptimeSecs float64    `json:"uptime_seconds"`
}

// Sampler computes CPU usage between successive calls.
type Sampler struct {
	mu         sync.Mutex
	prevTotal  uint64
	prevIdle   uint64
	lastCPUPct float64
}

func (s *Sampler) Read(diskPath string) Stats {
	var st Stats
	st.CPUPercent = s.cpu()
	st.CPUCores = cores()
	st.Load = load()
	mem := meminfo()
	st.MemTotal, st.MemAvail = mem["MemTotal"], mem["MemAvailable"]
	st.SwapTotal, st.SwapFree = mem["SwapTotal"], mem["SwapFree"]
	st.DiskTotal, st.DiskFree = Disk(diskPath)
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			st.UptimeSecs, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	return st
}

// Disk returns total and available bytes of the filesystem holding path.
func Disk(path string) (total, free uint64) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, 0
	}
	return fs.Blocks * uint64(fs.Bsize), fs.Bavail * uint64(fs.Bsize)
}

func (s *Sampler) cpu() float64 {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	line, _, _ := strings.Cut(string(b), "\n")
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0
	}
	var total, idle uint64
	for i, v := range f[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		total += n
		if i == 3 || i == 4 { // idle, iowait
			idle += n
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prevTotal > 0 && total > s.prevTotal {
		dt, di := total-s.prevTotal, idle-s.prevIdle
		s.lastCPUPct = 100 * float64(dt-di) / float64(dt)
	}
	s.prevTotal, s.prevIdle = total, idle
	return s.lastCPUPct
}

func cores() int {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "cpu") && len(l) > 3 && l[3] >= '0' && l[3] <= '9' {
			n++
		}
	}
	return n
}

func load() [3]float64 {
	var out [3]float64
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return out
	}
	f := strings.Fields(string(b))
	for i := 0; i < 3 && i < len(f); i++ {
		out[i], _ = strconv.ParseFloat(f[i], 64)
	}
	return out
}

func meminfo() map[string]uint64 {
	out := map[string]uint64{}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(fields[0], 10, 64)
		out[k] = n * 1024
	}
	return out
}
