# Rezultatele Fazei 0 (c1): spike-ul de înregistrare

Raport din 2026-10-30, generat de `recreport` din rezultatele spike-ului (PH0-30). Fiecare poartă e judecată o singură dată, de regula ei din spike (C17); raportul arată verdictele, cu motivele lor, și cifrele din spatele lor. Gazdele apar doar cu pseudonime: `pc` (PC-ul dvs. cu Windows), `pi` (Raspberry Pi-ul dvs.), iar `lab-linux`, `lab-windows` și `lab-qemu` sunt runner-ele CI. Raportul se scrie doar dacă scanerul de redactare nu găsește nimic în el (PH0-22, G28); îl citiți înainte de commit. Acesta e raportul spike-ului c1; raportul Fazei 0, cu tabelul G1–G19, îl scrie ultimul plan al Fazei 0 (G34).

## Rezumat pentru proprietar

- **G10: Pană de curent (MI1):** fără verdict: lipsesc măsurători — lab-linux-qemu-reset: lipsește (și încă 2 motive, mai jos).
- **G11: Independența înregistrării (MI2):** trece.
- **G12: Niveluri hardware (MI3):** pică → pi: numărul recomandat devine 3 — pi: CPU total 75,2 % la 4 camere (limita ≤ 60 %).
- **G13: `age` cu acces aleator (MI4):** trece.
- **G18: Redare prin canalul de date (MI9):** trece.
- **Pi-ul dvs.:** Raspberry Pi 4 Model B Rev 1.5. Nivelul H1b din §4.8, cu 4 camere. Numărul rezultat e la „Modelul Pi-ului și numerele recomandate”.
- **Set de laborator incomplet:** `lab-windows-kill9`: lipsește partea 7 din 21; G10 nu are verdict pentru el până la o nouă rulare din laborator.

Motivele fiecărui verdict sunt la „Verdicte și decizii declanșate”; cifrele, pe gazdă, la „Rezultate pe gazdă”.

## Verdicte și decizii declanșate (PH0-30)

| Poartă | Verdict |
| --- | --- |
| G10: Pană de curent (MI1) | fără verdict: lipsesc măsurători |
| G11: Independența înregistrării (MI2) | trece |
| G12: Niveluri hardware (MI3) | pică → pi: numărul recomandat devine 3 |
| G13: `age` cu acces aleator (MI4) | trece |
| G18: Redare prin canalul de date (MI9) | trece |

### G10: Pană de curent (MI1)

- **Verdict:** fără verdict: lipsesc măsurători
- **Decizie declanșată:** Fără decizie până nu există toate măsurătorile; motivele sunt mai jos.
- **Ce se schimbă în §2 sau în cerințe:** Nimic până atunci.

**Motive:**

- lab-linux-qemu-reset: lipsește
- lab-windows-kill9: 19 din 20 rulări valide
- lab-windows-kill9: 9 din 10 rulări criptate

### G11: Independența înregistrării (MI2)

- **Verdict:** trece
- **Decizie declanșată:** Nicio decizie: ingestul rămâne cel din §4.2.
- **Ce se schimbă în §2 sau în cerințe:** Nimic în §2 sau în cerințe.

**Note (în afara verdictului):**

- Repornirea ipcam-hub făcută de recspike este o limită inferioară pentru recuperarea prin SCM (Windows) și systemd (Linux) din Faza 1 (C07).

### G12: Niveluri hardware (MI3)

- **Verdict:** pică → pi: numărul recomandat devine 3
- **Decizie declanșată:** Planul B al lui G12: pi: numărul recomandat devine 3.
- **Ce se schimbă în §2 sau în cerințe:** §2.2: Decizie tehnică „pi: numărul recomandat devine 3”, cu trimitere la G12 (PH0-31); §4.8: numerele recomandate măsurate.

**Motive:**

- pi: CPU total 75,2 % la 4 camere (limita ≤ 60 %)

**Note (în afara verdictului):**

