# Rezultatele Fazei 0 (b): spike-ul video

Raport din 2026-10-19, generat de `spikereport` din fișierele de rezultate ale spike-ului. Rulări citite: 34; folosite la praguri: 28; invalide, deci excluse (întrerupte, eșuate sau la încărcător): 4; înlocuite de o rulare mai nouă a aceluiași scenariu: 1; doar informative: 1. Cifrele brute sunt în fișierele CSV de la sfârșit (PH0-30).
Textele din rezultate trec printr-un filtru care scoate adrese, URL-uri, adrese MAC, e-mailuri și valorile etichetate `serial` și `ssid`, iar raportul se scrie doar dacă scanerul de redactare nu mai găsește nimic în el (PH0-22, R51); proprietarul îl citește înainte de commit.

## Rezumat pentru proprietar

- **Android (AND-1, samsung SM-A245F):** **GO condiționat**. Cu 4 plăci trece tot ce e critic, dar cu 9 plăci pică MV3/9, MV4/9. Plafonul de 9 plăci pe telefon (A6) se schimbă doar cu aprobarea dvs.
- **Windows (WIN-1):** **plan B: grilă desktop 9**. A picat MV6: grila desktop coboară la 9 plăci (APP-30). Windows nu poate da NO-GO.
- **Web (Chrome, Edge, Firefox):** **fără verdict: lipsesc măsurători**. Lipsește o condiție a lui G7: hub-ul nu a putut asculta pe 5353/udp (mDNS) alături de clientul DNS din Windows în rulările S10 `firefox-20261018-154500-s10.json`.
- **Argon2id (G19, MV15):** trece: rulări max 2566 ms (mediana 2336 ms; 10 din 10).
- **Codecuri (G9):** fără verdict: lipsesc măsurători (S9 pe Chrome și Edge); măsurat până acum: AND-1: mx-h265: fallback; mx-high51: ok · WIN-1: mx-h265: fallback; mx-high51: ok · Firefox: mx-h265: fallback; mx-high51: unsupported-profile.
- **Nemăsurat încă:** Chrome: MV3/16, MV7, MV10, G9; Edge: MV2, MV3/9, G9; Firefox: MV1, MV3/9 (motivele sunt la dispozitive).
- **De decis de dvs.:** plafonul de plăci pe telefon (A6, acum 9), vezi G5 mai jos.
- **Atenție:** pentru MV5/9 (AND-1), o rulare anterioară a aceluiași scenariu a picat pragul; valoarea o dă cea mai nouă rulare, dar eșecul rămâne în tabele, lângă valoare (R40).
- **Atenție:** rulări cu jitter al hub-ului peste 20 ms: 1 (vezi Avertismente); cifrele lor pot fi mai proaste decât în realitate.

## Rezultate pe dispozitiv

### AND-1: Android (Capacitor)

**Dispozitiv:** samsung SM-A245F · Android 14 (API 34) · com.google.android.webview 120.0.6099 · memorie RAM aprox. 6 GB (5482 MiB raportați de sistem)

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV1 | p90 666 ms (mediana 536 ms, max 693 ms; 20 din 20 porniri) | p90 ≤ 1,5 s (20 porniri) | Da |
| MV2 | p95 sub 306 ms (100 mostre); p95 principal 401 ms (100 mostre) | p95 < 1 s (100 mostre) | Da |
| MV3/4 | fps minim 11,86; pierdute max 0,5 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Da |
| MV3/9 | fps minim 9,6; pierdute max 0,5 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Nu |
| MV4/4 | plăci hardware: 4 din 4 | toate plăcile grilei hardware | Da |
| MV4/9 | plăci hardware: 8 din 9 | toate plăcile grilei hardware | Nu |
| MV4/main | ecran complet: hardware (S3-4, S3-9) | ecranul complet (flux principal) mereu hardware | Da |
| MV5/4 | termic max LIGHT; fps în ultimele 2 min 11,95; baterie 0 %/h | termic ≤ MODERATE; fps final ≥ 10,8; baterie ≤ 15 %/h | Da |
| MV5/9 | termic max MODERATE; fps în ultimele 2 min 11,95; baterie 12 %/h; **atenție:** o rulare anterioară a picat pragul (`android-samsung-sm-a245f-20261018-102500-s3-9.json`): termic max SEVERE; fps în ultimele 2 min 11,95; baterie 12 %/h | termic ≤ MODERATE; fps final ≥ 10,8; baterie ≤ 15 %/h | Da |
| MV7 | memorie +2,1 %; heap JS rotunjit de Chromium (jsHeap: bucketed), nu se judecă (R46); reporniri neprovocate: 0 | creștere memorie ≤ 10 %; 0 reporniri neprovocate | Da |
| MV8 | 300 s în PiP; pauză maximă între cadre 210 ms; 2995 cadre | 5 min în PiP fără pauze de peste 2 s | Da |
| MV9 | reluare în 1480 ms după 123 s în fundal | live reluat în ≤ 3 s după 2 min în fundal | Da |
| MV10 | pornire max 1080 ms (mediana 803 ms; 20 din 20); salt max 846 ms (mediana 623 ms; 20 din 20) | pornire ≤ 2 s; salt ≤ 1,5 s (toate cele 20) | Da |
| MV12 | pereche host/host; cerere de permisiune: nu | perechea ICE aleasă nu e relay | Da |
| MV15 | rulări max 2566 ms (mediana 2336 ms; 10 din 10) | ≤ 3 s pe AND-1 (10 rulări) | Da |
| G9 | mx-h265: fallback; mx-high51: ok | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | Da |

