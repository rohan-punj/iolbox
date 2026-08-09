//go:build linux

package tool

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
	"testing"
)

const (
	t23EndpointHelperModeEnv    = "IOLBOX_T23_ENDPOINT_MODE"
	t23EndpointHelperSocketEnv  = "IOLBOX_T23_ENDPOINT_SOCKET"
	t23EndpointHelperOptionsEnv = "IOLBOX_T23_ENDPOINT_OPTIONS"
)

func TestT23EndpointUIDDrop(t *testing.T) {
	mode := os.Getenv(t23EndpointHelperModeEnv)
	if mode != "" {
		t23EndpointUIDDropChild(t, mode, os.Getenv(t23EndpointHelperSocketEnv), os.Getenv(t23EndpointHelperOptionsEnv))
		return
	}

	if os.Geteuid() != 0 {
		t.Skip("endpoint ownership test requires root")
	}
	ioltool, err := user.Lookup("ioltool")
	if err != nil {
		t.Skipf("ioltool account is unavailable: %v", err)
	}
	ioltoolUID64, err := strconv.ParseUint(ioltool.Uid, 10, 32)
	if err != nil || ioltoolUID64 == 0 {
		t.Skipf("ioltool account has unusable uid %q", ioltool.Uid)
	}
	ioltoolGID64, err := strconv.ParseUint(ioltool.Gid, 10, 32)
	if err != nil {
		t.Skipf("ioltool account has unusable gid %q", ioltool.Gid)
	}

	var other *user.User
	var otherUID64, otherGID64 uint64
	for _, name := range []string{"nobody", "daemon", "www-data", "_apt"} {
		candidate, lookupErr := user.Lookup(name)
		if lookupErr != nil {
			continue
		}
		candidateUID, parseErr := strconv.ParseUint(candidate.Uid, 10, 32)
		if parseErr != nil || candidateUID == 0 || candidateUID == ioltoolUID64 {
			continue
		}
		candidateGID, parseErr := strconv.ParseUint(candidate.Gid, 10, 32)
		if parseErr != nil {
			continue
		}
		other = candidate
		otherUID64 = candidateUID
		otherGID64 = candidateGID
		break
	}
	if other == nil {
		t.Skip("no distinct non-root account is available for the denial check")
	}

	runRoot := t.TempDir()
	// t.TempDir is private to root by default. The production run root is
	// traversable, while the per-node directory is the actual 0700 boundary.
	if err := os.Chmod(runRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	socketDir := SocketDir(runRoot, 23)
	if err := endpointPrepareSocketDir(socketDir, int(ioltoolUID64), int(ioltoolGID64)); err != nil {
		t.Fatalf("endpointPrepareSocketDir: %v", err)
	}
	optionsPath := OptionsFile(runRoot, 23)
	if err := endpointWriteOptions(socketDir, []byte(`{"mode":"t23"}`), int(ioltoolUID64), int(ioltoolGID64)); err != nil {
		t.Fatalf("endpointWriteOptions: %v", err)
	}

	if info, err := os.Stat(socketDir); err != nil {
		t.Fatalf("stat socket directory: %v", err)
	} else if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("socket directory mode = %o, want 700", got)
	}
	if info, err := os.Stat(optionsPath); err != nil {
		t.Fatalf("stat options file: %v", err)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("options file mode = %o, want 600", got)
	}

	t23EndpointRunAs(t, uint32(ioltoolUID64), uint32(ioltoolGID64), "allow", socketDir+"/gui.sock", optionsPath)
	t23EndpointRunAs(t, uint32(otherUID64), uint32(otherGID64), "deny", socketDir+"/gui.sock", optionsPath)
}

func t23EndpointRunAs(t *testing.T, uid, gid uint32, mode, socketPath, optionsPath string) {
	t.Helper()
	var output bytes.Buffer
	command := exec.Command(os.Args[0], "-test.run", "^TestT23EndpointUIDDrop$")
	command.Dir = "/"
	command.Env = append(os.Environ(),
		t23EndpointHelperModeEnv+"="+mode,
		t23EndpointHelperSocketEnv+"="+socketPath,
		t23EndpointHelperOptionsEnv+"="+optionsPath,
	)
	command.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uid, Gid: gid},
	}
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		t.Fatalf("uid %d helper mode %q failed: %v\n%s", uid, mode, err, output.String())
	}
}

func t23EndpointUIDDropChild(t *testing.T, mode, socketPath, optionsPath string) {
	t.Helper()
	if socketPath == "" || optionsPath == "" {
		t.Fatal("uid-drop helper paths are empty")
	}

	switch mode {
	case "allow":
		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			t.Fatalf("bind socket as ioltool: %v", err)
		}
		defer func() {
			_ = listener.Close()
			_ = os.Remove(socketPath)
		}()

		file, err := os.OpenFile(optionsPath, os.O_RDWR, 0)
		if err != nil {
			t.Fatalf("open options read-write as ioltool: %v", err)
		}
		defer file.Close()
		contents := make([]byte, 128)
		read, err := file.Read(contents)
		if err != nil {
			t.Fatalf("read options as ioltool: %v", err)
		}
		written, err := file.WriteAt(contents[:read], 0)
		if err != nil {
			t.Fatalf("write options as ioltool: %v", err)
		}
		if written != read {
			t.Fatalf("options write count = %d, want %d", written, read)
		}

	case "deny":
		if _, err := net.Listen("unix", socketPath); err == nil {
			t.Fatal("different non-root uid unexpectedly bound socket")
		} else if !os.IsPermission(err) {
			t.Fatalf("socket bind error = %v, want permission error", err)
		}
		file, err := os.OpenFile(optionsPath, os.O_RDWR, 0)
		if err == nil {
			_ = file.Close()
			t.Fatal("different non-root uid unexpectedly opened options read-write")
		} else if !os.IsPermission(err) {
			t.Fatalf("options open error = %v, want permission error", err)
		}

	default:
		t.Fatalf("unknown uid-drop helper mode %q", mode)
	}
}
