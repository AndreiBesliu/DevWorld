# Rezultatele Fazei 0 (b): spike-ul video

Raport din 2026-10-19, generat de `spikereport` din fișierele de rezultate ale spike-ului. Rulări citite: 24; folosite la praguri: 22; excluse (întrerupte sau eșuate): 1; înlocuite de o rulare mai nouă a aceluiași scenariu: 1. Cifrele brute sunt în fișierele CSV de la sfârșit (PH0-30).
Textele din rezultate trec printr-un filtru care scoate adrese, URL-uri, adrese MAC, e-mailuri și valorile etichetate `serial` și `ssid` (PH0-22); proprietarul citește raportul înainte de commit.

## Rezumat pentru proprietar

- **Android (AND-1, samsung SM-A245F):** **GO condiționat**. Cu 4 plăci trece tot ce e critic, dar cu 9 plăci pică MV3/9, MV4/9. Plafonul de 9 plăci pe telefon (A6) se schimbă doar cu aprobarea dvs.
- **Windows (WIN-1):** **plan B: grilă desktop 9**. MV6 pică: grila desktop coboară la 9 plăci (APP-30). Windows nu poate da NO-GO.
- **Web (Chrome, Edge, Firefox):** **GO**. Perechile ICE din Chrome, Edge, Firefox și din cele două WebView-uri nu trec prin relay.
- **Argon2id (G19, MV15):** trece: rulări max 2566 ms (mediana 2336 ms; 10 din 10).
- **Codecuri (G9):** AND-1: mx-h265: fallback; mx-high51: ok · Firefox: mx-h265: fallback; mx-high51: unsupported-profile.
- **Nemăsurat încă:** Chrome: MV3/16, MV7, MV10, G9; Edge: MV2, MV3/9, G9; Firefox: MV1, MV2, MV3/9 (motivele sunt în tabele).
- **De decis de dvs.:** plafonul de plăci pe telefon (A6, acum 9), vezi G5 mai jos.
- **Atenție:** pentru MV5/9 (AND-1), o rulare anterioară a aceluiași scenariu a picat pragul; valoarea o dă cea mai nouă rulare, dar eșecul rămâne în tabele, lângă valoare (R40).
- **Atenție:** rulări cu jitter al hub-ului peste 20 ms: 1 (vezi Avertismente); cifrele lor pot fi mai proaste decât în realitate.

## Rezultate pe dispozitiv

### AND-1: Android (Capacitor)

**Dispozitiv:** samsung SM-A245F · Android 14 (API 34) · com.google.android.webview 120.0.6099
De completat de proprietar: memoria RAM a telefonului (nu apare în rezultate).

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV1 | p90 666 ms (mediana 536 ms, max 693 ms; 20 din 20 porniri) | p90 ≤ 1,5 s (20 porniri) | Da |
| MV2 | p95 sub 306 ms (100 mostre); p95 principal 401 ms (100 mostre) | p95 < 1 s (100 mostre) | Da |
| MV3/4 | fps minim 11,86; pierdute max 0,5 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Da |
| MV3/9 | fps minim 9,6; pierdute max 0,5 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Nu |
| MV4/4 | plăci hardware: 4 din 4; ecran complet: hardware | ecran complet hardware; toate plăcile grilei hardware | Da |
| MV4/9 | plăci hardware: 8 din 9; ecran complet: hardware | ecran complet hardware; toate plăcile grilei hardware | Nu |
| MV5/4 | termic max LIGHT; fps în ultimele 2 min 11,95; baterie 0 %/h | termic ≤ MODERATE; fps final ≥ 10,8; baterie ≤ 15 %/h | Da |
| MV5/9 | termic max MODERATE; fps în ultimele 2 min 11,95; baterie 12 %/h; **atenție:** o rulare anterioară a picat pragul (`android-samsung-sm-a245f-20261018-102500-s3-9.json`): termic max SEVERE; fps în ultimele 2 min 11,95; baterie 12 %/h | termic ≤ MODERATE; fps final ≥ 10,8; baterie ≤ 15 %/h | Da |
| MV7 | memorie +2,1 %; heap JS +3,9 %; reporniri neprovocate: 0 | creștere memorie ≤ 10 %; 0 reporniri neprovocate | Da |
| MV8 | 300 s în PiP; pauză maximă între cadre 210 ms; 2995 cadre | 5 min în PiP fără pauze de peste 2 s | Da |
| MV9 | reluare în 1480 ms după 123 s în fundal | live reluat în ≤ 3 s după 2 min în fundal | Da |
| MV10 | pornire max 1080 ms (mediana 803 ms; 20 din 20); salt max 846 ms (mediana 623 ms; 20 din 20) | pornire ≤ 2 s; salt ≤ 1,5 s (toate cele 20) | Da |
| MV12 | pereche host/host; cerere de permisiune: nu | perechea ICE aleasă nu e relay | Da |
| MV15 | rulări max 2566 ms (mediana 2336 ms; 10 din 10) | ≤ 3 s pe AND-1 (10 rulări) | Da |
| G9 | mx-h265: fallback; mx-high51: ok | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | Da |

