// core-probe - Hardware Diagnostic Toolkit v2.0
// Zero external dependencies. Statically linkable.
//
// Build for legacy 32-bit Linux (kernel 2.6.x+):
//   CGO_ENABLED=0 GOOS=linux GOARCH=386 go build -ldflags="-s -w" -o core-probe main.go
//
// Build for modern 64-bit Linux:
//   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o core-probe main.go

package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// ─── Structs ──────────────────────────────────────────────────────────────────

type CPUInfo struct {
	Model             string
	PhysicalCores     int
	LogicalCores      int
	FreqMHz           float64
	Bits              int // 32 or 64
	HasVirtualization bool
	HasSSE2           bool
	HasAES            bool
}

type MemInfo struct {
	TotalMB     uint64
	FreeMB      uint64
	AvailableMB uint64
	SwapTotalMB uint64
	SwapFreeMB  uint64
}

type DiskInfo struct {
	Path        string
	TotalGB     float64
	FreeGB      float64
	UsedPercent float64
}

type ThermalInfo struct {
	Zone        string
	TempC       float64
	Critical    bool
}

type KernelInfo struct {
	Raw   string
	Major int
	Minor int
	Patch int
}

type EnvInfo struct {
	Kernel       KernelInfo
	GlibcPresent bool
	GlibcPath    string
	Hostname     string
}

// ─── Scoring ──────────────────────────────────────────────────────────────────

type ScoreBreakdown struct {
	CPUScore    int
	RAMScore    int
	DiskScore   int
	ThermalScore int
	KernelScore int
	Total       int
	Verdict     string
	UseCases    []string
	Warnings    []string
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	printBanner()

	fmt.Println("[ PROBING HARDWARE... ]")
	fmt.Println()

	cpu := probeCPU()
	mem := probeMemory()
	disks := probeDisks()
	thermals := probeThermal()
	env := probeEnv()
	score := evaluate(cpu, mem, disks, thermals, env)

	printCPU(cpu)
	printMemory(mem)
	printDisks(disks)
	printThermal(thermals)
	printEnv(env)
	printScore(score)
}

// ─── Probes ───────────────────────────────────────────────────────────────────

func probeCPU() CPUInfo {
	info := CPUInfo{
		Model:        "Unknown",
		LogicalCores: runtime.NumCPU(),
		Bits:         32,
	}

	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return info
	}
	defer f.Close()

	physicalIDs := make(map[string]bool)
	coreIDs := make(map[string]bool)
	currentPhysID := ""

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "model name":
			if info.Model == "Unknown" {
				info.Model = val
			}
		case "cpu MHz":
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				if info.FreqMHz == 0 {
					info.FreqMHz = f
				}
			}
		case "physical id":
			currentPhysID = val
			physicalIDs[val] = true
		case "core id":
			coreIDs[currentPhysID+":"+val] = true
		case "flags":
			flags := strings.Fields(val)
			for _, flag := range flags {
				switch flag {
				case "lm":
					info.Bits = 64
				case "vmx", "svm":
					info.HasVirtualization = true
				case "sse2":
					info.HasSSE2 = true
				case "aes":
					info.HasAES = true
				}
			}
		}
	}

	if len(physicalIDs) > 0 {
		info.PhysicalCores = len(coreIDs)
		if info.PhysicalCores == 0 {
			info.PhysicalCores = len(physicalIDs)
		}
	} else {
		// No physical id entries (common on older kernels / single socket)
		info.PhysicalCores = info.LogicalCores
	}

	return info
}

func probeMemory() MemInfo {
	info := MemInfo{}

	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return info
	}
	defer f.Close()

	parse := func(val string) uint64 {
		fields := strings.Fields(val)
		if len(fields) == 0 {
			return 0
		}
		n, _ := strconv.ParseUint(fields[0], 10, 64)
		return n / 1024 // kB -> MB
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "MemTotal":
			info.TotalMB = parse(val)
		case "MemFree":
			info.FreeMB = parse(val)
		case "MemAvailable":
			info.AvailableMB = parse(val)
		case "SwapTotal":
			info.SwapTotalMB = parse(val)
		case "SwapFree":
			info.SwapFreeMB = parse(val)
		}
	}

	return info
}

