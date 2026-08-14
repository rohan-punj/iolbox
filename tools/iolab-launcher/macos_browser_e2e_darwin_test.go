package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMacOSBrowserEquivalentE2E is intentionally opt-in. It drives the same
// HTTP and same-origin WebSocket endpoints as Safari/Chrome from the Mac host;
// the shell harness separately records whether /usr/bin/open produced a tab.
func TestMacOSBrowserEquivalentE2E(t *testing.T) {
	if os.Getenv("IOLBOX_M3_E2E") != "1" {
		t.Skip("set IOLBOX_M3_E2E=1 on the disposable Mac to run the hardware flow")
	}
	guiPort := defaultDarwinGUIPort
	if value := os.Getenv("IOLBOX_M3_GUI_PORT"); value != "" {
		if _, err := fmt.Sscanf(value, "%d", &guiPort); err != nil {
			t.Fatalf("invalid IOLBOX_M3_GUI_PORT %q: %v", value, err)
		}
	}
	ports, err := newDarwinPortContract(guiPort)
	if err != nil {
		t.Fatal(err)
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", ports.GUIPort)
	if status, err := m3GetRoot(baseURL); err != nil || status >= 500 {
		t.Fatalf("GET / status=%d err=%v", status, err)
	}

	imagePath := m3RequiredEnv(t, "IOLBOX_M3_IMAGE")
	yamlPath := m3RequiredEnv(t, "IOLBOX_M3_LAB_YAML")
	jsonPath := m3RequiredEnv(t, "IOLBOX_M3_LAB_JSON")
	rawYAML, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read lab YAML: %v", err)
	}
	rawJSON, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read lab JSON: %v", err)
	}

	control, err := dialControlWS(fmt.Sprintf("127.0.0.1:%d", ports.GUIPort))
	if err != nil {
		t.Fatalf("dial browser-equivalent control WebSocket: %v", err)
	}
	defer control.Close()
	if _, err := control.hello(); err != nil {
		t.Fatalf("control hello: %v", err)
	}

	imageFile, err := os.Open(imagePath)
	if err != nil {
		t.Fatalf("open IOL image: %v", err)
	}
	imageInfo, err := imageFile.Stat()
	if err != nil {
		imageFile.Close()
		t.Fatalf("stat IOL image: %v", err)
	}
	uploader := newHTTPImageUploader(baseURL)
	guestPath, err := uploader.upload(filepath.Base(imagePath), imageFile, imageInfo.ModTime().UnixNano())
	_ = imageFile.Close()
	if err != nil {
		t.Fatalf("browser-equivalent image upload: %v", err)
	}
	registeredRaw, err := control.request("image.register", map[string]string{"path": guestPath})
	if err != nil {
		t.Fatalf("image.register: %v", err)
	}
	var image struct {
		ID     string `json:"id"`
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(registeredRaw, &image); err != nil || image.ID == "" || image.SHA256 == "" {
		t.Fatalf("image.register result = %s / %v", registeredRaw, err)
	}

	// The server deliberately accepts conservative ASCII image basenames; the
	// path robustness criterion is exercised by the parent path, not by changing
	// that security policy. The two fixtures carry the same image placeholder.
	rawYAML = bytes.ReplaceAll(rawYAML, []byte("REPLACE_WITH_IMAGE_ID"), []byte(image.ID))
	rawJSON = bytes.ReplaceAll(rawJSON, []byte("REPLACE_WITH_IMAGE_ID"), []byte(image.ID))
	labID, ok := labDocID(string(rawYAML))
	if !ok {
		t.Fatal("lab YAML fixture has no safe id")
	}
	saved, err := control.request("lab.saveDoc", map[string]string{"lab": string(rawYAML)})
	if err != nil {
		t.Fatalf("lab.saveDoc: %v", err)
	}
	var savedResult struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(saved, &savedResult); err != nil || savedResult.ID != labID {
		t.Fatalf("lab.saveDoc result = %s / %v", saved, err)
	}
	listRaw, err := control.request("lab.listDocs", map[string]any{})
	if err != nil {
		t.Fatalf("lab.listDocs: %v", err)
	}
	var listed struct {
		Labs []string `json:"labs"`
	}
	if err := json.Unmarshal(listRaw, &listed); err != nil {
		t.Fatalf("decode lab.listDocs: %v", err)
	}
	if !containsRawLab(listed.Labs, labID) {
		t.Fatalf("lab.listDocs did not return %s", labID)
	}

	loadRaw, err := control.request("lab.load", struct {
		Lab json.RawMessage `json:"lab"`
	}{Lab: json.RawMessage(rawJSON)})
	if err != nil {
		t.Fatalf("lab.load: %v", err)
	}
	var loadResult struct {
		LabID    string   `json:"labId"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(loadRaw, &loadResult); err != nil {
		t.Fatalf("decode lab.load: %v", err)
	}
	if len(loadResult.Warnings) != 0 || loadResult.LabID == "" {
		t.Fatalf("lab.load result has warnings or no id: %s", loadRaw)
	}

	startRaw, err := control.request("lab.start", map[string]any{"labId": loadResult.LabID, "nodes": []int{0, 1}})
	if err != nil {
		t.Fatalf("lab.start: %v", err)
	}
	if m3FailedResult(startRaw) {
		t.Fatalf("lab.start returned failed nodes: %s", startRaw)
	}
	consoles := m3OpenConsoles(t, ports, []int{0, 1})
	for node, console := range consoles {
		t.Cleanup(func() { _ = console.conn.Close() })
		if console.prompt == "" {
			t.Fatalf("node %d did not expose an IOS prompt", node)
		}
	}
	// Disable pagination before running any command that can produce more than
	// one page of output (show version's banner routinely does). Without this,
	// IOS prints "--More--" and blocks further output until another keypress,
	// which the pager swallows rather than echoing as input -- so a later
	// command sent to a still-paginating console never even shows up as text.
	m3SendConcurrently(t, consoles, "terminal length 0\r")
	m3SendConcurrently(t, consoles, "show version\r")

	captureRaw, err := control.request("capture.start", map[string]any{"labId": loadResult.LabID, "link": 0, "mode": "live"})
	if err != nil {
		t.Fatalf("capture.start: %v", err)
	}
	var captureStart struct {
		CapturePort int `json:"capturePort"`
	}
	if err := json.Unmarshal(captureRaw, &captureStart); err != nil || captureStart.CapturePort < darwinCaptureStart || captureStart.CapturePort > darwinCaptureEnd {
		t.Fatalf("capture.start result = %s / %v", captureRaw, err)
	}
	// Lima's dynamic per-port forwarding notices a freshly-bound guest listener
	// via its own polling loop, so there is a brief window right after
	// capture.start's successful response where the host-side forward for
	// this specific port is not wired up yet even though the range rule is
	// structurally present -- retry rather than treat the first refusal as
	// fatal (same class of readiness gap as the console/control-socket races
	// above).
	var direct net.Conn
	captureDialDeadline := time.Now().Add(15 * time.Second)
	for {
		direct, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", captureStart.CapturePort), 2*time.Second)
		if err == nil {
			break
		}
		if time.Now().After(captureDialDeadline) {
			t.Fatalf("direct forwarded capture port %d: %v", captureStart.CapturePort, err)
		}
		time.Sleep(time.Second)
	}
	_ = direct.Close()

	captureConn, err := wsDialWithSession(fmt.Sprintf("127.0.0.1:%d", ports.GUIPort), "/capture/0")
	if err != nil {
		t.Fatalf("dial /capture/0: %v", err)
	}
	captureDone := make(chan []byte, 1)
	go func() { captureDone <- m3CollectBinary(captureConn, 30*time.Second) }()
	m3SendConcurrently(t, consoles, "ping 10.0.12.2\r")
	pcap := <-captureDone
	_ = captureConn.Close()
	packets, err := validateM3PCAPNG(pcap)
	if err != nil {
		t.Fatalf("pcapng validation: %v", err)
	}
	capturePath := os.Getenv("IOLBOX_M3_CAPTURE_PATH")
	if capturePath == "" {
		capturePath = filepath.Join(filepath.Dir(yamlPath), "M3 capture café.pcapng")
	}
	if err := os.MkdirAll(filepath.Dir(capturePath), 0o755); err != nil {
		t.Fatalf("create capture parent: %v", err)
	}
	if err := os.WriteFile(capturePath, pcap, 0o644); err != nil {
		t.Fatalf("write capture %s: %v", capturePath, err)
	}
	hash := sha256.Sum256(pcap)
	t.Logf("pcapng path=%s bytes=%d packets=%d capturePort=%d sha256=%s", capturePath, len(pcap), packets, captureStart.CapturePort, hex.EncodeToString(hash[:]))

	if _, err := control.request("lab.stop", map[string]any{"labId": loadResult.LabID, "nodes": nil}); err != nil {
		t.Fatalf("lab.stop: %v", err)
	}
	reloadRaw, err := control.request("lab.load", struct {
		Lab json.RawMessage `json:"lab"`
	}{Lab: json.RawMessage(rawJSON)})
	if err != nil {
		t.Fatalf("lab reload: %v", err)
	}
	var reload struct {
		LabID    string   `json:"labId"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(reloadRaw, &reload); err != nil || reload.LabID == "" || len(reload.Warnings) != 0 {
		t.Fatalf("lab reload result = %s / %v", reloadRaw, err)
	}
	t.Logf("browser-equivalent HTTP/WS flow passed for lab %s (reload %s)", labID, reload.LabID)
}

func m3RequiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required when IOLBOX_M3_E2E=1", name)
	}
	return value
}