**Note la praguri:**

- **MV1:** substream: H264
- **MV2:** jitter hub: 24,5 ms în rularea S2 (peste 20 ms): PC-ul hub-ului poate fi ocupat, deci cifrele clientului pot fi mai proaste decât în realitate
- **MV2:** sub: 3 din 100 mostre cu ceas nesigur (RTT > 50 ms sau offset mutat cu > 20 ms); intră în calcul
- **MV2:** RTT al ceasului: max 7 ms (eroarea offsetului e cel mult jumătate din el)
- **MV3/4:** ecran complet (flux principal, informativ): 19,83 fps, pierdute 0,3 %
- **MV3/9:** ecran complet (flux principal, informativ): 19,83 fps, pierdute 0,3 %
- **MV7:** memMb = PSS al procesului aplicației; randerul WebView e alt proces și nu e inclus, de aceea se citește și heap-ul JS
- **MV7:** creștere = media ultimelor 6 min față de media a 6 min de după primul minut, ferestre care acoperă întregi ciclurile de paginare (2 min) și de ecran complet (3 min)
- **MV12:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară
- **G9:** primul cadru: mx-h265 pe substream 310 ms, mx-high51 pe fluxul principal 641 ms

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| android-samsung-sm-a245f-20261018-095500-s1.json | S1 | 61 s | folosită | 1,2 ms |
| android-samsung-sm-a245f-20261018-100200-s2.json | S2 | 52 s | folosită | 24,5 ms, peste 20 ms |
| android-samsung-sm-a245f-20261018-101000-s3-4.json | S3-4 | 724 s | folosită | 1,1 ms |
| android-samsung-sm-a245f-20261018-094000-s3-9.json | S3-9 | 142 s | întreruptă, exclusă | 3,1 ms |
| android-samsung-sm-a245f-20261018-102500-s3-9.json | S3-9 | 724 s | înlocuită de una mai nouă | 1,6 ms |
| android-samsung-sm-a245f-20261018-104500-s3-9.json | S3-9 | 725 s | folosită | 1,8 ms |
| android-samsung-sm-a245f-20261018-112000-s4.json | S4 | 1803 s | folosită | 1,4 ms |
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
- S3-9 (android-samsung-sm-a245f-20261018-104500-s3-9.json): decoders: c2.mtk.avc.decoder ×8, OMX.google.h264.decoder ×1
- S3-9 (android-samsung-sm-a245f-20261018-104500-s3-9.json): main decoder: c2.mtk.avc.decoder
- S9 (android-samsung-sm-a245f-20261018-122500-s9.json): mx-h265/main: error CODEC_UNSUPPORTED
- S9 (android-samsung-sm-a245f-20261018-122500-s9.json): mx-h265/sub: bound H264
- S9 (android-samsung-sm-a245f-20261018-122500-s9.json): mx-h265: fallback
- S9 (android-samsung-sm-a245f-20261018-122500-s9.json): mx-high51/main: bound H264
- S9 (android-samsung-sm-a245f-20261018-122500-s9.json): mx-high51: ok
- S10 (android-samsung-sm-a245f-20261018-123000-s10.json): pair: host/host
- S10 (android-samsung-sm-a245f-20261018-123000-s10.json): prompt: no

### WIN-1: Windows (Tauri, WebView2)