- Încărcarea live (proxy): sesiunile nivelului, fiecare o grilă cu substream-ul fiecărei camere și un main, servite de pachetul session al spike-ului video din recspike, din RTSP-ul controlorului, către câte un peer Pion doar de recepție în reccontroller; main se preia de două ori, deci CPU-ul e o estimare acoperitoare. CPU-ul procesului live se împarte pe plăci după debitul lor (sub 512 kbps, main 4 Mbps).
- Redarea (proxy): citirea cu decriptare, la 1× (4 Mbps), a unui segment criptat de 5 min, în buclă. Segmentul este cel sintetic de mai jos, nu un segment înregistrat și închis, și rămâne în cache-ul de pagini al sistemului: decriptarea costă la fel, dar citirea de pe disc nu se măsoară.
- Exportul (proxy): decriptarea și recriptarea, la viteză maximă, a unui segment de 5 min, repetată fără pauză tot timpul probei; ieșirea se aruncă.
- Upload-ul (proxy): criptare age și HTTPS PUT, cu ritm fix (10 Mbps; pe H1b și H1c, debitul camerelor), spre un receptor din reccontroller care aruncă datele (fără cost, fără cont); CPU-ul se raportează la debitul atins.
- Segmentul de 5 min al redării, exportului și upload-ului are date aleatoare de mărimea unui main de 4 Mbps: costul criptării nu depinde de conținut.
- Procentele de CPU sunt din întreaga mașină (toate nucleele logice), ca CPU-ul total; ingest-ul per cameră (H1) este dintr-un nucleu, din CPU-ul ipcam-hub și al MediaMTX.
- RAM (proxy): vârful sumei memoriei rezidente a ipcam-hub, MediaMTX și celor patru încărcări (pe Windows, setul de lucru): fiecare încărcare are propriul runtime Go, deci cifra e acoperitoare.
- Părțile închise pe secundă per fișier (proxy): rândul de fsync pe secundă din §4.8 se măsoară ca (părți − 1) ÷ durata lor, din înregistrări; MTX-1 face un fsync la fiecare parte de 1 s, deci cifra stă aproape de 1 prin construcție și nu vede un fsync în plus. Fsync-ul de la închiderea segmentului, unul la 5 min, nu intră în ritm.
- Disc ÷ bitrate la camerele criptate se raportează, nu se judecă (R02): umplutura de la sfârșitul părților ține de format, iar PR-ul raportului propune formularea „≤ 1,03 × în clar; ≤ 1,05 × criptat”. La o cameră criptată sub 512 kbps, cu „Pierdere minimă la pană de curent” pornită (O17), fiecare parte ocupă o bucată age de 64 KiB: ≤ 5,7 GB pe zi la părți de 1 s.
- Rândul de timeline din §4.8 (24 h dintr-un catalog de 1 an × 16 camere, ≤ 50 ms) nu se judecă aici: cere part_index și un catalog de 1 an (HUB-20, Faza 1), iar catalogul minim din c1 are doar segmente și goluri pe câteva zile.

### G13: `age` cu acces aleator (MI4)

- **Verdict:** trece
- **Decizie declanșată:** Nicio decizie: redarea citește în continuare segmentele age cu acces aleator (§8.5); formatul nu se schimbă.
- **Ce se schimbă în §2 sau în cerințe:** Nimic în §2 sau în cerințe.

### G18: Redare prin canalul de date (MI9)

- **Verdict:** trece
- **Decizie declanșată:** Nicio decizie: redarea rămâne prin canalul de date (CTR-12).
- **Ce se schimbă în §2 sau în cerințe:** Nimic în §2 sau în cerințe.

## Modelul Pi-ului și numerele recomandate (§4.8)

Pi-ul dvs.: Raspberry Pi 4 Model B Rev 1.5 (citit din cpuinfo). Nivelul H1b din §4.8, cu 4 camere.