**Note la praguri:**

- **MV1:** substream: H264
- **MV1:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 3, NACK 3, PLI 21, cadre aruncate 0
- **MV2:** jitter hub: 24,5 ms în rularea S2 (peste 20 ms): PC-ul hub-ului poate fi ocupat, deci cifrele clientului pot fi mai proaste decât în realitate
- **MV2:** sub: 3 din 100 mostre cu ceas nesigur (RTT > 50 ms sau offset mutat cu > 20 ms); intră în calcul
- **MV2:** RTT al ceasului: max 7 ms (eroarea offsetului e cel mult jumătate din el)
- **MV3/4:** ecran complet (flux principal, informativ): 19,83 fps, pierdute 0,3 %
- **MV3/4:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 1, PLI 4, cadre aruncate 2
- **MV3/9:** ecran complet (flux principal, informativ): 19,83 fps, pierdute 0,3 %
- **MV3/9:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 1, NACK 1, PLI 9, cadre aruncate 3
- **MV7:** memMb = PSS al procesului aplicației, fără randerul WebView, care e alt proces; heap-ul JS e rotunjit de Chromium în Android WebView, deci MV7 se judecă doar pe memMb (R46)
- **MV7:** grila a paginat între 4 și 9 plăci (plafonul telefonului, A6)
- **MV7:** creștere = media ultimelor 6 min față de media a 6 min de după primul minut, ferestre care acoperă întregi ciclurile de paginare (2 min) și de ecran complet (3 min)
- **MV12:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară
- **G9:** primul cadru: mx-h265 pe substream 310 ms, mx-high51 pe fluxul principal 641 ms

**Rulări invalide** (nu intră în praguri; repetați scenariul cu «Repetă»):