**Dispozitiv:** AMD Ryzen 5 5600G with Radeon Graphics · Windows 11 Pro · WebView2 154.0.4258
De completat de proprietar: GPU-ul și memoria RAM ale PC-ului (nu apar în rezultate; procesorul e modelul de mai sus).

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV3/16 | fps minim 11,86; pierdute max 0,5 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Da |
| MV6 | 16 plăci: 30,8 % (max 32,7 %); flux principal 1080p: 7,6 % | 16 plăci ≤ 25 % CPU; flux principal ≤ 10 % CPU | Nu |
| MV7 | memorie +1,6 %; heap JS +2,5 %; reporniri neprovocate: 0 | creștere memorie ≤ 10 %; 0 reporniri neprovocate | Da |
| MV12 | pereche host/host; cerere de permisiune: nu | perechea ICE aleasă nu e relay | Da |

**Note la praguri:**

- **MV3/16:** ecran complet (flux principal, informativ): 19,83 fps, pierdute 0,3 %
- **MV6:** CPU total al PC-ului, cu hub-ul pe același PC: 43,8 %
- **MV7:** memMb = memorie privată (private bytes, R37) a aplicației și a proceselor WebView2; nu include secțiunile partajate și memoria GPU, unde Chromium ține cadrele decodate, deci o scurgere acolo nu apare în MV7
- **MV7:** creștere = media ultimelor 6 min față de media a 6 min de după primul minut, ferestre care acoperă întregi ciclurile de paginare (2 min) și de ecran complet (3 min)
- **MV12:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-131000-s3-16.json | S3-16 | 725 s | folosită | 2,2 ms |
| windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-134000-s4.json | S4 | 1802 s | folosită | 2 ms |
| windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-140500-s10.json | S10 | 3 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S3-16 (windows-tauri-amd-ryzen-5-5600g-with-radeon-graphics-20261018-131000-s3-16.json): decoder info: n/a
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
| MV7 | nemăsurat: S4 nu a rulat | creștere memorie ≤ 10 %; 0 reporniri neprovocate | — |
| MV10 | nemăsurat: S7 nu a rulat | pornire ≤ 2 s; salt ≤ 1,5 s (toate cele 20) | — |
| MV13 | pereche host/host; cerere de permisiune: necunoscut (n/a) | ambele capete host sau prflx, fără relay | Da |
| G9 | nemăsurat: S9 nu a rulat | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | — |

**Note la praguri:**

- **MV1:** substream: H264
- **MV2:** RTT al ceasului: max 1 ms (eroarea offsetului e cel mult jumătate din el)
- **MV3/9:** ecran complet (flux principal, informativ): 19,83 fps, pierdute 0,3 %
- **MV13:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| chrome-20261018-150000-s1.json | S1 | 61 s | folosită | 1,1 ms |
| chrome-20261018-150200-s2.json | S2 | 51 s | folosită | 1,2 ms |
| chrome-20261018-150500-s3-9.json | S3-9 | 723 s | folosită | 0,9 ms |
| chrome-20261018-152000-s10.json | S10 | 3 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S1 (chrome-20261018-150000-s1.json): sub: H264
- S3-9 (chrome-20261018-150500-s3-9.json): decoder info: n/a
- S3-9 (chrome-20261018-150500-s3-9.json): native metrics: n/a
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
| MV2 | nemăsurat: S2 nu a rulat | p95 < 1 s (100 mostre) | — |
| MV3/9 | nemăsurat: S3-9 nu a rulat | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | — |
| MV13 | pereche host/host; cerere de permisiune: necunoscut (n/a) | ambele capete host sau prflx, fără relay | Da |
| G9 | mx-h265: fallback; mx-high51: unsupported-profile | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | Da |

**Note la praguri:**