func m3GetRoot(baseURL string) (int, error) {
	resp, err := http.Get(baseURL + "/")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func containsRawLab(docs []string, id string) bool {
	for _, doc := range docs {
		if got, ok := labDocID(doc); ok && got == id {
			return true
		}
	}
	return false
}

func m3FailedResult(raw []byte) bool {
	var result struct {
		Failed []json.RawMessage `json:"failed"`
	}
	return json.Unmarshal(raw, &result) == nil && len(result.Failed) > 0
}

type m3Console struct {
	conn   net.Conn
	prompt string
}

// m3OpenConsoles dials each node's console and waits for a real EXEC prompt.
// A node reporting "running" from lab.start is not proof IOS has booted --
// full 17.18.02 boot has been measured at ~90s, and the console listener is
// not necessarily serving the instant the node starts: connecting too early
// gets a socket that is accepted then immediately closed, so the first read
// dies with EOF (see docs/macos-m1-handoff.md gotchas). Retry the whole
// connect+wait sequence against a boot-time budget instead of dialing once.
func m3OpenConsoles(t *testing.T, ports darwinPortContract, nodes []int) map[int]*m3Console {
	t.Helper()
	result := make(map[int]*m3Console, len(nodes))
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make(chan error, len(nodes))
	for _, node := range nodes {
		node := node
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, prompt, err := m3OpenConsoleWithRetry(fmt.Sprintf("127.0.0.1:%d", ports.GUIPort), node, 150*time.Second)
			if err != nil {
				errs <- fmt.Errorf("node %d: %w", node, err)
				return
			}
			mu.Lock()
			result[node] = &m3Console{conn: conn, prompt: prompt}
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	return result
}

// m3OpenConsoleWithRetry dials addr's /console/{node} and waits for a prompt.
// The dial itself is retried (an early dial can be accepted and then closed
// before IOL's console listener is really serving -- see the docs/macos-m1-
// handoff.md gotchas); once connected, m3ReadPrompt owns the boot-time wait.
func m3OpenConsoleWithRetry(addr string, node int, budget time.Duration) (net.Conn, string, error) {
	deadline := time.Now().Add(budget)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := wsDialWithSession(addr, fmt.Sprintf("/console/%d", node))
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		prompt, err := m3ReadPrompt(conn, time.Until(deadline))
		if err != nil {
			_ = conn.Close()
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		return conn, prompt, nil
	}
	return nil, "", fmt.Errorf("no prompt within %s: %w", budget, lastErr)
}

// m3ReadPrompt actively wakes the console rather than listening passively:
// IOS does not necessarily print a fresh prompt line on its own once boot
// completes, it prints one in response to input. hardware-m1.sh's proven
// console driver sends \r\n immediately on connect and re-pokes periodically
// while waiting (see its "wake the console" comment) for exactly this
// reason. Accepts either user-EXEC (">") or privileged-EXEC ("#") -- both
// show version and non-extended ping work from user EXEC, and IOL images
// ship with no enable secret, so requiring "#" specifically is unnecessary.
func m3ReadPrompt(conn net.Conn, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	poke := func() error {
		mask, err := newMaskKey()
		if err != nil {
			return err
		}
		return writeFrame(conn, wsOpBinary, []byte("\r\n"), &mask)
	}
	if err := poke(); err != nil {
		return "", fmt.Errorf("wake console: %w", err)
	}
	lastPoke := time.Now()
	var output bytes.Buffer
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		opcode, payload, err := readFrame(conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if time.Since(lastPoke) >= 5*time.Second {
					if err := poke(); err != nil {
						return "", fmt.Errorf("re-wake console: %w", err)
					}
					lastPoke = time.Now()
				}
				continue
			}
			return "", err
		}
		if opcode != wsOpBinary && opcode != wsOpText {
			continue
		}
		output.Write(payload)
		for _, line := range strings.Split(strings.ReplaceAll(output.String(), "\r", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if len(line) > 1 && (strings.HasSuffix(line, "#") || strings.HasSuffix(line, ">")) {
				return line, nil
			}
		}
	}
	return "", fmt.Errorf("timed out waiting for console prompt; received %d bytes", output.Len())
}

func m3SendConcurrently(t *testing.T, consoles map[int]*m3Console, input string) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, len(consoles))
	for node, console := range consoles {
		node, console := node, console
		wg.Add(1)
		go func() {
			defer wg.Done()
			mask, err := newMaskKey()
			if err != nil {
				errs <- fmt.Errorf("node %d mask: %w", node, err)
				return
			}
			if err := writeFrame(console.conn, wsOpBinary, []byte(input), &mask); err != nil {
				errs <- fmt.Errorf("node %d input: %w", node, err)
				return
			}
			// Telnet console echo often arrives one or two bytes per frame, so
			// the expected substring can straddle a frame boundary and never
			// appear within any single payload -- accumulate across reads
			// the same way m3ReadPrompt does, rather than checking each frame
			// in isolation.
			deadline := time.Now().Add(10 * time.Second)
			var output bytes.Buffer
			found := false
			for time.Now().Before(deadline) {
				_ = console.conn.SetReadDeadline(time.Now().Add(time.Second))
				opcode, payload, err := readFrame(console.conn)
				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						continue
					}
					errs <- fmt.Errorf("node %d output: %w", node, err)
					return
				}
				if opcode != wsOpBinary && opcode != wsOpText {
					continue
				}
				output.Write(payload)
				// A bare "contains a prompt character anywhere" check matches
				// mid-banner noise or a paginated "--More--" page before the
				// command has actually finished, which then strands the
				// console mid-pagination for whatever is sent next. Require
				// the LAST non-empty line specifically to be a fresh prompt,
				// the same completion signal m3ReadPrompt uses.
				lines := strings.Split(strings.ReplaceAll(output.String(), "\r", "\n"), "\n")
				for i := len(lines) - 1; i >= 0; i-- {
					line := strings.TrimSpace(lines[i])
					if line == "" {
						continue
					}
					if len(line) > 1 && (strings.HasSuffix(line, "#") || strings.HasSuffix(line, ">")) {
						found = true
					}
					break
				}
				if found {
					break
				}
			}
			if !found {
				errs <- fmt.Errorf("node %d returned no command output; received %d bytes", node, output.Len())
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func m3CollectBinary(conn net.Conn, timeout time.Duration) []byte {
	deadline := time.Now().Add(timeout)
	var output bytes.Buffer
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		opcode, payload, err := readFrame(conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			break
		}
		if opcode == wsOpBinary {
			output.Write(payload)
			if output.Len() > 48 {
				break
			}
		}
	}
	return output.Bytes()
}