- `android-samsung-sm-a245f-20261018-094000-s3-9.json` (S3-9): pagina a fost ascunsă la 142 s de la început: ecranul s-a stins sau aplicația a trecut în fundal.
- `android-samsung-sm-a245f-20261018-103500-s3-9.json` (S3-9): telefonul era la încărcător la 40 s de la început, deci bateria și temperatura nu se pot judeca (R48).
- `android-samsung-sm-a245f-20261018-115000-s5.json` (S5): aplicația a revenit din fundal după 95 s; MV9 cere cel puțin 2 minute în fundal.

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| android-samsung-sm-a245f-20261018-095500-s1.json | S1 | 61 s | folosită | 1,2 ms |
| android-samsung-sm-a245f-20261018-100200-s2.json | S2 | 52 s | folosită | 24,5 ms, peste 20 ms |
| android-samsung-sm-a245f-20261018-101000-s3-4.json | S3-4 | 724 s | folosită | 1,1 ms |
| android-samsung-sm-a245f-20261018-094000-s3-9.json | S3-9 | 142 s | întreruptă, exclusă | 3,1 ms |
| android-samsung-sm-a245f-20261018-102500-s3-9.json | S3-9 | 724 s | înlocuită de una mai nouă | 1,6 ms |
| android-samsung-sm-a245f-20261018-103500-s3-9.json | S3-9 | 48 s | întreruptă, exclusă | 1,4 ms |
| android-samsung-sm-a245f-20261018-104500-s3-9.json | S3-9 | 725 s | folosită | 1,8 ms |
| android-samsung-sm-a245f-20261018-112000-s4.json | S4 | 1803 s | folosită | 1,4 ms |
| android-samsung-sm-a245f-20261018-115000-s5.json | S5 | 101 s | întreruptă, exclusă | 1 ms |
| android-samsung-sm-a245f-20261018-115500-s5.json | S5 | 152 s | folosită | 0,9 ms |
| android-samsung-sm-a245f-20261018-120500-s6.json | S6 | 312 s | folosită | 1 ms |
| android-samsung-sm-a245f-20261018-121500-s7.json | S7 | 48 s | folosită | — |
| android-samsung-sm-a245f-20261018-122000-s8.json | S8 | 24 s | folosită | — |
| android-samsung-sm-a245f-20261018-122500-s9.json | S9 | 11 s | folosită | 1 ms |
| android-samsung-sm-a245f-20261018-123000-s10.json | S10 | 3 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S1 (android-samsung-sm-a245f-20261018-095500-s1.json): sub: H264
- S3-4 (android-samsung-sm-a245f-20261018-101000-s3-4.json): decoders: c2.mtk.avc.decoder ×4
- S3-4 (android-samsung-sm-a245f-20261018-101000-s3-4.json): main decoder: c2.mtk.avc.decoder
- S3-9 (android-samsung-sm-a245f-20261018-094000-s3-9.json): interrupted: the page was hidden 142 s into the run
- S3-9 (android-samsung-sm-a245f-20261018-094000-s3-9.json): decoder info: n/a
- S3-9 (android-samsung-sm-a245f-20261018-103500-s3-9.json): decoders: c2.mtk.avc.decoder ×9
- S3-9 (android-samsung-sm-a245f-20261018-103500-s3-9.json): interrupted: the phone was on the charger 40 s into the run
- S3-9 (android-samsung-sm-a245f-20261018-104500-s3-9.json): decoders: c2.mtk.avc.decoder ×8, OMX.google.h264.decoder ×1
- S3-9 (android-samsung-sm-a245f-20261018-104500-s3-9.json): main decoder: c2.mtk.avc.decoder
- S4 (android-samsung-sm-a245f-20261018-112000-s4.json): jsHeap: bucketed
- S5 (android-samsung-sm-a245f-20261018-115000-s5.json): connection at return: connected
- S5 (android-samsung-sm-a245f-20261018-115000-s5.json): session: kept
- S5 (android-samsung-sm-a245f-20261018-115000-s5.json): interrupted: back from the background after 95 s, at least 120 s are needed
- S5 (android-samsung-sm-a245f-20261018-115000-s5.json): background shorter than 2 min
- S5 (android-samsung-sm-a245f-20261018-115500-s5.json): connection at return: closed
- S5 (android-samsung-sm-a245f-20261018-115500-s5.json): the app closed the session after 60 s in the background (connection connected then)
- S5 (android-samsung-sm-a245f-20261018-115500-s5.json): session: reopened
- S9 (android-samsung-sm-a245f-20261018-122500-s9.json): mx-h265/main: error CODEC_UNSUPPORTED
- S9 (android-samsung-sm-a245f-20261018-122500-s9.json): mx-h265/sub: bound H264
- S9 (android-samsung-sm-a245f-20261018-122500-s9.json): mx-h265: fallback
- S9 (android-samsung-sm-a245f-20261018-122500-s9.json): mx-high51/main: bound H264
- S9 (android-samsung-sm-a245f-20261018-122500-s9.json): mx-high51: ok
- S10 (android-samsung-sm-a245f-20261018-123000-s10.json): pair: host/host
- S10 (android-samsung-sm-a245f-20261018-123000-s10.json): prompt: no

### WIN-1: Windows (Tauri, WebView2)

**Dispozitiv:** procesor AMD Ryzen 5 5600G with Radeon Graphics · Windows 11 Pro · WebView2 154.0.4258 · GPU AMD Radeon(TM) Graphics · memorie RAM aprox. 16 GB (16069 MiB raportați de sistem)

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV1 | p90 212 ms (mediana 163 ms, max 225 ms; 20 din 20 porniri) | p90 ≤ 1,5 s (20 porniri) | Da |
| MV2 | p95 sub 82 ms (100 mostre); p95 principal 106 ms (100 mostre) | p95 < 1 s (100 mostre) | Da |
| MV3/16 | fps minim 11,86; pierdute max 0,5 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Da |
| MV6 | 16 plăci: 30,8 % (max 32,7 %); flux principal 1080p: 7,6 % | 16 plăci ≤ 25 % CPU; flux principal ≤ 10 % CPU | Nu |
| MV7 | memorie +1,6 %; heap JS +2,5 %; reporniri neprovocate: 0 | creștere memorie ≤ 10 %; 0 reporniri neprovocate | Da |
| MV10 | pornire max 649 ms (mediana 503 ms; 20 din 20); salt max 490 ms (mediana 366 ms; 20 din 20) | pornire ≤ 2 s; salt ≤ 1,5 s (toate cele 20) | Da |
| MV12 | pereche host/host; cerere de permisiune: nu | perechea ICE aleasă nu e relay | Da |
| G9 | mx-h265: fallback; mx-high51: ok | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | Da |

**Note la praguri:**