| Nivel | Camere recomandate (§4.8) | Rezultat | Măsurat |
| --- | --- | --- | --- |
| H3: PC-ul dvs. | 8 (16 cu ≥ 6 nuclee) | 8 camere | măsurat pe PC-ul dvs. (G12, G13) |
| H1b: Pi-ul dvs. | 4 camere | 3 camere (G12) | măsurat pe Pi-ul dvs. (G12, G13) |
| H1 (Pi 5) | ≈ 8 | — | estimare, nemăsurată |
| H2 (N100) | 16 | — | estimare, nemăsurată |

Numerele rezultate: G12 — pică → pi: numărul recomandat devine 3; G13 — trece. Un plan B al lor coboară numărul recomandat al gazdei (minimum 1, O16), iar PR-ul raportului scrie numerele măsurate în §4.8. H1 și H2 se măsoară doar în Faza 0-bis, dacă doriți (G31).

## Rezultate pe gazdă

Fiecare rând vine din judecata porții lui; „Sursa” spune unde s-a măsurat valoarea: `pc`, `pi`, `lab` (runner-ele CI), `lan` (proba MI9 între dispozitivele dvs.), `ci-relay`, `ci-chrome`, `ci-loopback`, `ci` sau `telefon`. „✓” trece, „✗” pică, „–” e informativ sau fără verdict încă.

### PC (`pc`, H3)

| Cod | Valoare | Prag | Trece | Sursa |
| --- | --- | --- | --- | --- |
| MI1 pc-kill9: mediana | 0,60 s | ≤ 1 s | ✓ | pc |
| MI1 pc-kill9: maximul | 1,50 s | ≤ 2 s | ✓ | pc |
| MI1 pc-kill9: rulări eșuate | 0 din 20 | 0 | ✓ | pc |
| MI1 pc-kill9: Pierdere minimă la pană de curent: oprită (256 kbps, criptat) | 1,80 s | ≤ 4 s | ✓ | pc |
| MI1 pc-bugcheck: mediana | 0,60 s | ≤ 1 s | ✓ | pc |
| MI1 pc-bugcheck: maximul | 1,50 s | ≤ 2 s | ✓ | pc |
| MI1 pc-bugcheck: rulări eșuate | 0 din 20 | 0 | ✓ | pc |
| MI1 pc-bugcheck: Pierdere minimă la pană de curent: oprită (256 kbps, criptat) | 1,80 s | ≤ 4 s | ✓ | pc |
| MI1 real-pc: mediana | 0,60 s | ≤ 1 s | ✓ | pc |
| MI1 real-pc: maximul | 1,50 s | ≤ 2 s | ✓ | pc |
| MI1 real-pc: rulări eșuate | 0 din 5 | 0 | ✓ | pc |
| MI2 pc: reluarea după kill -9 | 3,00 s (20 din 20) | ≤ 10 s | ✓ | pc |
| MI2 pc: crash-uri ipcam-hub în 72 h | 0 (în 72,3 h) | 0 | ✓ | pc |
| MI2 pc: reporniri MediaMTX (HUB-13) | 0 |  | – | pc |
| MI2 pc: fereastra păstrată | 6,00 h |  | – | pc |
| MI3 pc: nivel și camere | H3, 8 camere, 3 sesiuni live |  | – | pc |
| MI3 pc: CPU total | 41,5 % | ≤ 60 % | ✓ | pc |
| MI3 pc: CPU per placă trimisă (sub) | 0,40 % | ≤ 1 % | ✓ | pc |
| MI3 pc: CPU per placă trimisă (main) | 1,60 % | ≤ 3 % | ✓ | pc |
| MI3 pc: CPU criptare + upload, per 10 Mbps | 2,10 % | ≤ 5 % | ✓ | pc |
| MI3 pc: RAM | 380 MB | ≤ 455 MB | ✓ | pc |
| MI3 pc: coada de scriere pe disc a MediaMTX, vârf (C1-R17, informativ) | 3,4 MB, suma vârfurilor fiecărei înregistrări (o limită superioară) |  | – | pc |
| MI3 pc: disc ÷ bitrate, camere în clar | 1,008 | ≤ 1,03 | ✓ | pc |
| MI3 pc: părți închise pe secundă per fișier deschis (MTX-1: un fsync la fiecare parte) | 1,00 | ≤ 1 | ✓ | pc |
| MI3 pc: latența adăugată, p95 | 72 ms | ≤ 150 ms | ✓ | pc |
| MI3 pc: pașii | 8 camere 1 h: trece |  | – | pc |
| MI3 pc: CPU-ul încărcării live (proxy, informativ, C1-R19) | 9,5 % |  | – | pc |
| MI3 pc: CPU-ul încărcării de redare (proxy, informativ, C1-R19) | 1,2 % |  | – | pc |
| MI3 pc: CPU-ul încărcării de export (proxy, informativ, C1-R19) | 12,4 % |  | – | pc |
| MI3 pc: CPU-ul încărcării de upload (proxy, informativ, C1-R19) | 2,3 % |  | – | pc |
| MI3 pc: disc ÷ bitrate, camere criptate (R02, informativ) | 1,041 |  | – | pc |
| MI3 pc: ritmul controlorului (R07) | ritmul controlorului p99 6 ms, cadre sărite 0 | ≤ 100 ms, cadre sărite 0 | – | pc |
| MI3 pc: stocare | SSD |  | – | pc |
| MI3 pc: 16 camere (≥ 6 nuclee) | măsurat: 16 camere 1 h: trece |  | – | pc |
| MI3 pc: pornire la rece până la înregistrare (1 cameră, cam01_main; uptime la evenimentul recording, deci include pornirea Windows) | 15,00 s (20 din 20) | ≤ 20 s | ✓ | pc |
| MI4 pc: citiri aleatoare de lungimea unei părți (1000, la cald, ~512 KiB) | max 31,5 ms (p50 12,5, p99 22,0) |  | – | pc |
| MI4 pc: primul cadru după salt (informativ) | max 410,0 ms |  | – | pc |
| MI4 pc: criptare pe un nucleu (informativ, H3) | 46,5 MB/s |  | – | pc |
| MI9 LAN (pc): debit canal bulk | 92,4 Mbps (RTT 1,2 ms) | ≥ 8 Mbps | ✓ | lan |
| MI9 salt: primul cadru întreg (IDR) primit (lan, pc) | max 180 ms (20 salturi) | fiecare ≤ 1500 ms | ✓ | lan |

