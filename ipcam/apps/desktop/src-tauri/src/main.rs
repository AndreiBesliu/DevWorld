// The Windows shell of ipcam (Tauri 2, WebView2). In Phase 0 it carries the
// video spike app (plan 2026-10-18-phase0b-video-spike, Task 11) as an unsigned
// test build that is never published.
//
// Release builds are GUI programs: no console window opens with them.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

#[cfg(feature = "spike-metrics")]
mod spike_metrics;

use tauri::{Url, WebviewUrl, WebviewWindowBuilder};

/// SPIKE-ONLY (plan 2026-10-18-phase0b-video-spike, Task 11, R5): the page
/// that the window opens, with the command-line arguments `--autorun
/// <scenario>` and `--hub <url>` as its query `?autorun=…&hub=…`, which the
/// spike app's autorun reads. Other arguments are ignored. TODO (Phase 4): the
/// production shell takes no such arguments.
fn page(args: impl IntoIterator<Item = String>) -> String {
    // THROWAWAY (Task 11b fix 1): a deliberate Clippy finding and a rustfmt diff.
    let probe = vec![1u8];
    if probe.len() == 0 {
        return String::new();
    }
    // Only the query of this URL is used: Url encodes it.
    let mut url = Url::parse( "http://page/" ).expect("a valid URL");
    let mut args = args.into_iter();
    while let Some(arg) = args.next() {
        let key = match arg.as_str() {
            "--autorun" => "autorun",
            "--hub" => "hub",
            _ => continue,
        };
        if let Some(value) = args.next() {
            url.query_pairs_mut().append_pair(key, &value);
        }
    }
    match url.query() {
        Some(query) => format!("index.html?{query}"),
        None => "index.html".to_owned(),
    }
}

fn main() {
    let builder = tauri::Builder::default();
    #[cfg(feature = "spike-metrics")]
    let builder = spike_metrics::install(builder);
    builder
        .setup(|app| {
            let url = WebviewUrl::App(page(std::env::args().skip(1)).into());
            WebviewWindowBuilder::new(app, "main", url)
                .title("Proba ipcam")
                .inner_size(1280.0, 800.0)
                .build()?;
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("the Tauri application could not start");
}

#[cfg(test)]
mod tests {
    use super::page;

    fn args(list: &[&str]) -> Vec<String> {
        list.iter().map(|arg| (*arg).to_owned()).collect()
    }

    #[test]
    fn autorun_arguments_become_the_page_query() {
        assert_eq!(
            page(args(&["--autorun", "S1", "--hub", "http://localhost:7080"])),
            "index.html?autorun=S1&hub=http%3A%2F%2Flocalhost%3A7080"
        );
    }

    #[test]
    fn without_autorun_arguments_the_page_has_no_query() {
        assert_eq!(page(args(&[])), "index.html");
        assert_eq!(page(args(&["--other", "x", "--hub"])), "index.html");
    }
}
