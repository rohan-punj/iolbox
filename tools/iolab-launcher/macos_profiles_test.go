package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseProfilesEnvAndExactQualification(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "macos", "lima", "profiles.env"))
	if err != nil {
		t.Fatal(err)
	}
	table, err := parseProfilesEnv(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if table.Default != "debian13" || len(table.Profiles) != 3 {
		t.Fatalf("default/profiles = %q/%d", table.Default, len(table.Profiles))
	}
	if table.Profiles["jammy"].ExpectedUnameR != "" {
		t.Errorf("jammy exact kernel field = %q, want empty", table.Profiles["jammy"].ExpectedUnameR)
	}
	q := qualificationFor(table, "jammy", "13.5", "22G74")
	if q.String() != "PASS (SUPPORTED)" {
		t.Fatalf("qualification = %q", q.String())
	}
	// 13.50 is numerically equal to 13.5 in a naive implementation, but it
	// is not the measured product string and must remain unmeasured.
	if got := qualificationFor(table, "jammy", "13.50", "22G74").String(); got != unmeasuredQualification {
		t.Fatalf("numeric-looking product unexpectedly qualified: %q", got)
	}
	if got := qualificationFor(table, "jammy", "13.5", "22G74-extra").String(); got != unmeasuredQualification {
		t.Fatalf("wrong build unexpectedly qualified: %q", got)
	}
}

func TestLoadShippedProfileResolvesReferencedAssets(t *testing.T) {
	root := filepath.Join("..", "..", "packaging", "macos")
	_, profile, err := loadMacOSProfile(root, "debian13")
	if err != nil {
		t.Fatal(err)
	}
	if profile.PinPath == "" || profile.TemplatePath == "" || profile.GuestDir == "" || profile.ImageDigest == "" {
		t.Fatalf("incomplete resolved profile: %+v", profile)
	}
	if profile.ImageBytes != 337313792 || profile.CPUs != "4" || profile.Memory != "4GiB" || profile.Disk != "15GiB" {
		t.Fatalf("pin values = %+v", profile)
	}
}

func TestParseProfilesRejectsMalformedTables(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"wrong profile field count", "IOLBOX_PROFILE_TABLE='a|DEFAULT|g|p|t|m|k|s'\nIOLBOX_QUALIFICATION_TABLE='a|1|2|P|S|e'", "profile row"},
		{"duplicate profile", "IOLBOX_PROFILE_TABLE='a|DEFAULT|g|p|t|m|k|s|\na|COMPATIBILITY|g|p|t|m|k|s|'\nIOLBOX_QUALIFICATION_TABLE='a|1|2|P|S|e'", "duplicate"},
		{"two defaults", "IOLBOX_PROFILE_TABLE='a|DEFAULT|g|p|t|m|k|s|\nb|DEFAULT|g|p|t|m|k|s|'\nIOLBOX_QUALIFICATION_TABLE='a|1|2|P|S|e'", "exactly one DEFAULT"},
		{"qualification field count", "IOLBOX_PROFILE_TABLE='a|DEFAULT|g|p|t|m|k|s|'\nIOLBOX_QUALIFICATION_TABLE='a|1|2|P|S'", "qualification row"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseProfilesEnv(tc.data)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestValidatePin(t *testing.T) {
	good := map[string]string{
		"IOLBOX_IMAGE_URL":    "https://example.invalid/image.qcow2",
		"IOLBOX_IMAGE_DIGEST": "sha256:" + strings.Repeat("a", 64),
		"IOLBOX_IMAGE_BYTES":  "123",
		"IOLBOX_CPUS":         "4",
		"IOLBOX_MEMORY":       "4GiB",
		"IOLBOX_DISK":         "15GiB",
	}
	if _, err := validatePin(good, "pin.env"); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]string){
		"bad URL":           func(v map[string]string) { v["IOLBOX_IMAGE_URL"] = "file:///tmp/x" },
		"bad digest hex":    func(v map[string]string) { v["IOLBOX_IMAGE_DIGEST"] = "sha256:" + strings.Repeat("z", 64) },
		"bad digest length": func(v map[string]string) { v["IOLBOX_IMAGE_DIGEST"] = "sha512:" + strings.Repeat("a", 64) },
		"bad bytes":         func(v map[string]string) { v["IOLBOX_IMAGE_BYTES"] = "12x" },
		"missing geometry":  func(v map[string]string) { delete(v, "IOLBOX_MEMORY") },
	} {
		t.Run(name, func(t *testing.T) {
			copyValues := make(map[string]string, len(good))
			for k, v := range good {
				copyValues[k] = v
			}
			mutate(copyValues)
			if _, err := validatePin(copyValues, "pin.env"); err == nil {
				t.Fatal("expected invalid pin to fail")
			}
		})
	}
	if _, err := validatePin(map[string]string{
		"IOLBOX_IMAGE_URL": "https://example.invalid/x", "IOLBOX_IMAGE_DIGEST": "PIN-ME",
		"IOLBOX_IMAGE_BYTES": "0", "IOLBOX_CPUS": "4", "IOLBOX_MEMORY": "4GiB", "IOLBOX_DISK": "15GiB",
	}, "pin.env"); exitCode(err) != exitPreflight {
		t.Fatalf("PIN-ME error = %v, exit code = %d", err, exitCode(err))
	}
}

