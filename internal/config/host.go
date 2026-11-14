package config

import (
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/netip"
	"os"
	"regexp"
	"slices"
	"strconv"
	"syscall"

	"github.com/CorneeldH/lighthouse/internal/hostconfig"
)

// Host is a validated host.toml.
type Host struct {
	Name        string
	TailscaleIP *string // nil without tailscale_ip: with no host.controller either, the host is not the controller host
	Controller  *bool   // host.controller: nil when absent; then tailscale_ip decides

	GateSlice             *string
	ScanMemoryMax         *string
	ScanCPUQuota          *string
	GateSlots             *int64
	GateMinMemAvailableGB *float64
	GateMinSwapFreeGB     *float64
}

// hostKeys are the keys `host.<key>` may name.
var hostKeys = []string{
	"host.name",
	"host.tailscale_ip",
	"host.controller",
	"host.gate_slice",
	"host.scan_memory_max",
	"host.scan_cpu_quota",
	"host.gate_slots",
	"host.gate_min_mem_available_gb",
	"host.gate_min_swap_free_gb",
}

// optionalHostKeys are the host keys a host without host.toml simply does not have: not set
// (rc 1), not a configuration error (rc 2).
var optionalHostKeys = hostKeys[2:]

var (
	scanMemoryRE = regexp.MustCompile(`^[1-9][0-9]*[KMGT]?\z`)
	scanCPURE    = regexp.MustCompile(`^[1-9][0-9]*%\z`)
)

func hostPath() (string, error) {
	p := hostconfig.Path()
	if p == "" {
		return "", errf("host.toml not found: no home directory")
	}
	return p, nil
}

// LoadHost reads and validates one host.toml.
func LoadHost(path string) (*Host, error) {
	p := pyPath(path)
	data, _, err := readTOML(path, fmt.Sprintf("host unknown: no host.toml (%s)", p))
	if err != nil {
		return nil, err
	}
	rawHost, ok := data["host"]
	if !ok {
		return nil, errf("%s: missing key host", p)
	}
	host, ok := rawHost.(map[string]any)
	if !ok {
		return nil, errf("%s: key host must be a table", p)
	}
	h := &Host{}

	rawName, ok := host["name"]
	if !ok {
		return nil, errf("%s: missing key host.name", p)
	}
	if name, isStr := rawName.(string); !isStr || name == "" {
		return nil, errf("%s: key host.name must be a non-empty string", p)
	} else {
		h.Name = name
	}

	if rawIP, ok := host["tailscale_ip"]; ok {
		ip, isStr := rawIP.(string)
		if !isStr || !hostAddress(ip) {
			return nil, errf("%s: key host.tailscale_ip must be an IPv4 address: %s", p, pyRepr(rawIP))
		}
		h.TailscaleIP = &ip
	}

	if v, ok := host["controller"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return nil, errf("%s: key host.controller must be true or false: %s", p, pyRepr(v))
		}
		h.Controller = &b
	}

	if v, ok := host["gate_slice"]; ok {
		s, isStr := v.(string)
		if !isStr || s == "" {
			return nil, errf("%s: key host.gate_slice must be a non-empty string", p)
		}
		h.GateSlice = &s
	}
	if h.ScanMemoryMax, err = scanLimit(p, host, "scan_memory_max", scanMemoryRE, "like 2G"); err != nil {
		return nil, err
	}
	if h.ScanCPUQuota, err = scanLimit(p, host, "scan_cpu_quota", scanCPURE, "like 200%"); err != nil {
		return nil, err
	}
	if v, ok := host["gate_slots"]; ok {
		n, isInt := v.(int64)
		if !isInt || n < 1 {
			return nil, errf("%s: key host.gate_slots must be a whole number of at least 1: %s", p, pyRepr(v))
		}
		h.GateSlots = &n
	}
	if h.GateMinMemAvailableGB, err = positiveNumber(p, host, "gate_min_mem_available_gb"); err != nil {
		return nil, err
	}
	if h.GateMinSwapFreeGB, err = positiveNumber(p, host, "gate_min_swap_free_gb"); err != nil {
		return nil, err
	}
	return h, nil
}

// hostAddress reports whether s is a dotted IPv4 address a host can have: not unspecified,
// loopback or multicast.
func hostAddress(s string) bool {
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is4() {
		return false
	}
	return !a.IsUnspecified() && !a.IsLoopback() && !a.IsMulticast()
}

func scanLimit(path string, host map[string]any, name string, pattern *regexp.Regexp, shape string) (*string, error) {
	v, ok := host[name]
	if !ok {
		return nil, nil
	}
	s, isStr := v.(string)
	if !isStr || !pattern.MatchString(s) {
		return nil, errf("%s: key host.%s must be a string %s: %s", path, name, shape, pyRepr(v))
	}
	return &s, nil
}

// positiveNumber is an optional threshold: a finite int or float above 0; a bool is not a
// number. TOML allows inf and nan; neither is a threshold a host can meet.
func positiveNumber(path string, host map[string]any, name string) (*float64, error) {
	v, ok := host[name]
	if !ok {
		return nil, nil
	}
	var f float64
	switch x := v.(type) {
	case int64:
		f = float64(x)
	case float64:
		f = x
	default:
		return nil, errf("%s: key host.%s must be a finite number above 0: %s", path, name, pyRepr(v))
	}
	if math.IsInf(f, 0) || math.IsNaN(f) || f <= 0 {
		return nil, errf("%s: key host.%s must be a finite number above 0: %s", path, name, pyRepr(v))
	}
	return &f, nil
}

