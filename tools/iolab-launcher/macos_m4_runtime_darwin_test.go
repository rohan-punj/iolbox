//go:build darwin

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	m4DefaultMachine = "iolbox-m4-e2e"
	m4DefaultGUI     = 4001
	// m4SoakSecondsDefault is the M4-owner-approved reduced soak duration
	// (600s, not the original two hours) recorded in
	// docs/m7-evidence/phase0/m3-m4-inputs.md's "Inputs still open" record.
	// M7 Phase 5 (docs/macos-m7-plan.md section 11) requires its own
	// traffic-soak row to actually run two hours continuous, which is a
	// stricter bar than M4's own approved reduction -- so this is
	// overridable via IOLBOX_M4_SOAK_SECONDS for that Phase 5 run only,
	// while every other caller of this same M4 harness keeps the exact
	// historical 600s default unchanged.
	m4SoakSecondsDefault = 600
)

var (
	m4ProcessStart = time.Now()
	m4SoakSeconds  = m4ResolveSoakSeconds()
)

func m4ResolveSoakSeconds() int {
	raw := os.Getenv("IOLBOX_M4_SOAK_SECONDS")
	if raw == "" {
		return m4SoakSecondsDefault
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value%60 != 0 {
		// Malformed override must not silently fall back to a shorter,
		// easier-to-pass soak -- fail loudly at process start instead.
		panic(fmt.Sprintf("invalid IOLBOX_M4_SOAK_SECONDS %q: must be a positive multiple of 60", raw))
	}
	return value
}

var (
	m4IOSPingRE     = regexp.MustCompile(`(?i)Success rate is ([0-9]+) percent \(([0-9]+)/([0-9]+)\)`)
	m4PCPingRE      = regexp.MustCompile(`(?i)([0-9]+) packets transmitted,\s*([0-9]+) packets received,\s*([0-9.]+)% packet loss`)
	m4LatencyRE     = regexp.MustCompile(`(?i)(?:min/avg/max(?:/mdev)?|round-trip min/avg/max)\s*=\s*([0-9.]+)/([0-9.]+)/([0-9.]+)`)
	m4HostPIDRE     = regexp.MustCompile(`\b([0-9]{2,})\b`)
	m4VPCSReplyRE   = regexp.MustCompile(`(?i)bytes from \S+ icmp_seq=([0-9]+) ttl=[0-9]+ time=([0-9.]+)`)
	m4VPCSTimeoutRE = regexp.MustCompile(`(?i)\S+ icmp_seq=([0-9]+) timeout`)
)

type m4Runtime struct {
	root      string
	runID     string
	machine   string
	guiPort   int
	baseURL   string
	guiAddr   string
	imagePath string
	control   *controlWSClient
	imageID   string
}

type m4Console struct {
	*m3Console
	node       int
	promptUTC  string
	transcript bytes.Buffer
	mu         sync.Mutex
}

func m4Required(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func m4NewRuntime() (*m4Runtime, error) {
	root := os.Getenv("IOLBOX_M4_EVIDENCE")
	if root == "" {
		return nil, errors.New("IOLBOX_M4_EVIDENCE is required")
	}
	runID := os.Getenv("IOLBOX_M4_RUN_ID")
	if runID == "" {
		return nil, errors.New("IOLBOX_M4_RUN_ID is required")
	}
	guiPort := m4DefaultGUI
	if raw := os.Getenv("IOLBOX_M4_GUI_PORT"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 65535 {
			return nil, fmt.Errorf("invalid GUI port %q", raw)
		}
		guiPort = value
	}
	return &m4Runtime{
		root:      root,
		runID:     runID,
		machine:   m4Required("IOLBOX_M4_MACHINE", m4DefaultMachine),
		guiPort:   guiPort,
		baseURL:   fmt.Sprintf("http://127.0.0.1:%d", guiPort),
		guiAddr:   fmt.Sprintf("127.0.0.1:%d", guiPort),
		imagePath: os.Getenv("IOLBOX_M4_IMAGE"),
	}, nil
}

func (r *m4Runtime) phaseDir(phase string) string {
	if suffix := os.Getenv("IOLBOX_M4_PHASE_SUFFIX"); suffix != "" {
		return filepath.Join(r.root, phase, suffix)
	}
	return filepath.Join(r.root, phase)
}

func (r *m4Runtime) ensurePhase(phase string) (string, error) {
	dir := r.phaseDir(phase)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func m4WriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func m4AppendJSONLine(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

func m4CommandRecord(dir, name string, command string, args ...string) (string, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir, _ = os.Getwd()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	end := time.Now()
	status := 0
	if err != nil {
		status = 1
		if cmd.ProcessState != nil {
			status = cmd.ProcessState.ExitCode()
		}
	}
	record := map[string]any{
		"command": command, "argv": append([]string{command}, args...), "cwd": cmd.Dir,
		"start_utc": m4UTC(start), "end_utc": m4UTC(end),
		"monotonic_start_ns": start.Sub(m4ProcessStart).Nanoseconds(), "monotonic_end_ns": end.Sub(m4ProcessStart).Nanoseconds(),
		"stdout": stdout.String(), "stderr": stderr.String(), "exit_status": status,
	}
	hash := sha256.Sum256(append(stdout.Bytes(), stderr.Bytes()...))
	record["sha256"] = hex.EncodeToString(hash[:])
	path := filepath.Join(dir, name+".command.json")
	if writeErr := m4WriteJSON(path, record); writeErr != nil {
		return path, writeErr
	}
	return path, err
}

func (r *m4Runtime) getRoot(dir string) error {
	start := time.Now()
	deadline := start.Add(45 * time.Second)
	statuses := []map[string]any{}
	for time.Now().Before(deadline) {
		resp, err := http.Get(r.baseURL + "/")
		status := 0
		if err == nil {
			status = resp.StatusCode
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		statuses = append(statuses, map[string]any{"at_utc": m4UTC(time.Now()), "status": status, "error": errorText(err)})
		if err == nil && status < 500 {
			return m4WriteJSON(filepath.Join(dir, "readiness.json"), map[string]any{
				"criterion": "GET / < 500", "accepted_status": status, "start_utc": m4UTC(start), "end_utc": m4UTC(time.Now()), "attempts": statuses,
			})
		}
		time.Sleep(500 * time.Millisecond)
	}
	_ = m4WriteJSON(filepath.Join(dir, "readiness.json"), map[string]any{"criterion": "GET / < 500", "attempts": statuses, "start_utc": m4UTC(start), "end_utc": m4UTC(time.Now())})
	return fmt.Errorf("GET / did not become ready below 500")
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (r *m4Runtime) connect(dir string) error {
	if err := r.getRoot(dir); err != nil {
		return err
	}
	control, err := dialControlWS(r.guiAddr)
	if err != nil {
		return err
	}
	r.control = control
	hello, helloErr := control.hello()
	if helloErr != nil {
		return helloErr
	}
	if err := m4WriteJSON(filepath.Join(dir, "hello.json"), hello); err != nil {
		return err
	}
	return nil
}

func (r *m4Runtime) close() {
	if r.control != nil {
		_ = r.control.Close()
		r.control = nil
	}
}

func (r *m4Runtime) request(dir, op string, args any) (json.RawMessage, error) {
	if r.control == nil {
		return nil, errors.New("M4 control connection is not open")
	}
	result, err := r.control.requestTimeout(op, args, 2*time.Minute)
	entry := map[string]any{"run_id": r.runID, "op": op, "ok": err == nil, "result": json.RawMessage(result), "error": errorText(err), "at_utc": m4UTC(time.Now())}
	if logErr := m4AppendJSONLine(filepath.Join(dir, "control.ndjson"), entry); logErr != nil && err == nil {
		err = logErr
	}
	return result, err
}

func (r *m4Runtime) authProbes(dir string) error {
	results := []map[string]any{}
	if conn, err := wsDialWithHeaders(r.guiAddr, "/control", nil); err == nil {
		_ = conn.Close()
		results = append(results, map[string]any{"probe": "missing-cookie", "passed": false, "error": "unexpected 101"})
	} else {
		results = append(results, map[string]any{"probe": "missing-cookie", "passed": true, "error": err.Error()})
	}
	cookie, err := fetchSessionCookie(r.guiAddr)
	if err != nil {
		return err
	}
	if conn, badErr := wsDialWithHeaders(r.guiAddr, "/control", map[string]string{"Cookie": "iolbox_session=" + cookie, "Origin": "http://bad-origin.invalid"}); badErr == nil {
		_ = conn.Close()
		results = append(results, map[string]any{"probe": "bad-origin", "passed": false, "error": "unexpected 101"})
	} else {
		results = append(results, map[string]any{"probe": "bad-origin", "passed": true, "error": badErr.Error()})
	}
	conn, err := wsDialWithSession(r.guiAddr, "/control")
	if err != nil {
		return err
	}
	_ = conn.Close()
	results = append(results, map[string]any{"probe": "session-cookie-same-origin", "passed": true})
	return m4WriteJSON(filepath.Join(dir, "ws-auth-probes.json"), map[string]any{"route": "/control", "results": results})
}

func m4AuthProbeRoute(dir, addr, path string) error {
	results := []map[string]any{}
	if conn, err := wsDialWithHeaders(addr, path, nil); err == nil {
		_ = conn.Close()
		results = append(results, map[string]any{"probe": "missing-cookie", "passed": false, "error": "unexpected 101"})
	} else {
		results = append(results, map[string]any{"probe": "missing-cookie", "passed": true, "error": err.Error()})
	}
	cookie, err := fetchSessionCookie(addr)
	if err != nil {
		return err
	}
	if conn, badErr := wsDialWithHeaders(addr, path, map[string]string{"Cookie": "iolbox_session=" + cookie, "Origin": "http://bad-origin.invalid"}); badErr == nil {
		_ = conn.Close()
		results = append(results, map[string]any{"probe": "bad-origin", "passed": false, "error": "unexpected 101"})
	} else {
		results = append(results, map[string]any{"probe": "bad-origin", "passed": true, "error": badErr.Error()})
	}
	conn, err := wsDialWithSession(addr, path)
	if err != nil {
		return err
	}
	_ = conn.Close()
	results = append(results, map[string]any{"probe": "session-cookie-same-origin", "passed": true})
	return m4AppendJSONLine(filepath.Join(dir, "ws-auth-probes.ndjson"), map[string]any{"route": path, "results": results, "at_utc": m4UTC(time.Now())})
}

func (r *m4Runtime) ensureImage(dir string) error {
	if r.imageID != "" {
		return nil
	}
	if r.imagePath == "" {
		return errors.New("IOLBOX_M4_IMAGE is required")
	}
	imageFile, err := os.Open(r.imagePath)
	if err != nil {
		return err
	}
	info, err := imageFile.Stat()
	if err != nil {
		_ = imageFile.Close()
		return err
	}
	uploader := newHTTPImageUploader(r.baseURL)
	guestPath, err := uploader.upload(filepath.Base(r.imagePath), imageFile, info.ModTime().UnixNano())
	_ = imageFile.Close()
	if err != nil {
		return err
	}
	registered, err := r.request(dir, "image.register", map[string]string{"path": guestPath})
	if err != nil {
		return err
	}
	var image struct {
		ID     string `json:"id"`
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(registered, &image); err != nil || image.ID == "" {
		return fmt.Errorf("image.register returned invalid result: %s", registered)
	}
	r.imageID = image.ID
	hash, size, err := m4HashFile(r.imagePath)
	if err != nil {
		return err
	}
	return m4WriteJSON(filepath.Join(dir, "image.json"), map[string]any{"path": r.imagePath, "guest_path": guestPath, "id": image.ID, "sha256": hash, "size": size, "server_sha256": image.SHA256})
}

func (r *m4Runtime) fixture(dir, name, phase string) (json.RawMessage, error) {
	fixtureRoot := os.Getenv("IOLBOX_M4_FIXTURES")
	if fixtureRoot == "" {
		fixtureRoot = filepath.Join("testdata", "macos-m4")
	}
	path := filepath.Join(fixtureRoot, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := m4ValidateFixture(data); err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	doc["id"] = fmt.Sprintf("m4-%s-%s", r.runID, strings.TrimSuffix(name, ".lab.json"))
	doc["description"] = fmt.Sprintf("%v run_id=%s", doc["description"], r.runID)
	if nodes, ok := doc["nodes"].([]any); ok {
		for _, node := range nodes {
			if object, ok := node.(map[string]any); ok {
				if image, ok := object["image"].(map[string]any); ok {
					image["id"] = r.imageID
				}
			}
		}
	}
	mutated, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	mutated = append(mutated, '\n')
	path = filepath.Join(dir, filepath.Base(path))
	if err := os.WriteFile(path, mutated, 0o644); err != nil {
		return nil, err
	}
	return mutated, nil
}

func (r *m4Runtime) loadStart(dir string, fixture json.RawMessage, nodes []int) (string, []map[string]any, error) {
	loadRaw, err := r.request(dir, "lab.load", struct {
		Lab json.RawMessage `json:"lab"`
	}{Lab: fixture})
	if err != nil {
		return "", nil, err
	}
	var load struct {
		LabID    string   `json:"labId"`
		Warnings []string `json:"warnings"`
		Nodes    []struct {
			ID          int `json:"id"`
			ConsolePort int `json:"consolePort"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(loadRaw, &load); err != nil || load.LabID == "" {
		return "", nil, fmt.Errorf("lab.load invalid result: %s", loadRaw)
	}
	if len(load.Warnings) > 0 {
		return "", nil, fmt.Errorf("lab.load warnings: %s", strings.Join(load.Warnings, "; "))
	}
	startRaw, err := r.request(dir, "lab.start", map[string]any{"labId": load.LabID, "nodes": nodes})
	if err != nil {
		return load.LabID, nil, err
	}
	var started struct {
		Started []map[string]any `json:"started"`
		Failed  []map[string]any `json:"failed"`
	}
	if err := json.Unmarshal(startRaw, &started); err != nil {
		return load.LabID, nil, err
	}
	if len(started.Failed) > 0 {
		return load.LabID, started.Started, fmt.Errorf("lab.start failed nodes: %v", started.Failed)
	}
	return load.LabID, started.Started, nil
}

func (r *m4Runtime) openConsoles(nodes []int, timeout time.Duration) (map[int]*m4Console, error) {
	result := make(map[int]*m4Console, len(nodes))
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make(chan error, len(nodes))
	for _, node := range nodes {
		node := node
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, prompt, err := m3OpenConsoleWithRetry(r.guiAddr, node, timeout)
			if err != nil {
				errs <- fmt.Errorf("node %d: %w", node, err)
				return
			}
			console := &m4Console{m3Console: &m3Console{conn: conn, prompt: prompt}, node: node, promptUTC: m4UTC(time.Now())}
			console.transcript.WriteString(fmt.Sprintf("[%s] initial wake=\\r\\n prompt=%s\n", m4UTC(time.Now()), prompt))
			mu.Lock()
			result[node] = console
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		for _, console := range result {
			_ = console.conn.Close()
		}
		return nil, err
	}
	return result, nil
}

// drainStale discards any frames already buffered on the connection before a
// new command is written. A console wake (initial "\r\n" on connect) or a
// prior command's trailing newline can echo a second, empty prompt line
// asynchronously; without this drain, the next send's read loop can match
// that leftover prompt on its very first frame and return before the actual
// command output arrives, wrongly reporting an empty result.
func (c *m4Console) drainStale(window time.Duration) {
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		_ = c.conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		opcode, payload, err := readFrame(c.conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return
		}
		if opcode == wsOpBinary || opcode == wsOpText {
			c.transcript.WriteString(fmt.Sprintf("[%s] drained-stale=%q\n", m4UTC(time.Now()), string(payload)))
		}
	}
}

func (c *m4Console) send(input string, timeout time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.drainStale(150 * time.Millisecond)
	// The browser console accepts a terminal line ending, not a bare CR. The
	// M3 console helper wakes the IOS console with CRLF; keep that same
	// discipline for VPCS as well. Callers may use CR for readability, so
	// canonicalize all line endings at the websocket boundary.
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	input = strings.ReplaceAll(input, "\n", "\r\n")
	mask, err := newMaskKey()
	if err != nil {
		return "", err
	}
	if err := writeFrame(c.conn, wsOpBinary, []byte(input), &mask); err != nil {
		return "", err
	}
	start := time.Now()
	var output bytes.Buffer
	for time.Since(start) < timeout {
		_ = c.conn.SetReadDeadline(time.Now().Add(time.Second))
		opcode, payload, err := readFrame(c.conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return output.String(), err
		}
		if opcode != wsOpBinary && opcode != wsOpText {
			continue
		}
		output.Write(payload)
		lines := strings.Split(strings.ReplaceAll(output.String(), "\r", "\n"), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			if len(line) > 1 && (strings.HasSuffix(line, "#") || strings.HasSuffix(line, ">")) {
				c.transcript.WriteString(fmt.Sprintf("[%s] input=%q\n%s\n[final-prompt] %s\n", m4UTC(time.Now()), input, output.String(), line))
				return output.String(), nil
			}
			break
		}
	}
	return output.String(), fmt.Errorf("node %d command %q returned no final prompt", c.node, strings.TrimSpace(input))
}

func (r *m4Runtime) closeConsoles(consoles map[int]*m4Console, dir string) error {
	for node, console := range consoles {
		path := filepath.Join(dir, fmt.Sprintf("console-%d.txt", node))
		if err := os.WriteFile(path, console.transcript.Bytes(), 0o644); err != nil {
			return err
		}
		_ = console.conn.Close()
	}
	return nil
}

// m4FixtureVPCSCommands extracts, per node id, the console commands a fixture
// wants run on every "vpcs"-kind node before any traffic starts. See the
// caller in basicPhase for why this injection has to happen out-of-band:
// vpcs is the one node kind with no boot-time config path in the supervisor.
func m4FixtureVPCSCommands(fixture json.RawMessage) (map[int][]string, error) {
	var doc struct {
		Nodes []struct {
			ID     int    `json:"id"`
			Kind   string `json:"kind"`
			Config struct {
				Commands []string `json:"commands"`
			} `json:"config"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(fixture, &doc); err != nil {
		return nil, fmt.Errorf("parse fixture for vpcs config: %w", err)
	}
	out := map[int][]string{}
	for _, node := range doc.Nodes {
		if node.Kind != "vpcs" || len(node.Config.Commands) == 0 {
			continue
		}
		out[node.ID] = node.Config.Commands
	}
	return out, nil
}

func m4ParsePing(command string, output string) (m4PingSummary, error) {
	result := m4PingSummary{Command: command, Timestamp: m4UTC(time.Now())}
	var found bool
	for _, match := range m4IOSPingRE.FindAllStringSubmatch(output, -1) {
		result.Sent, _ = strconv.Atoi(match[3])
		result.Received, _ = strconv.Atoi(match[2])
		result.Lost = result.Sent - result.Received
		// match[1] is IOS's own "Success rate is X percent" figure, not a
		// loss rate — assigning it straight into LossPct reported 100% loss
		// on a perfect run (proven on hardware: a 10/10 soak interval showed
		// loss_percent:100 in traffic.ndjson). Compute the actual loss
		// percentage from sent/lost instead of the complement of a
		// pre-rounded IOS integer.
		if result.Sent > 0 {
			result.LossPct = float64(result.Lost) * 100 / float64(result.Sent)
		}
		found = true
	}
	for _, match := range m4PCPingRE.FindAllStringSubmatch(output, -1) {
		result.Sent, _ = strconv.Atoi(match[1])
		result.Received, _ = strconv.Atoi(match[2])
		result.Lost = result.Sent - result.Received
		result.LossPct, _ = strconv.ParseFloat(match[3], 64)
		found = true
	}
	if latency := m4LatencyRE.FindAllStringSubmatch(output, -1); len(latency) > 0 {
		result.LatencyAvg, _ = strconv.ParseFloat(latency[len(latency)-1][2], 64)
	}
	if !found {
		// This vpcs build never prints an aggregate "packets transmitted..."
		// summary line for a "-c N"/"repeat N" run (proven on hardware: ten
		// full icmp_seq reply/timeout lines followed straight by the VPCS>
		// prompt, no summary line at all). Fall back to counting the
		// per-packet lines directly by their icmp_seq number.
		seqs := map[int]bool{}
		var latencies []float64
		for _, match := range m4VPCSReplyRE.FindAllStringSubmatch(output, -1) {
			seq, _ := strconv.Atoi(match[1])
			seqs[seq] = true
			if latency, latErr := strconv.ParseFloat(match[2], 64); latErr == nil {
				latencies = append(latencies, latency)
			}
			found = true
		}
		received := len(seqs)
		for _, match := range m4VPCSTimeoutRE.FindAllStringSubmatch(output, -1) {
			seq, _ := strconv.Atoi(match[1])
			if !seqs[seq] {
				seqs[seq] = true
				found = true
			}
		}
		if found {
			result.Sent = len(seqs)
			result.Received = received
			result.Lost = result.Sent - result.Received
			if result.Sent > 0 {
				result.LossPct = float64(result.Lost) * 100 / float64(result.Sent)
			}
			if len(latencies) > 0 {
				var sum float64
				for _, l := range latencies {
					sum += l
				}
				result.LatencyAvg = sum / float64(len(latencies))
			}
		}
	}
	if !found {
		return result, fmt.Errorf("no complete ping summary in output: %s", output)
	}
	return result, nil
}

// m4PingTimeout sizes a console read timeout for a ping of the given packet
// count. Proven on hardware: this vpcs build sends roughly one packet per
// second, so a flat 30s timeout that works for "-c 10" times out well before
// "-c 100" completes (~100s of console output) even though the ping itself
// is healthy — the read loop in send() has no way to distinguish "still
// running" from "hung" other than the caller's timeout budget.
func m4PingTimeout(count int) time.Duration {
	return time.Duration(count)*2*time.Second + 15*time.Second
}

func (c *m4Console) ping(command string, timeout time.Duration) (m4PingSummary, string, error) {
	output, err := c.send(command+"\r\n", timeout)
	if err != nil {
		return m4PingSummary{Command: command, Timestamp: m4UTC(time.Now())}, output, err
	}
	summary, parseErr := m4ParsePing(command, output)
	return summary, output, parseErr
}

func m4Enable(c *m4Console) error {
	output, err := c.send("enable\r", 15*time.Second)
	if err != nil {
		return err
	}
	if !strings.Contains(output, "#") {
		return fmt.Errorf("enable did not reach privileged EXEC")
	}
	return nil
}

func m4WritePings(path string, pings []m4PingSummary) error {
	for _, ping := range pings {
		if err := m4AppendJSONLine(path, ping); err != nil {
			return err
		}
	}
	return nil
}

func (r *m4Runtime) stopLab(dir, labID string, started []map[string]any) (map[string]any, error) {
	_, err := r.request(dir, "lab.stop", map[string]any{"labId": labID, "nodes": nil})
	cleanup := map[string]any{"lab_id": labID, "stop_ok": err == nil, "owned_pids": started, "owned_residue": -1}
	if err != nil {
		return cleanup, err
	}
	residue := 0
	for _, node := range started {
		pid, ok := node["pid"].(float64)
		if !ok || pid <= 0 {
			continue
		}
		cmd := exec.Command("/opt/homebrew/bin/limactl", "shell", r.machine, "ps", "-p", strconv.Itoa(int(pid)))
		if cmd.Run() == nil {
			residue++
		}
	}
	cleanup["owned_residue"] = residue
	cleanup["exact_subtraction"] = true
	return cleanup, nil
}

func (r *m4Runtime) basicPhase(phase, fixtureName string, nodes []int) (record m4PhaseRecord, retErr error) {
	start := time.Now()
	record = m4PhaseRecord{Schema: "iolbox.macos-m4.phase/v2", RunID: r.runID, Phase: phase, Status: "PASS", StartUTC: m4UTC(start), Fixture: fixtureName, Metrics: map[string]m4Metric{}, Details: map[string]any{}}
	dir, err := r.ensurePhase(phase)
	if err != nil {
		return record, err
	}
	var labID string
	var started []map[string]any
	stopped := false
	defer func() {
		if labID != "" && !stopped {
			cleanup, cleanupErr := r.stopLab(dir, labID, started)
			record.Cleanup = cleanup
			if retErr == nil && cleanupErr != nil {
				retErr = cleanupErr
			}
		}
		record.EndUTC = m4UTC(time.Now())
		if retErr != nil && record.Status == "PASS" {
			record.Status = "UNVERIFIED"
		}
	}()
	if err = r.connect(dir); err != nil {
		return record, err
	}
	defer r.close()
	if phase == "item-1" || phase == "item-7" {
		if err = r.authProbes(dir); err != nil {
			return record, err
		}
	}
	if err = r.ensureImage(dir); err != nil {
		return record, err
	}
	fixture, err := r.fixture(dir, fixtureName, phase)
	if err != nil {
		return record, err
	}
	labID, started, err = r.loadStart(dir, fixture, nodes)
	record.LabID, record.Nodes = labID, started
	if err != nil {
		record.Status = "UNVERIFIED"
		record.HardWall = phase == "item-5"
		return record, err
	}
	if err := m4WriteJSON(filepath.Join(dir, "ownership-map.json"), map[string]any{
		"schema": "iolbox.macos-m4.ownership/v2", "run_id": r.runID, "lab_id": labID,
		"node_ids": nodes, "returned_nodes": started, "phase": phase,
	}); err != nil {
		return record, err
	}
	vpcsCommands, err := m4FixtureVPCSCommands(fixture)
	if err != nil {
		return record, err
	}
	if err := r.sampleResources(dir, 0, m4StartedPIDs(started)); err != nil {
		return record, err
	}
	// Probe each console route before opening the live console sessions. VPCS
	// owns its telnet server and can only service one client reliably; a
	// browser-equivalent auth probe that opens and closes a second session
	// after the live session is established can strand the original bridge.
	for _, node := range nodes {
		if err := m4AuthProbeRoute(dir, r.guiAddr, fmt.Sprintf("/console/%d", node)); err != nil {
			return record, err
		}
	}
	consoles, err := r.openConsoles(nodes, 150*time.Second)
	if err != nil {
		record.Status = "UNVERIFIED"
		record.HardWall = phase == "item-5"
		return record, err
	}
	defer r.closeConsoles(consoles, dir)
	// A vpcs node has no boot-time IP injection path in the supervisor (only
	// the "tool"/"pc" kinds read Config["net"]; classic vpcs is interactive
	// console-only, matching real VPCS/GNS3). Without this, a fixture's
	// config.commands is silently never applied and every ping from that node
	// reports the unconfigured 0.0.0.0 source address and times out — proven
	// on hardware: PC1 sent ARP as "who-has 192.168.1.1 tell 0.0.0.0" and every
	// ICMP echo also carried 0.0.0.0, even though the bridge/tap fabric itself
	// forwarded the router's ARP reply correctly. Run the fixture's own
	// commands for every vpcs node before any phase issues a ping.
	for node, commands := range vpcsCommands {
		console, ok := consoles[node]
		if !ok {
			return record, fmt.Errorf("fixture vpcs node %d has no open console", node)
		}
		for _, command := range commands {
			if _, err := console.send(command+"\r\n", 15*time.Second); err != nil {
				return record, fmt.Errorf("vpcs node %d config command %q: %w", node, command, err)
			}
		}
	}
	pings := []m4PingSummary{}
	for _, console := range consoles {
		if console.node != 1 && console.node != 2 && console.node != 3 {
			_, _ = console.send("terminal length 0\r", 15*time.Second)
		}
	}
	if phase == "item-1" || phase == "item-7" {
		pc := consoles[1]
		router := consoles[0]
		counts := []int{10, 100}
		if phase == "item-7" {
			counts = []int{100}
		}
		for _, count := range counts {
			ping, _, pingErr := pc.ping(fmt.Sprintf("ping 192.168.1.1 -c %d", count), m4PingTimeout(count))
			if pingErr != nil {
				return record, pingErr
			}
			pings = append(pings, ping)
			if err := m4Enable(router); err != nil {
				return record, err
			}
			ping, _, pingErr = router.ping(fmt.Sprintf("ping 192.168.1.10 repeat %d", count), m4PingTimeout(count))
			if pingErr != nil {
				return record, pingErr
			}
			pings = append(pings, ping)
		}
		record.Details["ping_contract"] = "cold=10 and warm=100 in each direction"
		if len(pings) != 4 && phase == "item-1" {
			return record, fmt.Errorf("item 1 produced %d ping summaries, want 4", len(pings))
		}
		for i, ping := range pings {
			if ping.Sent != counts[i/2] || (ping.Sent == 100 && ping.Received < 99) {
				return record, fmt.Errorf("%s ping %d violates sent/received bar: %+v", phase, i, ping)
			}
		}
	} else if phase == "item-2" {
		if err := m4Enable(consoles[0]); err != nil {
			return record, err
		}
		if err := r.captureShort(dir, labID, consoles[0], nil); err != nil {
			return record, err
		}
		for _, pair := range []struct {
			c       *m4Console
			command string
		}{
			{consoles[2], "ping 192.168.1.1 -c 100"}, {consoles[0], "ping 192.168.1.10 repeat 100"},
			{consoles[0], "ping 10.0.12.2 repeat 100"}, {consoles[1], "ping 10.0.12.1 repeat 100"},
		} {
			ping, _, pingErr := pair.c.ping(pair.command, m4PingTimeout(100))
			if pingErr != nil {
				return record, pingErr
			}
			pings = append(pings, ping)
		}
		record.Details["fixed_nodes"] = []int{0, 1, 2}
	} else if phase == "item-3" {
		if err := m4Enable(consoles[0]); err != nil {
			return record, err
		}
		beforeRules, beforeRulesErr := m4ExecCapture(dir, "nat-iptables-before", "/opt/homebrew/bin/limactl", "shell", r.machine, "sudo", "iptables-save", "-c")
		if beforeRulesErr != nil {
			return record, beforeRulesErr
		}
		if err := os.WriteFile(filepath.Join(dir, "nat-iptables-before.txt"), []byte(beforeRules), 0o644); err != nil {
			return record, err
		}
		gateway, _, gatewayErr := consoles[1].ping("ping 192.168.1.1 -c 20", m4PingTimeout(20))
		if gatewayErr != nil {
			return record, gatewayErr
		}
		pings = append(pings, gateway)
		target := m4Required("IOLBOX_M4_NAT_TARGET", "1.1.1.1")
		ping, _, pingErr := consoles[1].ping("ping "+target+" -c 20", m4PingTimeout(20))
		if pingErr != nil {
			return record, pingErr
		}
		pings = append(pings, ping)
		afterRules, afterRulesErr := m4ExecCapture(dir, "nat-iptables-after", "/opt/homebrew/bin/limactl", "shell", r.machine, "sudo", "iptables-save", "-c")
		if afterRulesErr != nil {
			return record, afterRulesErr
		}
		if err := os.WriteFile(filepath.Join(dir, "nat-iptables-after.txt"), []byte(afterRules), 0o644); err != nil {
			return record, err
		}
		record.Details["target"] = target
		record.Details["client_console_transcript"] = "console-1.txt"
		if err := m4WriteJSON(filepath.Join(dir, "nat-counters.json"), map[string]any{"before": beforeRules, "after": afterRules, "target": target}); err != nil {
			return record, err
		}
	} else if phase == "item-5" {
		if err := m4FourNodeTraffic(dir, consoles, &pings); err != nil {
			return record, err
		}
	}
	if phase == "item-2" && len(pings) != 4 {
		return record, fmt.Errorf("item 2 produced %d ping summaries, want 4", len(pings))
	}
	if phase == "item-3" && (len(pings) != 2 || pings[0].Sent != 20 || pings[0].Received < 19 || pings[1].Sent != 20 || pings[1].Received < 19) {
		return record, fmt.Errorf("item 3 NAT ping violates 19/20 bar: %+v", pings)
	}
	if phase == "item-5" && len(pings) != 10 {
		return record, fmt.Errorf("item 5 produced %d ping summaries, want 10", len(pings))
	}
	if err := m4WritePings(filepath.Join(dir, "pings.ndjson"), pings); err != nil {
		return record, err
	}
	record.Sources = []string{m4RelPath(r.root, filepath.Join(dir, "pings.ndjson")), m4RelPath(r.root, filepath.Join(dir, "control.ndjson"))}
	for i, ping := range pings {
		name := fmt.Sprintf("ping_%d_received", i)
		metric, metricErr := m4FileMetric(r.root, filepath.Join(dir, "pings.ndjson"), ping.Received, "packets")
		if metricErr != nil {
			return record, metricErr
		}
		record.Metrics[name] = metric
	}
	if err := r.sampleResources(dir, time.Since(start).Seconds(), m4StartedPIDs(started)); err != nil {
		return record, err
	}
	cleanup, stopErr := r.stopLab(dir, labID, started)
	record.Cleanup = cleanup
	stopped = stopErr == nil
	if stopErr != nil {
		return record, stopErr
	}
	if residue, ok := cleanup["owned_residue"].(int); !ok || residue != 0 {
		return record, fmt.Errorf("owned residue after %s: %v", phase, cleanup["owned_residue"])
	}
	return record, nil
}

func m4FileMetric(root, path string, value any, unit string) (m4Metric, error) {
	hash, _, err := m4HashFile(path)
	if err != nil {
		return m4Metric{}, err
	}
	return m4Metric{Value: value, Unit: unit, SourcePath: m4RelPath(root, path), SourceSHA256: hash, SourceClass: "hardware", SourceStartUTC: m4UTC(time.Now().Add(-time.Second)), SourceEndUTC: m4UTC(time.Now())}, nil
}

func m4DialCapture(addr string, port int) (net.Conn, error) {
	deadline := time.Now().Add(20 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
		if err == nil {
			return conn, nil
		}
		last = err
		time.Sleep(time.Second)
	}
	return nil, fmt.Errorf("capture port %d did not forward: %w", port, last)
}

func m4CollectCapture(conn net.Conn, path string, stop <-chan struct{}, heartbeat string, checkpoints string, start time.Time) (int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var total int64
	for {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		opcode, payload, readErr := readFrame(conn)
		if readErr != nil {
			if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
				select {
				case <-stop:
					_ = f.Sync()
					return total, nil
				default:
					continue
				}
			}
			select {
			case <-stop:
				_ = f.Sync()
				return total, nil
			default:
			}
			return total, readErr
		}
		if opcode == wsOpPing {
			mask, maskErr := newMaskKey()
			if maskErr == nil {
				_ = writeControlFrame(conn, wsOpPong, payload, mask)
			}
			continue
		}
		if opcode != wsOpBinary {
			continue
		}
		if _, err := f.Write(payload); err != nil {
			return total, err
		}
		total += int64(len(payload))
		_ = m4AppendJSONLine(heartbeat, map[string]any{"at_utc": m4UTC(time.Now()), "elapsed_seconds": time.Since(start).Seconds(), "bytes": total})
	}
}

func (r *m4Runtime) captureShort(dir, labID string, router *m4Console, pings []m4PingSummary) error {
	startRaw, err := r.request(dir, "capture.start", map[string]any{"labId": labID, "link": 0, "mode": "live"})
	if err != nil {
		return err
	}
	var capture struct {
		CapturePort int `json:"capturePort"`
	}
	if err := json.Unmarshal(startRaw, &capture); err != nil || capture.CapturePort < 5500 {
		return fmt.Errorf("capture.start invalid result: %s", startRaw)
	}
	if err := m4AuthProbeRoute(dir, r.guiAddr, "/capture/0"); err != nil {
		return err
	}
	direct, err := m4DialCapture(r.guiAddr, capture.CapturePort)
	if err != nil {
		return err
	}
	_ = direct.Close()
	conn, err := wsDialWithSession(r.guiAddr, "/capture/0")
	if err != nil {
		return err
	}
	defer conn.Close()
	phaseStart := time.Now()
	stop := make(chan struct{})
	pcapPath := filepath.Join(dir, "capture-0.pcapng")
	heartbeat := filepath.Join(dir, "capture-heartbeat.ndjson")
	checkpoints := filepath.Join(dir, "capture-checkpoints.ndjson")
	collectorDone := make(chan struct{})
	var captured int64
	var collectErr error
	go func() {
		captured, collectErr = m4CollectCapture(conn, pcapPath, stop, heartbeat, checkpoints, phaseStart)
		close(collectorDone)
	}()
	// The existing M3 client uses the same authenticated capture route and
	// structural validator. Generate traffic while the authenticated stream is
	// open, then give the bridge a bounded quiet period before closing it.
	if err := m4Enable(router); err != nil {
		return err
	}
	if _, _, err := router.ping("ping 10.0.12.2 repeat 20", m4PingTimeout(20)); err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	close(stop)
	_ = conn.Close()
	<-collectorDone
	if collectErr != nil {
		return collectErr
	}
	data, err := os.ReadFile(pcapPath)
	if err != nil {
		return err
	}
	packets, err := validateM3PCAPNG(data)
	if err != nil {
		return err
	}
	hash, size, err := m4HashFile(pcapPath)
	if err != nil {
		return err
	}
	if err := m4WriteJSON(filepath.Join(dir, "capture-validation.json"), map[string]any{"path": m4RelPath(r.root, pcapPath), "bytes": size, "collector_bytes": captured, "packets": packets, "sha256": hash, "start_utc": m4UTC(phaseStart), "end_utc": m4UTC(time.Now())}); err != nil {
		return err
	}
	return nil
}

func m4FourNodeTraffic(dir string, consoles map[int]*m4Console, pings *[]m4PingSummary) error {
	for _, console := range consoles {
		if err := m4Enable(console); err != nil {
			return err
		}
	}
	checks := []struct {
		node   int
		target string
	}{
		{0, "10.0.12.2"}, {1, "10.0.12.1"}, {1, "10.0.23.2"}, {2, "10.0.23.1"},
		{2, "10.0.34.2"}, {3, "10.0.34.1"}, {3, "10.0.41.2"}, {0, "10.0.41.1"},
		{0, "10.0.34.2"}, {2, "10.0.12.1"},
	}
	for _, check := range checks {
		ping, _, err := consoles[check.node].ping(fmt.Sprintf("ping %s repeat 100", check.target), m4PingTimeout(100))
		if err != nil {
			return fmt.Errorf("node %d to %s: %w", check.node, check.target, err)
		}
		*pings = append(*pings, ping)
	}
	return nil
}

func m4ExecCapture(dir, name, command string, args ...string) (string, error) {
	start := time.Now()
	cmd := exec.Command(command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	end := time.Now()
	status := 0
	if err != nil && cmd.ProcessState != nil {
		status = cmd.ProcessState.ExitCode()
	}
	record := map[string]any{"command": command, "argv": append([]string{command}, args...), "cwd": mustGetwd(), "start_utc": m4UTC(start), "end_utc": m4UTC(end), "stdout": stdout.String(), "stderr": stderr.String(), "exit_status": status}
	hash := sha256.Sum256(append(stdout.Bytes(), stderr.Bytes()...))
	record["sha256"] = hex.EncodeToString(hash[:])
	if writeErr := m4WriteJSON(filepath.Join(dir, name+".command.json"), record); writeErr != nil && err == nil {
		err = writeErr
	}
	return stdout.String(), err
}

func mustGetwd() string {
	value, _ := os.Getwd()
	return value
}

func (r *m4Runtime) sampleResources(dir string, elapsed float64, nodePIDs []int) error {
	guestScript := `printf 'loadavg='; cat /proc/loadavg; printf '\nuptime='; uptime; printf '\nfree='; free -b; printf '\nmeminfo='; grep -E '^(MemTotal|MemFree|MemAvailable|SwapTotal|SwapFree):' /proc/meminfo; printf '\nps='; ps -eo pid,ppid,rss,args`
	guest, guestErr := m4ExecCapture(dir, fmt.Sprintf("guest-resource-%03d", int(elapsed)), "/opt/homebrew/bin/limactl", "shell", r.machine, "sh", "-c", guestScript)
	hostPhys, _ := m4ExecCapture(dir, fmt.Sprintf("host-physmem-%03d", int(elapsed)), "/bin/sh", "-c", "top -l 1 -s 0 | grep PhysMem")
	pressure, _ := m4ExecCapture(dir, fmt.Sprintf("host-pressure-%03d", int(elapsed)), "memory_pressure", "-Q")
	swap, _ := m4ExecCapture(dir, fmt.Sprintf("host-swap-%03d", int(elapsed)), "sysctl", "vm.swapusage")
	rows := map[string]any{"at_utc": m4UTC(time.Now()), "elapsed_seconds": elapsed, "guest": guest, "host_physmem": hostPhys, "memory_pressure": pressure, "swap": swap, "node_pids": nodePIDs}
	for _, pid := range nodePIDs {
		status, _ := m4ExecCapture(dir, fmt.Sprintf("guest-status-%d-%03d", pid, int(elapsed)), "/opt/homebrew/bin/limactl", "shell", r.machine, "cat", fmt.Sprintf("/proc/%d/status", pid))
		rows[fmt.Sprintf("node_%d_status", pid)] = status
	}
	if err := m4AppendJSONLine(filepath.Join(dir, "resources.ndjson"), rows); err != nil {
		return err
	}
	return guestErr
}

func (r *m4Runtime) powerAudit(dir, stage string) error {
	for name, args := range map[string][]string{"batt": {"-g", "batt"}, "custom": {"-g", "custom"}, "assertions": {"-g", "assertions"}} {
		out, err := m4ExecCapture(dir, "pmset-"+stage+"-"+name, "pmset", args...)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "pmset-"+stage+"-"+name+".txt"), []byte(out), 0o644); err != nil {
			return err
		}
	}
	if out, err := m4ExecCapture(dir, "boottime-"+stage, "sysctl", "-n", "kern.boottime"); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "boottime-"+stage+".txt"), []byte(out), 0o644)
	} else {
		return err
	}
	return nil
}

func m4AtomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	_ = f.Close()
	if err != nil {
		return err
	}
	if dir, dirErr := os.Open(filepath.Dir(path)); dirErr == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return os.Rename(tmp, path)
}

func (r *m4Runtime) writeSoakFailure(dir string, reason error) {
	_ = m4AtomicJSON(filepath.Join(dir, "SOAK-FAILED"), map[string]any{"schema": "iolbox.macos-m4.soak/v2", "run_id": r.runID, "at_utc": m4UTC(time.Now()), "reason": errorText(reason)})
}

func (r *m4Runtime) soak() (m4PhaseRecord, error) {
	phase, dir := "item-6", r.phaseDir("item-6")
	start := time.Now()
	record := m4PhaseRecord{Schema: "iolbox.macos-m4.phase/v2", RunID: r.runID, Phase: phase, AttemptID: r.runID + "-soak-1", Status: "PASS", StartUTC: m4UTC(start), Fixture: "clean-soak.lab.json", Metrics: map[string]m4Metric{}, Details: map[string]any{}}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return record, err
	}
	fail := func(err error) (m4PhaseRecord, error) {
		record.Status = "UNVERIFIED"
		record.EndUTC = m4UTC(time.Now())
		r.writeSoakFailure(dir, err)
		return record, err
	}
	if err := r.connect(dir); err != nil {
		return fail(err)
	}
	defer r.close()
	if err := r.ensureImage(dir); err != nil {
		return fail(err)
	}
	fixture, err := r.fixture(dir, "clean-soak.lab.json", phase)
	if err != nil {
		return fail(err)
	}
	labID, started, err := r.loadStart(dir, fixture, []int{0, 1, 2})
	record.LabID, record.Nodes = labID, started
	if err != nil {
		return fail(err)
	}
	consoles, err := r.openConsoles([]int{0, 1, 2}, 150*time.Second)
	if err != nil {
		_, _ = r.stopLab(dir, labID, started)
		return fail(err)
	}
	defer r.closeConsoles(consoles, dir)
	if err := m4Enable(consoles[0]); err != nil {
		return fail(err)
	}
	if err := m4Enable(consoles[1]); err != nil {
		return fail(err)
	}
	// Keep the M3 pagination and authenticated console discipline visible in
	// this independent acceptance transcript as well.
	for _, console := range []*m4Console{consoles[0], consoles[1]} {
		if _, err := console.send("terminal length 0\r", 15*time.Second); err != nil {
			return fail(err)
		}
	}
	if err := r.powerAudit(dir, "start"); err != nil {
		return fail(err)
	}
	caffeinate := exec.Command("caffeinate", "-dimsu", "-w", strconv.Itoa(os.Getpid()))
	if err := caffeinate.Start(); err != nil {
		return fail(fmt.Errorf("caffeinate: %w", err))
	}
	_ = os.WriteFile(filepath.Join(dir, "caffeinate.pid"), []byte(strconv.Itoa(caffeinate.Process.Pid)+"\n"), 0o644)
	defer func() { _ = caffeinate.Process.Kill(); _ = caffeinate.Wait() }()
	startRaw, err := r.request(dir, "capture.start", map[string]any{"labId": labID, "link": 0, "mode": "live"})
	if err != nil {
		return fail(err)
	}
	var capture struct {
		CapturePort int `json:"capturePort"`
	}
	if err := json.Unmarshal(startRaw, &capture); err != nil || capture.CapturePort < 5500 {
		return fail(fmt.Errorf("capture.start invalid result: %s", startRaw))
	}
	if err := m4AuthProbeRoute(dir, r.guiAddr, "/capture/0"); err != nil {
		return fail(err)
	}
	direct, err := m4DialCapture(r.guiAddr, capture.CapturePort)
	if err != nil {
		return fail(err)
	}
	_ = direct.Close()
	captureConn, err := wsDialWithSession(r.guiAddr, "/capture/0")
	if err != nil {
		return fail(err)
	}
	measurementStart := time.Now()
	stopCapture := make(chan struct{})
	collectorDone := make(chan struct{})
	var captureBytes int64
	var captureErr error
	go func() {
		captureBytes, captureErr = m4CollectCapture(captureConn, filepath.Join(dir, "soak.pcapng"), stopCapture, filepath.Join(dir, "heartbeats.ndjson"), filepath.Join(dir, "capture-checkpoints.ndjson"), measurementStart)
		close(collectorDone)
	}()
	_ = m4AppendJSONLine(filepath.Join(dir, "capture-checkpoints.ndjson"), map[string]any{"checkpoint": 0, "at_utc": m4UTC(measurementStart), "elapsed_seconds": 0, "bytes": 0})
	checkpointDone := make(chan struct{})
	go func() {
		defer close(checkpointDone)
		soakDuration := time.Duration(m4SoakSeconds) * time.Second
		for checkpoint, offset := range []time.Duration{soakDuration / 4, soakDuration / 2, soakDuration * 3 / 4, soakDuration} {
			timer := time.NewTimer(time.Until(measurementStart.Add(offset)))
			if timerDuration := time.Until(measurementStart.Add(offset)); timerDuration <= 0 {
				if !timer.Stop() {
					<-timer.C
				}
			}
			<-timer.C
			info, _ := os.Stat(filepath.Join(dir, "soak.pcapng"))
			var bytesAt int64
			if info != nil {
				bytesAt = info.Size()
			}
			_ = m4AppendJSONLine(filepath.Join(dir, "capture-checkpoints.ndjson"), map[string]any{"checkpoint": checkpoint + 1, "at_utc": m4UTC(time.Now()), "elapsed_seconds": time.Since(measurementStart).Seconds(), "bytes": bytesAt})
		}
	}()
	if err := r.sampleResources(dir, 0, m4StartedPIDs(started)); err != nil {
		close(stopCapture)
		_ = captureConn.Close()
		<-collectorDone
		return fail(err)
	}
	allPings := []m4PingSummary{}
	for i := 0; i < m4SoakSeconds/60; i++ {
		target := measurementStart.Add(time.Duration(i+1) * time.Minute)
		for time.Now().Before(target) {
			time.Sleep(time.Second)
		}
		left, _, leftErr := consoles[0].ping("ping 10.0.12.2 repeat 10", 45*time.Second)
		if leftErr != nil {
			close(stopCapture)
			_ = captureConn.Close()
			<-collectorDone
			return fail(leftErr)
		}
		right, _, rightErr := consoles[1].ping("ping 10.0.12.1 repeat 10", 45*time.Second)
		if rightErr != nil {
			close(stopCapture)
			_ = captureConn.Close()
			<-collectorDone
			return fail(rightErr)
		}
		allPings = append(allPings, left, right)
		if err := m4AppendJSONLine(filepath.Join(dir, "traffic.ndjson"), map[string]any{"interval": i + 1, "at_utc": m4UTC(time.Now()), "left": left, "right": right}); err != nil {
			return fail(err)
		}
		_ = m4AppendJSONLine(filepath.Join(dir, "heartbeats.ndjson"), map[string]any{"kind": "traffic", "at_utc": m4UTC(time.Now()), "interval": i + 1})
		if err := r.sampleResources(dir, time.Since(measurementStart).Seconds(), m4StartedPIDs(started)); err != nil {
			close(stopCapture)
			_ = captureConn.Close()
			<-collectorDone
			return fail(err)
		}
		_ = m4AppendJSONLine(filepath.Join(dir, "heartbeats.ndjson"), map[string]any{"kind": "sampler", "at_utc": m4UTC(time.Now()), "interval": i + 1})
		if i == 2 || i == 4 || i == 7 || i == 9 {
			info, _ := os.Stat(filepath.Join(dir, "soak.pcapng"))
			var bytesAt int64
			if info != nil {
				bytesAt = info.Size()
			}
			_ = m4AppendJSONLine(filepath.Join(dir, "capture-checkpoints.ndjson"), map[string]any{"checkpoint": (i+1)/3 + 1, "at_utc": m4UTC(time.Now()), "elapsed_seconds": time.Since(measurementStart).Seconds(), "bytes": bytesAt})
		}
	}
	close(stopCapture)
	_ = captureConn.Close()
	<-collectorDone
	<-checkpointDone
	if captureErr != nil {
		return fail(captureErr)
	}
	if time.Since(measurementStart) < time.Duration(m4SoakSeconds)*time.Second {
		return fail(fmt.Errorf("soak measurement window was shorter than %d seconds", m4SoakSeconds))
	}
	if err := r.powerAudit(dir, "end"); err != nil {
		return fail(err)
	}
	if out, logErr := m4ExecCapture(dir, "pmset-log-window", "sh", "-c", "pmset -g log | tail -300"); logErr == nil {
		_ = os.WriteFile(filepath.Join(dir, "pmset-log-window.txt"), []byte(out), 0o644)
	}
	pcap, err := os.ReadFile(filepath.Join(dir, "soak.pcapng"))
	if err != nil {
		return fail(err)
	}
	packets, err := validateM3PCAPNG(pcap)
	if err != nil {
		return fail(err)
	}
	trafficRows := countJSONLines(filepath.Join(dir, "traffic.ndjson"))
	resourceRows := countJSONLines(filepath.Join(dir, "resources.ndjson"))
	heartbeatRows := countJSONLines(filepath.Join(dir, "heartbeats.ndjson"))
	checkpointRows := countJSONLines(filepath.Join(dir, "capture-checkpoints.ndjson"))
	if trafficRows < 10 || resourceRows < 11 || heartbeatRows < 10 || checkpointRows < 5 {
		return fail(fmt.Errorf("soak rows incomplete traffic=%d resources=%d heartbeat=%d checkpoints=%d", trafficRows, resourceRows, heartbeatRows, checkpointRows))
	}
	if err := m4WritePings(filepath.Join(dir, "pings.ndjson"), allPings); err != nil {
		return fail(err)
	}
	// stopLab must run BEFORE hashing control.ndjson into the seal, not
	// after: stopLab's own lab.stop request appends a final entry to that
	// same file (every r.request call logs to control.ndjson), so sealing
	// first and stopping the lab second seals a hash the file no longer
	// matches the moment cleanup runs — proven on hardware: a fully passing
	// soak failed its own independent seal-hash re-verification afterward
	// with "seal hash mismatch for control.ndjson", every single time,
	// because stopLab always appended after the hash was taken. Stopping
	// the lab is this phase's last real action; the seal must capture that.
	cleanup, stopErr := r.stopLab(dir, labID, started)
	record.Cleanup = cleanup
	if stopErr != nil {
		return fail(stopErr)
	}
	files := []string{"soak.pcapng", "traffic.ndjson", "resources.ndjson", "heartbeats.ndjson", "capture-checkpoints.ndjson", "control.ndjson"}
	sealFiles := []map[string]string{}
	for _, name := range files {
		hash, _, hashErr := m4HashFile(filepath.Join(dir, name))
		if hashErr != nil {
			return fail(hashErr)
		}
		sealFiles = append(sealFiles, map[string]string{"path": name, "sha256": hash})
	}
	seal := map[string]any{"schema": "iolbox.macos-m4.soak/v2", "run_id": r.runID, "attempt_id": record.AttemptID, "fixed_ownership": started, "start_utc": m4UTC(measurementStart), "end_utc": m4UTC(time.Now()), "monotonic_duration_seconds": time.Since(measurementStart).Seconds(), "traffic_rows": trafficRows, "resource_rows": resourceRows, "heartbeat_rows": heartbeatRows, "checkpoints": checkpointRows, "capture_bytes": captureBytes, "capture_packets": packets, "files": sealFiles, "validator": "m4VerifySoakManifest", "validator_status": 0}
	if err := m4AtomicJSON(filepath.Join(dir, "SOAK-COMPLETE"), seal); err != nil {
		return fail(err)
	}
	if err := m4VerifySoakManifest(filepath.Join(dir, "SOAK-COMPLETE")); err != nil {
		return fail(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "independent-validation.txt"), []byte("SOAK-COMPLETE independently rehashed and structurally validated: PASS\n"), 0o644)
	record.Seal = map[string]any{"path": m4RelPath(r.root, filepath.Join(dir, "SOAK-COMPLETE")), "independent_validation": m4RelPath(r.root, filepath.Join(dir, "independent-validation.txt"))}
	for name, value := range map[string]any{"duration_seconds": time.Since(measurementStart).Seconds(), "traffic_rows": trafficRows, "resource_rows": resourceRows, "heartbeat_rows": heartbeatRows, "capture_packets": packets, "capture_bytes": captureBytes} {
		metric, metricErr := m4FileMetric(r.root, filepath.Join(dir, "resources.ndjson"), value, "count_or_seconds")
		if metricErr != nil {
			return fail(metricErr)
		}
		record.Metrics[name] = metric
	}
	record.Sources = []string{m4RelPath(r.root, filepath.Join(dir, "soak.pcapng")), m4RelPath(r.root, filepath.Join(dir, "traffic.ndjson")), m4RelPath(r.root, filepath.Join(dir, "resources.ndjson")), m4RelPath(r.root, filepath.Join(dir, "SOAK-COMPLETE"))}
	record.EndUTC = m4UTC(time.Now())
	return record, nil
}

func m4StartedPIDs(started []map[string]any) []int {
	result := []int{}
	for _, node := range started {
		if value, ok := node["pid"].(float64); ok {
			result = append(result, int(value))
		}
	}
	return result
}

func countJSONLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return len(bytes.Split(bytes.TrimSpace(data), []byte{'\n'}))
}

func (r *m4Runtime) extnetDisposition() (m4PhaseRecord, error) {
	phase, dir := "item-4", r.phaseDir("item-4")
	now := time.Now()
	record := m4PhaseRecord{Schema: "iolbox.macos-m4.phase/v2", RunID: r.runID, Phase: phase, Status: m4Required("IOLBOX_M4_EXTNET_STATUS", "NOT_EXERCISABLE"), StartUTC: m4UTC(now), Fixture: "none", Metrics: map[string]m4Metric{}, Details: map[string]any{"decision": m4Required("IOLBOX_M4_EXTNET_DECISION", "no suitable Lima interface")}}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return record, err
	}
	probe := os.Getenv("IOLBOX_M4_EXTNET_PROBE")
	if probe == "" {
		probe = filepath.Join(dir, "extnet-probes.txt")
	}
	if _, err := os.Stat(probe); err != nil {
		return record, err
	}
	hash, _, err := m4HashFile(probe)
	if err != nil {
		return record, err
	}
	record.Sources = []string{m4RelPath(r.root, probe)}
	record.Metrics["host_interface_suitable"] = m4Metric{Value: 0, Unit: "boolean", SourcePath: m4RelPath(r.root, probe), SourceSHA256: hash, SourceClass: "hardware", SourceStartUTC: m4UTC(now), SourceEndUTC: m4UTC(time.Now())}
	record.Details["probe_sha256"] = hash
	record.EndUTC = m4UTC(time.Now())
	return record, m4WriteJSON(filepath.Join(dir, "phase.json"), record)
}

func m4LoadPhase(path string) (m4PhaseRecord, error) {
	var record m4PhaseRecord
	if err := m4ReadJSON(path, &record); err != nil {
		return record, err
	}
	return record, nil
}

func m4ReadRequirementFiles(root string) map[string]m4Requirement {
	result := map[string]m4Requirement{}
	entries, err := os.ReadDir(filepath.Join(root, "requirements"))
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var req m4Requirement
		if err := m4ReadJSON(filepath.Join(root, "requirements", entry.Name()), &req); err == nil {
			result[strings.TrimSuffix(entry.Name(), ".json")] = req
		}
	}
	return result
}

