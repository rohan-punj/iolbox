package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const unmeasuredQualification = "UNMEASURED — CANARY REQUIRED"

const (
	exitOK         = 0
	exitUsage      = 1
	exitCanary     = 2
	exitPreflight  = 3
	exitApt        = 4
	exitVerify     = 5
	minFreeDiskGiB = 5
)

type launcherError struct {
	code int
	err  error
}

func (e *launcherError) Error() string { return e.err.Error() }
func (e *launcherError) Unwrap() error { return e.err }

func codedError(code int, format string, args ...any) error {
	return &launcherError{code: code, err: fmt.Errorf(format, args...)}
}

type macOSProfile struct {
	Name           string
	Role           string
	GuestLabel     string
	PinEnv         string
	YAMLTemplate   string
	MultiarchStep  string
	KernelHoldStep string
	KernelSeries   string
	ExpectedUnameR string
	// CanaryStep/InstallStep/VerifyStep are Phase-4-era per-profile guest step
	// overrides (native-arm64 needs its own canary/install/verify scripts
	// because the shared Rosetta-era 30-canary.sh/40-install-payload.sh/
	// 50-verify.sh hard-assert Rosetta translation). Empty is the pre-Phase-4
	// default: use canaryStep()/installStep()/verifyStep() below, which fall
	// back to the historical fixed filenames so every profiles.env row (and
	// every existing macOSProfile{} test literal) that never set these three
	// columns keeps its exact old behavior.
	CanaryStep  string
	InstallStep string
	VerifyStep  string
	PinPath     string
	TemplatePath string
	GuestDir     string
	AssetRoot    string
	ImageURL     string
	ImageDigest  string
	ImageBytes   int64
	CPUs         string
	Memory       string
	Disk         string
}

// canaryStep/installStep/verifyStep resolve the profile's guest step
// filenames, defaulting to the historical Rosetta-era fixed names when a
// profile (or a test literal) never set the Phase-4 override columns.
func (p macOSProfile) canaryStep() string {
	if p.CanaryStep != "" {
		return p.CanaryStep
	}
	return "30-canary.sh"
}

func (p macOSProfile) installStep() string {
	if p.InstallStep != "" {
		return p.InstallStep
	}
	return "40-install-payload.sh"
}

func (p macOSProfile) verifyStep() string {
	if p.VerifyStep != "" {
		return p.VerifyStep
	}
	return "50-verify.sh"
}

type qualification struct {
	Profile  string
	Product  string
	Build    string
	Verdict  string
	Status   string
	Evidence string
}

func (q qualification) String() string {
	if q.Verdict == "" {
		return unmeasuredQualification
	}
	return q.Verdict + " (" + q.Status + ")"
}

type profileTable struct {
	Profiles       map[string]macOSProfile
	Qualifications []qualification
	Default        string
}

func extractQuotedAssignment(data, name string) (string, error) {
	marker := name + "='"
	start := strings.Index(data, marker)
	if start < 0 {
		return "", fmt.Errorf("missing %s multiline assignment", name)
	}
	valueStart := start + len(marker)
	end := strings.IndexByte(data[valueStart:], '\'')
	if end < 0 {
		return "", fmt.Errorf("unterminated %s multiline assignment", name)
	}
	return data[valueStart : valueStart+end], nil
}

func splitTableRows(value string, wantFields int, label string) ([][]string, error) {
	var rows [][]string
	for lineNo, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) != wantFields {
			return nil, fmt.Errorf("%s row %d has %d fields, want %d", label, lineNo+1, len(fields), wantFields)
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		rows = append(rows, fields)
	}
	return rows, nil
}

// splitProfileRows tolerates the pre-Phase-4 9-field row shape (name|role|
// guest_label|pin_env|yaml_template|multiarch_step|kernel_hold_step|
// kernel_series|expected_uname_r) alongside the Phase-4 12-field shape that
// appends canary_step|install_step|verify_step. A 9-field row is padded with
// three empty trailing fields, which macOSProfile.canaryStep()/installStep()/
// verifyStep() then resolve to the historical fixed filenames.
func splitProfileRows(value, label string) ([][]string, error) {
	var rows [][]string
	for lineNo, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "|")
		switch len(fields) {
		case 9:
			fields = append(fields, "", "", "")
		case 12:
			// already the Phase-4 shape
		default:
			return nil, fmt.Errorf("%s row %d has %d fields, want 9 or 12", label, lineNo+1, len(fields))
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		rows = append(rows, fields)
	}
	return rows, nil
}

