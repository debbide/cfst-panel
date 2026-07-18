package cfst

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// PrepareIPFile returns an IP list path for CloudflareST.
// When sampleCount > 0, it randomly samples up to sampleCount addresses from
// CIDR/IP lines in sourcePath and writes them to a temp sample file under dataDir.
// sampleCount <= 0 means use sourcePath as-is.
func PrepareIPFile(sourcePath, dataDir, tag string, sampleCount int) (string, int, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return "", 0, fmt.Errorf("ip file path is empty")
	}
	if sampleCount <= 0 {
		return sourcePath, 0, nil
	}
	entries, err := readIPEntries(sourcePath)
	if err != nil {
		return "", 0, err
	}
	if len(entries) == 0 {
		return "", 0, fmt.Errorf("no IPs/CIDRs found in %s", sourcePath)
	}

	// If the source already looks like an explicit host list (no large CIDRs),
	// and total hosts are small, just use it.
	totalHint := estimateTotal(entries)
	if totalHint > 0 && totalHint <= int64(sampleCount) {
		return sourcePath, int(totalHint), nil
	}

	ips, err := sampleIPs(entries, sampleCount)
	if err != nil {
		return "", 0, err
	}
	if len(ips) == 0 {
		return "", 0, fmt.Errorf("sampled 0 IPs from %s", sourcePath)
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", 0, err
	}
	if tag == "" {
		tag = "v4"
	}
	out := filepath.Join(dataDir, fmt.Sprintf("ip-sample-%s.txt", tag))
	if err := writeLines(out, ips); err != nil {
		return "", 0, err
	}
	return out, len(ips), nil
}

type ipEntry struct {
	ip    net.IP
	ipnet *net.IPNet
	raw   string
}

func readIPEntries(path string) ([]ipEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []ipEntry
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// allow ip:port by stripping port for pure IPv4
		if ip := net.ParseIP(line); ip != nil {
			out = append(out, ipEntry{ip: ip, raw: line})
			continue
		}
		if strings.Contains(line, "/") {
			ip, ipnet, err := net.ParseCIDR(line)
			if err != nil {
				continue
			}
			out = append(out, ipEntry{ip: ip, ipnet: ipnet, raw: line})
			continue
		}
		// maybe "1.1.1.1:443"
		if host, _, err := net.SplitHostPort(line); err == nil {
			if ip := net.ParseIP(host); ip != nil {
				out = append(out, ipEntry{ip: ip, raw: host})
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func estimateTotal(entries []ipEntry) int64 {
	var total int64
	for _, e := range entries {
		if e.ipnet == nil {
			total++
			continue
		}
		ones, bits := e.ipnet.Mask.Size()
		if ones < 0 || bits <= 0 {
			continue
		}
		hostBits := bits - ones
		if hostBits >= 31 {
			// huge; signal unknown large
			return -1
		}
		total += 1 << hostBits
		if total > 5_000_000 {
			return -1
		}
	}
	return total
}

func sampleIPs(entries []ipEntry, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, n)
	out := make([]string, 0, n)

	// Prefer sampling across all entries so large CIDRs don't monopolize.
	// Weighted by log2(size) would be nicer; use repeated random entry picks.
	for attempts := 0; len(out) < n && attempts < n*40; attempts++ {
		e, err := pickEntry(entries)
		if err != nil {
			return nil, err
		}
		ip, err := randomIPFromEntry(e)
		if err != nil {
			continue
		}
		s := ip.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}

func pickEntry(entries []ipEntry) (ipEntry, error) {
	if len(entries) == 0 {
		return ipEntry{}, fmt.Errorf("no entries")
	}
	// weight by min(size, 4096) so tiny ranges still appear but big ones dominate a bit
	type weighted struct {
		e ipEntry
		w int64
	}
	var ws []weighted
	var sum int64
	for _, e := range entries {
		w := int64(1)
		if e.ipnet != nil {
			ones, bits := e.ipnet.Mask.Size()
			if ones >= 0 && bits > ones {
				hostBits := bits - ones
				if hostBits > 12 {
					hostBits = 12 // cap weight at /20 equivalent
				}
				w = 1 << hostBits
			}
		}
		ws = append(ws, weighted{e: e, w: w})
		sum += w
	}
	if sum <= 0 {
		return entries[0], nil
	}
	r, err := rand.Int(rand.Reader, big.NewInt(sum))
	if err != nil {
		return ipEntry{}, err
	}
	x := r.Int64()
	for _, item := range ws {
		if x < item.w {
			return item.e, nil
		}
		x -= item.w
	}
	return ws[len(ws)-1].e, nil
}

func randomIPFromEntry(e ipEntry) (net.IP, error) {
	if e.ipnet == nil {
		if e.ip == nil {
			return nil, fmt.Errorf("empty ip")
		}
		return append(net.IP(nil), e.ip...), nil
	}
	ip := e.ipnet.IP
	if ip4 := ip.To4(); ip4 != nil {
		ones, bits := e.ipnet.Mask.Size()
		hostBits := bits - ones
		if hostBits <= 0 {
			return append(net.IP(nil), ip4...), nil
		}
		// Generate random host part.
		max := new(big.Int).Lsh(big.NewInt(1), uint(hostBits))
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return nil, err
		}
		mask4 := net.IP(e.ipnet.Mask).To4()
                if mask4 == nil {
                        return append(net.IP(nil), ip4...), nil
                }
                base := binary.BigEndian.Uint32(ip4.Mask(net.IPMask(mask4)))
		val := base + uint32(n.Uint64())
		out := make(net.IP, 4)
		binary.BigEndian.PutUint32(out, val)
		return out, nil
	}
	// IPv6
	ip6 := ip.To16()
	if ip6 == nil {
		return nil, fmt.Errorf("invalid ip")
	}
	ones, bits := e.ipnet.Mask.Size()
	hostBits := bits - ones
	out := make(net.IP, 16)
	copy(out, ip6)
	if hostBits <= 0 {
		return out, nil
	}
	// randomize trailing hostBits (cap practical randomness to 64 bits of host)
	randBits := hostBits
	if randBits > 64 {
		randBits = 64
	}
	max := new(big.Int).Lsh(big.NewInt(1), uint(randBits))
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return nil, err
	}
	b := n.Bytes()
	// apply to end of address
	for i := 0; i < len(b); i++ {
		out[15-i] ^= b[len(b)-1-i]
	}
	// ensure still in network by masking network bits
	for i := 0; i < 16; i++ {
		out[i] = (out[i] &^ e.ipnet.Mask[i]) | (ip6[i] & e.ipnet.Mask[i])
	}
	return out, nil
}

func writeLines(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, line := range lines {
		if _, err := w.WriteString(line); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	return w.Flush()
}