### Raspberry Pi (`pi`) pe card SD (G16)

Rezultatele Pi-ului de pe card SD, separat de celelalte (G16): avertismentul cardului SD confirmat de proprietar: «DA», la 2026-10-27 18:04 UTC. Seturile de pe card SD: `MI1 pi-kill9`, `MI1 pi-sysrq`, `MI1 real-pi`, `MI2 pi-72h`, `MI3 pi-mi3`, `MI4 pi-mi4`, `MI9 lan`.

| Cod | Valoare | Prag | Trece | Sursa |
| --- | --- | --- | --- | --- |
| MI1 pi-kill9: mediana | 0,60 s | ≤ 1 s | ✓ | pi |
| MI1 pi-kill9: maximul | 1,50 s | ≤ 2 s | ✓ | pi |
| MI1 pi-kill9: rulări eșuate | 0 din 20 | 0 | ✓ | pi |
| MI1 pi-kill9: Pierdere minimă la pană de curent: oprită (256 kbps, criptat) | 1,80 s | ≤ 4 s | ✓ | pi |
| MI1 pi-sysrq: mediana | 0,60 s | ≤ 1 s | ✓ | pi |
| MI1 pi-sysrq: maximul | 1,50 s | ≤ 2 s | ✓ | pi |
| MI1 pi-sysrq: rulări eșuate | 0 din 20 | 0 | ✓ | pi |
| MI1 pi-sysrq: Pierdere minimă la pană de curent: oprită (256 kbps, criptat) | 1,80 s | ≤ 4 s | ✓ | pi |
| MI1 real-pi: mediana | 0,60 s | ≤ 1 s | ✓ | pi |
| MI1 real-pi: maximul | 1,50 s | ≤ 2 s | ✓ | pi |
| MI1 real-pi: rulări eșuate | 0 din 5 | 0 | ✓ | pi |
| MI2 pi: reluarea după kill -9 | 3,00 s (20 din 20) | ≤ 10 s | ✓ | pi |
| MI2 pi: starea cardului SD | OK → DEGRADAT |  | – | pi |
| MI2 pi: crash-uri ipcam-hub în 72 h | 0 (în 72,3 h) | 0 | ✓ | pi |
| MI2 pi: reporniri MediaMTX (HUB-13) | 0 |  | – | pi |
| MI2 pi: fereastra păstrată | 6,00 h |  | – | pi |
| MI3 pi: nivel și camere | H1b, 4 camere, 1 sesiune live |  | – | pi |
| MI3 pi: CPU total | 75,2 % | ≤ 60 % | ✗ | pi |
| MI3 pi: RAM | 340 MB | ≤ 345 MB | ✓ | pi |
| MI3 pi: coada de scriere pe disc a MediaMTX, vârf (C1-R17, informativ) | 3,4 MB, suma vârfurilor fiecărei înregistrări (o limită superioară) |  | – | pi |
| MI3 pi: pașii | 4 camere 1 h: pică; 2 camere 15 min: trece; 3 camere 15 min: trece; 3 camere 1 h: trece |  | – | pi |
| MI3 pi: CPU-ul încărcării live (proxy, informativ, C1-R19) | 9,5 % |  | – | pi |
| MI3 pi: CPU-ul încărcării de redare (proxy, informativ, C1-R19) | 1,2 % |  | – | pi |
| MI3 pi: CPU-ul încărcării de export (proxy, informativ, C1-R19) | 12,4 % |  | – | pi |
| MI3 pi: CPU-ul încărcării de upload (proxy, informativ, C1-R19) | 2,3 % |  | – | pi |
| MI3 pi: disc ÷ bitrate, camere în clar (informativ) | 1,008 |  | – | pi |
| MI3 pi: disc ÷ bitrate, camere criptate (R02, informativ) | 1,041 |  | – | pi |
| MI3 pi: ritmul controlorului (R07) | ritmul controlorului p99 6 ms, cadre sărite 0 | ≤ 100 ms, cadre sărite 0 | – | pi |
| MI3 pi: stocare | card SD, cu avertismentul confirmat de proprietar (G16) |  | – | pi |
| MI3 pi: pornire la rece până la înregistrare (1 cameră, cam01_main; uptime la evenimentul recording, deci include pornirea Linux; informativ) | 15,00 s (20 din 20) |  | – | pi |
| MI4 pi: citiri aleatoare de lungimea unei părți (1000, la rece, ~512 KiB) | max 31,5 ms (p50 12,5, p99 22,0) | fiecare ≤ 50 ms | ✓ | pi |
| MI4 pi: citiri de 64 KiB (informativ) | max 9,5 ms (p50 3,5, p99 7,0) |  | – | pi |
| MI4 pi: citiri de 4 MiB (informativ) | max 95,0 ms (p50 70,0, p99 90,0) |  | – | pi |
| MI4 pi: primul cadru după salt (segment de 5 min) | max 410,0 ms (20 salturi) | ≤ 1500 ms | ✓ | pi |
| MI4 pi: criptare pe un nucleu | 46,5 MB/s | ≥ 20,0 MB/s | ✓ | pi |
| MI4 pi: criptare pe toate nucleele (informativ) | 130,0 MB/s |  | – | pi |
| MI9 LAN (pi): debit canal bulk | 41,7 Mbps (RTT 2,4 ms) | ≥ 8 Mbps | ✓ | lan |
| MI9 salt: primul cadru întreg (IDR) primit (lan, pi) | max 520 ms (20 salturi) | fiecare ≤ 1500 ms | ✓ | lan |