func probeDisks() []DiskInfo {
	var disks []DiskInfo

	// Always probe root
	mountpoints := []string{"/", "/home", "/data", "/var"}

	seen := make(map[uint64]bool)

	for _, mp := range mountpoints {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(mp, &stat); err != nil {
			continue
		}

		// Deduplicate by total block count + block size fingerprint
		fskey := stat.Blocks*uint64(stat.Bsize) + uint64(stat.Bsize)
		if seen[fskey] {
			continue
		}
		seen[fskey] = true

		totalBytes := stat.Blocks * uint64(stat.Bsize)
		freeBytes := stat.Bfree * uint64(stat.Bsize)
		usedBytes := totalBytes - freeBytes

		totalGB := float64(totalBytes) / (1024 * 1024 * 1024)
		freeGB := float64(freeBytes) / (1024 * 1024 * 1024)

		usedPct := 0.0
		if totalBytes > 0 {
			usedPct = float64(usedBytes) / float64(totalBytes) * 100
		}

		// Skip pseudo-filesystems (size 0 or unreasonably small)
		if totalGB < 0.01 {
			continue
		}

		disks = append(disks, DiskInfo{
			Path:        mp,
			TotalGB:     totalGB,
			FreeGB:      freeGB,
			UsedPercent: usedPct,
		})
	}

	return disks
}

func probeThermal() []ThermalInfo {
	var zones []ThermalInfo

	// Probe up to 8 thermal zones
	for i := 0; i < 8; i++ {
		base := fmt.Sprintf("/sys/class/thermal/thermal_zone%d", i)

		tempRaw, err := readFileTrimmed(base + "/temp")
		if err != nil {
			break // No more zones
		}

		milliC, err := strconv.ParseInt(tempRaw, 10, 64)
		if err != nil {
			continue
		}
		tempC := float64(milliC) / 1000.0

		zoneType := "unknown"
		if t, err := readFileTrimmed(base + "/type"); err == nil {
			zoneType = t
		}

		critical := tempC >= 85.0

		zones = append(zones, ThermalInfo{
			Zone:     fmt.Sprintf("zone%d (%s)", i, zoneType),
			TempC:    tempC,
			Critical: critical,
		})
	}

	return zones
}

func probeEnv() EnvInfo {
	info := EnvInfo{}

	// Hostname
	if h, err := os.Hostname(); err == nil {
		info.Hostname = h
	}

	// Kernel version from /proc/version
	raw, err := readFileTrimmed("/proc/version")
	if err == nil {
		info.Kernel.Raw = raw
		// Format: "Linux version X.Y.Z-..."
		fields := strings.Fields(raw)
		if len(fields) >= 3 {
			parseKernelVersion(&info.Kernel, fields[2])
		}
	}

	// Detect glibc without executing anything — check common paths
	glibcPaths := []string{
		"/lib/libc.so.6",
		"/lib/x86_64-linux-gnu/libc.so.6",
		"/lib/i386-linux-gnu/libc.so.6",
		"/lib/aarch64-linux-gnu/libc.so.6",
		"/lib/arm-linux-gnueabihf/libc.so.6",
		"/usr/lib/libc.so.6",
	}
	for _, p := range glibcPaths {
		if _, err := os.Stat(p); err == nil {
			info.GlibcPresent = true
			info.GlibcPath = p
			break
		}
	}

	return info
}

// ─── Evaluation ───────────────────────────────────────────────────────────────

