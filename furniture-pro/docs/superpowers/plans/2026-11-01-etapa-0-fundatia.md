# Etapa 0 – Fundația: plan de implementare

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** O aplicație Windows „Furniture Pro” goală, care afișează versiunea nucleului de calcul Rust. GitHub o testează și o construiește automat, iar instalatorul Windows se poate descărca.

**Architecture:** Un workspace Rust în `furniture-pro/` cu două crate-uri:
- `crates/fp-core`: nucleul de calcul, fără nicio dependență de interfață;
- `app/src-tauri`: cadrul Tauri, subțire, care conține doar comenzi ce apelează nucleul.

Interfața React din `app/` folosește tipuri TypeScript generate automat din Rust cu ts-rs, deci contractul nucleu–interfață are o singură sursă. GitHub Actions rulează testele pe Linux și construiește instalatorul pe Windows.

**Tech Stack:** Rust 1.97.0 (edition 2024), Tauri 2.12, serde 1, ts-rs 12, Node 22, npm, React 19.3, Vite 8, TypeScript 6.0, Vitest 5, Testing Library, jsdom, cargo-deny 0.20, GitHub Actions.

**Spec:** `furniture-pro/docs/superpowers/specs/2026-11-01-furniture-pro-arhitectura-design.md`: secțiunea 8 (Etapa 0) și principiile 3, 6, 7, 9 din secțiunea 3.1.

**Ramură:** `furniture-pro/etapa-0`, creată din `main` după integrarea PR-ului cu specificația. La final se deschide un PR ciornă.

## Global Constraints

- Lucrezi doar în `furniture-pro/` și în `.github/workflows/furniture-pro-ci.yml`, conform `CLAUDE.md` de la rădăcina depozitului. Modificarea din `.github/` se menționează în PR.
- Codul, identificatorii, comentariile din cod și mesajele de commit sunt în engleză. Textele din interfață și documentele sunt în română, cu diacritice corecte (ă, â, î, ș, ț).
- Versiunea produsului are o singură sursă: `version = "0.1.0"` în `[workspace.package]` din `furniture-pro/Cargo.toml`. `tauri.conf.json` și `package.json` nu conțin câmpul `version`.
- Rust: toolchain fixat `1.97.0` în `rust-toolchain.toml`, edition `2024`, `resolver = "3"`.
- Licența proprie: `license = "LicenseRef-Proprietary"` și `publish = false` pentru crate-uri; `"private": true` și `"license": "UNLICENSED"` în `package.json`.
- Dependențe permise doar cu licențe permisive (MIT, Apache-2.0, BSD, ISC, Zlib, Unicode, CC0, BSL-1.0, 0BSD, Unlicense, CDLA-Permissive-2.0) sau MPL-2.0. Nicio dependență GPL, LGPL sau AGPL. Dacă una pare necesară, te oprești și raportezi.
- Interfața nu calculează nimic. Primește date doar prin comenzi Tauri care apelează `fp-core` (regula 7 a proprietarului).
- Tipurile TypeScript pentru datele venite din Rust se generează cu ts-rs în `app/src/bindings/` și se comit. Nu se scriu și nu se editează manual.
- Orice eroare a nucleului se afișează vizibil în interfață. Nu se afișează niciodată valori goale sau inventate (principiul 7).
- Identificatorul aplicației este `ro.furniturepro.app` (provizoriu, se poate schimba până la Etapa 10).
- Instalatorul Windows este NSIS (`"targets": ["nsis"]`), nesemnat în Etapa 0.
- Acțiunile GitHub folosite: `actions/checkout@v7`, `actions/setup-node@v7`, `actions/upload-artifact@v7`, `Swatinem/rust-cache@v2`, `EmbarkStudios/cargo-deny-action@v2`.

## Review Focus