func parseProfilesEnv(data string) (profileTable, error) {
	profilesText, err := extractQuotedAssignment(data, "IOLBOX_PROFILE_TABLE")
	if err != nil {
		return profileTable{}, err
	}
	qualText, err := extractQuotedAssignment(data, "IOLBOX_QUALIFICATION_TABLE")
	if err != nil {
		return profileTable{}, err
	}
	profileRows, err := splitProfileRows(profilesText, "profile")
	if err != nil {
		return profileTable{}, err
	}
	qualRows, err := splitTableRows(qualText, 6, "qualification")
	if err != nil {
		return profileTable{}, err
	}

	table := profileTable{Profiles: make(map[string]macOSProfile)}
	defaults := 0
	for _, row := range profileRows {
		for i, field := range row[:8] {
			if field == "" {
				return profileTable{}, fmt.Errorf("profile %q field %d is empty", row[0], i+1)
			}
		}
		if _, exists := table.Profiles[row[0]]; exists {
			return profileTable{}, fmt.Errorf("duplicate profile %q", row[0])
		}
		p := macOSProfile{
			Name:           row[0],
			Role:           row[1],
			GuestLabel:     row[2],
			PinEnv:         row[3],
			YAMLTemplate:   row[4],
			MultiarchStep:  row[5],
			KernelHoldStep: row[6],
			KernelSeries:   row[7],
			ExpectedUnameR: row[8],
			CanaryStep:     row[9],
			InstallStep:    row[10],
			VerifyStep:     row[11],
		}
		if p.Role == "DEFAULT" {
			defaults++
			table.Default = p.Name
		}
		table.Profiles[p.Name] = p
	}
	if defaults != 1 {
		return profileTable{}, fmt.Errorf("profile table must contain exactly one DEFAULT row (found %d)", defaults)
	}
	for _, row := range qualRows {
		for i, field := range row {
			if field == "" {
				return profileTable{}, fmt.Errorf("qualification row for %q field %d is empty", row[0], i+1)
			}
		}
		if _, ok := table.Profiles[row[0]]; !ok {
			return profileTable{}, fmt.Errorf("qualification references unknown profile %q", row[0])
		}
		table.Qualifications = append(table.Qualifications, qualification{
			Profile: row[0], Product: row[1], Build: row[2], Verdict: row[3], Status: row[4], Evidence: row[5],
		})
	}
	return table, nil
}

func qualificationFor(table profileTable, profile, product, build string) qualification {
	for _, q := range table.Qualifications {
		if q.Profile == profile && q.Product == product && q.Build == build {
			return q
		}
	}
	return qualification{}
}

func parsePinEnv(data string) (map[string]string, error) {
	values := make(map[string]string)
	for lineNo, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Errorf("pin env line %d is not KEY=VALUE", lineNo+1)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		values[key] = value
	}
	return values, nil
}

var digestPattern = regexp.MustCompile(`^(sha256|sha512):([0-9a-fA-F]+)$`)

func validatePin(values map[string]string, pinPath string) (map[string]string, error) {
	url := values["IOLBOX_IMAGE_URL"]
	if !strings.HasPrefix(url, "https://") {
		return nil, codedError(exitUsage, "invalid or missing pinned image URL in %s", pinPath)
	}
	digest := values["IOLBOX_IMAGE_DIGEST"]
	if digest == "PIN-ME" {
		return nil, codedError(exitPreflight, "refusing to provision an unpinned image from %s", pinPath)
	}
	match := digestPattern.FindStringSubmatch(digest)
	if match == nil || (match[1] == "sha256" && len(match[2]) != 64) || (match[1] == "sha512" && len(match[2]) != 128) {
		return nil, codedError(exitUsage, "invalid algorithm-qualified image digest in %s", pinPath)
	}
	byteText := values["IOLBOX_IMAGE_BYTES"]
	imageBytes, err := strconv.ParseInt(byteText, 10, 64)
	if err != nil || imageBytes < 0 {
		return nil, codedError(exitUsage, "image byte count must be decimal in %s", pinPath)
	}
	for _, key := range []string{"IOLBOX_CPUS", "IOLBOX_MEMORY", "IOLBOX_DISK"} {
		if strings.TrimSpace(values[key]) == "" {
			return nil, codedError(exitUsage, "missing %s in %s", key, pinPath)
		}
	}
	return values, nil
}

