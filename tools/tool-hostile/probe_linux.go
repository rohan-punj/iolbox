//go:build linux

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	capNetAdmin       = 12
	prCapBsetRaise    = 24
	prCapAmbient      = 47
	prCapAmbientRaise = 2
	sioCGIFFLAGS      = 0x8913
	sioCSIFFLAGS      = 0x8914
	iffPromisc        = 0x0100
	capsetVersion3    = 0x20080522
)

type capHeader struct {
	Version uint32
	PID     int32
}

type capData struct {
	Effective   uint32
	Permitted   uint32
	Inheritable uint32
}

func runHostileProbe() error {
	if err := printCapabilityState(); err != nil {
		return fmt.Errorf("read capability state: %w", err)
	}

	child := exec.Command("/proc/self/exe", "--cap-regain-child")
	output, err := child.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "CAP_REGAIN_DENIED") {
		fmt.Printf("CAP_REGAIN_SUCCEEDED output=%q err=%v\n", strings.TrimSpace(string(output)), err)
		return errors.New("a dropped capability was reacquired after exec")
	}
	fmt.Println("CAP_REGAIN_DENIED")

	if err := checkHostNetworkView(); err != nil {
		return err
	}
	if err := checkNetAdminDenied(); err != nil {
		return err
	}
	if err := checkCgroupEscapeDenied(); err != nil {
		return err
	}
	if err := checkAcceptedHostRead(); err != nil {
		return err
	}
	return nil
}

func printCapabilityState() error {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "CapEff:") || strings.HasPrefix(line, "CapPrm:") ||
			strings.HasPrefix(line, "CapInh:") || strings.HasPrefix(line, "CapAmb:") ||
			strings.HasPrefix(line, "CapBnd:") || strings.HasPrefix(line, "NoNewPrivs:") {
			fmt.Println(line)
		}
	}
	return nil
}

func checkHostNetworkView() error {
	hostIface := os.Getenv("IOLBOX_HOST_IFACE")
	if hostIface == "" {
		return errors.New("IOLBOX_HOST_IFACE is required")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("enumerate interfaces: %w", err)
	}
	for _, iface := range interfaces {
		if iface.Name == hostIface {
			fmt.Printf("HOST_IFACE_VISIBLE %s\n", hostIface)
			return errors.New("root-namespace interface visible from tool netns")
		}
	}
	fmt.Printf("HOST_IFACE_DENIED %s\n", hostIface)

	routeData, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return fmt.Errorf("read route table: %w", err)
	}
	if strings.Contains(string(routeData), hostIface) {
		fmt.Printf("HOST_ROUTE_VISIBLE %s\n", hostIface)
		return errors.New("root-namespace route visible from tool netns")
	}
	fmt.Printf("HOST_ROUTE_DENIED %s\n", hostIface)
	return nil
}

func checkNetAdminDenied() error {
	iface := os.Getenv("IOLBOX_TOOL_IFACE")
	if iface == "" {
		iface = "eth1"
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("create ioctl socket: %w", err)
	}
	defer syscall.Close(fd)

	var ifr [40]byte
	copy(ifr[:16], iface)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), sioCGIFFLAGS, uintptr(unsafe.Pointer(&ifr[0]))); errno != 0 {
		return fmt.Errorf("read %s flags: %w", iface, errno)
	}
	flags := *(*uint16)(unsafe.Pointer(&ifr[16]))
	*(*uint16)(unsafe.Pointer(&ifr[16])) = flags | iffPromisc
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), sioCSIFFLAGS, uintptr(unsafe.Pointer(&ifr[0])))
	if errno == 0 {
		fmt.Println("NET_ADMIN_SUCCEEDED")
		return errors.New("unprivileged tool changed interface flags")
	}
	fmt.Printf("NET_ADMIN_DENIED %v\n", errno)
	return nil
}

func checkCgroupEscapeDenied() error {
	cgroupPath := os.Getenv("IOLBOX_CGROUP_PATH")
	if cgroupPath == "" {
		return errors.New("IOLBOX_CGROUP_PATH is required")
	}
	parent := filepath.Dir(filepath.Clean(cgroupPath))
	err := os.WriteFile(filepath.Join(parent, "cgroup.procs"), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644)
	if err == nil {
		fmt.Println("CGROUP_ESCAPE_SUCCEEDED")
		return errors.New("tool moved itself out of its cage")
	}
	fmt.Printf("CGROUP_ESCAPE_DENIED %v\n", err)
	return nil
}

func checkAcceptedHostRead() error {
	path := os.Getenv("IOLBOX_HOST_FILE")
	if path == "" {
		return errors.New("IOLBOX_HOST_FILE is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("accepted host-file read failed: %w", err)
	}
	fmt.Printf("HOST_FILE_ACCEPTED_RISK %s\n", strings.TrimSpace(string(data)))
	return nil
}

func attemptCapRegain() error {
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prCapBsetRaise, capNetAdmin, 0, 0, 0, 0); errno == 0 {
		return nil
	}
	data := capData{Effective: 1 << capNetAdmin, Permitted: 1 << capNetAdmin, Inheritable: 1 << capNetAdmin}
	header := capHeader{Version: capsetVersion3}
	if _, _, errno := syscall.Syscall(syscall.SYS_CAPSET, uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&data)), 0); errno == 0 {
		return nil
	}
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prCapAmbient, prCapAmbientRaise, capNetAdmin, 0, 0, 0); errno == 0 {
		return nil
	}
	return errors.New("all regain attempts rejected")
}

func memoryHog() {
	var blocks [][]byte
	for {
		block := make([]byte, 1024*1024)
		for i := range block {
			block[i] = byte(i)
		}
		blocks = append(blocks, block)
		if len(blocks)%4 == 0 {
			fmt.Printf("MEMORY_HOG_MB %d\n", len(blocks))
		}
	}
}

func forkBomb() {
	children := make([]int, 0, 64)
	for {
		child, err := startSelf("--sleep-child")
		if err != nil {
			fmt.Printf("FORK_BOUNDED %v children=%d\n", err, len(children))
			return
		}
		children = append(children, child)
	}
}