1. **Comanda către nucleu eșuează** (de exemplu, din cauza unei politici de securitate greșite în versiunea finală) → interfața afișează „Nucleul de calcul nu răspunde: …”, nu o pagină goală. Test în Task 2: `shows a visible error when the core command fails`.
2. **Nucleul răspunde lent** → interfața afișează „Se încarcă…”, nu un ecran gol. Test în Task 2: `shows a loading state before the core answers`.
3. **Un tip Rust se schimbă, dar fișierele TypeScript nu se regenerează** → CI eșuează. Pas în Task 4: verificarea `git status --porcelain -- app/src/bindings`.
4. **Numele comenzii diferă între interfață și Rust** → testul Rust eșuează. Test în Task 3: `get_core_info_command_returns_camel_case_core_info`.
5. **Diacriticele românești** (ș, ț cu virgulă dedesubt) ajung deformate → testele din Task 2 compară textul exact „Etapa 0 – fundația”, iar `index.html` are `<meta charset="UTF-8">` și `lang="ro"`.

---

## Structura fișierelor

```
.github/workflows/furniture-pro-ci.yml   CI: teste pe Linux, instalator pe Windows
furniture-pro/
  Cargo.toml                 workspace Rust: membri, versiune, licență, dependențe comune
  rust-toolchain.toml        versiunea Rust fixată
  .cargo/config.toml         unde scrie ts-rs fișierele TypeScript generate
  deny.toml                  licențele permise pentru dependențe
  .gitignore
  crates/fp-core/
    Cargo.toml
    src/lib.rs               exporturile publice ale nucleului
    src/info.rs              CoreInfo, core_info(), PRODUCT_NAME + teste
  app/
    package.json, package-lock.json, tsconfig.json, vite.config.ts, index.html, app-icon.svg
    src/main.tsx             pornirea React
    src/App.tsx              ecranul principal (încărcare / informații / eroare)
    src/App.test.tsx         testele interfeței
    src/core/api.ts          singurul loc care apelează comenzile Tauri
    src/strings.ro.ts        toate textele interfeței, în română
    src/styles.css
    src/test-setup.ts
    src/bindings/CoreInfo.ts GENERAT de ts-rs (nu se editează)
    src-tauri/
      Cargo.toml, build.rs, tauri.conf.json, capabilities/default.json
      icons/                 GENERATE din app-icon.svg
      src/main.rs            pornirea aplicației Windows
      src/lib.rs             comenzile Tauri, register_commands(), run() + teste
  docs/tehnic/00-fundatia.md documentația modulului „Fundația”
```

---

### Task 1: Workspace Rust și nucleul `fp-core` cu `CoreInfo`

**Files:**
- Create: `furniture-pro/Cargo.toml`, `furniture-pro/rust-toolchain.toml`, `furniture-pro/.cargo/config.toml`, `furniture-pro/.gitignore`
- Create: `furniture-pro/crates/fp-core/Cargo.toml`, `furniture-pro/crates/fp-core/src/lib.rs`, `furniture-pro/crates/fp-core/src/info.rs`
- Generated and committed: `furniture-pro/app/src/bindings/CoreInfo.ts`
- Test: `#[cfg(test)] mod tests` în `furniture-pro/crates/fp-core/src/info.rs`

**Interfaces:**
- Consumes: —
- Produces:
  - `fp_core::PRODUCT_NAME: &str = "Furniture Pro"`;
  - `fp_core::CoreInfo { pub product_name: String, pub core_version: String }`, cu derive `Debug, Clone, PartialEq, Eq, serde::Serialize, ts_rs::TS`, `#[serde(rename_all = "camelCase")]` și `#[ts(export)]`;
  - `fp_core::core_info() -> CoreInfo`;
  - fișierul TypeScript `app/src/bindings/CoreInfo.ts`, care exportă tipul `CoreInfo = { productName: string, coreVersion: string }`.

- [ ] **Step 1: Create the workspace files**

`furniture-pro/Cargo.toml`:
```toml
[workspace]
resolver = "3"
members = ["crates/fp-core"]

[workspace.package]
version = "0.1.0"
edition = "2024"
rust-version = "1.97"
license = "LicenseRef-Proprietary"
publish = false

[workspace.dependencies]
serde = { version = "1", features = ["derive"] }
serde_json = "1"
ts-rs = "12"
```

`furniture-pro/rust-toolchain.toml`:
```toml
[toolchain]
channel = "1.97.0"
components = ["rustfmt", "clippy"]
```