- **MV1:** substream: H264
- **MV1:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 20, cadre aruncate 0
- **MV2:** RTT al ceasului: max 1 ms (eroarea offsetului e cel mult jumătate din el)
- **MV3/16:** ecran complet (flux principal, informativ): 19,83 fps, pierdute 0,3 %
- **MV3/16:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 16, cadre aruncate 1
- **MV6:** CPU total al PC-ului, cu hub-ul pe același PC: 43,8 %
- **MV7:** memMb = memorie privată (private bytes, R37) a aplicației și a proceselor WebView2; nu include secțiunile partajate și memoria GPU, unde Chromium ține cadrele decodate, deci o scurgere acolo nu apare în MV7
- **MV7:** grila a paginat între 9 și 16 plăci: pe PC, MV7 se judecă doar cu 16 plăci (G6, R45)
- **MV7:** creștere = media ultimelor 6 min față de media a 6 min de după primul minut, ferestre care acoperă întregi ciclurile de paginare (2 min) și de ecran complet (3 min)
- **MV12:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară
- **G9:** primul cadru: mx-h265 pe substream 140 ms, mx-high51 pe fluxul principal 411 ms

**Rulări invalide** (nu intră în praguri; repetați scenariul cu «Repetă»):

- `windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-125300-s2.json` (S2): s-a pierdut sesiunea cu hub-ul la 21 s de la început (conexiunea s-a închis); verificați că PC-ul cu spikehub rămâne pornit și în aceeași rețea (R49).

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-125000-s1.json | S1 | 61 s | folosită | 1,1 ms |
| windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-125300-s2.json | S2 | 21 s | întreruptă, exclusă | — |
| windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-125600-s2.json | S2 | 50 s | folosită | 1,2 ms |
| windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-131000-s3-16.json | S3-16 | 725 s | folosită | 2,2 ms |
| windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-134000-s4.json | S4 | 1802 s | folosită | 2 ms |
| windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-130500-s7.json | S7 | 40 s | folosită | — |
| windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-140000-s9.json | S9 | 11 s | folosită | 1 ms |
| windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-140500-s10.json | S10 | 3 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S1 (windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-125000-s1.json): sub: H264
- S2 (windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-125300-s2.json): interrupted: the hub session was lost 21 s into the run (connection closed)
- S3-16 (windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-131000-s3-16.json): decoder info: n/a
- S9 (windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-140000-s9.json): mx-h265/main: error CODEC_UNSUPPORTED
- S9 (windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-140000-s9.json): mx-h265/sub: bound H264
- S9 (windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-140000-s9.json): mx-h265: fallback
- S9 (windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-140000-s9.json): mx-high51/main: bound H264
- S9 (windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-140000-s9.json): mx-high51: ok
- S10 (windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-140500-s10.json): pair: host/host
- S10 (windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-140500-s10.json): prompt: no

### Web: Chrome

**Dispozitiv:** Windows · Chrome/141.0.0

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV1 | p90 203 ms (mediana 177 ms, max 209 ms; 20 din 20 porniri) | p90 ≤ 1,5 s (20 porniri) | Da |
| MV2 | p95 sub 96 ms (100 mostre); p95 principal 130 ms (100 mostre) | p95 < 1 s (100 mostre) | Da |
| MV3/9 | fps minim 11,86; pierdute max 0,5 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Da |
| MV3/16 | nemăsurat: S3-16 nu a rulat | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | — |
| MV7 | nemăsurat: S4 a rulat fără 16 plăci; pe PC, MV7 se judecă doar pe o rulare S4 cu 16 plăci (G6, R45); informativ, nu decide: S4 cu 9 plăci (`chrome-20261018-151000-s4.json`): heap JS +3 %; reporniri neprovocate: 0 | creștere memorie ≤ 10 %; 0 reporniri neprovocate | — |
| MV10 | nemăsurat: S7 nu a rulat | pornire ≤ 2 s; salt ≤ 1,5 s (toate cele 20) | — |
| MV13 | pereche host/host; cerere de permisiune: necunoscut (n/a) | ambele capete host sau prflx, fără relay | Da |
| G9 | nemăsurat: S9 nu a rulat | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | — |

**Note la praguri:**

- **MV1:** substream: H264
- **MV1:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 20, cadre aruncate 0
- **MV2:** RTT al ceasului: max 1 ms (eroarea offsetului e cel mult jumătate din el)
- **MV3/9:** ecran complet (flux principal, informativ): 19,83 fps, pierdute 0,3 %
- **MV3/9:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 9, cadre aruncate 0
- **MV13:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| chrome-20261018-150000-s1.json | S1 | 61 s | folosită | 1,1 ms |
| chrome-20261018-150200-s2.json | S2 | 51 s | folosită | 1,2 ms |
| chrome-20261018-150500-s3-9.json | S3-9 | 723 s | folosită | 0,9 ms |
| chrome-20261018-151000-s4.json | S4 | 1802 s | informativă: pe PC, MV7 cere S4 cu 16 plăci | 1,3 ms |
| chrome-20261018-152000-s10.json | S10 | 3 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S1 (chrome-20261018-150000-s1.json): sub: H264
- S3-9 (chrome-20261018-150500-s3-9.json): decoder info: n/a
- S3-9 (chrome-20261018-150500-s3-9.json): native metrics: n/a
- S4 (chrome-20261018-151000-s4.json): native metrics: n/a
- S10 (chrome-20261018-152000-s10.json): pair: host/host
- S10 (chrome-20261018-152000-s10.json): prompt: n/a