func evaluate(cpu CPUInfo, mem MemInfo, disks []DiskInfo, thermals []ThermalInfo, env EnvInfo) ScoreBreakdown {
	s := ScoreBreakdown{}

	// --- CPU Score (0-30) ---
	// Base: cores
	switch {
	case cpu.PhysicalCores >= 4:
		s.CPUScore += 30
	case cpu.PhysicalCores >= 2:
		s.CPUScore += 20
	case cpu.PhysicalCores == 1:
		s.CPUScore += 8
	}
	// 64-bit bonus
	if cpu.Bits == 64 {
		s.CPUScore += 5
	}
	// Virtualization bonus (useful for edge node)
	if cpu.HasVirtualization {
		s.CPUScore += 3
	}
	if s.CPUScore > 30 {
		s.CPUScore = 30
	}

	// --- RAM Score (0-30) ---
	switch {
	case mem.TotalMB >= 4096:
		s.RAMScore = 30
	case mem.TotalMB >= 2048:
		s.RAMScore = 24
	case mem.TotalMB >= 1024:
		s.RAMScore = 16
	case mem.TotalMB >= 512:
		s.RAMScore = 10
	case mem.TotalMB >= 256:
		s.RAMScore = 5
	default:
		s.RAMScore = 1
		s.Warnings = append(s.Warnings, fmt.Sprintf("critically low RAM: %dMB", mem.TotalMB))
	}

	// --- Disk Score (0-20) ---
	if len(disks) > 0 {
		largest := disks[0]
		for _, d := range disks {
			if d.TotalGB > largest.TotalGB {
				largest = d
			}
		}
		switch {
		case largest.TotalGB >= 60:
			s.DiskScore = 20
		case largest.TotalGB >= 20:
			s.DiskScore = 15
		case largest.TotalGB >= 8:
			s.DiskScore = 10
		case largest.TotalGB >= 2:
			s.DiskScore = 5
		default:
			s.DiskScore = 2
			s.Warnings = append(s.Warnings, fmt.Sprintf("very small disk: %.1fGB", largest.TotalGB))
		}
		if largest.UsedPercent > 90 {
			s.DiskScore -= 5
			s.Warnings = append(s.Warnings, fmt.Sprintf("disk %.0f%% full", largest.UsedPercent))
		}
	}

	// --- Thermal Score (0-10) ---
	s.ThermalScore = 10
	if len(thermals) == 0 {
		s.ThermalScore = 5 // Can't verify — neutral
	} else {
		for _, t := range thermals {
			if t.Critical {
				s.ThermalScore = 0
				s.Warnings = append(s.Warnings, fmt.Sprintf("CRITICAL TEMP on %s: %.1fC", t.Zone, t.TempC))
			} else if t.TempC >= 75 {
				if s.ThermalScore > 4 {
					s.ThermalScore = 4
				}
				s.Warnings = append(s.Warnings, fmt.Sprintf("high temp on %s: %.1fC", t.Zone, t.TempC))
			}
		}
	}

	// --- Kernel Score (0-10) ---
	switch {
	case env.Kernel.Major >= 4:
		s.KernelScore = 10
	case env.Kernel.Major == 3:
		s.KernelScore = 8
	case env.Kernel.Major == 2 && env.Kernel.Minor == 6:
		s.KernelScore = 5
	case env.Kernel.Major == 2:
		s.KernelScore = 2
		s.Warnings = append(s.Warnings, fmt.Sprintf("very old kernel %d.%d — limited modern software support", env.Kernel.Major, env.Kernel.Minor))
	default:
		s.KernelScore = 0
	}

	s.Total = s.CPUScore + s.RAMScore + s.DiskScore + s.ThermalScore + s.KernelScore

	// --- Use Case Recommendations ---
	switch {
	case s.Total >= 75:
		s.Verdict = "EXCELLENT — Full revival candidate"
		s.UseCases = []string{
			"Lightweight web server (nginx/caddy)",
			"Git server / self-hosted CI",
			"Home lab node / hypervisor",
			"NAS with samba/nfs",
			"VPN gateway / firewall",
		}
	case s.Total >= 55:
		s.Verdict = "GOOD — Viable for targeted deployment"
		s.UseCases = []string{
			"DNS/DHCP server (dnsmasq)",
			"MQTT broker / IoT gateway",
			"Monitoring node (prometheus exporter)",
			"Retro workstation (lightweight WM)",
		}
	case s.Total >= 35:
		s.Verdict = "MARGINAL — Single-purpose only"
		s.UseCases = []string{
			"Serial console server",
			"Network boot / PXE server",
			"Logging aggregator",
		}
	default:
		s.Verdict = "POOR — Parts or disposal recommended"
		s.UseCases = []string{
			"Hardware testing / burn-in rig",
			"Dedicated single-task appliance only",
		}
	}

	return s
}

// ─── Output ───────────────────────────────────────────────────────────────────