`furniture-pro/.cargo/config.toml` face ca ts-rs să scrie în `furniture-pro/app/src/bindings`:
```toml
[env]
TS_RS_EXPORT_DIR = { value = "app/src/bindings", relative = true }
```

`furniture-pro/.gitignore`:
```
/target/
/app/node_modules/
/app/dist/
/app/src-tauri/gen/
```

`crates/fp-core/Cargo.toml`:
- pachetul se numește `fp-core`;
- câmpurile `version`, `edition`, `rust-version`, `license` și `publish` se iau din workspace (`*.workspace = true`);
- dependențe: `serde` și `ts-rs`, din workspace;
- dependență doar pentru teste (`dev-dependencies`): `serde_json`, din workspace.

`src/lib.rs` declară `mod info;` și reexportă `CoreInfo`, `core_info` și `PRODUCT_NAME`.

- [ ] **Step 2: Write the failing tests in `crates/fp-core/src/info.rs`**

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn core_info_reports_product_name_and_package_version() {
        let info = core_info();
        assert_eq!(info.product_name, "Furniture Pro");
        assert_eq!(info.core_version, env!("CARGO_PKG_VERSION"));
    }

    #[test]
    fn core_info_serializes_with_camel_case_keys() {
        let json = serde_json::to_value(core_info()).unwrap();
        assert_eq!(
            json,
            serde_json::json!({
                "productName": "Furniture Pro",
                "coreVersion": env!("CARGO_PKG_VERSION")
            })
        );
    }
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd furniture-pro && cargo test -p fp-core`
Expected: compilarea eșuează cu „cannot find function `core_info`” (sau tipul `CoreInfo` nu există).

- [ ] **Step 4: Implement `PRODUCT_NAME`, `CoreInfo` and `core_info() -> CoreInfo` in `info.rs`**

`core_version` vine din `env!("CARGO_PKG_VERSION")`, deci versiunea are o singură sursă: workspace-ul. Adaugă un comentariu de documentare (`///`) pentru fiecare element public.

- [ ] **Step 5: Run the tests to verify they pass and that the bindings were generated**

Run: `cargo test -p fp-core`
Expected: testele de mai sus trec, plus testul generat automat de ts-rs (`export_bindings_coreinfo`).

Run: `cat app/src/bindings/CoreInfo.ts`
Expected: fișierul există și conține `export type CoreInfo = { productName: string, coreVersion: string, };`. ts-rs poate adăuga un comentariu de antet.

- [ ] **Step 6: Format and lint**

Run: `cargo fmt --all && cargo clippy --workspace --all-targets -- -D warnings`
Expected: nicio eroare și niciun avertisment.

- [ ] **Step 7: Commit**

```bash
git add furniture-pro/Cargo.toml furniture-pro/Cargo.lock furniture-pro/rust-toolchain.toml furniture-pro/.cargo furniture-pro/.gitignore furniture-pro/crates furniture-pro/app/src/bindings
git commit -m "Add Rust workspace and fp-core crate with CoreInfo"
```

---

### Task 2: Interfața React care afișează informațiile nucleului

**Files:**
- Create: `furniture-pro/app/package.json`, `tsconfig.json`, `vite.config.ts`, `index.html`
- Create: `furniture-pro/app/src/main.tsx`, `App.tsx`, `core/api.ts`, `strings.ro.ts`, `styles.css`, `test-setup.ts`
- Generated: `furniture-pro/app/package-lock.json`
- Test: `furniture-pro/app/src/App.test.tsx`

**Interfaces:**
- Consumes:
  - tipul `CoreInfo` din `src/bindings/CoreInfo.ts` (Task 1);
  - numele comenzii Tauri `get_core_info` (implementată în Task 3).
- Produces:
  - `getCoreInfo(): Promise<CoreInfo>` în `src/core/api.ts`, care apelează `invoke<CoreInfo>("get_core_info")` din `@tauri-apps/api/core`;
  - scripturile npm din tabelul de mai jos;
  - serverul de dezvoltare Vite pe portul `1420`, cu `strictPort: true`.

