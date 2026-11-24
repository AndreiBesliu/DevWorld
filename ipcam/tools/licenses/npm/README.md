# License texts quoted by hand

npm packages whose published tarball has no license file. Each folder,
`<name>@<version>/`, holds the license files of the package's repository at
that version's tag, copied unchanged; `tools/scripts/third-party-notices.sh`
quotes them when the installed package has none, and fails for a package
that has neither.

- `@fabianlars/tauri-plugin-oauth@2.1.0/`: `LICENSE_MIT` and
  `LICENSE_APACHE-2.0` of https://github.com/FabianLars/tauri-plugin-oauth at
  `v2.1.0` (package.json: `MIT OR Apache-2.0`).