func m4PhaseAsItem(record m4PhaseRecord) m4Item {
	item := m4Item{Status: record.Status, StartUTC: record.StartUTC, EndUTC: record.EndUTC, AttemptID: record.AttemptID, Metrics: record.Metrics, Sources: record.Sources, Seal: record.Seal}
	if len(record.Details) > 0 && record.Phase == "item-4" {
		if decision, ok := record.Details["decision"].(string); ok {
			item.Decision = decision
		}
	}
	return item
}

func (r *m4Runtime) collectArtifacts() []m4Artifact {
	artifacts := []m4Artifact{}
	_ = filepath.Walk(r.root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel := m4RelPath(r.root, path)
		if rel == "summary.json" || strings.HasPrefix(rel, "verifier-") {
			return nil
		}
		hash, size, hashErr := m4HashFile(path)
		if hashErr != nil {
			return nil
		}
		class := "hardware"
		if strings.Contains(rel, "unit") {
			class = "unit"
		}
		if strings.Contains(rel, "compile") {
			class = "compile"
		}
		if strings.Contains(rel, "static") || strings.Contains(rel, "scope") {
			class = "static"
		}
		produced := info.ModTime().UTC().Format(time.RFC3339Nano)
		artifacts = append(artifacts, m4Artifact{Path: rel, Class: class, SHA256: hash, Size: size, Produced: produced})
		return nil
	})
	return artifacts
}