| Script | Comandă | Ce face |
|---|---|---|
| `dev` | `vite` | pornește serverul de dezvoltare |
| `build` | `tsc --noEmit && vite build` | construiește interfața în `app/dist` |
| `typecheck` | `tsc --noEmit` | verifică tipurile |
| `test` | `vitest run` | rulează testele |
| `tauri` | `tauri` | comenzile Tauri |

- [ ] **Step 1: Create the project files and install the dependencies**

`package.json`: `"name": "furniture-pro-app"`, `"private": true`, `"license": "UNLICENSED"`, `"type": "module"`, `"engines": { "node": ">=22.12" }`, fără câmpul `version`.

Rulează din `furniture-pro/app`:
```bash
npm install react@^19.3 react-dom@^19.3 @tauri-apps/api@^2.12
npm install -D @tauri-apps/cli@^2.12 vite@^8 @vitejs/plugin-react@^6 typescript@~6.0 vitest@^5 jsdom @testing-library/react@^16 @testing-library/dom @testing-library/jest-dom@^7 @types/react@^19.3 @types/react-dom@^19.3
```

`vite.config.ts` folosește `defineConfig` din `vitest/config`, cu:
- `plugins: [react()]`;
- `clearScreen: false`;
- `server: { port: 1420, strictPort: true }`;
- `test: { environment: "jsdom", setupFiles: ["./src/test-setup.ts"] }`.

`tsconfig.json`: `strict: true`, `jsx: "react-jsx"`, `target: "ES2022"`, `module: "ESNext"`, `moduleResolution: "bundler"`, `lib: ["ES2022", "DOM", "DOM.Iterable"]`, `noEmit: true`, `include: ["src", "vite.config.ts"]`.

`index.html`: `<html lang="ro">`, `<meta charset="UTF-8">`, `<title>Furniture Pro</title>`, `<div id="root">` și scriptul modul `/src/main.tsx`.

`src/test-setup.ts`:
- importă `@testing-library/jest-dom/vitest`;
- dacă `window.crypto.getRandomValues` lipsește, îl definește cu `randomFillSync` din `node:crypto`, pentru `mockIPC` din Tauri.

`src/strings.ro.ts` conține toate textele interfeței, exact așa:
```ts
export const ro = {
  stageLabel: "Etapa 0 – fundația",
  loading: "Se încarcă…",
  coreVersion: (version: string) => `Nucleul de calcul: versiunea ${version}`,
  coreError: (message: string) => `Nucleul de calcul nu răspunde: ${message}`,
} as const;
```

- [ ] **Step 2: Write the failing tests in `src/App.test.tsx`**

Folosește `mockIPC` și `clearMocks` din `@tauri-apps/api/mocks`; `clearMocks()` rulează în `afterEach`.

```tsx
it("shows the product name and core version from the core", async () => {
  mockIPC((cmd) => (cmd === "get_core_info" ? { productName: "Furniture Pro", coreVersion: "0.1.0" } : undefined));
  render(<App />);
  expect(await screen.findByRole("heading", { name: "Furniture Pro" })).toBeInTheDocument();
  expect(screen.getByText("Nucleul de calcul: versiunea 0.1.0")).toBeInTheDocument();
  expect(screen.getByText("Etapa 0 – fundația")).toBeInTheDocument();
});

it("shows a loading state before the core answers", () => {
  mockIPC(() => new Promise(() => {}));
  render(<App />);
  expect(screen.getByText("Se încarcă…")).toBeInTheDocument();
});

it("shows a visible error when the core command fails", async () => {
  mockIPC(() => { throw new Error("comanda lipsește"); });
  render(<App />);
  expect(await screen.findByText("Nucleul de calcul nu răspunde: comanda lipsește")).toBeInTheDocument();
  expect(screen.queryByText(/versiunea/)).not.toBeInTheDocument();
});
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd furniture-pro/app && npm test`
Expected: FAIL, pentru că `./App` nu există.

- [ ] **Step 4: Implement `App`, `getCoreInfo`, `main.tsx` and `styles.css`**

`App` are trei stări: `loading`, `ready(CoreInfo)` și `error(string)`. Apelează `getCoreInfo()` o singură dată, la montare, și ignoră răspunsul dacă a fost demontat între timp.