- **MV13:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară
- **MV13:** mDNS indisponibil pe hub: n-a putut rezolva candidații .local ai browserelor; un filtru de multicast nu pică MV13 dacă perechea e prflx
- **G9:** primul cadru: mx-h265 pe substream 95 ms, mx-high51 pe substream 89 ms
- **G9:** mx-h265/sub: H264, în profilul de rezervă 4d001f (R23: browserul nu oferă profilul sursei)
- **G9:** mx-high51/sub: H264, în profilul de rezervă 4d001f (R23: browserul nu oferă profilul sursei)

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| firefox-20261018-154000-s9.json | S9 | 11 s | folosită | 1,1 ms |
| firefox-20261018-154500-s10.json | S10 | 3 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S9 (firefox-20261018-154000-s9.json): mx-h265/main: error CODEC_UNSUPPORTED
- S9 (firefox-20261018-154000-s9.json): mx-h265/sub: bound H264, fallback profile 4d001f
- S9 (firefox-20261018-154000-s9.json): mx-h265: fallback
- S9 (firefox-20261018-154000-s9.json): mx-high51/main: error CODEC_UNSUPPORTED
- S9 (firefox-20261018-154000-s9.json): mx-high51/sub: bound H264, fallback profile 4d001f
- S9 (firefox-20261018-154000-s9.json): mx-high51: unsupported-profile
- S10 (firefox-20261018-154500-s10.json): pair: host/host
- S10 (firefox-20261018-154500-s10.json): prompt: n/a

## Avertismente

- Jitter al hub-ului: rulările de mai jos au avut o fereastră de 10 s cu p99 al abaterii de trimitere de peste 20 ms. PC-ul hub-ului poate să fi fost ocupat (lucru în paralel, antivirus), deci cifrele clientului din aceste rulări pot fi mai proaste decât în realitate; repetați-le cu PC-ul liber.
  - `android-samsung-sm-a245f-20261018-100200-s2.json` (S2): jitter maxim 24,5 ms, peste 20 ms.
- Rulări întrerupte: 1 (pagina a fost ascunsă: ecranul s-a stins sau aplicația a trecut în fundal). Nu intră în praguri, iar aplicația oferă să le repetați: `android-samsung-sm-a245f-20261018-094000-s3-9.json`.
- mDNS indisponibil pe hub în rulările S10 `firefox-20261018-154500-s10.json`: hub-ul nu a putut rezolva candidații .local ai browserelor.

## Verdicte și decizii declanșate

| Poartă | Verdict sau rezultat |
| --- | --- |
| G5: Grila Android (AND-1) | GO condiționat |
| G6: Windows WebView2 (WIN-1) | plan B: grilă desktop 9 |
| G7: Chrome LNA și mDNS (web) | GO |
| G9: Matricea de codecuri | AND-1: mx-h265: fallback; mx-high51: ok · Firefox: mx-h265: fallback; mx-high51: unsupported-profile |
| G19: Argon2id pe Android mediu (MV15) | trece: rulări max 2566 ms (mediana 2336 ms; 10 din 10) |

### G5: Grila Android (AND-1)

- **Verdict:** GO condiționat
- **Decizie declanșată:** Cu 4 plăci trece tot ce e critic, dar cu 9 plăci pică MV3/9, MV4/9. Plafonul pe telefon (A6: maximum 9 plăci) se schimbă doar cu aprobarea dvs. Până la decizia dvs. plafonul rămâne 9, iar plăcile care nu pot decoda trec pe snapshot (§6.5).
- **Ce se schimbă în §2 sau în cerințe:** O schimbare aprobată se scrie în §2.1, ca modificare a lui A6, nu în §2.2. Decizia proprietarului asupra plafonului pe telefon: de completat de proprietar.

### G6: Windows WebView2 (WIN-1)

- **Verdict:** plan B: grilă desktop 9
- **Decizie declanșată:** MV6 pică: grila desktop coboară la 9 plăci (APP-30). Windows nu poate da NO-GO.
- **Ce se schimbă în §2 sau în cerințe:** §2.2: Decizie tehnică „grilă desktop 9”, cu trimitere la G6.

### G7: Chrome LNA și mDNS (web)

- **Verdict:** GO
- **Decizie declanșată:** Nicio decizie: perechile ICE din Chrome, Edge, Firefox și din cele două WebView-uri nu trec prin relay. MV12 în Chrome, cu origine publică, rămâne pentru Faza 0-bis.
- **Ce se schimbă în §2 sau în cerințe:** Nimic în §2 sau în cerințe.

### G9: Matricea de codecuri

