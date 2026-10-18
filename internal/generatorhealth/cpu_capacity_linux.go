//go:build linux

package generatorhealth

import (
	"bufio"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	cgroupV2QuotaSource = "cgroup_v2_cpu_quota_hierarchy"
	cgroupV1QuotaSource = "cgroup_v1_cpu_quota_hierarchy"
	maxCgroupAncestors  = 256
)

func currentCPUCapacity(logicalCPUs, maxProcs int) cpuCapacity {
	return cpuCapacityFromQuota(logicalCPUs, maxProcs, "/", "/proc/self/cgroup", "/proc/self/mountinfo")
}

func cpuCapacityFromQuota(logicalCPUs, maxProcs int, root, cgroupFile, mountInfoFile string) cpuCapacity {
	if logicalCPUs < 1 || maxProcs < 1 {
		return cpuCapacity{}
	}
	runtimeCapacity := float64(logicalCPUs)
	if maxProcs < logicalCPUs {
		runtimeCapacity = float64(maxProcs)
	}
	quotaCores, source, found, known := readLinuxCPUQuota(root, cgroupFile, mountInfoFile)
	if !found {
		return cpuCapacity{cores: runtimeCapacity, source: "cgroup_cpu_limit_unavailable", known: false}
	}
	if !known {
		return cpuCapacity{cores: runtimeCapacity, source: "cgroup_cpu_limit_unreadable", known: false}
	}
	if quotaCores <= 0 {
		return cpuCapacity{cores: runtimeCapacity, source: "runtime_logical_cpus_and_gomaxprocs", known: true}
	}
	capacity := effectiveCapacity(logicalCPUs, maxProcs, quotaCores)
	return cpuCapacity{cores: capacity, source: source, known: capacity > 0 && !math.IsInf(capacity, 0) && !math.IsNaN(capacity)}
}

func effectiveCapacity(logicalCPUs, maxProcs int, quotaCores float64) float64 {
	if logicalCPUs < 1 || maxProcs < 1 || quotaCores <= 0 || math.IsNaN(quotaCores) || math.IsInf(quotaCores, 0) {
		return 0
	}
	return math.Min(math.Min(float64(logicalCPUs), float64(maxProcs)), quotaCores)
}

// readLinuxCPUQuota returns the effective finite CPU quota when the process's
// cgroup exposes one. A known unlimited quota returns found and known with a
// zero core limit; missing or unreadable cgroup evidence stays unknown.
func readLinuxCPUQuota(root, cgroupFile, mountInfoFile string) (cores float64, source string, found, known bool) {
	cgroupData, err := os.ReadFile(filepath.Join(root, strings.TrimPrefix(cgroupFile, "/")))
	if err != nil {
		return 0, "", false, false
	}
	mountData, err := os.ReadFile(filepath.Join(root, strings.TrimPrefix(mountInfoFile, "/")))
	if err != nil {
		return 0, "", false, false
	}
	mount, cgroupPath, version, err := locateCPUController(string(cgroupData), string(mountData))
	if err != nil {
		return 0, "", false, false
	}
	directory, err := mapCgroupPath(root, mount, cgroupPath)
	if err != nil {
		return 0, "", true, false
	}
	rootQuotaCores, rootErr := readHierarchicalCPUQuota(root, mount.point, directory, version)
	quotaSource := cgroupV1QuotaSource
	if version == 2 {
		quotaSource = cgroupV2QuotaSource
	}
	if rootErr != nil {
		return 0, quotaSource, true, false
	}
	return rootQuotaCores, quotaSource, true, true
}

// readHierarchicalCPUQuota walks from the process cgroup to the mounted
// hierarchy root and returns the tightest finite quota. Any missing or
// malformed ancestor evidence leaves the result unknown.
func readHierarchicalCPUQuota(root, mountPoint, processDirectory string, version int) (float64, error) {
	mountDirectory := filepath.Clean(filepath.Join(root, strings.TrimPrefix(mountPoint, "/")))
	directory := filepath.Clean(processDirectory)
	if !isWithinDirectory(mountDirectory, directory) {
		return 0, errors.New("process cgroup directory is outside the mounted hierarchy")
	}
	var minimum float64
	for depth := 0; depth < maxCgroupAncestors; depth++ {
		var quota, period float64
		var unlimited bool
		var err error
		if version == 2 {
			quota, period, unlimited, err = readV2CPUQuota(filepath.Join(directory, "cpu.max"))
		} else {
			quota, period, unlimited, err = readV1CPUQuota(directory)
		}
		if err != nil {
			return 0, err
		}
		if !unlimited {
			cores := quota / period
			if minimum == 0 || cores < minimum {
				minimum = cores
			}
		}
		if directory == mountDirectory {
			return minimum, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory || !isWithinDirectory(mountDirectory, parent) {
			return 0, errors.New("cgroup hierarchy did not reach its mount root")
		}
		directory = parent
	}
	return 0, errors.New("cgroup hierarchy exceeds the supported ancestor depth")
}

func isWithinDirectory(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

type cgroupMount struct {
	root       string
	point      string
	version    int
	controller string
}

func locateCPUController(cgroupData, mountData string) (cgroupMount, string, int, error) {
	controllerPath := ""
	version := 0
	scanner := bufio.NewScanner(strings.NewReader(cgroupData))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[1] == "" {
			controllerPath, version = parts[2], 2
			break
		}
		for _, controller := range strings.Split(parts[1], ",") {
			if controller == "cpu" {
				controllerPath, version = parts[2], 1
				break
			}
		}
		if version == 1 {
			break
		}
	}
	if err := scanner.Err(); err != nil || version == 0 {
		return cgroupMount{}, "", 0, errors.New("process CPU cgroup is unavailable")
	}

	for _, line := range strings.Split(mountData, "\n") {
		fields := strings.Fields(line)
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
				break
			}
		}
		if separator < 6 || len(fields) <= separator+3 {
			continue
		}
		filesystem := fields[separator+1]
		controllers := fields[separator+3]
		if version == 2 && filesystem != "cgroup2" {
			continue
		}
		if version == 1 && (filesystem != "cgroup" || !containsController(controllers, "cpu")) {
			continue
		}
		mountRoot := unescapeMountField(fields[3])
		mountPoint := unescapeMountField(fields[4])
		mount := cgroupMount{root: filepath.Clean(mountRoot), point: filepath.Clean(mountPoint), version: version}
		if mountRoot == "/" || controllerPath == mountRoot || strings.HasPrefix(controllerPath, strings.TrimSuffix(mountRoot, "/")+"/") {
			return mount, filepath.Clean(controllerPath), version, nil
		}
	}
	return cgroupMount{}, "", 0, errors.New("process CPU cgroup mount is unavailable")
}

