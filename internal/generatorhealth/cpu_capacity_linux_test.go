//go:build linux

package generatorhealth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadLinuxCPUQuotaFromCgroupV2(t *testing.T) {
	tests := []struct {
		name      string
		quota     string
		wantCores float64
		wantFound bool
		wantKnown bool
	}{
		{name: "finite quota", quota: "400000 100000", wantCores: 4, wantFound: true, wantKnown: true},
		{name: "fractional quota", quota: "50000 100000", wantCores: 0.5, wantFound: true, wantKnown: true},
		{name: "unlimited quota", quota: "max 100000", wantFound: true, wantKnown: true},
		{name: "malformed quota", quota: "400000 invalid", wantFound: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeCgroupFixture(t, root, "0::/container/job\n",
				"29 23 0:26 /container /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
				map[string]string{
					"sys/fs/cgroup/cpu.max":     "max 100000",
					"sys/fs/cgroup/job/cpu.max": test.quota,
				})
			cores, source, found, known := readLinuxCPUQuota(root, "/proc/self/cgroup", "/proc/self/mountinfo")
			if cores != test.wantCores || source != cgroupV2QuotaSource || found != test.wantFound || known != test.wantKnown {
				t.Fatalf("readLinuxCPUQuota() = (%v, %q, %t, %t)", cores, source, found, known)
			}
		})
	}
}

func TestReadLinuxCPUQuotaFromCgroupV1(t *testing.T) {
	root := t.TempDir()
	writeCgroupFixture(t, root, "2:cpu,cpuacct:/workers/limited\n",
		"31 22 0:27 /workers /sys/fs/cgroup/cpu rw,relatime - cgroup cgroup rw,cpu,cpuacct\n",
		map[string]string{
			"sys/fs/cgroup/cpu/cpu.cfs_quota_us":          "-1",
			"sys/fs/cgroup/cpu/cpu.cfs_period_us":         "100000",
			"sys/fs/cgroup/cpu/limited/cpu.cfs_quota_us":  "250000",
			"sys/fs/cgroup/cpu/limited/cpu.cfs_period_us": "100000",
		})
	cores, source, found, known := readLinuxCPUQuota(root, "/proc/self/cgroup", "/proc/self/mountinfo")
	if cores != 2.5 || source != cgroupV1QuotaSource || !found || !known {
		t.Fatalf("readLinuxCPUQuota() = (%v, %q, %t, %t)", cores, source, found, known)
	}
}

func TestReadLinuxCPUQuotaUsesTightestCgroupAncestor(t *testing.T) {
	tests := []struct {
		name       string
		cgroup     string
		mountInfo  string
		files      map[string]string
		wantCores  float64
		wantSource string
	}{
		{
			name:       "v2 parent is tighter than process cgroup",
			cgroup:     "0::/container/parent/job\n",
			mountInfo:  "29 23 0:26 /container /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
			files:      map[string]string{"sys/fs/cgroup/cpu.max": "max 100000", "sys/fs/cgroup/parent/cpu.max": "200000 100000", "sys/fs/cgroup/parent/job/cpu.max": "400000 100000"},
			wantCores:  2,
			wantSource: cgroupV2QuotaSource,
		},
		{
			name:       "unknown when ancestor quota is unreadable",
			wantSource: cgroupV2QuotaSource,
			cgroup:     "0::/container/parent/job\n",
			mountInfo:  "29 23 0:26 /container /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
			files:      map[string]string{"sys/fs/cgroup/cpu.max": "max 100000", "sys/fs/cgroup/parent/job/cpu.max": "400000 100000"},
		},
		{
			name:       "v1 parent is tighter than process cgroup",
			cgroup:     "2:cpu,cpuacct:/workers/parent/job\n",
			mountInfo:  "31 22 0:27 /workers /sys/fs/cgroup/cpu rw,relatime - cgroup cgroup rw,cpu,cpuacct\n",
			files:      v1QuotaFixture(map[string][2]string{"/": {"-1", "100000"}, "/parent": {"150000", "100000"}, "/parent/job": {"500000", "100000"}}),
			wantCores:  1.5,
			wantSource: cgroupV1QuotaSource,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeCgroupFixture(t, root, test.cgroup, test.mountInfo, test.files)
			cores, source, found, known := readLinuxCPUQuota(root, "/proc/self/cgroup", "/proc/self/mountinfo")
			wantKnown := test.name != "unknown when ancestor quota is unreadable" && test.wantCores > 0
			if !found || source != test.wantSource || known != wantKnown || cores != test.wantCores {
				t.Fatalf("readLinuxCPUQuota() = (%v, %q, %t, %t)", cores, source, found, known)
			}
		})
	}
}

