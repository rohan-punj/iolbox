// Package attackcommon contains the shared command-line, logging, safety, and
// packet-checksum helpers used by the standalone secbench attack prototypes.
package attackcommon

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"sync"
	"time"
)

const AllowedIface = "eth1"

// Options is the common portion of the Python common.base_parser contract.
type Options struct {
	Iface    string
	Selftest bool
	Count    int
	Interval float64
}

// BaseParser creates the common flag set. Command-specific flags are added by
// each command before it parses os.Args[1:]. ContinueOnError preserves the
// normal command-line error path without calling os.Exit from this package.
func BaseParser(description string) (*flag.FlagSet, *Options) {
	fs := flag.NewFlagSet(description, flag.ContinueOnError)
	opts := &Options{}
	fs.StringVar(&opts.Iface, "iface", AllowedIface, "lab NIC to operate on -- MUST be eth1")
	fs.BoolVar(&opts.Selftest, "selftest", false, "build the packet(s) in memory and print OK, do not send anything")
	fs.IntVar(&opts.Count, "count", 0, "number of iterations; 0 = run until stopped")
	fs.Float64Var(&opts.Interval, "interval", 1.0, "seconds to sleep between iterations")
	return fs, opts
}

var statusMu sync.Mutex

// FormatStatus returns one complete, newline-terminated status line. The
// command uses an unbuffered os.Stdout write in Status so the supervisor's live
// pipe observes each line as it is emitted.
func FormatStatus(level, msg string, now time.Time) string {
	return fmt.Sprintf("[%s] %s %s\n", level, now.Format("15:04:05"), msg)
}

func Status(level, msg string) {
	statusMu.Lock()
	defer statusMu.Unlock()
	_, _ = fmt.Fprint(os.Stdout, FormatStatus(level, msg, time.Now()))
}

// EnforceLabIface is the allowlist safety rail from common.enforce_lab_iface.
func EnforceLabIface(iface string) error {
	if iface == AllowedIface {
		return nil
	}
	return fmt.Errorf("refusing to run on '%s' — attacks are locked to the lab NIC (%s) only", iface, AllowedIface)
}

func SelftestOK(name, detail string) {
	msg := fmt.Sprintf("selftest %s: packet builds cleanly", name)
	if detail != "" {
		msg += " — " + detail
	}
	Status("OK", msg)
	_, _ = fmt.Fprintf(os.Stdout, "PASS: selftest %s\n", name)
}

// RunLoop calls sendFn with zero-based iteration numbers, matching
// common.run_loop. It sleeps only between iterations, and emits the INFO line
// only when the finite loop completes normally.
func RunLoop(count int, interval float64, sendFn func(int) error) (int, error) {
	n := 0
	for count <= 0 || n < count {
		if err := sendFn(n); err != nil {
			return n, err
		}
		n++
		if count <= 0 || n < count {
			time.Sleep(intervalDuration(interval))
		}
	}
	Status("INFO", fmt.Sprintf("stopped after %d iteration(s)", n))
	return n, nil
}

func intervalDuration(interval float64) time.Duration {
	if interval <= 0 || math.IsNaN(interval) {
		return 0
	}
	maxSeconds := float64(math.MaxInt64) / float64(time.Second)
	if interval >= maxSeconds {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(interval * float64(time.Second))
}

// ForgedMAC is the common.py forged_mac equivalent: a fresh, locally
// administered unicast source address with a fixed 02 first octet.
func ForgedMAC() net.HardwareAddr {
	mac := make(net.HardwareAddr, 6)
	if _, err := cryptorand.Read(mac[1:]); err != nil {
		// crypto/rand is expected on the target Linux appliance. Keep packet
		// construction usable even if its entropy source is unavailable.
		seed := uint64(time.Now().UnixNano())
		binary.BigEndian.PutUint32(mac[1:5], uint32(seed))
		mac[5] = byte(seed >> 32)
	}
	mac[0] = 0x02
	return mac
}

// InterfaceMAC is the iface_mac equivalent. It falls back to a valid forged
// source so selftests can build packets in environments without eth1.
func InterfaceMAC(ifaceName string) net.HardwareAddr {
	iface, err := net.InterfaceByName(ifaceName)
	if err == nil && len(iface.HardwareAddr) == 6 {
		mac := append(net.HardwareAddr(nil), iface.HardwareAddr...)
		allZero := true
		for _, octet := range mac {
			if octet != 0 {
				allZero = false
				break
			}
		}
		if !allZero {
			return mac
		}
	}
	return ForgedMAC()
}

// CDPChecksum matches the checksum calculation used by the provided Scapy
// CDPv2_HDR source. data must contain the version, TTL, a zeroed checksum
// field, and all CDP TLVs. The odd-length transform is intentionally retained
// from Scapy's _CDPChecksum._check_len rather than silently using a different
// padding convention.
func CDPChecksum(data []byte) uint16 {
	buf := append([]byte(nil), data...)
	if len(buf)%2 != 0 {
		last := buf[len(buf)-1]
		buf = append(buf[:len(buf)-1], 0, last)
		if last > 0x80 {
			buf[len(buf)-2] = 0xff
			buf[len(buf)-1] = last - 1
		}
	}

	var sum uint32
	for i := 0; i < len(buf); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(buf[i : i+2]))
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// ParseMAC validates the six-octet Ethernet MAC addresses accepted by the
// packet builders.
func ParseMAC(value string) (net.HardwareAddr, error) {
	mac, err := net.ParseMAC(value)
	if err != nil || len(mac) != 6 {
		if err == nil {
			err = errors.New("MAC address must contain six octets")
		}
		return nil, fmt.Errorf("invalid MAC %q: %w", value, err)
	}
	return mac, nil
}