| Stare | Ce afișează |
|---|---|
| `loading` | `ro.loading` |
| `ready` | un `<h1>` cu `productName`, apoi `ro.stageLabel` și `ro.coreVersion(coreVersion)` |
| `error` | `ro.coreError(message)` |

`message` este `error.message` dacă eroarea este un `Error`, altfel `String(error)`.

`styles.css` conține un layout centrat, cu fontul de sistem: `"Segoe UI", system-ui, sans-serif`.

- [ ] **Step 5: Run the tests, type check and build**

Run: `npm test`
Expected: 3 teste trec.

Run: `npm run typecheck && npm run build`
Expected: nicio eroare; `app/dist/index.html` există.

- [ ] **Step 6: Commit**

```bash
git add furniture-pro/app/package.json furniture-pro/app/package-lock.json furniture-pro/app/tsconfig.json furniture-pro/app/vite.config.ts furniture-pro/app/index.html furniture-pro/app/src
git commit -m "Add React UI that shows core info with loading and error states"
```

---

### Task 3: Cadrul Tauri care expune comanda `get_core_info`

**Files:**
- Modify: `furniture-pro/Cargo.toml`: adaugă `"app/src-tauri"` la `members`, plus `tauri = "2.12"` și `tauri-build = "2"` la `[workspace.dependencies]`
- Create: `furniture-pro/app/src-tauri/Cargo.toml`, `build.rs`, `tauri.conf.json`, `capabilities/default.json`, `src/main.rs`, `src/lib.rs`
- Create: `furniture-pro/app/app-icon.svg`; generated: `furniture-pro/app/src-tauri/icons/`
- Test: `#[cfg(test)] mod tests` în `furniture-pro/app/src-tauri/src/lib.rs`

**Interfaces:**
- Consumes: `fp_core::core_info()` și `fp_core::CoreInfo` (Task 1); scripturile npm `dev` și `build` (Task 2).
- Produces:
  - comanda Tauri `get_core_info`, care întoarce JSON-ul `{ "productName": …, "coreVersion": … }`;
  - `pub fn register_commands<R: tauri::Runtime>(builder: tauri::Builder<R>) -> tauri::Builder<R>`, singurul loc în care se înregistrează comenzile;
  - `pub fn run()`;
  - pe Windows, instalatorul `furniture-pro/target/release/bundle/nsis/Furniture Pro_0.1.0_x64-setup.exe`.

- [ ] **Step 1: Install the Linux system packages Tauri needs (container and CI)**

Run: `apt-get install -y libwebkit2gtk-4.1-dev build-essential curl wget file libxdo-dev libssl-dev libayatana-appindicator3-dev librsvg2-dev`
Expected: instalarea se termină fără erori. Pe un sistem fără drepturi de root, prefixează comanda cu `sudo`.

- [ ] **Step 2: Create the crate files**

`app/src-tauri/Cargo.toml`:
- pachetul se numește `furniture-pro`, cu câmpurile comune din workspace;
- `[lib] name = "furniture_pro_lib"`;
- dependențe: `fp-core = { path = "../../crates/fp-core" }`, `tauri` (workspace), `serde` (workspace);
- dependență pentru construcție (`build-dependencies`): `tauri-build` (workspace);
- dependențe pentru teste (`dev-dependencies`): `tauri` cu `features = ["test"]` și `serde_json`.

`build.rs` apelează `tauri_build::build()`.

`tauri.conf.json`:
```json
{
  "$schema": "https://schema.tauri.app/config/2",
  "productName": "Furniture Pro",
  "identifier": "ro.furniturepro.app",
  "build": {
    "beforeDevCommand": "npm run dev",
    "devUrl": "http://localhost:1420",
    "beforeBuildCommand": "npm run build",
    "frontendDist": "../dist"
  },
  "app": {
    "windows": [
      { "title": "Furniture Pro", "width": 1280, "height": 800, "minWidth": 1024, "minHeight": 700 }
    ],
    "security": {
      "csp": "default-src 'self' ipc: http://ipc.localhost; style-src 'self' 'unsafe-inline'"
    }
  },
  "bundle": {
    "active": true,
    "targets": ["nsis"],
    "icon": ["icons/32x32.png", "icons/128x128.png", "icons/128x128@2x.png", "icons/icon.icns", "icons/icon.ico"]
  }
}
```

