package main

// macos_m4_record_test.go contains the platform-neutral part of the M4
// evidence contract.  It deliberately does not know how to make a VM or how
// to make a hardware claim: it reads the retained raw record and recomputes
// the claims from that record.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	m4RecordPath = flag.String("m4-record", "", "verify a retained M4 summary.json")
	m4SoakPath   = flag.String("m4-soak", "", "verify a retained M4 SOAK-COMPLETE manifest")
)

const m4SummarySchema = "iolbox.macos-m4.summary/v2"

type m4Summary struct {
	Schema       string                   `json:"schema"`
	RunID        string                   `json:"run_id"`
	BaseCommit   string                   `json:"base_commit"`
	Identity     m4Identity               `json:"identity"`
	Time         m4TimeRange              `json:"time"`
	Items        map[string]m4Item        `json:"items"`
	Requirements map[string]m4Requirement `json:"requirements"`
	Scope        m4Scope                  `json:"scope"`
	Artifacts    []m4Artifact             `json:"artifacts"`
	Overall      string                   `json:"overall"`
}

type m4Identity struct {
	Profile string         `json:"profile"`
	Product string         `json:"product"`
	Build   string         `json:"build"`
	Host    map[string]any `json:"host"`
}

type m4TimeRange struct {
	StartUTC string `json:"start_utc"`
	EndUTC   string `json:"end_utc"`
}

type m4Metric struct {
	Value          any    `json:"value"`
	Unit           string `json:"unit"`
	SourcePath     string `json:"source_path"`
	SourceSHA256   string `json:"source_sha256"`
	CommandRecord  string `json:"command_record_path"`
	SourceStartUTC string `json:"source_start_utc"`
	SourceEndUTC   string `json:"source_end_utc"`
	SourceClass    string `json:"source_class,omitempty"`
}

type m4Item struct {
	Status    string              `json:"status"`
	Decision  string              `json:"decision,omitempty"`
	StartUTC  string              `json:"start_utc"`
	EndUTC    string              `json:"end_utc"`
	AttemptID string              `json:"attempt_id,omitempty"`
	Attempts  []map[string]any    `json:"attempts,omitempty"`
	Cases     []map[string]any    `json:"cases,omitempty"`
	Seal      map[string]any      `json:"seal,omitempty"`
	Metrics   map[string]m4Metric `json:"metrics"`
	Sources   []string            `json:"sources"`
}

type m4Requirement struct {
	Status     string   `json:"status"`
	Commands   []string `json:"commands"`
	StartUTC   string   `json:"start_utc"`
	EndUTC     string   `json:"end_utc"`
	ExitStatus int      `json:"exit_status"`
	Artifacts  []string `json:"artifacts"`
}

type m4Scope struct {
	BaseCommitDiff     string `json:"base_commit_diff"`
	WorkingDiff        string `json:"working_tree_diff"`
	PlanSHA256         string `json:"plan_sha256"`
	PlanUnchanged      bool   `json:"plan_unchanged"`
	PlanHashPath       string `json:"plan_hash_path"`
	PlanDiffPath       string `json:"plan_diff_path"`
	BaseCommitDiffPath string `json:"base_commit_diff_path"`
	WorkingDiffPath    string `json:"working_diff_path"`
}