Reluările de după kill -9 (MI2) stau lângă stările cardului: pe un disc încetinit, care ține locul unui card SD sub încărcare, laboratorul spike-ului a măsurat reluări de 13–37 s, din fsync-urile reconcilierii și ale stării rulării. Limita de 10 s a lui G11 poate pica din latența fsync a cardului, pe care planul B al lui G11 (ingestul inversat) nu o schimbă.

### Laborator și CI

Rulările din laborator (`lab-linux`, `lab-windows`, `lab-qemu`) și din CI, citite din adnotările rulărilor (C32).

| Cod | Valoare | Prag | Trece | Sursa |
| --- | --- | --- | --- | --- |
| MI1 lab-linux-kill9: mediana | 0,50 s | ≤ 1 s | ✓ | lab |
| MI1 lab-linux-kill9: maximul | 1,50 s | ≤ 2 s | ✓ | lab |
| MI1 lab-linux-kill9: rulări eșuate | 0 din 20 | 0 | ✓ | lab |
| MI1 lab-linux-kill9: Pierdere minimă la pană de curent: oprită (256 kbps, criptat) | 1,80 s | ≤ 4 s | ✓ | lab |
| MI1 lab-linux-qemu-reset: mediana | lipsește |  | – | lab |
| MI1 lab-linux-qemu-reset: maximul | lipsește |  | – | lab |
| MI1 lab-linux-qemu-reset: rulări eșuate | lipsește |  | – | lab |
| MI1 lab-linux-qemu-reset: Pierdere minimă la pană de curent: oprită (256 kbps, criptat) | lipsește | ≤ 4 s | – | lab |
| MI1 lab-windows-kill9: mediana | 0,50 s | ≤ 1 s | – | lab |
| MI1 lab-windows-kill9: maximul | 1,50 s | ≤ 2 s | – | lab |
| MI1 lab-windows-kill9: rulări eșuate | 0 din 19 | 0 | – | lab |
| MI1 lab-windows-kill9: Pierdere minimă la pană de curent: oprită (256 kbps, criptat) | 1,80 s | ≤ 4 s | ✓ | lab |
| ENC-06: aceiași octeți ca age și rage | da | identici | ✓ | ci |
| MI9 TURN-TLS/443 la 50 ms RTT (lab-linux): debit canal bulk | 15,8 Mbps (RTT 50,5 ms) | ≥ 5 Mbps | ✓ | ci-relay |
| MI9 salt: primul cadru întreg (IDR) primit (ci-relay, lab-linux) | max 318 ms (20 salturi) | fiecare ≤ 1500 ms | ✓ | ci-relay |
| MI9 Chrome: primul cadru afișat după salt | max 115,6 ms (20 salturi) | fiecare ≤ 1500 ms | ✓ | ci-chrome |
| MI9 loopback CI (informativ) | 460,2 Mbps; salt max 16,5 ms |  | – | ci-loopback |