func containsController(optionList, wanted string) bool {
	for _, option := range strings.Split(optionList, ",") {
		if option == wanted {
			return true
		}
	}
	return false
}

func unescapeMountField(value string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(value)
}

func mapCgroupPath(root string, mount cgroupMount, cgroupPath string) (string, error) {
	if filepath.IsAbs(root) == false || filepath.IsAbs(mount.point) == false || filepath.IsAbs(cgroupPath) == false {
		return "", errors.New("cgroup paths must be absolute")
	}
	for _, segment := range strings.Split(filepath.ToSlash(cgroupPath), "/") {
		if segment == ".." {
			return "", errors.New("process cgroup path contains traversal")
		}
	}
	mountRoot := filepath.Clean(mount.root)
	cgroupPath = filepath.Clean(cgroupPath)
	if mountRoot != "/" && cgroupPath != mountRoot && !strings.HasPrefix(cgroupPath, strings.TrimSuffix(mountRoot, "/")+"/") {
		return "", errors.New("process cgroup path is outside its mount root")
	}
	relative := strings.TrimPrefix(cgroupPath, mountRoot)
	if mountRoot == "/" {
		relative = strings.TrimPrefix(cgroupPath, "/")
	} else {
		relative = strings.TrimPrefix(relative, "/")
	}
	directory := filepath.Join(root, strings.TrimPrefix(mount.point, "/"), relative)
	cleanRoot := filepath.Clean(root)
	cleanDirectory := filepath.Clean(directory)
	rootPrefix := cleanRoot + string(os.PathSeparator)
	if cleanRoot == string(os.PathSeparator) {
		rootPrefix = cleanRoot
	}
	if cleanDirectory != cleanRoot && !strings.HasPrefix(cleanDirectory, rootPrefix) {
		return "", errors.New("resolved cgroup directory escapes the filesystem root")
	}
	return cleanDirectory, nil
}

func readV2CPUQuota(path string) (quota, period float64, unlimited bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false, err
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || fields[1] == "" {
		return 0, 0, false, errors.New("malformed cgroup v2 cpu.max")
	}
	periodValue, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil || periodValue == 0 {
		return 0, 0, false, errors.New("invalid cgroup v2 CPU period")
	}
	if fields[0] == "max" {
		return 0, float64(periodValue), true, nil
	}
	quotaValue, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || quotaValue == 0 {
		return 0, 0, false, errors.New("invalid cgroup v2 CPU quota")
	}
	return float64(quotaValue), float64(periodValue), false, nil
}

func readV1CPUQuota(directory string) (quota, period float64, unlimited bool, err error) {
	quotaData, err := os.ReadFile(filepath.Join(directory, "cpu.cfs_quota_us"))
	if err != nil {
		return 0, 0, false, err
	}
	periodData, err := os.ReadFile(filepath.Join(directory, "cpu.cfs_period_us"))
	if err != nil {
		return 0, 0, false, err
	}
	quotaValue, err := strconv.ParseInt(strings.TrimSpace(string(quotaData)), 10, 64)
	if err != nil || quotaValue == 0 || quotaValue < -1 {
		return 0, 0, false, errors.New("invalid cgroup v1 CPU quota")
	}
	periodValue, err := strconv.ParseUint(strings.TrimSpace(string(periodData)), 10, 64)
	if err != nil || periodValue == 0 {
		return 0, 0, false, errors.New("invalid cgroup v1 CPU period")
	}
	if quotaValue == -1 {
		return 0, float64(periodValue), true, nil
	}
	return float64(quotaValue), float64(periodValue), false, nil
}