`capabilities/default.json`: identificator `default`, fereastra `main`, `"permissions": ["core:default"]`.

`app/app-icon.svg` este o pictogramă provizorie, fără text, până la branding (Etapa 10):
```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
  <rect width="1024" height="1024" rx="180" fill="#7a4a24"/>
  <rect x="192" y="200" width="640" height="160" rx="24" fill="#f3e6cf"/>
  <rect x="192" y="432" width="640" height="160" rx="24" fill="#f3e6cf"/>
  <rect x="192" y="664" width="640" height="160" rx="24" fill="#f3e6cf"/>
</svg>
```

Run: `cd furniture-pro/app && npm run tauri icon app-icon.svg`
Expected: `src-tauri/icons/` conține `32x32.png`, `128x128.png`, `128x128@2x.png`, `icon.icns` și `icon.ico`.

- [ ] **Step 3: Write the failing test in `src/lib.rs`**

```rust
#[test]
fn get_core_info_command_returns_camel_case_core_info() {
    // Build a mock app through register_commands(tauri::test::mock_builder()),
    // invoke "get_core_info" with tauri::test::get_ipc_response,
    // and deserialize the response as serde_json::Value.
    assert_eq!(
        response,
        serde_json::json!({
            "productName": "Furniture Pro",
            "coreVersion": env!("CARGO_PKG_VERSION")
        })
    );
}
```

Testul folosește modulul `tauri::test` din Tauri 2.12 (`mock_builder`, `mock_context(noop_assets())`, `WebviewWindowBuilder`, `get_ipc_response`, `INVOKE_KEY`), după exemplul din documentația lui (docs.rs/tauri, modulul `test`). Testul trece prin `register_commands`, deci verifică exact înregistrarea pe care o folosește aplicația reală.

- [ ] **Step 4: Run the test to verify it fails**

Run: `cd furniture-pro && cargo test -p furniture-pro`
Expected: compilarea eșuează, pentru că `register_commands` nu există.

- [ ] **Step 5: Implement the command, `register_commands`, `run` and `main.rs`**

- `#[tauri::command] fn get_core_info() -> fp_core::CoreInfo` doar apelează `fp_core::core_info()`, fără nicio altă logică.
- `run()` construiește aplicația din `register_commands(tauri::Builder::default())` și pornește cu `tauri::generate_context!()`.
- `main.rs` are `#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]` și apelează `furniture_pro_lib::run()`.

- [ ] **Step 6: Run all tests and lint**

Run: `cargo test --workspace && cargo clippy --workspace --all-targets -- -D warnings && cargo fmt --all --check`
Expected: toate testele trec (2 din `fp-core`, testul ts-rs și testul de mai sus); niciun avertisment.

- [ ] **Step 7: Check a full release build on Linux**

Run: `cd furniture-pro/app && npm run tauri build -- --no-bundle`
Expected: construcția se termină, iar `furniture-pro/target/release/furniture-pro` există.

- [ ] **Step 8: Commit**

```bash
git add furniture-pro/Cargo.toml furniture-pro/Cargo.lock furniture-pro/app/app-icon.svg furniture-pro/app/src-tauri
git commit -m "Add Tauri shell exposing get_core_info from fp-core"
```

---

### Task 4: Construcție și testare automată pe GitHub, cu politica de licențe

**Files:**
- Create: `.github/workflows/furniture-pro-ci.yml` (la rădăcina depozitului)
- Create: `furniture-pro/deny.toml`

**Interfaces:**
- Consumes: comenzile din Task 1–3 (`cargo test --workspace`; `npm ci`, `npm run typecheck`, `npm test`, `npm run build`, `npm run tauri build`).
- Produces: un workflow cu două joburi, `checks` și `windows-installer`, și artefactul descărcabil `furniture-pro-windows-installer`.

- [ ] **Step 1: Write `furniture-pro/deny.toml` and check it locally**