- **Rezultat:** AND-1: mx-h265: fallback; mx-high51: ok · Firefox: mx-h265: fallback; mx-high51: unsupported-profile
- **Decizie declanșată:** Regula SDP și primul cadru în 4 s aleg corect pe căile măsurate: H.265 cade pe substream cu eticheta „calitate redusă: H.265 nesuportat”, iar un profil H.264 nesuportat pe eticheta „profil video nesuportat”. Rezultatele pe cale se notează.
- **Ce se schimbă în §2 sau în cerințe:** §2.2: Decizie tehnică cu rezultatele G9 de mai sus (căile iOS rămân pentru Faza 0-bis).

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
- Dintre rulările valide ale aceluiași scenariu pe același dispozitiv, valoarea o dă cea mai nouă (după ceasul dispozitivului). O rulare anterioară care a picat pragul nu dispare: e arătată lângă valoare, cu avertisment (R40), ca repetarea până la o trecere să nu ascundă un eșec. Rulările întrerupte și cele eșuate sunt listate, dar nu intră în praguri.
- MV2 se calculează cu offsetul dintre ceasul dispozitivului și al hub-ului (ping pe canalul de date), nu cu ceasul dispozitivului; mostrele exclud cadrele din GOP-ul păstrat în cache la legarea unei camere, care au vârsta cache-ului în loc de întârzierea transportului (R26).
- MV2 păstrează mostrele cu ceas nesigur (RTT > 50 ms sau offset mutat cu > 20 ms) și le raportează în notele pragului, fără să le excludă: eroarea offsetului e cel mult jumătate din RTT-ul ceasului.
- Specificația nu dă un percentil pentru MV10 (este un prag critic al AND-1), așa că MV10 și MV15 judecă fiecare citire: toate cele 20 de porniri (≤ 2 s) și toate cele 20 de salturi (≤ 1,5 s) la MV10, toate cele 10 rulări (≤ 3 s) la MV15; nicio pornire, salt sau rulare nu poate expira. MV1 folosește p90 din 20, cum cere specificația. O pornire, un salt sau o rulare care nu s-a terminat (MV1, MV10, MV15) se numără ca infinit de lent.
- MV5 judecă fps-ul pe media ultimelor 2 minute din `gridFps` (60 de ferestre de 2 s), nu pe toată rularea: o scădere din cauza căldurii se vede la sfârșit.
- MV5 calculează ritmul bateriei din prima și ultima citire, raportat la durata măsurată a grilei (ferestrele de 2 s din `gridFps`), nu la numărul de citiri: o citire a shell-ului care a expirat nu îl umflă. Procentul bateriei e întreg, deci rezoluția ritmului e de circa 6 %/h (1 % în circa 10 min): două puncte dau circa 12 %/h (trece), trei circa 18 %/h (pică), iar pragul de 15 %/h cade între aceste trepte.
- MV6 judecă media lui `cpuPctApp` (procesul aplicației și procesele WebView2, ca pondere din tot PC-ul), nu maximul și nu `cpuPctTotal`, care include și hub-ul de pe același PC; fluxul principal se judecă pe media lui `mainCpuPctApp`, de la prima rulare a grilei care l-a măsurat.
- MV8: specificația cere 5 min în PiP cu video care avansează; raportul cere 300 s în PiP, cel puțin un cadru și nicio pauză între cadre de peste 2 s. Pragul de 2 s pentru pauză e alegerea raportului, nu a specificației.
- Pe WIN-1, `memMb` (MV7) e memorie privată (private bytes, R37) a aplicației și a proceselor WebView2: nu include secțiunile partajate și memoria GPU, unde Chromium ține cadrele decodate, deci o scurgere acolo nu apare în MV7.
- MV14 (linii Kotlin pentru PiP și rețeaua locală) nu apare în rezultate: îl numără `tools/scripts/count-native-lines.sh` în CI.

## Date brute

Câte un fișier CSV pe dispozitiv, lângă acest raport, cu o linie pe mostră (coloanele: `device,scenario,run,status,key,index,value`; `status` e `used`, `superseded`, `interrupted` sau `failed`; `hubJitterMs` e jitterul hub-ului al rulării):

- `phase0-data/phase0b-android.csv`
- `phase0-data/phase0b-chrome.csv`
- `phase0-data/phase0b-edge.csv`
- `phase0-data/phase0b-firefox.csv`
- `phase0-data/phase0b-windows-tauri.csv`

## Fișiere necitite

Aceste fișiere din dosarul de rezultate nu sunt rezultate utilizabile și nu intră în raport:

- `broken-20261018-155000-s1.json`: nu e JSON valid

