//! SPIKE-ONLY (plan 2026-10-18-phase0b-video-spike, Task 11), behind the cargo
//! feature `spike-metrics`: the Tauri command `spike_metrics`, which the spike
//! app reads as `window.spikeNative.metrics()` (native-tauri.ts) for MV6 (CPU)
//! and MV7 (memory) on WIN-1.
//!
//! The app's share is the app process plus the `msedgewebview2.exe` processes
//! under it: WebView2 decodes and renders in its own processes, so the app
//! process alone would hide most of the cost. CPU percentages cover the time
//! since the previous call (the first call: since the app started), and both
//! are shares of the whole machine, 0–100, like Task Manager's.

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

/// What `spike_metrics` answers. The page maps it to its NativeMetrics.
#[derive(Debug, Clone, PartialEq, Serialize)]
struct SpikeMetrics {
    cpu_pct_total: f32,
    cpu_pct_app: f32,
    rss_mb: f32,
    webview2_procs: u32,
    cpu_model: String,
    gpu: String,
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
}

/// What the sampler reads about the machine besides its processes.
#[derive(Debug, Clone, PartialEq)]
struct Machine {
    cpus: usize,
    cpu_pct_total: f32,
    cpu_model: String,
    gpu: String,
}

/// The metrics of the app process `app` and of the WebView2 processes under
/// it: the browser process is the app's child, and its renderer, GPU and
/// utility processes are the browser's children.
fn sum(app: u32, procs: &[Proc], machine: &Machine) -> SpikeMetrics {
    let mut members = vec![app];
    loop {
        let found: Vec<u32> = procs
            .iter()
            .filter(|p| {
                !members.contains(&p.pid)
                    && p.name.eq_ignore_ascii_case(WEBVIEW2)
                    && p.parent.is_some_and(|parent| members.contains(&parent))
            })
            .map(|p| p.pid)
            .collect();
        if found.is_empty() {
            break;
        }
        members.extend(found);
    }
    let counted = procs.iter().filter(|p| members.contains(&p.pid));
    let (cpu_pct, rss_bytes) = counted.fold((0.0, 0), |(cpu, rss), p| {
        (cpu + p.cpu_pct, rss + p.rss_bytes)
    });
    SpikeMetrics {
        cpu_pct_total: machine.cpu_pct_total,
        cpu_pct_app: cpu_pct / machine.cpus.max(1) as f32,
        rss_mb: rss_bytes as f32 / MIB,
        webview2_procs: u32::try_from(members.len() - 1).unwrap_or(u32::MAX),
        cpu_model: machine.cpu_model.clone(),
        gpu: machine.gpu.clone(),
    }
}

/// Reads the system for the command. It keeps one `System`, because sysinfo
/// computes CPU usage from the difference between two refreshes.
struct Sampler {
    system: Mutex<System>,
    app: Pid,
    cpu_model: String,
    gpu: String,
}

impl Sampler {
    fn new() -> Self {
        let cpus = CpuRefreshKind::nothing().with_cpu_usage();
        let mut system = System::new_with_specifics(RefreshKind::nothing().with_cpu(cpus));
        refresh(&mut system);
        let cpu_model = system
            .cpus()
            .first()
            .map(|cpu| cpu.brand().trim().to_owned());
        Self {
            system: Mutex::new(system),
            app: Pid::from_u32(std::process::id()),
            cpu_model: cpu_model.unwrap_or_default(),
            gpu: gpu_names(),
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
            })
            .collect();
        let machine = Machine {
            cpus: system.cpus().len(),
            cpu_pct_total: system.global_cpu_usage(),
            cpu_model: self.cpu_model.clone(),
            gpu: self.gpu.clone(),
        };
        sum(self.app.as_u32(), &procs, &machine)
    }
}

fn refresh(system: &mut System) {
    system.refresh_cpu_usage();
    let kind = ProcessRefreshKind::nothing().with_cpu().with_memory();
    system.refresh_processes_specifics(ProcessesToUpdate::All, true, kind);
}

/// The names of the hardware display adapters (DXGI), comma-separated; empty
/// when there is none. Software adapters such as the Microsoft Basic Render
/// Driver are left out.
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
        let end = desc
            .Description
            .iter()
            .position(|&c| c == 0)
            .unwrap_or(desc.Description.len());
        names.push(String::from_utf16_lossy(&desc.Description[..end]));
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

    fn proc(pid: u32, parent: Option<u32>, name: &str, cpu_pct: f32, rss_mib: u64) -> Proc {
        Proc {
            pid,
            parent,
            name: name.to_owned(),
            cpu_pct,
            rss_bytes: rss_mib * MIB,
        }
    }

    #[test]
    fn spike_metrics_sums_children() {
        let procs = [
            proc(4, None, "System", 3.0, 1),
            proc(100, Some(40), "ipcam-desktop.exe", 5.0, 50),
            // The WebView2 browser process is the app's child; its renderer,
            // GPU and utility processes are the browser's children.
            proc(200, Some(100), "msedgewebview2.exe", 10.0, 100),
            proc(300, Some(200), "msedgewebview2.exe", 20.0, 200),
            proc(301, Some(200), "MSEdgeWebView2.exe", 4.0, 30),
            // Another app's WebView2, and a child of the app that is not WebView2.
            proc(400, Some(999), "msedgewebview2.exe", 50.0, 500),
            proc(500, Some(100), "conhost.exe", 1.0, 5),
        ];
        let machine = Machine {
            cpus: 4,
            cpu_pct_total: 37.5,
            cpu_model: "Intel(R) Core(TM) i5-12400".to_owned(),
            gpu: "Intel(R) UHD Graphics 730".to_owned(),
        };

        let metrics = sum(100, &procs, &machine);

        assert_eq!(
            metrics,
            SpikeMetrics {
                cpu_pct_total: 37.5,
                // (5 + 10 + 20 + 4) % of one CPU, over 4 CPUs.
                cpu_pct_app: 9.75,
                rss_mb: 380.0,
                webview2_procs: 3,
                cpu_model: "Intel(R) Core(TM) i5-12400".to_owned(),
                gpu: "Intel(R) UHD Graphics 730".to_owned(),
            }
        );
    }
}