### Web: Edge

**Dispozitiv:** Windows · Edg/141.0.0

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV1 | p90 221 ms (mediana 196 ms, max 227 ms; 20 din 20 porniri) | p90 ≤ 1,5 s (20 porniri) | Da |
| MV2 | nemăsurat: S2 nu a rulat | p95 < 1 s (100 mostre) | — |
| MV3/9 | nemăsurat: S3-9 nu a rulat | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | — |
| MV13 | pereche host/prflx; cerere de permisiune: necunoscut (n/a) | ambele capete host sau prflx, fără relay | Da |
| G9 | nemăsurat: S9 nu a rulat | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | — |

**Note la praguri:**

- **MV1:** substream: H264
- **MV13:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| edge-20261018-153000-s1.json | S1 | 61 s | folosită | 0,9 ms |
| edge-20261018-153300-s10.json | S10 | 3 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S1 (edge-20261018-153000-s1.json): sub: H264
- S10 (edge-20261018-153300-s10.json): pair: host/prflx
- S10 (edge-20261018-153300-s10.json): prompt: n/a

### Web: Firefox

**Dispozitiv:** Windows · Firefox/132.0

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV1 | nemăsurat: S1 nu a rulat | p90 ≤ 1,5 s (20 porniri) | — |
| MV2 | p95 sub 73 ms (100 mostre); principal: n/a, browserul nu oferă H.264 Main (în produs, retragere pe substream, §6.3 pasul 7) | p95 < 1 s (100 mostre) | Da |
| MV3/9 | nemăsurat: S3-9 nu a rulat | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | — |
| MV13 | pereche host/host; cerere de permisiune: necunoscut (n/a) | ambele capete host sau prflx, fără relay | Da |
| G9 | mx-h265: fallback; mx-high51: unsupported-profile | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | Da |

**Note la praguri:**

- **MV2:** RTT al ceasului: max 1 ms (eroarea offsetului e cel mult jumătate din el)
- **MV13:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară
- **MV13:** mDNS indisponibil pe hub: n-a putut rezolva candidații .local ai browserelor; un filtru de multicast nu pică MV13 dacă perechea e prflx
- **G9:** primul cadru: mx-h265 pe substream 95 ms, mx-high51 pe substream 89 ms
- **G9:** mx-h265/sub: H264, în profilul de rezervă 4d001f (R23: browserul nu oferă profilul sursei)
- **G9:** mx-high51/sub: H264, în profilul de rezervă 4d001f (R23: browserul nu oferă profilul sursei)

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| firefox-20261018-153800-s2.json | S2 | 30 s | folosită | 1 ms |
| firefox-20261018-154000-s9.json | S9 | 11 s | folosită | 1,1 ms |
| firefox-20261018-154500-s10.json | S10 | 3 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S2 (firefox-20261018-153800-s2.json): main: bind error CODEC_UNSUPPORTED
- S9 (firefox-20261018-154000-s9.json): mx-h265/main: error CODEC_UNSUPPORTED
- S9 (firefox-20261018-154000-s9.json): mx-h265/sub: bound H264, fallback profile 4d001f
- S9 (firefox-20261018-154000-s9.json): mx-h265: fallback
- S9 (firefox-20261018-154000-s9.json): mx-high51/main: error CODEC_UNSUPPORTED
- S9 (firefox-20261018-154000-s9.json): mx-high51/sub: bound H264, fallback profile 4d001f
- S9 (firefox-20261018-154000-s9.json): mx-high51: unsupported-profile
- S10 (firefox-20261018-154500-s10.json): pair: host/host
- S10 (firefox-20261018-154500-s10.json): prompt: n/a

### Web: Chrome pe Android (sistem neașteptat: nu intră în verdicte)

**Dispozitiv:** Android · Chrome/141.0.0

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV1 | p90 504 ms (mediana 406 ms, max 530 ms; 20 din 20 porniri) | p90 ≤ 1,5 s (20 porniri) | Da |

**Note la praguri:**

- **MV1:** substream: H264
- **MV1:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 5, NACK 5, PLI 22, cadre aruncate 0

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| chrome-20261018-160000-s1.json | S1 | 63 s | folosită | 1,2 ms |