func v1QuotaFixture(entries map[string][2]string) map[string]string {
	files := make(map[string]string, len(entries)*2)
	for path, quota := range entries {
		base := filepath.Join("sys", "fs", "cgroup", "cpu", filepath.FromSlash(path))
		files[filepath.Join(base, "cpu.cfs_quota_us")] = quota[0]
		files[filepath.Join(base, "cpu.cfs_period_us")] = quota[1]
	}
	return files
}

func TestMapCgroupPathRespectsMountRootAndRejectsTraversal(t *testing.T) {
	tests := []struct {
		name       string
		mountRoot  string
		cgroupPath string
		want       string
		wantErr    bool
	}{
		{name: "namespace path under mount root", mountRoot: "/docker.slice", cgroupPath: "/docker.slice/job", want: filepath.Join("/fixture", "sys", "fs", "cgroup", "job")},
		{name: "reject sibling path", mountRoot: "/docker.slice", cgroupPath: "/other/job", wantErr: true},
		{name: "reject traversal", mountRoot: "/", cgroupPath: "/../../outside", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := mapCgroupPath("/fixture", cgroupMount{root: test.mountRoot, point: "/sys/fs/cgroup"}, test.cgroupPath)
			if test.wantErr {
				if err == nil {
					t.Fatalf("mapCgroupPath() = %q, want error", got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("mapCgroupPath() = (%q, %v), want (%q, nil)", got, err, test.want)
			}
		})
	}
}

func TestCurrentCPUCapacityUsesQuotaAndClampsToGOMAXPROCS(t *testing.T) {
	root := t.TempDir()
	writeCgroupFixture(t, root, "0::/\n",
		"29 23 0:26 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
		map[string]string{"sys/fs/cgroup/cpu.max": "400000 100000"})

	cores, _, found, known := readLinuxCPUQuota(root, "/proc/self/cgroup", "/proc/self/mountinfo")
	if !found || !known {
		t.Fatal("expected finite quota evidence")
	}
	for _, test := range []struct {
		logicalCPUs int
		maxProcs    int
		want        float64
	}{{12, 12, 4}, {12, 2, 2}, {2, 12, 2}} {
		capacity := effectiveCapacity(test.logicalCPUs, test.maxProcs, cores)
		if capacity != test.want {
			t.Fatalf("effectiveCapacity(%d, %d, %v) = %v, want %v", test.logicalCPUs, test.maxProcs, cores, capacity, test.want)
		}
	}
}

func TestCurrentLinuxCPUCapacityReportsUnknownWhenCgroupEvidenceIsMissing(t *testing.T) {
	root := t.TempDir()
	capacity := cpuCapacityFromQuota(12, 12, root, "/proc/self/cgroup", "/proc/self/mountinfo")
	if capacity.known || capacity.cores != 12 || capacity.source != "cgroup_cpu_limit_unavailable" {
		t.Fatalf("missing cgroup evidence must remain unknown: %+v", capacity)
	}
}

func writeCgroupFixture(t *testing.T, root, cgroup, mountInfo string, files map[string]string) {
	t.Helper()
	if files == nil {
		files = make(map[string]string)
	}
	if _, ok := files["proc/self/status"]; !ok {
		files["proc/self/status"] = "Name:\tfixture\nCpus_allowed_list:\t0-11\n"
	}
	for name, content := range map[string]string{
		"proc/self/cgroup":    cgroup,
		"proc/self/mountinfo": mountInfo,
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadLinuxProcessAllowedCPUs(t *testing.T) {
	root := t.TempDir()
	statusPath := filepath.Join(root, "proc/self/status")
	for _, test := range []struct {
		name    string
		status  string
		want    int
		wantErr bool
	}{
		{name: "sparse allowed list", status: "Name:\tfixture\nCpus_allowed_list:\t0-1,4\n", want: 3},
		{name: "missing status file", wantErr: true},
		{name: "missing field", status: "Name:\tfixture\n", wantErr: true},
		{name: "duplicate field", status: "Cpus_allowed_list:\t0-1\nCpus_allowed_list:\t0-3\n", wantErr: true},
		{name: "malformed list", status: "Cpus_allowed_list:\t0-2,2-4\n", wantErr: true},
		{name: "oversized status", status: strings.Repeat("x", maxProcStatusBytes+1), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.status == "" {
				if err := os.Remove(statusPath); err != nil {
					if !os.IsNotExist(err) {
						t.Fatal(err)
					}
				}
			} else if err := os.WriteFile(statusPath, []byte(test.status), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readLinuxProcessAllowedCPUs(root, "/proc/self/status")
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("readLinuxProcessAllowedCPUs() = (%d, %v), want (%d, error=%t)", got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestParseEffectiveCPUSet(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int
	}{
		{"0", 1}, {"0-3", 4}, {"0-1,4,7-9", 6}, {" 0,2-3\n", 3},
	} {
		got, err := parseEffectiveCPUSet(test.input)
		if err != nil || got != test.want {
			t.Errorf("parseEffectiveCPUSet(%q) = (%d, %v), want %d", test.input, got, err, test.want)
		}
	}
	for _, input := range []string{
		"", " ", "1-0", "2-3,1", "0-3,3-4", "0,0", "1,,3",
		"-1", "0-", "0-1-2", "a", "0-1048577", "0, 2",
	} {
		if got, err := parseEffectiveCPUSet(input); err == nil {
			t.Errorf("invalid cpuset %q accepted with %d cores", input, got)
		}
	}
}

func TestLinuxCapacityUsesCurrentProcessAllowedCPUsForV1CpusetAndAffinity(t *testing.T) {
	root := t.TempDir()
	writeCgroupFixture(t, root, "2:cpu,cpuacct:/workers/limited\n",
		"31 22 0:27 /workers /sys/fs/cgroup/cpu rw,relatime - cgroup cgroup rw,cpu,cpuacct\n",
		map[string]string{
			"proc/self/status":                            "Name:\tfixture\nCpus_allowed_list:\t0-3\n",
			"sys/fs/cgroup/cpu/cpu.cfs_quota_us":          "800000",
			"sys/fs/cgroup/cpu/cpu.cfs_period_us":         "100000",
			"sys/fs/cgroup/cpu/limited/cpu.cfs_quota_us":  "800000",
			"sys/fs/cgroup/cpu/limited/cpu.cfs_period_us": "100000",
		})
	capacity := cpuCapacityFromQuota(12, 12, root, "/proc/self/cgroup", "/proc/self/mountinfo")
	if capacity.cores != 4 || capacity.source != "cgroup_v1_cpu_quota_hierarchy_and_process_cpu_allowed_list" || !capacity.known {
		t.Fatalf("v1 cgroup did not apply the kernel-allowed CPU list: %+v", capacity)
	}
}

func TestLinuxCapacityDetectsProcessAllowedCPUChangesAfterStartup(t *testing.T) {
	root := t.TempDir()
	statusPath := filepath.Join(root, "proc/self/status")
	writeCgroupFixture(t, root, "0::/container/job\n",
		"29 23 0:26 /container /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
		map[string]string{
			"proc/self/status":                        "Name:\tfixture\nCpus_allowed_list:\t0-3\n",
			"sys/fs/cgroup/cpu.max":                   "max 100000",
			"sys/fs/cgroup/job/cpu.max":               "400000 100000",
			"sys/fs/cgroup/job/cpuset.cpus.effective": "0-3",
		})
	before := cpuCapacityFromQuota(12, 12, root, "/proc/self/cgroup", "/proc/self/mountinfo")
	if err := os.WriteFile(statusPath, []byte("Name:\tfixture\nCpus_allowed_list:\t0-1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	after := cpuCapacityFromQuota(12, 12, root, "/proc/self/cgroup", "/proc/self/mountinfo")
	if before.cores != 4 || after.cores != 2 || !before.known || !after.known || before.source == after.source {
		t.Fatalf("process CPU allowance change was not detected: before=%+v after=%+v", before, after)
	}
}

func TestLinuxCapacityCombinesV2QuotaAndEffectiveCPUSet(t *testing.T) {
	tests := []struct {
		name, quota, cpuset, source string
		want                        float64
		known                       bool
		maxProcs                    int
	}{
		{"cpuset tighter than quota", "400000 100000", "0-1", "cgroup_v2_cpu_quota_and_cpuset", 2, true, 12},
		{"quota tighter than cpuset", "200000 100000", "0-7", "cgroup_v2_cpu_quota_and_cpuset", 2, true, 12},
		{"unlimited quota restricted to cpuset", "max 100000", "0,2-3", "cgroup_v2_cpuset_effective", 3, true, 12},
		{"runtime tighter than set", "400000 100000", "0-7", "cgroup_v2_cpu_quota_and_cpuset", 2, true, 2},
		{"malformed existing cpuset fails closed", "400000 100000", "0-2,2-3", "cgroup_v2_cpuset_unreadable", 4, false, 12},
		{"empty existing cpuset fails closed", "max 100000", "", "cgroup_v2_cpuset_unreadable", 12, false, 12},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeCgroupFixture(t, root, "0::/container/job\n",
				"29 23 0:26 /container /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
				map[string]string{
					"sys/fs/cgroup/cpu.max":                   "max 100000",
					"sys/fs/cgroup/job/cpu.max":               test.quota,
					"sys/fs/cgroup/job/cpuset.cpus.effective": test.cpuset,
				})
			capacity := cpuCapacityFromQuota(12, test.maxProcs, root, "/proc/self/cgroup", "/proc/self/mountinfo")
			if capacity.cores != test.want || capacity.source != test.source || capacity.known != test.known {
				t.Errorf("CPU capacity = %+v, want cores=%.2f source=%s known=%t", capacity, test.want, test.source, test.known)
			}
		})
	}
}

func TestLinuxCapacityKeepsLegacyBehaviorWithoutV2CPUSetFile(t *testing.T) {
	root := t.TempDir()
	writeCgroupFixture(t, root, "0::/container/job\n",
		"29 23 0:26 /container /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
		map[string]string{
			"sys/fs/cgroup/cpu.max":     "max 100000",
			"sys/fs/cgroup/job/cpu.max": "300000 100000",
		})
	capacity := cpuCapacityFromQuota(12, 12, root, "/proc/self/cgroup", "/proc/self/mountinfo")
	if capacity.cores != 3 || capacity.source != cgroupV2QuotaSource || !capacity.known {
		t.Fatalf("absent optional cpuset changed quota-only behavior: %+v", capacity)
	}
}

func TestLinuxCapacityDetectsV2CPUSetChanges(t *testing.T) {
	root := t.TempDir()
	cpusetPath := filepath.Join(root, "sys/fs/cgroup/job/cpuset.cpus.effective")
	writeCgroupFixture(t, root, "0::/container/job\n",
		"29 23 0:26 /container /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
		map[string]string{
			"sys/fs/cgroup/cpu.max":                   "max 100000",
			"sys/fs/cgroup/job/cpu.max":               "400000 100000",
			"sys/fs/cgroup/job/cpuset.cpus.effective": "0-3",
		})
	before := cpuCapacityFromQuota(12, 12, root, "/proc/self/cgroup", "/proc/self/mountinfo")
	if err := os.WriteFile(cpusetPath, []byte("0-1"), 0600); err != nil {
		t.Fatal(err)
	}
	after := cpuCapacityFromQuota(12, 12, root, "/proc/self/cgroup", "/proc/self/mountinfo")
	if before.cores != 4 || after.cores != 2 || !before.known || !after.known {
		t.Fatalf("effective cpuset change not detected: before=%+v after=%+v", before, after)
	}
}