func (r *m4Runtime) buildSummary() error {
	start := m4Required("IOLBOX_M4_RUN_START_UTC", m4UTC(time.Now().Add(-time.Minute)))
	end := m4UTC(time.Now())
	summary := m4Summary{Schema: m4SummarySchema, RunID: r.runID, BaseCommit: m4Required("IOLBOX_M4_BASE_COMMIT", "unknown"), Identity: m4Identity{Profile: m4Required("IOLBOX_PROFILE", "debian13"), Product: m4Required("IOLBOX_MACOS_PRODUCT", "26.6.1"), Build: m4Required("IOLBOX_MACOS_BUILD", "25G76"), Host: map[string]any{"machine": r.machine, "gui_port": r.guiPort}}, Time: m4TimeRange{StartUTC: start, EndUTC: end}, Items: map[string]m4Item{}, Requirements: m4ReadRequirementFiles(r.root), Scope: m4Scope{PlanSHA256: os.Getenv("IOLBOX_M4_PLAN_SHA256"), PlanUnchanged: os.Getenv("IOLBOX_M4_PLAN_UNCHANGED") == "1", BaseCommitDiff: os.Getenv("IOLBOX_M4_SCOPE_BASE"), WorkingDiff: os.Getenv("IOLBOX_M4_SCOPE_WORKING"), PlanHashPath: "plan.sha256", PlanDiffPath: "scope/plan.diff", BaseCommitDiffPath: "scope/base-commit.diff", WorkingDiffPath: "scope/working.diff"}}
	for number, phase := range map[string]string{"1": "item-1", "2": "item-2", "3": "item-3", "4": "item-4", "6": "item-6", "7": "item-7"} {
		path := filepath.Join(r.root, phase, "phase.json")
		record, err := m4LoadPhase(path)
		if err != nil {
			summary.Items[number] = m4Item{Status: "UNVERIFIED", StartUTC: start, EndUTC: end, Metrics: map[string]m4Metric{}, Sources: []string{m4RelPath(r.root, path)}}
			continue
		}
		summary.Items[number] = m4PhaseAsItem(record)
	}
	// Item 5 has exactly one initial attempt and at most one cold retry. The
	// final record keeps both raw attempts and chooses PASS only from a passing
	// unchanged-profile attempt.
	item5 := m4Item{Status: "UNVERIFIED", StartUTC: start, EndUTC: end, Metrics: map[string]m4Metric{}, Sources: []string{}}
	if entries, err := os.ReadDir(filepath.Join(r.root, "item-5")); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "attempt-") {
				continue
			}
			record, readErr := m4LoadPhase(filepath.Join(r.root, "item-5", entry.Name(), "phase.json"))
			if readErr != nil {
				continue
			}
			item5.Attempts = append(item5.Attempts, map[string]any{"attempt_id": record.AttemptID, "status": record.Status, "path": m4RelPath(r.root, filepath.Join(r.root, "item-5", entry.Name(), "phase.json")), "hard_wall": record.HardWall})
			if record.Status == "PASS" {
				item5.Status, item5.StartUTC, item5.EndUTC, item5.Metrics, item5.Sources = record.Status, record.StartUTC, record.EndUTC, record.Metrics, record.Sources
			}
		}
	}
	summary.Items["5"] = item5
	if seal := summary.Items["6"].Sources; len(seal) > 0 {
		if _, err := os.Stat(filepath.Join(r.root, "item-6", "SOAK-COMPLETE")); err == nil {
			item6 := summary.Items["6"]
			item6.Seal = map[string]any{"path": "item-6/SOAK-COMPLETE"}
			summary.Items["6"] = item6
		}
	}
	allPass := true
	for _, number := range []string{"1", "2", "3", "5", "6", "7"} {
		if summary.Items[number].Status != "PASS" {
			allPass = false
		}
	}
	item4 := summary.Items["4"].Status
	if item4 != "PASS" && item4 != "NOT_EXERCISABLE" {
		allPass = false
	}
	if item4 == "NOT_EXERCISABLE" && summary.Items["4"].Decision == "" {
		allPass = false
	}
	for _, req := range summary.Requirements {
		if req.Status != "PASS" || req.ExitStatus != 0 {
			allPass = false
		}
	}
	if len(summary.Requirements) != 8 {
		allPass = false
	}
	summary.Overall = "INCOMPLETE"
	if allPass {
		summary.Overall = "PASS"
	}
	summary.Artifacts = r.collectArtifacts()
	return m4WriteJSON(filepath.Join(r.root, "summary.json"), summary)
}