### Telefonul (Galaxy A24, AND-1)

| Cod | Valoare | Prag | Trece | Sursa |
| --- | --- | --- | --- | --- |
| MI4: agerange în WebView-ul telefonului | 31,5 Mbps | ≥ 12 Mbps | ✓ | telefon |

### Pe ansamblu

Rânduri care adună mai multe gazde, sau părți fără nicio măsurătoare.

| Cod | Valoare | Prag | Trece | Sursa |
| --- | --- | --- | --- | --- |
| MI1 Linux: rata de eșec (95 %) | < ≈ 5 % (0 eșecuri în 60 de rulări automate) | < ≈ 7 % | – | lab, pi |
| MI1 Windows: rata de eșec (95 %) | < ≈ 5 % (0 eșecuri în 59 de rulări automate) | < ≈ 7 % | – | lab, pc |
| MI1 tăieri reale: rata de eșec (95 %) | < ≈ 26 % (0 eșecuri în 10 tăieri reale) | < ≈ 26 % | – | pc, pi |
| MI3: timeline 24 h (catalog de 1 an × 16 camere) | amânat: HUB-20, Faza 1 | ≤ 50 ms | – |  |

**Note la tabele:**

- Rândurile „Pierdere minimă la pană de curent: oprită (256 kbps, criptat)” sunt rularea cu setarea oprită din fiecare set automat. Limita lor e cea din §4.5: o parte plus 64 KiB ÷ bitrate, rotunjit în sus la părți întregi, adică 4 s (4.000 ms la 256 kbps). Pierderea fluxului de 256 kbps din această rulare nu contează în G10 (O17, C1-R13): peste limită, apare doar aici și în notele lui G10, fără planul B; orice altă eroare a rulării contează ca la o rulare obișnuită.