func loadMacOSProfile(assetRoot, selected string) (profileTable, macOSProfile, error) {
	profilePath := filepath.Join(assetRoot, "lima", "profiles.env")
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return profileTable{}, macOSProfile{}, codedError(exitUsage, "read profile table: %v", err)
	}
	table, err := parseProfilesEnv(string(data))
	if err != nil {
		return profileTable{}, macOSProfile{}, codedError(exitUsage, "parse profile table: %v", err)
	}
	if selected == "" {
		selected = table.Default
	}
	p, ok := table.Profiles[selected]
	if !ok {
		return profileTable{}, macOSProfile{}, codedError(exitUsage, "unknown profile %q", selected)
	}
	p.AssetRoot = assetRoot
	p.PinPath = filepath.Join(assetRoot, "lima", p.PinEnv)
	p.TemplatePath = filepath.Join(assetRoot, "lima", p.YAMLTemplate)
	p.GuestDir = filepath.Join(assetRoot, "guest")
	pinData, err := os.ReadFile(p.PinPath)
	if err != nil {
		return profileTable{}, macOSProfile{}, codedError(exitUsage, "read pin file: %v", err)
	}
	values, err := validatePinMustKeepValues(string(pinData), p.PinPath)
	if err != nil {
		return profileTable{}, macOSProfile{}, err
	}
	p.ImageURL = values["IOLBOX_IMAGE_URL"]
	p.ImageDigest = values["IOLBOX_IMAGE_DIGEST"]
	p.ImageBytes, _ = strconv.ParseInt(values["IOLBOX_IMAGE_BYTES"], 10, 64)
	p.CPUs = values["IOLBOX_CPUS"]
	p.Memory = values["IOLBOX_MEMORY"]
	p.Disk = values["IOLBOX_DISK"]
	for _, path := range []string{p.TemplatePath, p.GuestDir, filepath.Join(p.GuestDir, "lib.sh"), filepath.Join(p.GuestDir, p.MultiarchStep), filepath.Join(p.GuestDir, p.KernelHoldStep), filepath.Join(p.GuestDir, p.canaryStep()), filepath.Join(p.GuestDir, p.installStep()), filepath.Join(p.GuestDir, p.verifyStep())} {
		if _, err := os.Stat(path); err != nil {
			return profileTable{}, macOSProfile{}, codedError(exitUsage, "profile asset is missing: %s", path)
		}
	}
	return table, p, nil
}

// loadProfileTableOnly parses profiles.env without validating any single
// profile's pin/guest assets. resolveProfileSelection needs the table (row
// names, DEFAULT) before it knows which row it is going to select; the
// selected row's pin/guest assets are validated afterwards by the normal
// loadMacOSProfile call.
func loadProfileTableOnly(assetRoot string) (profileTable, error) {
	profilePath := filepath.Join(assetRoot, "lima", "profiles.env")
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return profileTable{}, codedError(exitUsage, "read profile table: %v", err)
	}
	table, err := parseProfilesEnv(string(data))
	if err != nil {
		return profileTable{}, codedError(exitUsage, "parse profile table: %v", err)
	}
	return table, nil
}

func validatePinMustKeepValues(data, pinPath string) (map[string]string, error) {
	values, err := parsePinEnv(data)
	if err != nil {
		return nil, codedError(exitUsage, "parse pin file %s: %v", pinPath, err)
	}
	return validatePin(values, pinPath)
}