func printBanner() {
	fmt.Println("====================================================")
	fmt.Println("   CORE-PROBE  //  Hardware Diagnostic Toolkit      ")
	fmt.Println("   v2.0  //  github.com/core-probe                  ")
	fmt.Println("====================================================")
	fmt.Printf("   host arch : %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println("====================================================")
	fmt.Println()
}

func printCPU(cpu CPUInfo) {
	fmt.Println("[ CPU ]")
	fmt.Printf("  model       : %s\n", cpu.Model)
	fmt.Printf("  phys cores  : %d\n", cpu.PhysicalCores)
	fmt.Printf("  logical     : %d\n", cpu.LogicalCores)
	fmt.Printf("  bits        : %d-bit\n", cpu.Bits)
	if cpu.FreqMHz > 0 {
		fmt.Printf("  freq        : %.0f MHz\n", cpu.FreqMHz)
	}
	flags := []string{}
	if cpu.HasVirtualization {
		flags = append(flags, "virt")
	}
	if cpu.HasSSE2 {
		flags = append(flags, "sse2")
	}
	if cpu.HasAES {
		flags = append(flags, "aes")
	}
	if len(flags) > 0 {
		fmt.Printf("  features    : %s\n", strings.Join(flags, ", "))
	}
	fmt.Println()
}

func printMemory(mem MemInfo) {
	fmt.Println("[ MEMORY ]")
	fmt.Printf("  total       : %d MB\n", mem.TotalMB)
	fmt.Printf("  available   : %d MB\n", mem.AvailableMB)
	fmt.Printf("  free        : %d MB\n", mem.FreeMB)
	if mem.SwapTotalMB > 0 {
		fmt.Printf("  swap        : %d MB total / %d MB free\n", mem.SwapTotalMB, mem.SwapFreeMB)
	} else {
		fmt.Printf("  swap        : none\n")
	}
	fmt.Println()
}

func printDisks(disks []DiskInfo) {
	fmt.Println("[ DISK ]")
	if len(disks) == 0 {
		fmt.Println("  no mountpoints readable")
	}
	for _, d := range disks {
		fmt.Printf("  %-10s  %.1f GB total / %.1f GB free / %.0f%% used\n",
			d.Path, d.TotalGB, d.FreeGB, d.UsedPercent)
	}
	fmt.Println()
}

func printThermal(thermals []ThermalInfo) {
	fmt.Println("[ THERMAL ]")
	if len(thermals) == 0 {
		fmt.Println("  /sys/class/thermal not available on this system")
	}
	for _, t := range thermals {
		status := "ok"
		if t.Critical {
			status = "!!! CRITICAL !!!"
		} else if t.TempC >= 75 {
			status = "HIGH"
		}
		fmt.Printf("  %-25s %.1f C  [%s]\n", t.Zone, t.TempC, status)
	}
	fmt.Println()
}

func printEnv(env EnvInfo) {
	fmt.Println("[ ENVIRONMENT ]")
	fmt.Printf("  hostname    : %s\n", env.Hostname)
	if env.Kernel.Major > 0 {
		fmt.Printf("  kernel      : %d.%d.%d\n", env.Kernel.Major, env.Kernel.Minor, env.Kernel.Patch)
	} else {
		fmt.Printf("  kernel      : %s\n", env.Kernel.Raw)
	}
	if env.GlibcPresent {
		fmt.Printf("  glibc       : detected at %s\n", env.GlibcPath)
		fmt.Printf("  note        : dynamic linking available but not required by this binary\n")
	} else {
		fmt.Printf("  glibc       : not found — fully static environment\n")
	}
	fmt.Println()
}

func printScore(s ScoreBreakdown) {
	fmt.Println("====================================================")
	fmt.Println("[ WORTHINESS SCORE ]")
	fmt.Println("----------------------------------------------------")
	fmt.Printf("  cpu         : %d / 30\n", s.CPUScore)
	fmt.Printf("  ram         : %d / 30\n", s.RAMScore)
	fmt.Printf("  disk        : %d / 20\n", s.DiskScore)
	fmt.Printf("  thermal     : %d / 10\n", s.ThermalScore)
	fmt.Printf("  kernel      : %d / 10\n", s.KernelScore)
	fmt.Println("----------------------------------------------------")
	fmt.Printf("  TOTAL       : %d / 100\n", s.Total)
	fmt.Println("----------------------------------------------------")
	fmt.Printf("  VERDICT     : %s\n", s.Verdict)
	fmt.Println()

	if len(s.UseCases) > 0 {
		fmt.Println("[ RECOMMENDED USE CASES ]")
		for _, u := range s.UseCases {
			fmt.Printf("  - %s\n", u)
		}
		fmt.Println()
	}

	if len(s.Warnings) > 0 {
		fmt.Println("[ WARNINGS ]")
		for _, w := range s.Warnings {
			fmt.Printf("  ! %s\n", w)
		}
		fmt.Println()
	}

	fmt.Println("====================================================")
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func readFileTrimmed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func parseKernelVersion(k *KernelInfo, s string) {
	// Strip suffixes like "-generic", "-amd64", etc.
	clean := s
	if idx := strings.IndexAny(s, "-+"); idx != -1 {
		clean = s[:idx]
	}
	parts := strings.Split(clean, ".")
	if len(parts) >= 1 {
		k.Major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		k.Minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) >= 3 {
		k.Patch, _ = strconv.Atoi(parts[2])
	}
}