## Intrarea raportului

- Plicuri de rezultate judecate: 148, din 1 dosar de rezultate și 2 rulări CI; dintre ele, rezultate AGE ale aplicației: 1.
- Set de laborator incomplet (C32): `lab-windows-kill9`: lipsește partea 7 din 21; partea lui din G10 rămâne fără verdict.
- Alte notificări din CI, informative: `coturn` (1), `mi9 relay RTT` (1), `netem` (1).

## Pentru decizia R01: pierderea MTX-2 (mtx2-loss)

Testul TestMTX2_WorstCaseLoss din CI taie fluxuri criptate la momente aleatoare și măsoară cât se pierde și cât disc în plus ocupă criptarea, cu umplutura de la bitrate mic pornită și oprită (O17). Cifrele sprijină Decizia tehnică R01 din PR-ul raportului: reconcilierea păstrează eșantioanele întregi ale ultimei părți parțial scrise.

10000 de tăieturi; testul trece: da.

| kbps | Umplutura (O17) | Mediana | Maximul | Disc în plus |
| --- | --- | --- | --- | --- |
| 4000 | pornită | 0,61 s | 1,20 s | 4,0 % |
| 4000 | oprită | 0,61 s | 1,20 s | 4,0 % |
| 512 | pornită | 0,69 s | 1,75 s | 18,7 % |
| 512 | oprită | 1,05 s | 2,38 s | 0,5 % |
| 450 | pornită | 0,62 s | 1,41 s | 18,6 % |
| 450 | oprită | 1,12 s | 2,49 s | 0,4 % |
| 384 | pornită | 0,60 s | 1,10 s | 35,1 % |
| 384 | oprită | 1,20 s | 2,65 s | 0,4 % |
| 256 | pornită | 0,60 s | 1,10 s | 103,3 % |
| 256 | oprită | 1,57 s | 3,55 s | 0,3 % |

## Constatare pentru Faza 1: cine închide canalul bulk (§7.6)

În proba MI9 din Chrome, când hub-ul închidea canalul `bulk/<id>` imediat după ultimul lui octet, Chrome pierdea uneori octeții canalului următor, deschis cât închiderea era în curs (1 rulare din 7, local, cu Chrome 155). De aceea, în spike, clientul închide canalul după ce a primit tot, iar hub-ul îl închide singur doar după 10 s. E o informație pentru proiectarea redării din Faza 1 (§7.6), nu un verdict.

## Rămas pentru Faza 0-bis

Fiecare element pornește când aprobați bugetul pentru el (OPN-09, OPN-16; până atunci, 0 €) și are verdict înainte de faza care îl cere (§16.9). Tabelul e cel din §16.9, pentru ce lasă acest spike (G33).

| Element | Poartă | Cost estimat | Termen |
| --- | --- | --- | --- |
| H1 (Pi 5) și H2 (N100): G10, G12 și G13 pe aceste gazde; până atunci, ≈ 8 și 16 camere sunt „estimare, nemăsurată” | G10, G12, G13 | OWN-15 (opțional) | fără termen |
| `agerange` pe iOS-1 (MI4, punctul 3 din ENC-08) | G13 (partea 3) | un iPhone împrumutat; Apple Developer, 99 USD/an (OWN-05) | înainte de Faza 4 |

