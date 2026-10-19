#!/usr/bin/env bash
# Tests of tools/scripts/check-guide-labels.sh on the program files in
# testdata/guide-labels: a hub's sources (hub.go.txt) and its build (hub.bin,
# made here, without the text that the sources keep behind a build tag), an
# app's texts (i18n.ts.txt) and bundle (bundle.js.txt), an Android app's
# strings.xml, and texts that hold "Da" and "Nu" only inside longer words.
#
#   tools/scripts/test/check-guide-labels.test.sh
#
# Prints one line per case and exits 1 when a case fails.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${here}/../check-guide-labels.sh"
data="${here}/testdata/guide-labels"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# The hub's build: the texts among NUL bytes, as a binary holds them.
printf '\177ELF\0\0%s\0%s\0%s\0\0' 'Adresele acestui calculator în rețeaua locală:' \
  'Camera %s este conectată: %s, profil %s, %s.' 'Apăsați Enter pentru a închide.' > "${tmp}/hub.bin"

programs=(
  --program hub "${data}/hub.go.txt"
  --program hub "${tmp}/hub.bin"
  --program app "${data}/i18n.ts.txt" "${data}/bundle.js.txt"
  --program android "${data}/strings.xml"
)

# shellcheck source=tools/scripts/test/lib.sh
source "${here}/lib.sh"

# guide <name>: writes standard input as the guide <name>.md and prints its path.
guide() {
  cat > "${tmp}/$1.md"
  printf '%s' "${tmp}/$1.md"
}

ok="$(guide ok <<'EOF'
# Ghid

Textele între «» apar exact așa în programe.

1. În fereastra hub-ului apare «Adresele acestui calculator în rețeaua locală:»<!--hub-->.
2. Apoi «Camera … este conectată»<!--hub-->, iar la final «Apăsați Enter pentru a închide.»<!--hub-->.
3. Deschideți «Proba ipcam»<!--android--> și apăsați «Verifică conexiunea»<!--app-->.

| Stare | Ce faceți |
|---|---|
| «gata în … s»<!--app--> | nimic |

La întrebare, răspundeți «Da»<!--app--> sau «Nu»<!--app-->. Un text repetat, «Da»<!--app-->, se verifică o dată.
Mesajul «Conexiunea a fost refuzată (refused).»<!--app--> are paranteze.
EOF
)"
check 'every quote is in the files of its program' 0 \
  --has "${ok}: 9 quoted texts checked, 0 not found" --lacks 'not found in' -- \
  "${script}" "${ok}" "${programs[@]}"

wrong="$(guide wrong-program <<'EOF'
Apăsați «Verifică conexiunea»<!--hub-->.
EOF
)"
check 'an app text quoted as the hub'"'"'s is not found (no union of all programs)' 1 \
  --has 'not found in hub (hub.go.txt): «Verifică conexiunea»' --has 'not found in hub (hub.bin): «Verifică conexiunea»' \
  --has '1 quoted texts checked, 1 not found' -- \
  "${script}" "${wrong}" "${programs[@]}"

build="$(guide not-built <<'EOF'
Apare «Doar în sursă.»<!--hub-->
EOF
)"
check 'a text in the sources but not in the build is not found' 1 \
  --has 'not found in hub (hub.bin): «Doar în sursă.»' --lacks 'hub.go.txt' -- \
  "${script}" "${build}" "${programs[@]}"

short="$(guide short <<'EOF'
Răspundeți «Da»<!--app--> sau «Nu»<!--app-->.
EOF
)"
check 'short quotes are found between string delimiters' 0 --has '2 quoted texts checked, 0 not found' -- \
  "${script}" "${short}" "${programs[@]}"
check 'short quotes are not found inside longer words' 1 \
  --has 'not found in app (words.ts.txt): «Da»' --has 'not found in app (words.ts.txt): «Nu»' -- \
  "${script}" "${short}" --program app "${data}/words.ts.txt"
xml="$(guide short-xml <<'EOF'
Apăsați «Da»<!--android-->.
EOF
)"
printf '<resources><string name="yes">Da</string></resources>\n' > "${tmp}/yes.xml"
check 'a short quote is found between the tags of strings.xml' 0 --has '1 quoted texts checked, 0 not found' -- \
  "${script}" "${xml}" --program android "${tmp}/yes.xml"

escaped="$(guide escaped <<'EOF'
Mesajul «Conexiunea a fost refuzată (refused).»<!--app-->.
EOF
)"
printf 'Conexiunea a fost refuzată Xrefused)Y\n' > "${tmp}/near.txt"
check 'the special characters of a quote match only themselves' 1 \
  --has 'not found in app (near.txt): «Conexiunea a fost refuzată (refused).»' -- \
  "${script}" "${escaped}" --program app "${tmp}/near.txt"

unnamed="$(guide unnamed <<'EOF'
Apăsați «Verifică conexiunea».
EOF
)"
check 'a quote without a program fails' 1 --has 'unnamed.md:1: «Verifică conexiunea» names no program' -- \
  "${script}" "${unnamed}" "${programs[@]}"

unknown="$(guide unknown <<'EOF'
Apăsați «Verifică conexiunea»<!--tv-->.
EOF
)"
check 'a quote of a program without files fails' 1 \
  --has 'unknown.md:1: «Verifică conexiunea» names the program tv, which has no files here' -- \
  "${script}" "${unknown}" "${programs[@]}"

unclosed="$(guide unclosed <<'EOF'
Corect: «Da»<!--app-->.
O citare «deschisă
EOF
)"
check 'a quote that is not closed on its line fails' 1 --has 'not closed on their line' -- \
  "${script}" "${unclosed}" "${programs[@]}"

nested="$(guide nested <<'EOF'
Apare «întrerupt: apăsați «Repetă».»<!--app-->
EOF
)"
check 'a quote inside a quote fails' 1 --has 'a quote inside a quote' -- \
  "${script}" "${nested}" "${programs[@]}"

none="$(guide none <<'EOF'
Un ghid fără citate, doar cu „texte ale sistemului”.
EOF
)"
check 'a guide that quotes nothing fails' 1 --has 'quotes no text between « and »' -- \
  "${script}" "${none}" "${programs[@]}"

check 'no arguments is a usage error' 2 --has usage -- "${script}"
check 'a file without a program is a usage error' 2 --has usage -- "${script}" "${ok}" "${data}/i18n.ts.txt"
check 'a program without files is a usage error' 2 --has usage -- "${script}" "${ok}" --program app
check 'a program without a name is a usage error' 2 --has usage -- "${script}" "${ok}" --program
check 'a missing file is a usage error' 2 --has "${tmp}/nothing.js: no such file" -- \
  "${script}" "${ok}" --program app "${tmp}/nothing.js"

finish check-guide-labels.test.sh