Secțiunea `[licenses]`:
- `allow` conține exact licențele din Global Constraints, cu identificatorii SPDX `MIT`, `Apache-2.0`, `Apache-2.0 WITH LLVM-exception`, `BSD-2-Clause`, `BSD-3-Clause`, `ISC`, `Zlib`, `Unicode-3.0`, `Unicode-DFS-2016`, `CC0-1.0`, `BSL-1.0`, `0BSD`, `Unlicense`, `CDLA-Permissive-2.0`, `MPL-2.0`;
- `confidence-threshold = 0.9`;
- crate-urile proprii (`publish = false`) sunt ignorate prin `private = { ignore = true }`.

Run: `cargo install cargo-deny --locked --version 0.20.2 && cd furniture-pro && cargo deny check licenses`
Expected: `licenses ok`.

Dacă o dependență are o licență permisivă care lipsește din listă, o adaugi și o menționezi în commit. Dacă licența este GPL, LGPL sau AGPL, te oprești și raportezi.

- [ ] **Step 2: Write `.github/workflows/furniture-pro-ci.yml`**

Reguli generale:
- **Rulează la:** `push` pe `main`, `pull_request` și `workflow_dispatch`. Pentru `push` și `pull_request`, filtrul `paths` este `furniture-pro/**` și `.github/workflows/furniture-pro-ci.yml`.
- **Concurență:** `concurrency: { group: furniture-pro-${{ github.ref }}, cancel-in-progress: true }`.
- **Folder de lucru:** `defaults.run.working-directory: furniture-pro`.

Jobul `checks` (`ubuntu-24.04`), în ordine:
1. `actions/checkout@v7`;
2. pachetele de sistem din Task 3, Step 1 (cu `sudo apt-get`);
3. `rustup toolchain install` (citește `rust-toolchain.toml`);
4. `Swatinem/rust-cache@v2` cu `workspaces: furniture-pro -> target`;
5. `actions/setup-node@v7` cu `node-version: 22`, `cache: npm` și `cache-dependency-path: furniture-pro/app/package-lock.json`;
6. în `furniture-pro/app`: `npm ci`, `npm run typecheck`, `npm test`, `npm run build`;
7. `cargo fmt --all --check`;
8. `cargo clippy --workspace --all-targets -- -D warnings`;
9. `cargo test --workspace`;
10. verificarea fișierelor generate: `test -z "$(git status --porcelain -- app/src/bindings)"`, cu un mesaj de eroare care cere rularea `cargo test` și comiterea fișierelor din `app/src/bindings`;
11. `EmbarkStudios/cargo-deny-action@v2` cu `manifest-path: furniture-pro/Cargo.toml` și `command: check licenses`.

Jobul `windows-installer` (`windows-latest`, `needs: checks`), în ordine:
1. checkout, `rustup toolchain install`, `rust-cache`, `setup-node` și `npm ci`, ca mai sus;
2. `cargo test -p fp-core` (nucleul testat și pe Windows);
3. `npm run tauri build`, rulat în `furniture-pro/app`;
4. `actions/upload-artifact@v7` cu `name: furniture-pro-windows-installer`, `path: furniture-pro/target/release/bundle/nsis/*-setup.exe`, `if-no-files-found: error` și `retention-days: 14`.

- [ ] **Step 3: Check the YAML syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/furniture-pro-ci.yml'))"`, din rădăcina depozitului.
Expected: nicio eroare.

- [ ] **Step 4: Commit, push and open the draft PR**

```bash
git add .github/workflows/furniture-pro-ci.yml furniture-pro/deny.toml
git commit -m "Add furniture-pro CI: Linux checks and Windows installer build"
git push -u origin furniture-pro/etapa-0
```

Deschide un PR ciornă către `main`. Descrierea menționează fișierul nou din `.github/workflows/`.

- [ ] **Step 5: Verify CI on GitHub**

Run: `gh api "repos/AndreiBesliu/DevWorld/actions/runs?branch=furniture-pro/etapa-0" --jq '.workflow_runs[0] | {status, conclusion}'`, repetat până la `"status": "completed"`.
Expected: `"conclusion": "success"`. Lista de artefacte a rulării (`gh api repos/AndreiBesliu/DevWorld/actions/runs/<id>/artifacts`) conține `furniture-pro-windows-installer`.