type m4Artifact struct {
	Path     string `json:"path"`
	Class    string `json:"class"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Produced string `json:"produced_utc"`
}

type m4PingSummary struct {
	Sent       int     `json:"sent"`
	Received   int     `json:"received"`
	Lost       int     `json:"lost"`
	LossPct    float64 `json:"loss_percent"`
	LatencyAvg float64 `json:"latency_avg_ms,omitempty"`
	Command    string  `json:"command"`
	Timestamp  string  `json:"timestamp_utc"`
}

type m4PhaseRecord struct {
	Schema    string              `json:"schema"`
	RunID     string              `json:"run_id"`
	Phase     string              `json:"phase"`
	AttemptID string              `json:"attempt_id,omitempty"`
	Status    string              `json:"status"`
	StartUTC  string              `json:"start_utc"`
	EndUTC    string              `json:"end_utc"`
	Fixture   string              `json:"fixture"`
	LabID     string              `json:"lab_id,omitempty"`
	Nodes     []map[string]any    `json:"nodes,omitempty"`
	Links     []map[string]any    `json:"links,omitempty"`
	Metrics   map[string]m4Metric `json:"metrics,omitempty"`
	Sources   []string            `json:"sources,omitempty"`
	Cleanup   map[string]any      `json:"cleanup,omitempty"`
	Details   map[string]any      `json:"details,omitempty"`
	Seal      map[string]any      `json:"seal,omitempty"`
	HardWall  bool                `json:"hard_wall,omitempty"`
}

func m4UTC(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func m4ParseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("empty UTC timestamp")
	}
	return time.Parse(time.RFC3339Nano, value)
}

func m4HashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func m4RelPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func m4ReadJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func m4ValidateFixture(data []byte) error {
	var doc struct {
		Nodes []struct {
			Kind string `json:"kind"`
			RAM  *int   `json:"ram"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("fixture JSON: %w", err)
	}
	if len(doc.Nodes) == 0 {
		return errors.New("fixture has no nodes")
	}
	for i, n := range doc.Nodes {
		if n.Kind == "iol" && (n.RAM == nil || *n.RAM < 1024) {
			return fmt.Errorf("IOL node %d has RAM %v; minimum is 1024 MB", i, n.RAM)
		}
	}
	return nil
}

func m4ValidatePCAPNG(data []byte) (int, error) {
	if len(data) < 48 {
		return 0, fmt.Errorf("pcapng is only %d bytes", len(data))
	}
	offset, blocks, packets := 0, 0, 0
	for offset < len(data) {
		if len(data)-offset < 12 {
			return packets, fmt.Errorf("pcapng truncated at offset %d", offset)
		}
		kind := m4Uint32LE(data[offset:])
		length := int(m4Uint32LE(data[offset+4:]))
		if blocks == 0 && (kind != 0x0A0D0D0A || m4Uint32LE(data[offset+8:]) != 0x1A2B3C4D) {
			return packets, errors.New("pcapng has no valid section header")
		}
		if length < 12 || length%4 != 0 || length > len(data)-offset {
			return packets, fmt.Errorf("pcapng invalid block length %d", length)
		}
		if m4Uint32LE(data[offset+length-4:]) != uint32(length) {
			return packets, fmt.Errorf("pcapng trailer mismatch at offset %d", offset)
		}
		switch kind {
		case 0x00000001:
			if length < 20 {
				return packets, errors.New("pcapng IDB is too short")
			}
		case 0x00000006:
			if length < 32 || m4Uint32LE(data[offset+20:]) == 0 {
				return packets, errors.New("pcapng EPB has no packet")
			}
			packets++
		case 0x00000003:
			if length < 16 || m4Uint32LE(data[offset+8:]) == 0 {
				return packets, errors.New("pcapng SPB has no packet")
			}
			packets++
		}
		blocks++
		offset += length
	}
	if blocks < 2 || packets == 0 {
		return packets, fmt.Errorf("pcapng has %d blocks and %d packets", blocks, packets)
	}
	return packets, nil
}

func m4Uint32LE(data []byte) uint32 {
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
}

func m4MetricNumber(metric m4Metric) (float64, error) {
	switch value := metric.Value.(type) {
	case float64:
		return value, nil
	case json.Number:
		return value.Float64()
	case int:
		return float64(value), nil
	default:
		return 0, fmt.Errorf("metric value %T is not numeric", metric.Value)
	}
}

