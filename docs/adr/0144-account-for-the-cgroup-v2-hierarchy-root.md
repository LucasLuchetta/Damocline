# ADR 0144: account for the cgroup v2 hierarchy root

The Linux kernel exposes `cpu.max` on non-root cgroups. A quota walk can
finish at a cgroup v2 mount whose reported root is `/` when `cpu.max` is
absent and `cgroup.subtree_control` confirms that the root distributes the
CPU controller. All descendant quota files are still checked and the
tightest finite quota still applies.

A mount rooted at a subtree, an unreadable or malformed quota, and
missing controller evidence still leave capacity unknown. If a cgroup
namespace exposes a non-root cgroup as `/`, enabling the CPU controller
for children requires that controller to be enabled for the exposed
cgroup itself, where the kernel supplies `cpu.max`.

See the [Linux cgroup v2 CPU interface documentation](https://docs.kernel.org/admin-guide/cgroup-v2.html#cpu-interface-files).
