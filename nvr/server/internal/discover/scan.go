// Package discover finds cameras on the local network (ONVIF WS-Discovery and an RTSP
// port sweep of the host's own small subnets) and works out their stream URLs (ONVIF
// GetStreamUri, or common RTSP paths).
package discover

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// RTSPPorts are probed on every host of the scanned subnets.
var RTSPPorts = []int{554, 8554}

// Device is one physical device found on the network. A device may answer on several
// IPs (bardi does), so devices are merged by MAC address when it is known.
type Device struct {
	MAC     string   `json:"mac,omitempty"`
	IPs     []string `json:"ips"`
	Ports   []int    `json:"ports"`
	ONVIF   *ONVIF   `json:"onvif,omitempty"`
	Server  string   `json:"server,omitempty"` // RTSP "Server:" header, hints at the brand
	Gateway bool     `json:"gateway"`          // a go2rtc relay (e.g. a location gateway)
}

type ONVIF struct {
	XAddr    string `json:"xaddr"`
	Name     string `json:"name,omitempty"`
	Hardware string `json:"hardware,omitempty"`
}

// Subnet is one network the scan covers.
type Subnet struct {
	Interface string `json:"interface"`
	CIDR      string `json:"cidr"`
}

// LocalSubnets lists private IPv4 networks of the host's physical interfaces that are
// small enough to sweep (/22 or smaller). Docker bridges, VPNs (ZeroTier) and loopback
// are skipped.
func LocalSubnets() []Subnet {
	var out []Subnet
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || skipIface(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || !ipn.IP.IsPrivate() {
				continue
			}
			if ones, _ := ipn.Mask.Size(); ones < 22 {
				continue
			}
			// Two interfaces on the same network (wired + WiFi) are swept once.
			network := ipn.IP.Mask(ipn.Mask).String()
			dup := false
			for _, o := range out {
				if _, on, _ := net.ParseCIDR(o.CIDR); on != nil && on.IP.String() == network {
					dup = true
				}
			}
			if !dup {
				out = append(out, Subnet{Interface: ifc.Name, CIDR: ipn.String()})
			}
		}
	}
	return out
}

func skipIface(name string) bool {
	for _, p := range []string{"docker", "br-", "veth", "zt", "tailscale", "wg", "tun", "virbr", "cni", "flannel"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func hosts(cidr string) []string {
	ip, ipn, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil
	}
	self := ip.To4().String()
	var out []string
	for cur := ipn.IP.Mask(ipn.Mask).To4(); ipn.Contains(cur); cur = next(cur) {
		s := cur.String()
		last := cur[3]
		if s == self || last == 0 || last == 255 {
			continue
		}
		out = append(out, s)
	}
	return out
}

func next(ip net.IP) net.IP {
	n := make(net.IP, 4)
	copy(n, ip)
	for i := 3; i >= 0; i-- {
		n[i]++
		if n[i] != 0 {
			break
		}
	}
	return n
}

// Scan runs WS-Discovery and the RTSP sweep in parallel and merges the results.
func Scan(ctx context.Context, subnets []Subnet) ([]Device, error) {
	type hit struct {
		ip   string
		port int
	}
	var (
		mu    sync.Mutex
		hits  []hit
		onvif map[string]ONVIF
		wg    sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		onvif = probeONVIF(ctx, 3*time.Second)
	}()

	jobs := make(chan hit)
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// WiFi devices (e.g. a gateway STB) can take several hundred ms to answer.
			d := net.Dialer{Timeout: time.Second}
			for j := range jobs {
				c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(j.ip, fmt.Sprint(j.port)))
				if err != nil {
					continue
				}
				c.Close()
				mu.Lock()
				hits = append(hits, j)
				mu.Unlock()
			}
		}()
	}
	for _, sn := range subnets {
		for _, h := range hosts(sn.CIDR) {
			for _, p := range RTSPPorts {
				select {
				case jobs <- hit{h, p}:
				case <-ctx.Done():
				}
			}
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// An ONVIF answer may advertise another IP of the same device; touch it so the
	// kernel learns its MAC and the two IPs merge into one device.
	for _, o := range onvif {
		if host := hostOf(o.XAddr); host != "" {
			if c, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", hostPort(o.XAddr)); err == nil {
				c.Close()
			}
		}
	}
	arp := readARP()
	byKey := map[string]*Device{}
	get := func(ip string) *Device {
		key := ip
		if mac := arp[ip]; mac != "" {
			key = mac
		}
		d := byKey[key]
		if d == nil {
			d = &Device{MAC: arp[ip]}
			byKey[key] = d
		}
		if !contains(d.IPs, ip) {
			d.IPs = append(d.IPs, ip)
		}
		return d
	}
	for _, h := range hits {
		d := get(h.ip)
		if !containsInt(d.Ports, h.port) {
			d.Ports = append(d.Ports, h.port)
		}
	}
	for ip, o := range onvif {
		o := o
		d := get(ip)
		d.ONVIF = &o
		// The XAddr may name another IP of the same device.
		if host := hostOf(o.XAddr); host != "" && host != ip && arp[host] != "" && arp[host] == arp[ip] && !contains(d.IPs, host) {
			d.IPs = append(d.IPs, host)
		}
	}
	out := make([]Device, 0, len(byKey))
	for _, d := range byKey {
		sort.Ints(d.Ports)
		sort.Slice(d.IPs, func(i, j int) bool { return ipLess(d.IPs[i], d.IPs[j]) })
		for _, p := range d.Ports {
			if srv := rtspServer(ctx, d.IPs[0], p); srv != "" {
				d.Server = srv
				d.Gateway = strings.Contains(strings.ToLower(srv), "go2rtc")
				break
			}
		}
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return ipLess(out[i].IPs[0], out[j].IPs[0]) })
	return out, nil
}

// readARP maps IPv4 address → MAC from the kernel's neighbour table.
func readARP() map[string]string {
	out := map[string]string{}
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 4 && fields[3] != "00:00:00:00:00:00" {
			out[fields[0]] = strings.ToLower(fields[3])
		}
	}
	return out
}

var xaddrRe = regexp.MustCompile(`https?://[^\s<]+`)

func hostOf(xaddr string) string {
	m := xaddrRe.FindString(xaddr)
	if m == "" {
		return ""
	}
	h := strings.TrimPrefix(strings.TrimPrefix(m, "http://"), "https://")
	h, _, _ = strings.Cut(h, "/")
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

// hostPort is the host:port of an ONVIF XAddr (port 80 when absent).
func hostPort(xaddr string) string {
	h := strings.TrimPrefix(strings.TrimPrefix(xaddrRe.FindString(xaddr), "http://"), "https://")
	h, _, _ = strings.Cut(h, "/")
	if _, _, err := net.SplitHostPort(h); err != nil {
		h = net.JoinHostPort(h, "80")
	}
	return h
}

func ipLess(a, b string) bool {
	x, y := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if x == nil || y == nil {
		return a < b
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func containsInt(list []int, n int) bool {
	for _, v := range list {
		if v == n {
			return true
		}
	}
	return false
}
