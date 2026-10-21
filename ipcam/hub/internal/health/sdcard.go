package health

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// mmcMajor is the block major of MMC and SD cards (mmcblk*).
const mmcMajor = 179

// mount is a line of /proc/self/mountinfo.
type mount struct {
	major, minor uint64
	point        string // where it is mounted, unescaped
	source       string // the mount source, such as /dev/mmcblk0p2 or /dev/root
}

// sdCard reports whether the mount comes from an SD card: a /dev/mmcblk*
// source, or major 179, which also covers the /dev/root of older Raspberry
// Pi OS kernels and a bind mount into a container.
func (m mount) sdCard() bool {
	return m.major == mmcMajor || strings.HasPrefix(m.source, "/dev/mmcblk")
}

// IsSdCard reports whether recDir lies on an SD card (T40, HUB-17): the
// mount that holds it, the one in mountinfoPath (/proc/self/mountinfo) with
// the longest mount point that recDir lies under, comes from /dev/mmcblk*
// or has major 179. recDir need not exist yet: its nearest existing
// ancestor's symlinks are resolved. It is false when the mount table cannot
// be read, as on Windows.
func IsSdCard(mountinfoPath, recDir string) bool {
	m, ok := mountOf(mountinfoPath, recDir)
	return ok && m.sdCard()
}

// mountOf returns the mount of mountinfoPath that holds dir: the one with
// the longest mount point that dir lies under, and of those, the last one
// listed, which is mounted over the others.
func mountOf(mountinfoPath, dir string) (mount, bool) {
	b, err := os.ReadFile(mountinfoPath) //nolint:gosec // /proc/self/mountinfo, or a test's fixture
	if err != nil {
		return mount{}, false
	}
	path := resolve(dir)
	var best mount
	found := false
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		m, ok := parseMountInfo(s.Text())
		if !ok || !under(path, m.point) {
			continue
		}
		if !found || len(m.point) >= len(best.point) {
			best, found = m, true
		}
	}
	return best, found
}

// parseMountInfo reads a mountinfo line: "<id> <parent> <major>:<minor>
// <root> <mount point> <options> [<optional fields>…] - <type> <source>
// <super options>".
func parseMountInfo(line string) (mount, bool) {
	fields := strings.Fields(line)
	sep := -1
	for i := 6; i < len(fields); i++ {
		if fields[i] == "-" {
			sep = i
			break
		}
	}
	if sep < 0 || sep+2 >= len(fields) {
		return mount{}, false
	}
	majorText, minorText, ok := strings.Cut(fields[2], ":")
	if !ok {
		return mount{}, false
	}
	major, err1 := strconv.ParseUint(majorText, 10, 32)
	minor, err2 := strconv.ParseUint(minorText, 10, 32)
	if err1 != nil || err2 != nil {
		return mount{}, false
	}
	return mount{major: major, minor: minor, point: filepath.Clean(unescapeMountInfo(fields[4])), source: unescapeMountInfo(fields[sep+2])}, true
}

// unescapeMountInfo undoes the kernel's octal escapes of a mountinfo field:
// \040 for a space, \011 for a tab, \012 for a line break, \134 for a
// backslash.
func unescapeMountInfo(field string) string {
	if !strings.Contains(field, `\`) {
		return field
	}
	var b strings.Builder
	for i := 0; i < len(field); i++ {
		if field[i] == '\\' && i+3 < len(field) {
			if n, err := strconv.ParseUint(field[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(field[i])
	}
	return b.String()
}

// under reports whether path is point or lies under it.
func under(path, point string) bool {
	if path == point {
		return true
	}
	if !strings.HasSuffix(point, string(filepath.Separator)) {
		point += string(filepath.Separator)
	}
	return strings.HasPrefix(path, point)
}

// resolve returns dir as an absolute path without symlinks: those of its
// nearest existing ancestor are resolved, and the rest, which does not
// exist yet, is kept.
func resolve(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	rest := ""
	for p := abs; ; {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return abs
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}