**Note din rulări**, așa cum le-a scris aplicația:

- S1 (chrome-20261018-160000-s1.json): sub: H264

## Avertismente

- Jitter al hub-ului: rulările de mai jos au avut o fereastră de 10 s cu p99 al abaterii de trimitere de peste 20 ms. PC-ul hub-ului poate să fi fost ocupat (lucru în paralel, antivirus), deci cifrele clientului din aceste rulări pot fi mai proaste decât în realitate; repetați-le cu PC-ul liber.
  - `android-samsung-sm-a245f-20261018-100200-s2.json` (S2): jitter maxim 24,5 ms, peste 20 ms.
- Rulări invalide: 4 (întrerupte, eșuate sau la încărcător). Nu intră în praguri; motivul fiecăreia e la dispozitivul ei, sub «Rulări invalide», iar aplicația oferă «Repetă»: `android-samsung-sm-a245f-20261018-094000-s3-9.json`, `android-samsung-sm-a245f-20261018-103500-s3-9.json`, `android-samsung-sm-a245f-20261018-115000-s5.json`, `windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-125300-s2.json`.
- Rulări Chrome pe Android: ghidul rulează browserele pe PC-ul cu Windows, deci aceste rulări sunt listate separat, în secțiunea lor de la «Rezultate pe dispozitiv», nu intră în verdicte și nu înlocuiesc rulările de pe PC.
- mDNS indisponibil pe hub în rulările S10 `firefox-20261018-154500-s10.json`: hub-ul nu a putut rezolva candidații .local ai browserelor.

## Verdicte și decizii declanșate

| Poartă | Verdict sau rezultat |
| --- | --- |
| G5: Grila Android (AND-1) | GO condiționat |
| G6: Windows WebView2 (WIN-1) | plan B: grilă desktop 9 |
| G7: Chrome LNA și mDNS (web) | fără verdict: lipsesc măsurători |
| G9: Matricea de codecuri | fără verdict: lipsesc măsurători (S9 pe Chrome și Edge); măsurat până acum: AND-1: mx-h265: fallback; mx-high51: ok · WIN-1: mx-h265: fallback; mx-high51: ok · Firefox: mx-h265: fallback; mx-high51: unsupported-profile |
| G19: Argon2id pe Android mediu (MV15) | trece: rulări max 2566 ms (mediana 2336 ms; 10 din 10) |

### G5: Grila Android (AND-1)

- **Verdict:** GO condiționat
- **Decizie declanșată:** Cu 4 plăci trece tot ce e critic, dar cu 9 plăci pică MV3/9, MV4/9. Plafonul pe telefon (A6: maximum 9 plăci) se schimbă doar cu aprobarea dvs. Până la decizia dvs. plafonul rămâne 9, iar plăcile care nu pot decoda trec pe snapshot (§6.5).
- **Ce se schimbă în §2 sau în cerințe:** O schimbare aprobată se scrie în §2.1, ca modificare a lui A6, nu în §2.2. Decizia proprietarului asupra plafonului pe telefon: de completat de proprietar.

### G6: Windows WebView2 (WIN-1)

- **Verdict:** plan B: grilă desktop 9
- **Decizie declanșată:** A picat MV6: grila desktop coboară la 9 plăci (APP-30, planul B din G6). Windows nu poate da NO-GO.
- **Ce se schimbă în §2 sau în cerințe:** §2.2: Decizie tehnică „grilă desktop 9”, cu trimitere la G6.

### G7: Chrome LNA și mDNS (web)

- **Verdict:** fără verdict: lipsesc măsurători
- **Decizie declanșată:** Fără decizie: hub-ul nu a putut asculta pe 5353/udp (mDNS) alături de clientul DNS din Windows în rulările S10 `firefox-20261018-154500-s10.json`, iar G7 cere și această condiție. Ce urmează (o corectură în hub sau planul B al lui G7) se decide în PR, cu aceste rulări.
- **Ce se schimbă în §2 sau în cerințe:** Nimic până atunci.

### G9: Matricea de codecuri

- **Rezultat:** fără verdict: lipsesc măsurători (S9 pe Chrome și Edge); măsurat până acum: AND-1: mx-h265: fallback; mx-high51: ok · WIN-1: mx-h265: fallback; mx-high51: ok · Firefox: mx-h265: fallback; mx-high51: unsupported-profile
- **Decizie declanșată:** Fără decizie: G9 cere matricea de codecuri pe toate căile client, iar S9 lipsește pe Chrome și Edge. Rulați S9 acolo, în ghid, și repetați raportul.
- **Ce se schimbă în §2 sau în cerințe:** Nimic până atunci.

### G19: Argon2id pe Android mediu (MV15)

