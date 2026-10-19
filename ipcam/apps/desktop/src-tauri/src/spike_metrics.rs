//! SPIKE-ONLY (plan 2026-10-18-phase0b-video-spike, Tasks 11 and 11b), behind
//! the cargo feature `spike-metrics`: the Tauri command `spike_metrics`, which
//! the spike app reads as `window.spikeNative.metrics()` (native-tauri.ts) for
//! MV6 (CPU) and MV7 (memory) on WIN-1, with the OS and WebView2 versions.
//!
//! The app's share is the app process plus the `msedgewebview2.exe` processes
//! under it: WebView2 decodes and renders in its own processes, so the app
//! process alone would hide most of the cost. CPU percentages cover the time
//! since the previous call, and both are shares of the whole machine, 0–100,
//! like Task Manager's. sysinfo's first `cpu_usage()` of a process it has not
//! seen before is not meaningful (there is no earlier sample), so neither is
//! the first call's CPU: every WebView2 process starts after the sampler. The
//! spike app's scenario runner makes that call before a scenario and discards
//! its CPU. A WebView2 process that starts later is likewise off in the first
//! call that sees it.
//!
//! The sums fail closed (R52): when no WebView2 process runs under the app,
//! they would cover the app process alone, so they are `None` (null, n/a in the
//! page), and `webview2_procs`, which the page records, is 0. The shell is
//! single-instance (main.rs) because every instance shares the first one's
//! WebView2 processes. The answer also names the PC's CPU, GPU and memory,
//! which the report notes for WIN-1.

use serde::Serialize;
use std::sync::{Mutex, PoisonError};
use sysinfo::{CpuRefreshKind, Pid, ProcessRefreshKind, ProcessesToUpdate, RefreshKind, System};
use tauri::{Builder, Runtime, State};

const WEBVIEW2: &str = "msedgewebview2.exe";
const MIB: f32 = 1024.0 * 1024.0;

/// Registers the command and the sampler it reads.
pub fn install<R: Runtime>(builder: Builder<R>) -> Builder<R> {
    builder
        .manage(Sampler::new())
        .invoke_handler(tauri::generate_handler![spike_metrics])
}

/// Runs on the async runtime, not the main thread: a refresh of every process takes a while.
#[tauri::command(async)]
fn spike_metrics(sampler: State<'_, Sampler>) -> SpikeMetrics {
    sampler.sample()
}

/// What `spike_metrics` answers. The page maps it to its NativeMetrics. The
/// app's sums are `None` when no WebView2 process is counted (R52).
#[derive(Debug, Clone, PartialEq, Serialize)]
struct SpikeMetrics {
    cpu_pct_total: f32,
    cpu_pct_app: Option<f32>,
    /// The working sets, in which a page that several processes share counts in each.
    rss_mb: Option<f32>,
    /// Private bytes (commit charge), which the page records as memMb (R37).
    private_mb: Option<f32>,
    webview2_procs: u32,
    /// The PC's physical memory in MiB; 0 when the shell cannot tell.
    ram_mb: u64,
    cpu_model: String,
    gpu: String,
    /// The long OS version, such as "Windows 11 Pro".
    os: String,
    /// The WebView2 Runtime version, such as "154.0.4258.62".
    webview: String,
}

/// One process, as the sampler reads it from the system.
#[derive(Debug, Clone, PartialEq)]
struct Proc {
    pid: u32,
    parent: Option<u32>,
    name: String,
    /// Percent of one logical CPU since the previous sample.
    cpu_pct: f32,
    rss_bytes: u64,
    private_bytes: u64,
}

/// What the sampler reads about the machine besides its processes.
#[derive(Debug, Clone, Default, PartialEq)]
struct Machine {
    cpus: usize,
    cpu_pct_total: f32,
    /// Physical memory, MiB.
    ram_mb: u64,
    cpu_model: String,
    gpu: String,
    os: String,
    webview: String,
}

