//go:build linux

package generatorhealth

import (
	"os"
	"path/filepath"
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
				map[string]string{"sys/fs/cgroup/job/cpu.max": test.quota})
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
			"sys/fs/cgroup/cpu/limited/cpu.cfs_quota_us":  "250000",
			"sys/fs/cgroup/cpu/limited/cpu.cfs_period_us": "100000",
		})
	cores, source, found, known := readLinuxCPUQuota(root, "/proc/self/cgroup", "/proc/self/mountinfo")
	if cores != 2.5 || source != cgroupV1QuotaSource || !found || !known {
		t.Fatalf("readLinuxCPUQuota() = (%v, %q, %t, %t)", cores, source, found, known)
	}
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
		maxProcs int
		want     float64
	}{{12, 4}, {2, 2}} {
		capacity := effectiveCapacity(test.maxProcs, cores)
		if capacity != test.want {
			t.Fatalf("effectiveCapacity(%d, %v) = %v, want %v", test.maxProcs, cores, capacity, test.want)
		}
	}
}

func TestCurrentLinuxCPUCapacityReportsUnknownWhenCgroupEvidenceIsMissing(t *testing.T) {
	root := t.TempDir()
	capacity := cpuCapacityFromQuota(12, root, "/proc/self/cgroup", "/proc/self/mountinfo")
	if capacity.known || capacity.cores != 12 || capacity.source != "cgroup_cpu_limit_unavailable" {
		t.Fatalf("missing cgroup evidence must remain unknown: %+v", capacity)
	}
}

func writeCgroupFixture(t *testing.T, root, cgroup, mountInfo string, files map[string]string) {
	t.Helper()
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