- **Rezultat:** trece: rulări max 2566 ms (mediana 2336 ms; 10 din 10)
- **Decizie declanșată:** Nicio decizie: parametrii impliciți Argon2id rămân cei din §10.3 (m=64 MiB, t=3, p=4).
- **Ce se schimbă în §2 sau în cerințe:** Nimic în §2 sau în cerințe.

## Rămas pentru Faza 0-bis

Fiecare element pornește când aprobați bugetul pentru el (OPN-09, OPN-16; până atunci, 0 €) și are verdict înainte de faza care îl cere (spec §16.9).

| Element | Poartă | Cost estimat | Termen |
| --- | --- | --- | --- |
| Verdictul iOS: MV1–MV11 și MV14 pe iOS-1 (iPhone real, iOS ≥ 17.1), smoke automat pe iOS-2 în CI | G2, G3, G4 | un iPhone împrumutat (clasa iPhone 12 sau mai nouă); Apple Developer, 99 USD/an (OWN-05) | înainte de Faza 4; recomandat cât mai devreme |
| Părțile iOS din G7, G8, G9 și G13: MV12 și MV13 în Safari și WKWebView, MV11 pe iOS-1, matricea de codecuri pe căile iOS | G7, G8, G9, G13 | ca la rândul de mai sus | odată cu verdictul iOS, înainte de Faza 4 |
| MV11 (TURN-TLS/443 pe operator mobil), criticele remote (MV1 și MV2 la distanță) și partea din MV12 care cere Chrome cu origine publică | G8 complet, G7 (MV12 în Chrome) | VPS UE (OWN-11), Cloudflare TURN (OWN-12), domeniul pentru originea publică (OWN-04); pentru al doilea operator mobil, un SIM preplătit (≈ 5 € [estimare]) sau telefonul cuiva din familie | înainte de Faza 2 |
| AND-2: Android de intrare (Android 10, 3–4 GB RAM) pe Firebase Test Lab, MV3 și MV5 cu 4 plăci (informativ: fixează plafonul implicit) | informativ, legat de A6 | OWN-17 | înainte de Faza 4 |

## Limitări ale măsurătorilor