/// The metrics of the app process `app` and of the WebView2 processes under
/// it: the browser process is the app's child, and its renderer, GPU and
/// utility processes are the browser's children. Without any, the app's sums
/// are `None` (R52).
fn sum(app: u32, procs: &[Proc], machine: &Machine) -> SpikeMetrics {
    let joins = |p: &Proc, members: &[u32]| {
        !members.contains(&p.pid)
            && p.name.eq_ignore_ascii_case(WEBVIEW2)
            && p.parent.is_some_and(|parent| members.contains(&parent))
    };
    let mut members = vec![app];
    while let Some(p) = procs.iter().find(|p| joins(p, &members)) {
        members.push(p.pid);
    }
    let counted: Vec<&Proc> = procs.iter().filter(|p| members.contains(&p.pid)).collect();
    let found = members.len() > 1;
    let total = |f: fn(&Proc) -> f32| found.then(|| counted.iter().copied().map(f).sum::<f32>());
    SpikeMetrics {
        cpu_pct_total: machine.cpu_pct_total,
        cpu_pct_app: total(|p| p.cpu_pct).map(|cpu| cpu / machine.cpus.max(1) as f32),
        rss_mb: total(|p| p.rss_bytes as f32 / MIB),
        private_mb: total(|p| p.private_bytes as f32 / MIB),
        webview2_procs: u32::try_from(members.len() - 1).unwrap_or(u32::MAX),
        ram_mb: machine.ram_mb,
        cpu_model: machine.cpu_model.clone(),
        gpu: machine.gpu.clone(),
        os: machine.os.clone(),
        webview: machine.webview.clone(),
    }
}

/// Reads the system for the command. It keeps one `System`, because sysinfo
/// computes CPU usage from the difference between two refreshes.
struct Sampler {
    system: Mutex<System>,
    app: Pid,
    /// The machine as read at start; each sample reads its CPU usage again.
    machine: Machine,
}

impl Sampler {
    fn new() -> Self {
        let cpus = CpuRefreshKind::nothing().with_cpu_usage();
        let mut system = System::new_with_specifics(RefreshKind::nothing().with_cpu(cpus));
        refresh(&mut system);
        system.refresh_memory();
        let machine = Machine {
            cpus: system.cpus().len(),
            cpu_pct_total: system.global_cpu_usage(),
            ram_mb: system.total_memory() >> 20,
            cpu_model: system
                .cpus()
                .first()
                .map_or(String::new(), |c| c.brand().trim().to_owned()),
            gpu: gpu_names(),
            os: System::long_os_version().unwrap_or_default(),
            webview: tauri::webview_version().unwrap_or_default(),
        };
        Self {
            system: Mutex::new(system),
            app: Pid::from_u32(std::process::id()),
            machine,
        }
    }

    fn sample(&self) -> SpikeMetrics {
        let mut system = self.system.lock().unwrap_or_else(PoisonError::into_inner);
        refresh(&mut system);
        let procs: Vec<Proc> = system
            .processes()
            .values()
            .map(|p| Proc {
                pid: p.pid().as_u32(),
                parent: p.parent().map(Pid::as_u32),
                name: p.name().to_string_lossy().into_owned(),
                cpu_pct: p.cpu_usage(),
                rss_bytes: p.memory(),
                // On Windows, sysinfo reads PrivateUsage into virtual_memory().
                private_bytes: p.virtual_memory(),
            })
            .collect();
        let machine = Machine {
            cpu_pct_total: system.global_cpu_usage(),
            ..self.machine.clone()
        };
        sum(self.app.as_u32(), &procs, &machine)
    }
}

fn refresh(system: &mut System) {
    system.refresh_cpu_usage();
    let kind = ProcessRefreshKind::nothing().with_cpu().with_memory();
    system.refresh_processes_specifics(ProcessesToUpdate::All, true, kind);
}