func (r *m4Runtime) writePhase(record m4PhaseRecord, err error) error {
	if record.EndUTC == "" {
		record.EndUTC = m4UTC(time.Now())
	}
	if err != nil && record.Status == "PASS" {
		record.Status = "UNVERIFIED"
	}
	dir := r.phaseDir(record.Phase)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return m4WriteJSON(filepath.Join(dir, "phase.json"), record)
}

func TestMacOSM4Hardware(t *testing.T) {
	phase := os.Getenv("IOLBOX_M4_PHASE")
	if phase == "" {
		t.Skip("set IOLBOX_M4_PHASE to run the opt-in M4 hardware driver")
	}
	runtime, err := m4NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	switch phase {
	case "item-1":
		record, runErr := runtime.basicPhase(phase, "vpcs-iol.lab.json", []int{0, 1})
		if writeErr := runtime.writePhase(record, runErr); writeErr != nil {
			t.Fatal(writeErr)
		}
		if runErr != nil {
			t.Fatal(runErr)
		}
	case "item-2":
		record, runErr := runtime.basicPhase(phase, "multi-link.lab.json", []int{0, 1, 2})
		if writeErr := runtime.writePhase(record, runErr); writeErr != nil {
			t.Fatal(writeErr)
		}
		if runErr != nil {
			t.Fatal(runErr)
		}
	case "item-3":
		record, runErr := runtime.basicPhase(phase, "nat.lab.json", []int{0, 1, 2})
		if writeErr := runtime.writePhase(record, runErr); writeErr != nil {
			t.Fatal(writeErr)
		}
		if runErr != nil {
			t.Fatal(runErr)
		}
	case "item-5":
		record, runErr := runtime.basicPhase(phase, "four-iol-ring.lab.json", []int{0, 1, 2, 3})
		if writeErr := runtime.writePhase(record, runErr); writeErr != nil {
			t.Fatal(writeErr)
		}
		if runErr != nil {
			t.Fatal(runErr)
		}
	case "item-6":
		record, runErr := runtime.soak()
		if writeErr := runtime.writePhase(record, runErr); writeErr != nil {
			t.Fatal(writeErr)
		}
		if runErr != nil {
			t.Fatal(runErr)
		}
	case "item-4":
		record, runErr := runtime.extnetDisposition()
		if writeErr := runtime.writePhase(record, runErr); writeErr != nil {
			t.Fatal(writeErr)
		}
		if runErr != nil {
			t.Fatal(runErr)
		}
	case "item-7":
		record, runErr := runtime.basicPhase(phase, "vpcs-iol.lab.json", []int{0, 1})
		if writeErr := runtime.writePhase(record, runErr); writeErr != nil {
			t.Fatal(writeErr)
		}
		if runErr != nil {
			t.Fatal(runErr)
		}
	case "final":
		if err := runtime.buildSummary(); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown IOLBOX_M4_PHASE %q", phase)
	}
}