- Jitterul hub-ului (R28) e p99 al abaterii de trimitere pe ferestre de 10 s; o rulare e marcată când o fereastră depășește 20 ms.
- Dintre rulările valide ale aceluiași scenariu pe același dispozitiv, valoarea o dă cea mai nouă (după ceasul dispozitivului). O rulare anterioară care a picat pragul nu dispare: e arătată lângă valoare, cu avertisment (R40), ca repetarea până la o trecere să nu ascundă un eșec. Rulările invalide (întrerupte, eșuate sau la încărcător) sunt listate, dar nu intră în praguri.
- MV2 se calculează cu offsetul dintre ceasul dispozitivului și al hub-ului (ping pe canalul de date), nu cu ceasul dispozitivului; mostrele exclud cadrele din GOP-ul păstrat în cache la legarea unei camere, care au vârsta cache-ului în loc de întârzierea transportului (R26).
- MV2 păstrează mostrele cu ceas nesigur (RTT > 50 ms sau offset mutat cu > 20 ms) și le raportează în notele pragului, fără să le excludă: eroarea offsetului e cel mult jumătate din RTT-ul ceasului.
- Specificația nu dă un percentil pentru MV10 (este un prag critic al AND-1), așa că MV10 și MV15 judecă fiecare citire: toate cele 20 de porniri (≤ 2 s) și toate cele 20 de salturi (≤ 1,5 s) la MV10, toate cele 10 rulări (≤ 3 s) la MV15; nicio pornire, salt sau rulare nu poate expira. MV1 folosește p90 din 20, cum cere specificația. O pornire, un salt sau o rulare care nu s-a terminat (MV1, MV10, MV15) se numără ca infinit de lent.
- MV5 judecă fps-ul pe media ultimelor 2 minute din `gridFps` (60 de ferestre de 2 s), nu pe toată rularea: o scădere din cauza căldurii se vede la sfârșit.
- MV5 calculează ritmul bateriei din prima și ultima citire, raportat la durata măsurată a grilei (ferestrele de 2 s din `gridFps`), nu la numărul de citiri: o citire a shell-ului care a expirat nu îl umflă. Procentul bateriei e întreg, deci rezoluția ritmului e de circa 6 %/h (1 % în circa 10 min): două puncte dau circa 12 %/h (trece), trei circa 18 %/h (pică), iar pragul de 15 %/h cade între aceste trepte.
- MV6 judecă media lui `cpuPctApp` (procesul aplicației și procesele WebView2, ca pondere din tot PC-ul), nu maximul și nu `cpuPctTotal`, care include și hub-ul de pe același PC; fluxul principal se judecă pe media lui `mainCpuPctApp`, de la prima rulare a grilei care l-a măsurat.
- MV8: specificația cere 5 min în PiP cu video care avansează; raportul cere 300 s în PiP, cel puțin un cadru și nicio pauză între cadre de peste 2 s. Pragul de 2 s pentru pauză e alegerea raportului, nu a specificației.
- Pe WIN-1, `memMb` (MV7) e memorie privată (private bytes, R37) a aplicației și a proceselor WebView2: nu include secțiunile partajate și memoria GPU, unde Chromium ține cadrele decodate, deci o scurgere acolo nu apare în MV7.
- Pe WIN-1, CPU-ul și memoria aplicației (MV6, MV7) se folosesc doar dacă fiecare citire a găsit cel puțin un proces WebView2 (`webview2Procs` ≥ 1, R52); altfel pragul rămâne nemăsurat, iar G6 fără verdict.
- Pe AND-1, MV7 e memoria procesului aplicației (PSS), fără randerul WebView, care e alt proces, deci o scurgere în rander nu apare în MV7. Heap-ul JS e rotunjit de Chromium acolo (randerul Android WebView nu e legat de un site), așa că o rulare S4 cu nota `jsHeap: bucketed` se judecă doar pe `memMb` (R46). Pe PC (Chrome, Edge, WebView2) heap-ul JS e precis, iar MV7 judecă heap-ul JS și memoria procesului.
- Pe PC (WIN-1 și browsere), MV7 se judecă doar pe o rulare S4 cu 16 plăci (`tiles` = 16; G6 cere MV7 cu 16 plăci, R45); o rulare S4 cu mai puține plăci e listată ca informativă și nu decide. Pe AND-1, S4 paginează între 4 și 9 plăci (plafonul A6).
- MV4 are două părți: plăcile grilei (MV4/4, MV4/9) și ecranul complet (MV4/main), judecat pe toate rulările de grilă care au citit decodorul fluxului principal. Specificația trece MV4 doar printre pragurile critice cu 9 plăci, deci amândouă părțile se judecă alături de acestea (R54). Pentru ecranul complet motivul e separat: fluxul principal e același la orice grilă, deci plafonul de plăci (A6) nu rezolvă o decodare software, iar ce se face se decide cu proprietarul, la analiza rezultatelor.
- Pierderile (`rtpLost`, `nack`, `pli`, `framesDropped`: sumele pe rulare din statisticile `inbound-rtp`) apar lângă MV1 și MV3 doar ca informație, fără prag (R50). Hub-ul răspunde la NACK, dar nu la PLI: camerele simulate redau fișiere pre-codate cu un GOP de 1 s, deci o placă își revine la următorul cadru-cheie, în cel mult o secundă.
- În spike nu există STUN sau TURN (nici în aplicație, nici în hub), deci perechile srflx și relay nu pot apărea: la MV12 și MV13, „trece” înseamnă că s-a format o pereche directă în LAN. O pereche care nu se formează oprește orice scenariu pe acel dispozitiv, nu doar S10.
- O rulare S3 cu o citire la încărcător (`charging` sau `mainCharging` = 1) e invalidă (R48), iar o rulare întreruptă, din orice motiv (inclusiv pierderea sesiunii cu hub-ul, R49), nu e o măsurătoare; amândouă sunt listate la dispozitiv, sub «Rulări invalide».
- Rulările browserelor se grupează după browser și familia sistemului de operare: o rulare Chrome pe telefon nu înlocuiește și nu se amestecă cu cea de pe PC; o rulare pe alt sistem decât Windows e listată separat și nu intră în verdicte.
- MV2 în Firefox se judecă pe substream: Firefox nu oferă H.264 Main, iar în produs fluxul principal trece pe substream (§6.3 pasul 7).
- MV14 (linii Kotlin pentru PiP și rețeaua locală) nu apare în rezultate: îl numără `tools/scripts/count-native-lines.sh` în CI.

## Date brute

Câte un fișier CSV pe dispozitiv, lângă acest raport, cu o linie pe mostră (coloanele: `device,scenario,run,status,key,index,value`; `status` e `used`, `superseded`, `interrupted`, `failed`, `invalid` sau `informative`; `hubJitterMs` e jitterul hub-ului al rulării):

- `phase0-data/phase0b-android.csv`
- `phase0-data/phase0b-chrome-android.csv`
- `phase0-data/phase0b-chrome.csv`
- `phase0-data/phase0b-edge.csv`
- `phase0-data/phase0b-firefox.csv`
- `phase0-data/phase0b-windows-tauri.csv`

## Fișiere necitite

Aceste fișiere din dosarul de rezultate nu sunt rezultate utilizabile și nu intră în raport:

- `broken-20261018-155000-s1.json`: nu e JSON valid