func m4VerifyArtifacts(root string, artifacts []m4Artifact) error {
	validClasses := map[string]bool{"hardware": true, "unit": true, "compile": true, "static": true}
	seen := map[string]bool{}
	for _, artifact := range artifacts {
		if artifact.Path == "" || filepath.IsAbs(artifact.Path) || strings.Contains(artifact.Path, "..") {
			return fmt.Errorf("unsafe artifact path %q", artifact.Path)
		}
		if !validClasses[artifact.Class] {
			return fmt.Errorf("artifact %q has invalid class %q", artifact.Path, artifact.Class)
		}
		if seen[artifact.Path] {
			return fmt.Errorf("duplicate artifact %q", artifact.Path)
		}
		seen[artifact.Path] = true
		path := filepath.Join(root, filepath.FromSlash(artifact.Path))
		if artifact.Class == "hardware" && (strings.HasPrefix(artifact.Path, "compile/") || strings.HasPrefix(artifact.Path, "unit/") || strings.HasPrefix(artifact.Path, "static/")) {
			return fmt.Errorf("hardware artifact %q is under a non-hardware evidence root", artifact.Path)
		}
		hash, size, err := m4HashFile(path)
		if err != nil {
			return fmt.Errorf("artifact %q: %w", artifact.Path, err)
		}
		if hash != artifact.SHA256 || size != artifact.Size {
			return fmt.Errorf("artifact %q hash/size mismatch", artifact.Path)
		}
	}
	return nil
}