Un job roșu se repară după cauza lui, nu prin dezactivarea testelor.

---

### Task 5: Documentația modulului „Fundația” și predarea către proprietar

**Files:**
- Create: `furniture-pro/docs/tehnic/00-fundatia.md`
- Modify: `furniture-pro/README.md`, `furniture-pro/CLAUDE.md`

**Interfaces:**
- Consumes: structura, comenzile și numele din Task 1–4.
- Produces: documentația Etapei 0 și instrucțiunile de verificare pentru proprietar.

- [ ] **Step 1: Write `docs/tehnic/00-fundatia.md` (Romanian, written for a non-programmer reader)**

Secțiuni, în ordine:
1. **Ce conține Etapa 0.**
2. **Structura folderelor:** un tabel, câte un rând pentru fiecare folder din „Structura fișierelor”.
3. **Cum circulă datele:** interfața → comanda `get_core_info` → `fp-core` → înapoi.
4. **Contractul nucleu–interfață:** ts-rs; `app/src/bindings` nu se editează manual.
5. **Versiunea:** sursa unică este `furniture-pro/Cargo.toml`.
6. **Testele:** fiecare comandă și ce dovedește.
7. **Construcția automată:** cele două joburi și verificarea licențelor.
8. **Cum descarci și instalezi aplicația pe Windows:**
   - pe GitHub: tab-ul **Actions** → rularea **furniture-pro CI** a ramurii → **Artifacts** → `furniture-pro-windows-installer`;
   - dezarhivezi și rulezi `Furniture Pro_0.1.0_x64-setup.exe`;
   - la mesajul „Windows a protejat PC-ul”: **Mai multe informații** → **Rulează oricum**. Avertismentul apare pentru că instalatorul nu este încă semnat digital (Etapa 10).
9. **Ce trebuie să vezi:** fereastra „Furniture Pro”, cu textele „Etapa 0 – fundația” și „Nucleul de calcul: versiunea 0.1.0”.
10. **Limitări cunoscute:**
    - instalatorul este nesemnat;
    - identificatorul și pictograma sunt provizorii;
    - pe un Windows fără WebView2, instalatorul are nevoie de internet la prima instalare.
11. **Comenzi pentru dezvoltatori.**

- [ ] **Step 2: Update `furniture-pro/README.md` and `furniture-pro/CLAUDE.md`**

`README.md`:
- „Stare” devine „Etapa 0 (fundația): în lucru”;
- la „Documente” se adaugă `docs/tehnic/00-fundatia.md` și planul acestei etape.

`CLAUDE.md`:
- secțiunea „Current phase” spune că Etapa 0 se implementează după acest plan;
- se adaugă secțiunea „Developer commands”, cu comenzile din Task 1–4 (inclusiv pachetele Linux);
- se adaugă regula „Never hand-edit `app/src/bindings`; run `cargo test` to regenerate”.

- [ ] **Step 3: Check that every path named in the docs exists**

Run: `ls furniture-pro/docs/tehnic/00-fundatia.md furniture-pro/app/src/bindings/CoreInfo.ts .github/workflows/furniture-pro-ci.yml furniture-pro/deny.toml`
Expected: toate cele 4 căi există.

- [ ] **Step 4: Commit and push**

```bash
git add furniture-pro/docs/tehnic furniture-pro/README.md furniture-pro/CLAUDE.md
git commit -m "Document the furniture-pro foundation and how to verify Etapa 0"
git push
```
Expected: CI rulează din nou și este verde.

- [ ] **Step 5: Hand over to the owner**

Îi trimiți proprietarului, în română și pe înțelesul lui:
- linkul către PR;
- linkul către rularea CI care conține instalatorul;
- pașii de verificare din secțiunile 8 și 9 ale documentației.

Etapa se închide doar după confirmarea lui:
1. proprietarul confirmă că a instalat aplicația și vede textele corecte;
2. PR-ul se integrează în `main`, la cererea lui;
3. pe commit-ul de integrare se creează eticheta `furniture-pro/etapa-0-aprobata` și se publică pe GitHub (`git push origin furniture-pro/etapa-0-aprobata`).
