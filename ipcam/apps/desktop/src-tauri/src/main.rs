// The Windows shell of ipcam (Tauri 2, WebView2). In Phase 0 it carries the
// video spike app (plan 2026-10-18-phase0b-video-spike, Task 11) as an unsigned
// test build that is never published.
//
// Release builds are GUI programs: no console window opens with them.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

#[cfg(feature = "spike-metrics")]
mod spike_metrics;

use tauri::{Manager, Url, WebviewUrl, WebviewWindowBuilder};

/// SPIKE-ONLY (plan 2026-10-18-phase0b-video-spike, Task 11, R5): the page
/// that the window opens, with the command-line arguments `--autorun
/// <scenario>` and `--hub <url>` as its query `?autorun=…&hub=…`, which the
/// spike app's autorun reads. Other arguments are ignored. TODO (Phase 4): the
/// production shell takes no such arguments.
fn page(args: impl IntoIterator<Item = String>) -> String {
    // Only the query of this URL is used: Url encodes it.
    let mut url = Url::parse("http://page/").expect("a valid URL");
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
    // Single instance (R52): a second launch restores and focuses the first
    // window, then exits. Every instance would share the first one's WebView2
    // processes, so a second window would measure none of its own; and the
    // installer's «Finish» page starts the app, so a second launch is likely.
    let builder =
        tauri::Builder::default().plugin(tauri_plugin_single_instance::init(|app, _, _| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.unminimize();
                let _ = window.set_focus();
            }
        }));
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
