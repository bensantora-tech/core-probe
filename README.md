# core-probe

**Hardware Diagnostic Toolkit for Forgotten Machines**

Got an old PC gathering dust? A decade-old server sitting in a closet? `core-probe` tells you exactly what that hardware is worth — and what you can still do with it.

It runs as a single binary file with no installation required. Drop it on the machine, run it, get answers.

---

## What It Does

`core-probe` boots up, reads the hardware directly, scores what it finds, and prints a plain-english verdict. No guessing, no googling specs.

It checks:

- **CPU** — model, core count, architecture (32/64-bit), and capability flags
- **Memory** — total RAM, available RAM, and swap
- **Disk** — storage size and how full it is, across all major mountpoints
- **Temperature** — reads all thermal sensors to catch overheating before you commit to a deployment
- **Environment** — kernel version and whether the system has glibc (tells you what modern software can run on it)

It then produces a **score out of 100** and a **verdict** — from *Excellent revival candidate* down to *Parts or disposal recommended* — along with a list of specific things the machine is actually suited for.

---

## Sample Output

```
====================================================
   CORE-PROBE  //  Hardware Diagnostic Toolkit
   v2.0  //  github.com/ben/core-probe
====================================================
   host arch : linux/amd64
====================================================

[ PROBING HARDWARE... ]

[ CPU ]
  model       : Intel(R) Core(TM)2 Duo CPU E8400 @ 3.00GHz
  phys cores  : 2
  logical     : 2
  bits        : 64-bit
  freq        : 3000 MHz
  features    : sse2

[ MEMORY ]
  total       : 3947 MB
  available   : 3201 MB
  free        : 2900 MB
  swap        : 2047 MB total / 2047 MB free

[ DISK ]
  /           476.3 GB total / 301.2 GB free / 37% used

[ THERMAL ]
  zone0 (x86_pkg_temp)      41.0 C  [ok]

[ ENVIRONMENT ]
  hostname    : oldbox
  kernel      : 5.4.0
  glibc       : detected at /lib/x86_64-linux-gnu/libc.so.6
  note        : dynamic linking available but not required by this binary

====================================================
[ WORTHINESS SCORE ]
----------------------------------------------------
  cpu         : 25 / 30
  ram         : 24 / 30
  disk        : 20 / 20
  thermal     : 10 / 10
  kernel      : 10 / 10
----------------------------------------------------
  TOTAL       : 89 / 100
----------------------------------------------------
  VERDICT     : EXCELLENT — Full revival candidate

[ RECOMMENDED USE CASES ]
  - Lightweight web server (nginx/caddy)
  - Git server / self-hosted CI
  - Home lab node / hypervisor
  - NAS with samba/nfs
  - VPN gateway / firewall

====================================================
```

---

## How to Use It

### The easy way — run a pre-built binary

1. Download the right binary for your machine from the [Releases](../../releases) page
2. Copy it to the target machine (USB drive, `scp`, whatever works)
3. Open a terminal and run:

```bash
chmod +x core-probe
./core-probe
```

That's it. No installation. No internet connection needed on the target machine.

**Which binary do I download?**

| Binary | Use for |
|---|---|
| `core-probe-x86` | Old 32-bit PCs (pre-2005 era) |
| `core-probe-x64` | Most modern or semi-modern 64-bit machines |
| `core-probe-arm` | Raspberry Pi and ARM single-board computers |

Not sure? If the machine is from after 2005 and it's a regular PC or laptop, grab `core-probe-x64`.

---

### The developer way — build it yourself

You'll need [Go](https://go.dev/dl/) installed (version 1.21 or later).

```bash
git clone https://github.com/ben/core-probe
cd core-probe
```

**Build for your current machine:**
```bash
go build -o core-probe main.go
./core-probe
```

**Build for a legacy 32-bit machine:**
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=386 go build -ldflags="-s -w" -o core-probe-x86 main.go
```

**Build for a standard 64-bit machine:**
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o core-probe-x64 main.go
```

**Build for ARM (Raspberry Pi etc):**
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -ldflags="-s -w" -o core-probe-arm main.go
```

The `-ldflags="-s -w"` strips debug symbols to keep the binary small. `CGO_ENABLED=0` ensures it has zero external dependencies — it will run on any Linux system regardless of what libraries are or aren't installed.

---

## Scoring

The score is built from five categories:

| Category | Max Points | What it measures |
|---|---|---|
| CPU | 30 | Core count, 64-bit support, virtualization capability |
| RAM | 30 | Total memory — the single biggest factor in what software will run |
| Disk | 20 | Storage capacity and available space |
| Thermal | 10 | Whether the machine is running at safe temperatures |
| Kernel | 10 | How modern the Linux kernel is |

**Verdicts:**

| Score | Verdict |
|---|---|
| 75 – 100 | EXCELLENT — Full revival candidate |
| 55 – 74 | GOOD — Viable for targeted deployment |
| 35 – 54 | MARGINAL — Single-purpose only |
| 0 – 34 | POOR — Parts or disposal recommended |

---

## Requirements

**Target machine (the old hardware being probed):**
- Linux kernel 2.6 or later — that covers anything from roughly 2004 onward
- No runtime, no libraries, no package manager needed
- Works without a network connection

**Development machine (only needed if building from source):**
- Go 1.21+
- Linux, macOS, or Windows

---

## Why a Static Binary?

Most software today assumes a modern operating system with up-to-date libraries. Old machines often can't run modern software not because they're too slow, but because the software refuses to start without library versions that don't exist on old systems.

`core-probe` is compiled with `CGO_ENABLED=0` — this tells the Go compiler to bundle everything the program needs into a single self-contained file. There is nothing to install, no version conflicts, no dependency errors. If the machine can run Linux at all, it can run `core-probe`.

---

## Project Structure

```
core-probe/
├── main.go       # Everything. Single file, no external packages.
└── go.mod        # Go module definition.
```

Intentionally minimal. The entire toolkit is one Go source file.

---

## License
MIT 

## Creator 
Ben Santora - github.com/bensantora-tech
