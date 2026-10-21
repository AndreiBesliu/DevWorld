package health

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
)

// Cores returns the CPU's physical cores and logical CPUs from r.CPUInfo
// (R07): the logical CPUs are its "processor" entries, and the physical
// cores the distinct (physical id, core id) pairs, so that neither
// hyperthreads nor a second socket's repeated core ids are miscounted. A
// CPU that lists no core ids, as ARM does, has one core per logical CPU.
func Cores(r Roots) (physical, logical int, err error) {
	b, err := os.ReadFile(r.CPUInfo) //nolint:gosec // /proc/cpuinfo, or a test's fixture
	if err != nil {
		return 0, 0, err
	}
	type core struct{ socket, id string }
	cores := map[core]bool{}
	var cur core
	// An empty line ends a processor's entry.
	endEntry := func() {
		if cur.id != "" {
			cores[cur] = true
		}
		cur = core{}
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		key, value, ok := strings.Cut(s.Text(), ":")
		if !ok {
			endEntry()
			continue
		}
		switch value = strings.TrimSpace(value); strings.TrimSpace(key) {
		case "processor":
			logical++
		case "physical id":
			cur.socket = value
		case "core id":
			cur.id = value
		}
	}
	endEntry()
	if err := s.Err(); err != nil {
		return 0, 0, err
	}
	if logical == 0 {
		return 0, 0, errors.New("health: cpuinfo lists no processor")
	}
	if physical = len(cores); physical == 0 {
		physical = logical
	}
	return physical, logical, nil
}

// RAMMb returns the RAM in MiB: MemTotal of r.MemInfo.
func RAMMb(r Roots) (int, error) {
	b, err := os.ReadFile(r.MemInfo) //nolint:gosec // /proc/meminfo, or a test's fixture
	if err != nil {
		return 0, err
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		value, ok := strings.CutPrefix(s.Text(), "MemTotal:")
		if !ok {
			continue
		}
		kb, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "kB")), 10, 64)
		if err != nil {
			return 0, errors.New("health: meminfo's MemTotal is not in kB")
		}
		return int(kb / 1024), nil
	}
	if err := s.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("health: meminfo has no MemTotal")
}