/// The names of the display adapters (DXGI), comma-separated; empty when there
/// is none. Adapters that DXGI flags as software, such as the Microsoft Basic
/// Render Driver next to a GPU, are left out. On a machine without a GPU
/// driver, such as the CI runner, that driver is not flagged and is named.
#[cfg(windows)]
fn gpu_names() -> String {
    use windows::Win32::Graphics::Dxgi::{
        CreateDXGIFactory1, DXGI_ADAPTER_FLAG_SOFTWARE, IDXGIFactory1,
    };
    // SAFETY: plain DXGI calls; the windows crate releases the COM objects.
    let Ok(factory) = (unsafe { CreateDXGIFactory1::<IDXGIFactory1>() }) else {
        return String::new();
    };
    let mut names = Vec::new();
    // EnumAdapters1 fails with DXGI_ERROR_NOT_FOUND after the last adapter.
    for index in 0.. {
        // SAFETY: as above.
        let Ok(adapter) = (unsafe { factory.EnumAdapters1(index) }) else {
            break;
        };
        // SAFETY: as above.
        let Ok(desc) = (unsafe { adapter.GetDesc1() }) else {
            continue;
        };
        if desc.Flags & DXGI_ADAPTER_FLAG_SOFTWARE.0.cast_unsigned() != 0 {
            continue;
        }
        // The name ends at the first NUL.
        let name = desc.Description.split(|&c| c == 0).next();
        names.push(String::from_utf16_lossy(name.unwrap_or_default()));
    }
    names.join(", ")
}

#[cfg(not(windows))]
fn gpu_names() -> String {
    String::new()
}

#[cfg(test)]
mod tests {
    use super::*;

    const MIB: u64 = 1024 * 1024;

    fn proc(
        pid: u32,
        parent: Option<u32>,
        name: &str,
        cpu_pct: f32,
        rss_mib: u64,
        private_mib: u64,
    ) -> Proc {
        Proc {
            pid,
            parent,
            name: name.to_owned(),
            cpu_pct,
            rss_bytes: rss_mib * MIB,
            private_bytes: private_mib * MIB,
        }
    }

    #[test]
    fn spike_metrics_sums_children() {
        let procs = [
            proc(4, None, "System", 3.0, 1, 0),
            proc(100, Some(40), "ipcam-desktop.exe", 5.0, 50, 30),
            // The WebView2 browser process is the app's child; its renderer,
            // GPU and utility processes are the browser's children.
            proc(200, Some(100), "msedgewebview2.exe", 10.0, 100, 40),
            proc(300, Some(200), "msedgewebview2.exe", 20.0, 200, 150),
            proc(301, Some(200), "MSEdgeWebView2.exe", 4.0, 30, 10),
            // Another app's WebView2, and a child of the app that is not WebView2.
            proc(400, Some(999), "msedgewebview2.exe", 50.0, 500, 400),
            proc(500, Some(100), "conhost.exe", 1.0, 5, 2),
        ];
        let machine = Machine {
            cpus: 4,
            cpu_pct_total: 37.5,
            ram_mb: 16069,
            cpu_model: "Intel(R) Core(TM) i5-12400".to_owned(),
            gpu: "Intel(R) UHD Graphics 730".to_owned(),
            os: "Windows 11 Pro".to_owned(),
            webview: "154.0.4258.62".to_owned(),
        };

        let metrics = sum(100, &procs, &machine);

        assert_eq!(
            metrics,
            SpikeMetrics {
                cpu_pct_total: 37.5,
                // (5 + 10 + 20 + 4) % of one CPU, over 4 CPUs.
                cpu_pct_app: Some(9.75),
                rss_mb: Some(380.0),
                // 30 + 40 + 150 + 10 MiB.
                private_mb: Some(230.0),
                webview2_procs: 3,
                ram_mb: 16069,
                cpu_model: "Intel(R) Core(TM) i5-12400".to_owned(),
                gpu: "Intel(R) UHD Graphics 730".to_owned(),
                os: "Windows 11 Pro".to_owned(),
                webview: "154.0.4258.62".to_owned(),
            }
        );
    }

    #[test]
    fn without_webview2_the_app_sums_are_na() {
        // A second instance: the WebView2 processes run under the first one (R52).
        let app = proc(100, Some(40), "ipcam-desktop.exe", 5.0, 50, 30);
        let first = proc(200, Some(90), "msedgewebview2.exe", 10.0, 100, 40);
        let metrics = sum(100, &[app, first], &Machine::default());
        let sums = (metrics.cpu_pct_app, metrics.rss_mb, metrics.private_mb);
        assert_eq!((sums, metrics.webview2_procs), ((None, None, None), 0));
    }
}