## Limitări ale măsurătorilor

- Pasul de restream al hubd (`live`, go2rtc) nu e măsurat: controlorul servește camerele simulate direct prin RTSP, iar MediaMTX le citește cu serverele lui oprite; MI1 nu include bufferul din memorie al restream-ului (milisecunde), iar MI3 nici CPU-ul lui (C06).
- Nu s-a înregistrat nicio cameră reală: c1 înregistrează doar camere simulate, deci comportamentul la înregistrarea unei camere reale (VBR, GOP neregulat) nu e acoperit (C28).
- SCTP-ul din Android WebView nu e măsurat: canalul de date se măsoară în Go, pe LAN și prin TURN-TLS/443, și în Chrome, în CI (G26).
- Încărcările MI3 sunt proxy-uri ale funcțiilor hub-ului (live, redare, export, upload); notele lui G12 spun ce ține locul fiecăreia (G20).
- Repornirea ipcam-hub făcută de recspike e o limită inferioară pentru recuperarea prin SCM (Windows) și systemd (Linux) din Faza 1 (C07).
- „Primul cadru” (MI4 și MI9) e timpul de la cererea de salt până la sosirea octeților primului eșantion IDR de la țintă sau de după ea, pe calea de citire a hub-ului; în Chrome se adaugă primul cadru afișat (requestVideoFrameCallback) (G21, G26).
- Rândul de pornire la rece e măsurat pe rulările MI1 cu repornire (pc-bugcheck pe PC, pi-sysrq pe Pi), cu camera lor, una singură (cam01_main), și include pornirea sistemului de operare.
- Legătura dintre recspike și reccontroller nu are cod de asociere: se bazează pe rețeaua de acasă. Un alt dispozitiv din aceeași rețea s-ar putea da drept una dintre gazde; controlorul primește de la o gazdă doar rezultatul MI9 pe LAN și nu înlocuiește niciun rezultat pe care îl are (C1-R25).

## Date brute

Câte un fișier CSV pe măsurătoare, lângă acest raport, cu o linie pe mostră (PH0-30):

- `phase0-data/phase0c-recorder-mi1.csv`: o linie pe fluxul fiecărei rulări, și pentru rulările care nu contează, cu motivul lor (`invalidReason`); coloanele: `host,set,runId,part,attempt,slot,valid,invalidReason,shutdownKind,encrypted,padLowBitrate,lossMs,stream,bitrateKbps,streamEncrypted,streamPadLowBitrate,streamLossMs,streamLimitMs,errBoundMs,reconciled,unreadable,tornParts,hub04Clear,pacingP99Ms,skippedAUs,resumeMs,coldBootMs,cameras,ntpWaitMs,ntpSynced,writeCache,storage`.
- `phase0-data/phase0c-recorder-mi2.csv`: proba de 72 h a fiecărei gazde, cu stările cardului SD; coloanele: `host,os,set,runId,storage,key,index,value`.
- `phase0-data/phase0c-recorder-mi3.csv`: prima oră, bugetele, încărcările, pașii și ora la 16 camere; coloanele: `host,os,set,runId,storage,key,index,value`.
- `phase0-data/phase0c-recorder-mi4.csv`: fiecare citire și salt, criptarea, rulările AGE ale aplicației și verificarea ENC-06; coloanele: `host,os,set,runId,storage,key,index,value`.
- `phase0-data/phase0c-recorder-mi9.csv`: debitul, fiecare salt și fiecare cadru afișat al fiecărui segment al probei; `bound` e „≥” la saltul întrerupt de sfârșitul probei, o limită inferioară; coloanele: `host,os,set,runId,storage,source,transport,key,index,value,bound`.
