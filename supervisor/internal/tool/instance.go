package tool

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	instInstanceFile = "instance-id"
	instObjectFile   = "tool-objects.json"
	instLockFile     = ".tool-state.lock"
)

// The durable identity and object state rely on iolbox's charter assumption
// that it is single-supervisor-per-host (PLAN.md:4). The durable ID and the
// delegated-cgroup subtree scope stale objects across generations and separate
// installs; they do not defend against an unsupported concurrent second
// supervisor sharing this install's data directory.

// InstanceID returns the durable UUID for the installation rooted at stateDir.
// It is created once so a supervisor restart can identify objects left by a
// previous generation after a crash.
func InstanceID(stateDir string) (string, error) {
	if err := instEnsureStateDir(stateDir); err != nil {
		return "", err
	}

	var instanceID string
	lockPath := filepath.Join(stateDir, instLockFile)
	if err := instWithFileLock(lockPath, func() error {
		instancePath := filepath.Join(stateDir, instInstanceFile)
		data, err := os.ReadFile(instancePath)
		if err == nil {
			instanceID = strings.TrimSpace(string(data))
			if instanceID == "" {
				return fmt.Errorf("tool: empty instance id in %q", instancePath)
			}
			if err := os.Chmod(instancePath, 0o600); err != nil {
				return fmt.Errorf("tool: set instance id mode %q: %w", instancePath, err)
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("tool: read instance id %q: %w", instancePath, err)
		}

		instanceID, err = instNewUUID()
		if err != nil {
			return err
		}
		file, err := os.OpenFile(instancePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fmt.Errorf("tool: create instance id %q: %w", instancePath, err)
		}
		if err := file.Chmod(0o600); err != nil {
			_ = file.Close()
			_ = os.Remove(instancePath)
			return fmt.Errorf("tool: set instance id mode %q: %w", instancePath, err)
		}
		if _, err := file.WriteString(instanceID + "\n"); err != nil {
			_ = file.Close()
			_ = os.Remove(instancePath)
			return fmt.Errorf("tool: write instance id %q: %w", instancePath, err)
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			_ = os.Remove(instancePath)
			return fmt.Errorf("tool: sync instance id %q: %w", instancePath, err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(instancePath)
			return fmt.Errorf("tool: close instance id %q: %w", instancePath, err)
		}
		return nil
	}); err != nil {
		return "", err
	}
	return instanceID, nil
}

// LoadObjectState reads the durable record of kernel objects created by this
// install. A missing state file is the normal empty-state case; malformed JSON
// is returned because silently treating it as empty could leak stale objects.
func LoadObjectState(stateDir string) (ObjectState, error) {
	if err := instEnsureStateDir(stateDir); err != nil {
		return ObjectState{}, err
	}

	var state ObjectState
	lockPath := filepath.Join(stateDir, instLockFile)
	if err := instWithFileLock(lockPath, func() error {
		var err error
		state, err = instReadObjectState(filepath.Join(stateDir, instObjectFile))
		return err
	}); err != nil {
		return ObjectState{}, err
	}
	return state, nil
}

// RecordObject persists one object before its kernel creation. The state file
// is updated under the same lock as identity creation, and replacement is
// fsync-then-rename so a crash cannot leave a half-written JSON document.
func RecordObject(stateDir, instanceID string, rec ObjectRecord) error {
	if err := instEnsureStateDir(stateDir); err != nil {
		return err
	}
	lockPath := filepath.Join(stateDir, instLockFile)
	return instWithFileLock(lockPath, func() error {
		state, err := instReadObjectState(filepath.Join(stateDir, instObjectFile))
		if err != nil {
			return err
		}
		if state.InstanceID != "" && state.InstanceID != instanceID {
			return fmt.Errorf("tool: object state belongs to instance %q, not %q", state.InstanceID, instanceID)
		}
		state.InstanceID = instanceID
		if state.Objects == nil {
			state.Objects = make(map[string]ObjectRecord)
		}
		state.Objects[strconv.Itoa(rec.NodeID)] = rec
		return instWriteObjectState(filepath.Join(stateDir, instObjectFile), state)
	})
}

// PruneObject removes a cleanly torn-down node from the durable state. A
// missing file or a record belonging to another durable installation is left
// untouched so cleanup cannot erase foreign ownership data.
func PruneObject(stateDir, instanceID string, nodeID int) error {
	if err := instEnsureStateDir(stateDir); err != nil {
		return err
	}
	lockPath := filepath.Join(stateDir, instLockFile)
	return instWithFileLock(lockPath, func() error {
		state, err := instReadObjectState(filepath.Join(stateDir, instObjectFile))
		if err != nil {
			return err
		}
		if state.InstanceID == "" || state.InstanceID != instanceID {
			return nil
		}
		if state.Objects == nil {
			return nil
		}
		delete(state.Objects, strconv.Itoa(nodeID))
		return instWriteObjectState(filepath.Join(stateDir, instObjectFile), state)
	})
}

func instEnsureStateDir(stateDir string) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("tool: state directory is empty")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("tool: create state directory %q: %w", stateDir, err)
	}
	return nil
}

func instNewUUID() (string, error) {
	var raw [16]byte
	if _, err := cryptorand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("tool: generate instance id: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func instValidUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value[14] != '4' || value[19] != '8' && value[19] != '9' && value[19] != 'a' && value[19] != 'b' && value[19] != 'A' && value[19] != 'B' {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') && !(char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}

func instReadObjectState(path string) (ObjectState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ObjectState{}, nil
	}
	if err != nil {
		return ObjectState{}, fmt.Errorf("tool: read object state %q: %w", path, err)
	}
	var state ObjectState
	if err := json.Unmarshal(data, &state); err != nil {
		return ObjectState{}, fmt.Errorf("tool: decode object state %q: %w", path, err)
	}
	return state, nil
}

func instWriteObjectState(path string, state ObjectState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("tool: encode object state: %w", err)
	}
	stateDir := filepath.Dir(path)
	temporary, err := os.CreateTemp(stateDir, ".tool-objects-*.tmp")
	if err != nil {
		return fmt.Errorf("tool: create object-state temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("tool: set object-state temporary mode: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("tool: write object state temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("tool: sync object state temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("tool: close object state temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("tool: replace object state %q: %w", path, err)
	}
	removeTemporary = false
	return nil
}