func TestRenderTemplateAndD11LimaEnvironment(t *testing.T) {
	p := macOSProfile{ImageURL: "https://example.invalid/x", ImageDigest: "sha256:" + strings.Repeat("b", 64), CPUs: "4", Memory: "4GiB", Disk: "15GiB"}
	rendered, err := renderTemplate([]byte("url: @IOLBOX_IMAGE_URL@\ndigest: @IOLBOX_IMAGE_DIGEST@\ncpus: @CPUS@\nmem: @MEMORY@\ndisk: @DISK@\n"), p)
	if err != nil || strings.Contains(string(rendered), "@") {
		t.Fatalf("rendered=%q err=%v", rendered, err)
	}
	if _, err := renderTemplate([]byte("left @UNKNOWN@"), p); err == nil {
		t.Fatal("unresolved placeholder was accepted")
	}
	fake := filepath.Join(t.TempDir(), "limactl")
	if err := os.WriteFile(fake, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := discoverLimactl(fake, "", nil)
	if err != nil || found != fake {
		t.Fatalf("discoverLimactl = %q, %v", found, err)
	}
	runner := &sequenceRunner{outputs: map[string][]byte{"--version": []byte("limactl version 2.2.0\n")}}
	client, info, err := limaClientFor(t.Context(), found, runner)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "2.2.0" || client.info.Version != "2.2.0" {
		t.Fatalf("Lima version = %+v", info)
	}
	env := guestEnvironment(macOSProfile{Name: "debian13", KernelSeries: "6.12"}, hostFacts{Product: "26.6.1", Build: "25G76"}, info.Version, "iolbox-debian13", "payload.tar.gz", lifecycleConfig{})
	if env["IOLBOX_HOST_LIMA"] != "2.2.0" {
		t.Fatalf("IOLBOX_HOST_LIMA = %q", env["IOLBOX_HOST_LIMA"])
	}
}

func TestEveryShippedProfileRendersCompleteDarwinPortContract(t *testing.T) {
	root := filepath.Join("..", "..", "packaging", "macos")
	table, _, err := loadMacOSProfile(root, "debian13")
	if err != nil {
		t.Fatal(err)
	}
	for name, profile := range table.Profiles {
		profile.ImageURL = "https://example.invalid/m3-image"
		profile.ImageDigest = "sha256:" + strings.Repeat("a", 64)
		profile.CPUs = "4"
		profile.Memory = "4GiB"
		profile.Disk = "15GiB"
		raw, err := os.ReadFile(filepath.Join(root, "lima", profile.YAMLTemplate))
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := renderTemplateForPort(raw, profile, defaultDarwinGUIPort)
		if err != nil {
			t.Fatalf("render profile %s: %v", name, err)
		}
		rules, err := parseDarwinPortForwardRules(string(rendered))
		if err != nil {
			t.Fatalf("parse profile %s port contract: %v", name, err)
		}
		want := expectedDarwinPortForwardRules(darwinPortContract{GUIPort: defaultDarwinGUIPort})
		if len(rules) != len(want) {
			t.Fatalf("profile %s has %d port rules, want %d: %#v", name, len(rules), len(want), rules)
		}
		for i := range want {
			if !darwinPortForwardRulesEqual(rules[i], want[i]) {
				t.Fatalf("profile %s port rule %d = %#v, want %#v", name, i, rules[i], want[i])
			}
		}
	}
}