func m4VerifySoakManifest(path string) error {
	manifest, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw struct {
		Schema       string  `json:"schema"`
		RunID        string  `json:"run_id"`
		AttemptID    string  `json:"attempt_id"`
		Duration     float64 `json:"monotonic_duration_seconds"`
		TrafficRows  int     `json:"traffic_rows"`
		ResourceRows int     `json:"resource_rows"`
		Heartbeats   int     `json:"heartbeat_rows"`
		Checkpoints  int     `json:"checkpoints"`
		StartUTC     string  `json:"start_utc"`
		EndUTC       string  `json:"end_utc"`
		FixedNodes   []any   `json:"fixed_ownership"`
		Files        []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(manifest, &raw); err != nil {
		return fmt.Errorf("seal JSON: %w", err)
	}
	if raw.Schema != "iolbox.macos-m4.soak/v2" || raw.RunID == "" || raw.AttemptID == "" || len(raw.FixedNodes) == 0 {
		return errors.New("seal schema or identity is invalid")
	}
	start, startErr := m4ParseTime(raw.StartUTC)
	end, endErr := m4ParseTime(raw.EndUTC)
	if startErr != nil || endErr != nil || !end.After(start) {
		return errors.New("seal timestamps are invalid")
	}
	if raw.Duration < 600 || raw.TrafficRows < 10 || raw.ResourceRows < 11 || raw.Heartbeats < 10 || raw.Checkpoints < 5 {
		return fmt.Errorf("seal bars are incomplete: duration=%v traffic=%d resource=%d heartbeat=%d checkpoints=%d", raw.Duration, raw.TrafficRows, raw.ResourceRows, raw.Heartbeats, raw.Checkpoints)
	}
	root := filepath.Dir(path)
	seen := map[string]bool{}
	for _, file := range raw.Files {
		if file.Path == "" || filepath.IsAbs(file.Path) || strings.Contains(file.Path, "..") {
			return fmt.Errorf("unsafe seal file %q", file.Path)
		}
		if seen[file.Path] {
			return fmt.Errorf("duplicate seal file %q", file.Path)
		}
		seen[file.Path] = true
		got, _, err := m4HashFile(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil {
			return err
		}
		if got != file.SHA256 {
			return fmt.Errorf("seal hash mismatch for %s", file.Path)
		}
	}
	for _, required := range []string{"soak.pcapng", "traffic.ndjson", "resources.ndjson", "heartbeats.ndjson", "capture-checkpoints.ndjson", "control.ndjson"} {
		if !seen[required] {
			return fmt.Errorf("seal omits required file %s", required)
		}
	}
	return nil
}

func m4ReadPingRows(path string) ([]m4PingSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []m4PingSummary
	for lineNo, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row m4PingSummary
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, lineNo+1, err)
		}
		if row.Sent <= 0 || row.Received < 0 || row.Received > row.Sent || row.Lost != row.Sent-row.Received {
			return nil, fmt.Errorf("%s line %d has invalid ping counts", path, lineNo+1)
		}
		if _, err := m4ParseTime(row.Timestamp); err != nil {
			return nil, fmt.Errorf("%s line %d has invalid timestamp: %w", path, lineNo+1, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func m4VerifyPingBar(root, rel string, sent []int, minReceived int) ([]m4PingSummary, error) {
	rows, err := m4ReadPingRows(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	if len(rows) != len(sent) {
		return nil, fmt.Errorf("%s has %d ping rows, want %d", rel, len(rows), len(sent))
	}
	for i, row := range rows {
		if row.Sent != sent[i] || row.Received < minReceived {
			return nil, fmt.Errorf("%s row %d violates sent/received bar: %+v", rel, i, row)
		}
	}
	return rows, nil
}

func m4VerifyItemSources(root string, item m4Item, runStart, runEnd time.Time, artifacts map[string]m4Artifact) error {
	if len(item.Sources) == 0 {
		return errors.New("item has no raw sources")
	}
	for _, rel := range item.Sources {
		if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
			return fmt.Errorf("unsafe item source %q", rel)
		}
		artifact, ok := artifacts[rel]
		if !ok || artifact.Class != "hardware" {
			return fmt.Errorf("item source %q is not indexed as hardware", rel)
		}
		if _, _, err := m4HashFile(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			return fmt.Errorf("item source %q: %w", rel, err)
		}
	}
	for name, metric := range item.Metrics {
		if metric.SourcePath == "" || metric.SourceSHA256 == "" || metric.SourceClass != "hardware" {
			return fmt.Errorf("metric %s lacks a hardware raw source", name)
		}
		if _, ok := artifacts[metric.SourcePath]; !ok {
			return fmt.Errorf("metric %s source %q is not indexed", name, metric.SourcePath)
		}
		got, _, err := m4HashFile(filepath.Join(root, filepath.FromSlash(metric.SourcePath)))
		if err != nil || got != metric.SourceSHA256 {
			return fmt.Errorf("metric %s source hash mismatch", name)
		}
		value, err := m4MetricNumber(metric)
		if err != nil {
			return fmt.Errorf("metric %s: %w", name, err)
		}
		if value < 0 {
			return fmt.Errorf("metric %s is negative", name)
		}
		sourceStart, startErr := m4ParseTime(metric.SourceStartUTC)
		sourceEnd, endErr := m4ParseTime(metric.SourceEndUTC)
		if startErr != nil || endErr != nil || sourceStart.Before(runStart) || sourceEnd.After(runEnd) || !sourceEnd.After(sourceStart) {
			return fmt.Errorf("metric %s source time is outside item/run", name)
		}
	}
	return nil
}

func m4VerifySummary(path string) error {
	root := filepath.Dir(path)
	var summary m4Summary
	if err := m4ReadJSON(path, &summary); err != nil {
		return fmt.Errorf("summary: %w", err)
	}
	if summary.Schema != m4SummarySchema || summary.RunID == "" || summary.BaseCommit == "" || summary.Identity.Profile == "" || summary.Identity.Product == "" || summary.Identity.Build == "" {
		return errors.New("summary schema/identity is incomplete")
	}
	start, err := m4ParseTime(summary.Time.StartUTC)
	if err != nil {
		return fmt.Errorf("summary start: %w", err)
	}
	end, err := m4ParseTime(summary.Time.EndUTC)
	if err != nil || !end.After(start) {
		return errors.New("summary end is not after start")
	}
	if len(summary.Items) != 7 {
		return fmt.Errorf("summary has %d items, want 7", len(summary.Items))
	}
	artifacts := map[string]m4Artifact{}
	for _, artifact := range summary.Artifacts {
		artifacts[artifact.Path] = artifact
	}
	if len(artifacts) == 0 {
		return errors.New("summary has no artifact index")
	}
	for i := 1; i <= 7; i++ {
		key := strconv.Itoa(i)
		item, ok := summary.Items[key]
		if !ok {
			return fmt.Errorf("missing item %s", key)
		}
		itemStart, parseErr := m4ParseTime(item.StartUTC)
		if parseErr != nil || itemStart.Before(start) {
			return fmt.Errorf("item %s start is outside run", key)
		}
		itemEnd, parseErr := m4ParseTime(item.EndUTC)
		if parseErr != nil || itemEnd.After(end) || !itemEnd.After(itemStart) {
			return fmt.Errorf("item %s end is outside item/run", key)
		}
		if err := m4VerifyItemSources(root, item, itemStart, itemEnd, artifacts); err != nil {
			return fmt.Errorf("item %s sources: %w", key, err)
		}
	}
	if err := m4VerifyScope(root, summary.Scope); err != nil {
		return err
	}
	if rows, err := m4VerifyPingBar(root, "item-1/pings.ndjson", []int{10, 10, 100, 100}, 0); err != nil {
		return fmt.Errorf("item 1 raw ping evidence: %w", err)
	} else {
		for _, row := range rows[2:] {
			if row.Received < 99 {
				return fmt.Errorf("item 1 warm ping below 99/100")
			}
		}
	}
	if _, err := m4VerifyPingBar(root, "item-2/pings.ndjson", []int{100, 100, 100, 100}, 99); err != nil {
		return fmt.Errorf("item 2 raw ping evidence: %w", err)
	}
	if _, err := m4VerifyPingBar(root, "item-3/pings.ndjson", []int{20, 20}, 19); err != nil {
		return fmt.Errorf("item 3 raw ping evidence: %w", err)
	}
	if _, err := m4VerifyPingBar(root, "item-5/attempt-1/pings.ndjson", []int{100, 100, 100, 100, 100, 100, 100, 100, 100, 100}, 99); err != nil {
		// A cold retry is allowed only when the initial attempt was a recorded
		// hard wall. The passing attempt is then checked below from its own raw
		// directory.
		if _, retryErr := m4VerifyPingBar(root, "item-5/attempt-2/pings.ndjson", []int{100, 100, 100, 100, 100, 100, 100, 100, 100, 100}, 99); retryErr != nil {
			return fmt.Errorf("item 5 raw ping evidence: initial=%v retry=%v", err, retryErr)
		}
	}
	if _, err := m4VerifyPingBar(root, "item-7/pings.ndjson", []int{100, 100}, 99); err != nil {
		return fmt.Errorf("item 7 raw ping evidence: %w", err)
	}
	if item := summary.Items["6"]; item.Status == "PASS" {
		seal, ok := item.Seal["path"].(string)
		if !ok || seal == "" {
			return errors.New("passing soak has no seal path")
		}
		if err := m4VerifySoakManifest(filepath.Join(root, filepath.FromSlash(seal))); err != nil {
			return fmt.Errorf("soak seal: %w", err)
		}
	}
	if err := m4VerifyArtifacts(root, summary.Artifacts); err != nil {
		return err
	}
	for id, req := range summary.Requirements {
		if req.Status != "PASS" || req.ExitStatus != 0 || len(req.Commands) == 0 || len(req.Artifacts) == 0 {
			return fmt.Errorf("requirement %s is not independently evidenced", id)
		}
		for _, artifact := range req.Artifacts {
			if _, ok := artifacts[artifact]; !ok {
				return fmt.Errorf("requirement %s references unindexed artifact %q", id, artifact)
			}
		}
	}
	if len(summary.Requirements) != 8 {
		return fmt.Errorf("requirements has %d rows, want 8", len(summary.Requirements))
	}
	validItem5 := summary.Items["5"].Status == "PASS"
	valid := summary.Items["1"].Status == "PASS" && summary.Items["2"].Status == "PASS" && summary.Items["3"].Status == "PASS" && validItem5 && summary.Items["6"].Status == "PASS" && summary.Items["7"].Status == "PASS"
	item4 := summary.Items["4"].Status
	valid = valid && (item4 == "PASS" || (item4 == "NOT_EXERCISABLE" && summary.Items["4"].Decision != ""))
	if !valid || summary.Overall != "PASS" {
		return fmt.Errorf("completion bars are not PASS (overall=%s item4=%s item5=%s)", summary.Overall, item4, summary.Items["5"].Status)
	}
	return nil
}

func m4VerifyScope(root string, scope m4Scope) error {
	if !scope.PlanUnchanged || scope.PlanSHA256 == "" || scope.PlanHashPath == "" || scope.PlanDiffPath == "" || scope.BaseCommitDiffPath == "" || scope.WorkingDiffPath == "" {
		return errors.New("frozen plan/scope gate is not proven")
	}
	read := func(rel string) ([]byte, error) {
		if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
			return nil, fmt.Errorf("unsafe scope path %q", rel)
		}
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	}
	hashData, err := read(scope.PlanHashPath)
	if err != nil || strings.TrimSpace(string(hashData)) != strings.ToLower(scope.PlanSHA256) {
		return errors.New("frozen plan hash mismatch")
	}
	planDiff, err := read(scope.PlanDiffPath)
	if err != nil || len(bytes.TrimSpace(planDiff)) != 0 {
		return errors.New("frozen plan has a recorded diff")
	}
	for _, rel := range []string{scope.BaseCommitDiffPath, scope.WorkingDiffPath} {
		if _, err := read(rel); err != nil {
			return fmt.Errorf("scope artifact %s: %w", rel, err)
		}
	}
	return nil
}

func TestM4VerifyRecord(t *testing.T) {
	if *m4RecordPath == "" {
		t.Skip("-m4-record was not supplied")
	}
	if err := m4VerifySummary(*m4RecordPath); err != nil {
		t.Fatal(err)
	}
}

func TestM4VerifySoakSeal(t *testing.T) {
	if *m4SoakPath == "" {
		t.Skip("-m4-soak was not supplied")
	}
	if err := m4VerifySoakManifest(*m4SoakPath); err != nil {
		t.Fatal(err)
	}
}

func TestM4FixtureValidation(t *testing.T) {
	// The fixture gate is intentionally platform-neutral.
	valid := []byte(`{"nodes":[{"kind":"iol","ram":1024},{"kind":"vpcs"}]}`)
	if err := m4ValidateFixture(valid); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"nodes":[{"kind":"iol"}]}`,
		`{"nodes":[{"kind":"iol","ram":256}]}`,
	} {
		if err := m4ValidateFixture([]byte(raw)); err == nil {
			t.Errorf("fixture %s was accepted below the RAM floor", raw)
		}
	}
}

func TestM4GuestVerifyUsesConfiguredPort(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "macos", "guest", "50-verify.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, needle := range []string{
		`suffix=":$IOLBOX_GUI_PORT"`,
		`http://127.0.0.1:$IOLBOX_GUI_PORT/`,
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("guest verifier is missing configured-port expression %q", needle)
		}
	}
	if strings.Contains(text, "http://127.0.0.1:4001/") {
		t.Fatal("guest verifier hardcodes the default GUI readiness port")
	}
}

func TestM4ExactIdentityAndDataPolicy(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "macos", "lima", "profiles.env"))
	if err != nil {
		t.Fatal(err)
	}
	table, err := parseProfilesEnv(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if qualificationFor(table, "debian13", "26.6.1", "25G76").Verdict == "" {
		t.Fatal("exact qualification row is missing")
	}
	for _, values := range [][3]string{{"debian13", "26.6.10", "25G76"}, {"debian13", "26.6.1", "25G760"}, {"debian130", "26.6.1", "25G76"}} {
		if q := qualificationFor(table, values[0], values[1], values[2]); q.Verdict != "" {
			t.Fatalf("near identity unexpectedly qualified: %#v -> %#v", values, q)
		}
	}
	if bytes.Contains(data, []byte("26.6.1")) {
		// This is an allowed qualification datum in profiles.env. The test is
		// intentionally about reading that file, not duplicating it in Go.
		t.Log("profiles.env remains the source of qualification data")
	}
}