func validateM3PCAPNG(data []byte) (int, error) {
	if len(data) <= 48 {
		return 0, fmt.Errorf("capture is only %d bytes", len(data))
	}
	packets := 0
	blocks := 0
	for offset := 0; offset < len(data); {
		if len(data)-offset < 12 {
			return packets, fmt.Errorf("truncated block header at offset %d", offset)
		}
		blockType := uint32LE(data[offset:])
		if blocks == 0 {
			if blockType != 0x0A0D0D0A {
				return packets, fmt.Errorf("bad section-header magic at offset %d", offset)
			}
			if uint32LE(data[offset+8:]) != 0x1A2B3C4D {
				return packets, fmt.Errorf("bad byte-order magic at offset %d", offset)
			}
		}
		length := int(uint32LE(data[offset+4:]))
		if length < 12 || length%4 != 0 || length > len(data)-offset {
			return packets, fmt.Errorf("invalid block length %d at offset %d", length, offset)
		}
		if uint32LE(data[offset+length-4:]) != uint32(length) {
			return packets, fmt.Errorf("block length trailer mismatch at offset %d", offset)
		}
		blocks++
		switch blockType {
		case 0x00000001:
			if length < 20 {
				return packets, fmt.Errorf("short Ethernet IDB")
			}
		case 0x00000006:
			if length < 32 || uint32LE(data[offset+20:]) == 0 {
				return packets, fmt.Errorf("empty enhanced packet block")
			}
			packets++
		case 0x00000003:
			if length < 16 || uint32LE(data[offset+8:]) == 0 {
				return packets, fmt.Errorf("empty simple packet block")
			}
			packets++
		}
		offset += length
	}
	if blocks < 2 || packets == 0 {
		return packets, fmt.Errorf("capture has %d blocks and %d packets", blocks, packets)
	}
	return packets, nil
}

func uint32LE(data []byte) uint32 {
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
}