// IsLocal reports whether ip is on an interface of this machine. A bind measures that; a
// connect or ping says nothing about locality. With net.ipv4.ip_nonlocal_bind=1 every bind
// succeeds, and every machine recognises itself as the host.
func IsLocal(ip string) (bool, error) {
	conn, err := net.ListenPacket("udp4", net.JoinHostPort(ip, "0"))
	if err != nil {
		if errors.Is(err, syscall.EADDRNOTAVAIL) {
			return false, nil
		}
		return false, errf("local address %s not measurable: %v", ip, err)
	}
	_ = conn.Close()
	return true, nil
}

// isLocal is the seam for tests: a loopback address is forbidden in host.toml, so no real
// local address can be used.
var isLocal = IsLocal

// isController reports whether this host is the controller host: host.controller when set
// (the address is not asked for), otherwise true only when tailscale_ip is on this machine.
func (h *Host) isController() (bool, error) {
	if h.Controller != nil {
		return *h.Controller, nil
	}
	if h.TailscaleIP == nil {
		return false, nil
	}
	return isLocal(*h.TailscaleIP)
}

// Recognize reports whether this machine is the controller host that host.toml names:
// host.controller when set, otherwise true only when host.toml has a tailscale_ip and that
// address is on this machine. A host.toml with neither is another host (false, nil, and the
// address is not asked for). An error when host.toml is missing or broken, or the address is
// not measurable.
func Recognize() (bool, error) {
	path, err := hostPath()
	if err != nil {
		return false, err
	}
	host, err := LoadHost(path)
	if err != nil {
		return false, err
	}
	return host.isController()
}

// HostValue is the text of one `host.<key>`, or an error wrapping ErrNotSet.
func HostValue(key string) (string, error) {
	if !slices.Contains(hostKeys, key) {
		return "", notSet(key)
	}
	path, err := hostPath()
	if err != nil {
		return "", err
	}
	if slices.Contains(optionalHostKeys, key) {
		if _, statErr := os.Stat(path); statErr != nil {
			return "", notSet(key)
		}
	}
	h, err := LoadHost(path)
	if err != nil {
		return "", err
	}
	switch key {
	case "host.name":
		return h.Name, nil
	case "host.tailscale_ip":
		return optString(key, h.TailscaleIP)
	case "host.controller":
		if h.Controller == nil {
			return "", notSet(key)
		}
		return strconv.FormatBool(*h.Controller), nil
	case "host.gate_slice":
		return optString(key, h.GateSlice)
	case "host.scan_memory_max":
		return optString(key, h.ScanMemoryMax)
	case "host.scan_cpu_quota":
		return optString(key, h.ScanCPUQuota)
	case "host.gate_slots":
		if h.GateSlots == nil {
			return "", notSet(key)
		}
		return strconv.FormatInt(*h.GateSlots, 10), nil
	case "host.gate_min_mem_available_gb":
		return optFloat(key, h.GateMinMemAvailableGB)
	default:
		return optFloat(key, h.GateMinSwapFreeGB)
	}
}

func optString(key string, s *string) (string, error) {
	if s == nil {
		return "", notSet(key)
	}
	return *s, nil
}

// optFloat renders a whole float without its fraction ("2.0" becomes "2"), like Python's
// str(int(value)); any other float as str(value).
func optFloat(key string, f *float64) (string, error) {
	if f == nil {
		return "", notSet(key)
	}
	if *f == math.Trunc(*f) {
		return new(big.Float).SetFloat64(*f).Text('f', 0), nil
	}
	return FormatFloat(*f), nil
}

// HostCheck reports whether this machine is the controller host that host.toml names: rc 0
// and "host: <name>" on out; rc 1 when host.controller is false, or when it is absent and
// host.toml has no tailscale_ip or the address is not on this machine; rc 2 on an error.
func HostCheck(out, errOut io.Writer) int {
	path, err := hostPath()
	var host *Host
	is := false
	if err == nil {
		host, err = LoadHost(path)
	}
	if err == nil {
		is, err = host.isController()
	}
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "config: %v\n", err)
		return 2
	}
	if !is {
		switch {
		case host.Controller != nil:
			_, _ = fmt.Fprintf(errOut, "config: host %s (%s) has host.controller = false, so it is not the controller host\n",
				host.Name, pyPath(path))
		case host.TailscaleIP == nil:
			_, _ = fmt.Fprintf(errOut, "config: host %s (%s) has no tailscale_ip, so it is not the always-on host\n",
				host.Name, pyPath(path))
		default:
			_, _ = fmt.Fprintf(errOut, "config: host %s (%s) has tailscale_ip %s, but that address is not on this machine\n",
				host.Name, pyPath(path), *host.TailscaleIP)
		}
		return 1
	}
	_, _ = fmt.Fprintf(out, "host: %s\n", host.Name)
	return 0
}
