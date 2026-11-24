# Rezultatele Fazei 0 (b): spike-ul video

Raport din 2026-11-24, generat de `spikereport` din fișierele de rezultate ale spike-ului. Rulări citite: 46; folosite la praguri: 36; invalide, deci excluse (întrerupte, eșuate sau la încărcător): 10; înlocuite de o rulare mai nouă a aceluiași scenariu: 0; doar informative: 0. Cifrele brute sunt în fișierele CSV de la sfârșit (PH0-30).
Textele din rezultate trec printr-un filtru care scoate adrese, URL-uri, adrese MAC, e-mailuri și valorile etichetate `serial` și `ssid`, iar raportul se scrie doar dacă scanerul de redactare nu mai găsește nimic în el (PH0-22, R51); proprietarul îl citește înainte de commit.

## Rezumat pentru proprietar

- **Android (AND-1, samsung SM-A546B):** **GO provizoriu (fără iOS)**. Toate cele 9 praguri critice trec (MV1, MV2, MV3 cu 4 plăci, MV9, MV10 și, cu 9 plăci, MV3, MV4 pe plăci și pe ecranul complet, MV5). Verdictul e provizoriu: iOS se măsoară în Faza 0-bis.
- **Windows (WIN-1):** **GO**. MV6 și MV7 trec cu 16 plăci: CPU-ul (16 plăci și un flux principal) și memoria pe 30 de minute încap în praguri.
- **Web (Chrome, Edge, Firefox):** **GO**. Perechile ICE din Chrome, Edge, Firefox și din cele două WebView-uri nu trec prin relay.
- **Argon2id (G19, MV15):** trece: rulări max 328 ms (mediana 303 ms; 10 din 10).
- **Codecuri (G9):** AND-1: mx-h265: ok; mx-high51: ok · WIN-1: mx-h265: fallback; mx-high51: ok · Chrome: mx-h265: ok; mx-high51: ok · Edge: mx-h265: fallback; mx-high51: ok · Firefox: mx-h265: fallback; mx-high51: unsupported-profile.
- **Nemăsurat încă:** nimic din ce cere planul pe dispozitive (spec §16.3).
- **Atenție:** rulări cu jitter al hub-ului peste 20 ms: 12 (vezi Avertismente); cifrele lor pot fi mai proaste decât în realitate.

## Rezultate pe dispozitiv

### AND-1: Android (Capacitor)

**Dispozitiv:** samsung SM-A546B · Android 16 (API 36) · com.google.android.webview 157.0.8103 · memorie RAM aprox. 8 GB (7436 MiB raportați de sistem)

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV1 | p90 99 ms (mediana 38 ms, max 352 ms; 20 din 20 porniri) | p90 ≤ 1,5 s (20 porniri) | Da |
| MV2 | p95 sub 57 ms (100 mostre); p95 principal 83 ms (100 mostre) | p95 < 1 s (100 mostre) | Da |
| MV3/4 | fps minim 12; pierdute max 0 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Da |
| MV3/9 | fps minim 11,99; pierdute max 0 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Da |
| MV4/4 | plăci hardware: 4 din 4 | toate plăcile grilei hardware | Da |
| MV4/9 | plăci hardware: 9 din 9 | toate plăcile grilei hardware | Da |
| MV4/main | ecran complet: hardware (S3-4, S3-9) | ecranul complet (flux principal) mereu hardware | Da |
| MV5/4 | termic max NONE; fps în ultimele 2 min 12; baterie 12 %/h | termic ≤ MODERATE; fps final ≥ 10,8; baterie ≤ 15 %/h | Da |
| MV5/9 | termic max NONE; fps în ultimele 2 min 11,99; baterie 12 %/h | termic ≤ MODERATE; fps final ≥ 10,8; baterie ≤ 15 %/h | Da |
| MV7 | memorie +2,4 %; heap JS rotunjit de Chromium (jsHeap: bucketed), nu se judecă (R46); reporniri neprovocate: 0 | creștere memorie ≤ 10 %; 0 reporniri neprovocate | Da |
| MV8 | 300 s în PiP; pauză maximă între cadre 133 ms; 3599 cadre | 5 min în PiP fără pauze de peste 2 s | Da |
| MV9 | reluare în 760 ms după 340 s în fundal | live reluat în ≤ 3 s după 2 min în fundal | Da |
| MV10 | pornire max 1331 ms (mediana 1008 ms; 20 din 20); salt max 919 ms (mediana 571 ms; 20 din 20) | pornire ≤ 2 s; salt ≤ 1,5 s (toate cele 20) | Da |
| MV12 | pereche host/prflx; cerere de permisiune: da | perechea ICE aleasă nu e relay | Da |
| MV15 | rulări max 328 ms (mediana 303 ms; 10 din 10) | ≤ 3 s pe AND-1 (10 rulări) | Da |
| G9 | mx-h265: ok; mx-high51: ok | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | Da |

**Note la praguri:**

- **MV1:** substream: H264
- **MV1:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 1, cadre aruncate 15
- **MV2:** RTT al ceasului: max 3 ms (eroarea offsetului e cel mult jumătate din el)
- **MV3/4:** ecran complet (flux principal, informativ): 19,99 fps, pierdute 0 %
- **MV3/4:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 49, PLI 6, cadre aruncate 24
- **MV3/9:** ecran complet (flux principal, informativ): 19,99 fps, pierdute 0 %
- **MV3/9:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 88, PLI 12, cadre aruncate 41
- **MV7:** jitter hub: 49,8 ms în rularea S4 (peste 20 ms): PC-ul hub-ului poate fi ocupat, deci cifrele clientului pot fi mai proaste decât în realitate
- **MV7:** memMb = PSS al procesului aplicației, fără randerul WebView, care e alt proces; heap-ul JS e rotunjit de Chromium în Android WebView, deci MV7 se judecă doar pe memMb (R46)
- **MV7:** grila a paginat între 4 și 9 plăci (plafonul telefonului, A6)
- **MV7:** creștere = media ultimelor 6 min față de media a 6 min de după primul minut, ferestre care acoperă întregi ciclurile de paginare (2 min) și de ecran complet (3 min)
- **MV12:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară
- **G9:** primul cadru: mx-h265 pe fluxul principal 191 ms, mx-high51 pe fluxul principal 116 ms

**Rulări invalide** (nu intră în praguri; repetați scenariul cu «Repetă»):

- `android-samsung-sm-a546b-20261124-163951-s5.json` (S5): aplicația a revenit din fundal după 2 s; MV9 cere cel puțin 2 minute în fundal.
- `android-samsung-sm-a546b-20261124-165254-s6.json` (S6): fereastra mică (PiP) s-a închis după 4 s; MV8 cere 5 minute.
- `android-samsung-sm-a546b-20261124-171225-s7.json` (S7): pagina a fost ascunsă la 13 s de la început: ecranul s-a stins sau aplicația a trecut în fundal.

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| android-samsung-sm-a546b-20261124-151324-s1.json | S1 | 59 s | folosită | 0,4 ms |
| android-samsung-sm-a546b-20261124-151412-s2.json | S2 | 48 s | folosită | 1,2 ms |
| android-samsung-sm-a546b-20261124-160247-s3-4.json | S3-4 | 726 s | folosită | 6,1 ms |
| android-samsung-sm-a546b-20261124-154040-s3-9.json | S3-9 | 727 s | folosită | 14,8 ms |
| android-samsung-sm-a546b-20261124-163252-s4.json | S4 | 1805 s | folosită | 49,8 ms, peste 20 ms |
| android-samsung-sm-a546b-20261124-163951-s5.json | S5 | 419 s | întreruptă, exclusă | 38,3 ms, peste 20 ms |
| android-samsung-sm-a546b-20261124-165247-s5.json | S5 | 351 s | folosită | — |
| android-samsung-sm-a546b-20261124-165254-s6.json | S6 | 5 s | întreruptă, exclusă | — |
| android-samsung-sm-a546b-20261124-165759-s6.json | S6 | 301 s | folosită | 12,4 ms |
| android-samsung-sm-a546b-20261124-171225-s7.json | S7 | 13 s | întreruptă, exclusă | — |
| android-samsung-sm-a546b-20261124-171302-s7.json | S7 | 34 s | folosită | — |
| android-samsung-sm-a546b-20261124-171305-s8.json | S8 | 3 s | folosită | — |
| android-samsung-sm-a546b-20261124-171309-s9.json | S9 | 4 s | folosită | — |
| android-samsung-sm-a546b-20261124-151217-s10.json | S10 | 2 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S1 (android-samsung-sm-a546b-20261124-151324-s1.json): sub: H264
- S3-4 (android-samsung-sm-a546b-20261124-160247-s3-4.json): decoders: ExternalDecoder (MediaCodecVideoDecoder) ×4
- S3-4 (android-samsung-sm-a546b-20261124-160247-s3-4.json): main decoder: ExternalDecoder (MediaCodecVideoDecoder)
- S3-9 (android-samsung-sm-a546b-20261124-154040-s3-9.json): decoders: ExternalDecoder (MediaCodecVideoDecoder) ×9
- S3-9 (android-samsung-sm-a546b-20261124-154040-s3-9.json): main decoder: ExternalDecoder (MediaCodecVideoDecoder)
- S4 (android-samsung-sm-a546b-20261124-163252-s4.json): jsHeap: bucketed
- S5 (android-samsung-sm-a546b-20261124-163951-s5.json): connection at return: connected
- S5 (android-samsung-sm-a546b-20261124-163951-s5.json): session: kept
- S5 (android-samsung-sm-a546b-20261124-163951-s5.json): background shorter than 2 min
- S5 (android-samsung-sm-a546b-20261124-163951-s5.json): interrupted: back from the background after 2 s, at least 120 s are needed
- S5 (android-samsung-sm-a546b-20261124-165247-s5.json): connection at return: closed
- S5 (android-samsung-sm-a546b-20261124-165247-s5.json): the app closed the session after 60 s in the background (connection failed then)
- S5 (android-samsung-sm-a546b-20261124-165247-s5.json): session: reopened
- S6 (android-samsung-sm-a546b-20261124-165254-s6.json): pipVisible: 5/5 s
- S6 (android-samsung-sm-a546b-20261124-165254-s6.json): left picture-in-picture after 4 s
- S6 (android-samsung-sm-a546b-20261124-165254-s6.json): interrupted: picture-in-picture ended after 4 s, 300 s are needed
- S6 (android-samsung-sm-a546b-20261124-165759-s6.json): pipVisible: 300/300 s
- S7 (android-samsung-sm-a546b-20261124-171225-s7.json): interrupted: the page was hidden 13 s into the run
- S9 (android-samsung-sm-a546b-20261124-171309-s9.json): mx-h265/main: bound H265
- S9 (android-samsung-sm-a546b-20261124-171309-s9.json): mx-h265: ok
- S9 (android-samsung-sm-a546b-20261124-171309-s9.json): mx-high51/main: bound H264
- S9 (android-samsung-sm-a546b-20261124-171309-s9.json): mx-high51: ok
- S10 (android-samsung-sm-a546b-20261124-151217-s10.json): pair: host/prflx
- S10 (android-samsung-sm-a546b-20261124-151217-s10.json): prompt: yes

### WIN-1: Windows (Tauri, WebView2)

**Dispozitiv:** procesor AMD Ryzen 9 7950X 16-Core Processor · Windows 11 Home · WebView2 158.0.4370 · GPU NVIDIA GeForce RTX 3060, AMD Radeon(TM) Graphics · memorie RAM aprox. 32 GB (31849 MiB raportați de sistem)

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV1 | p90 8 ms (mediana 3 ms, max 29 ms; 20 din 20 porniri) | p90 ≤ 1,5 s (20 porniri) | Da |
| MV2 | p95 sub 63 ms (100 mostre); p95 principal 64 ms (100 mostre) | p95 < 1 s (100 mostre) | Da |
| MV3/16 | fps minim 11,99; pierdute max 0 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Da |
| MV6 | 16 plăci: 0,5 % (max 0,7 %); flux principal 1080p: 0,2 % | 16 plăci ≤ 25 % CPU; flux principal ≤ 10 % CPU | Da |
| MV7 | memorie -0,9 %; heap JS -4,5 %; reporniri neprovocate: 0 | creștere memorie ≤ 10 %; 0 reporniri neprovocate | Da |
| MV10 | pornire max 101 ms (mediana 45 ms; 20 din 20); salt max 38 ms (mediana 15 ms; 20 din 20) | pornire ≤ 2 s; salt ≤ 1,5 s (toate cele 20) | Da |
| MV12 | pereche host/prflx; cerere de permisiune: da | perechea ICE aleasă nu e relay | Da |
| G9 | mx-h265: fallback; mx-high51: ok | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | Da |

**Note la praguri:**

- **MV1:** substream: H264
- **MV1:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 0, cadre aruncate 0
- **MV2:** RTT al ceasului: max 0 ms (eroarea offsetului e cel mult jumătate din el)
- **MV3/16:** jitter hub: 40,3 ms în rularea S3-16 (peste 20 ms): PC-ul hub-ului poate fi ocupat, deci cifrele clientului pot fi mai proaste decât în realitate
- **MV3/16:** ecran complet (flux principal, informativ): 19,99 fps, pierdute 0 %
- **MV3/16:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 27, cadre aruncate 65
- **MV6:** jitter hub: 40,3 ms în rularea S3-16 (peste 20 ms): PC-ul hub-ului poate fi ocupat, deci cifrele clientului pot fi mai proaste decât în realitate
- **MV6:** CPU total al PC-ului, cu hub-ul pe același PC: 3,6 %
- **MV7:** jitter hub: 58 ms în rularea S4 (peste 20 ms): PC-ul hub-ului poate fi ocupat, deci cifrele clientului pot fi mai proaste decât în realitate
- **MV7:** memMb = memorie privată (private bytes, R37) a aplicației și a proceselor WebView2; nu include secțiunile partajate și memoria GPU, unde Chromium ține cadrele decodate, deci o scurgere acolo nu apare în MV7
- **MV7:** grila a paginat între 9 și 16 plăci: pe PC, MV7 se judecă doar cu 16 plăci (G6, R45)
- **MV7:** creștere = media ultimelor 6 min față de media a 6 min de după primul minut, ferestre care acoperă întregi ciclurile de paginare (2 min) și de ecran complet (3 min)
- **MV12:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară
- **G9:** primul cadru: mx-h265 pe substream 8 ms, mx-high51 pe fluxul principal 306 ms

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| windows-tauri-amd-ryzen-9-7950x-16-core-20261124-100916-s1.json | S1 | 58 s | folosită | 0,4 ms |
| windows-tauri-amd-ryzen-9-7950x-16-core-20261124-101003-s2.json | S2 | 47 s | folosită | 13,4 ms |
| windows-tauri-amd-ryzen-9-7950x-16-core-20261124-102206-s3-16.json | S3-16 | 723 s | folosită | 40,3 ms, peste 20 ms |
| windows-tauri-amd-ryzen-9-7950x-16-core-20261124-105208-s4.json | S4 | 1802 s | folosită | 58 ms, peste 20 ms |
| windows-tauri-amd-ryzen-9-7950x-16-core-20261124-105211-s7.json | S7 | 2 s | folosită | — |
| windows-tauri-amd-ryzen-9-7950x-16-core-20261124-105214-s9.json | S9 | 4 s | folosită | — |
| windows-tauri-amd-ryzen-9-7950x-16-core-20261124-100818-s10.json | S10 | 3 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S1 (windows-tauri-amd-ryzen-9-7950x-16-core-20261124-100916-s1.json): sub: H264
- S3-16 (windows-tauri-amd-ryzen-9-7950x-16-core-20261124-102206-s3-16.json): decoder info: n/a
- S9 (windows-tauri-amd-ryzen-9-7950x-16-core-20261124-105214-s9.json): mx-h265/main: error CODEC_UNSUPPORTED
- S9 (windows-tauri-amd-ryzen-9-7950x-16-core-20261124-105214-s9.json): mx-h265/sub: bound H264
- S9 (windows-tauri-amd-ryzen-9-7950x-16-core-20261124-105214-s9.json): mx-h265: fallback
- S9 (windows-tauri-amd-ryzen-9-7950x-16-core-20261124-105214-s9.json): mx-high51/main: bound H264
- S9 (windows-tauri-amd-ryzen-9-7950x-16-core-20261124-105214-s9.json): mx-high51: ok
- S10 (windows-tauri-amd-ryzen-9-7950x-16-core-20261124-100818-s10.json): pair: host/prflx
- S10 (windows-tauri-amd-ryzen-9-7950x-16-core-20261124-100818-s10.json): prompt: yes

### Web: Chrome

**Dispozitiv:** Windows · Chrome/157.0.0

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV1 | p90 80 ms (mediana 14 ms, max 1766 ms; 20 din 20 porniri) | p90 ≤ 1,5 s (20 porniri) | Da |
| MV2 | p95 sub 59 ms (100 mostre); p95 principal 77 ms (100 mostre) | p95 < 1 s (100 mostre) | Da |
| MV3/9 | fps minim 11,96; pierdute max 0 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Da |
| MV3/16 | fps minim 11,79; pierdute max 1 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Da |
| MV7 | heap JS -0,8 %; reporniri neprovocate: 0 | creștere memorie ≤ 10 %; 0 reporniri neprovocate | Da |
| MV10 | pornire max 108 ms (mediana 43 ms; 20 din 20); salt max 23 ms (mediana 16 ms; 20 din 20) | pornire ≤ 2 s; salt ≤ 1,5 s (toate cele 20) | Da |
| MV13 | pereche host/prflx; cerere de permisiune: da | ambele capete host sau prflx, fără relay | Da |
| G9 | mx-h265: ok; mx-high51: ok | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | Da |

**Note la praguri:**

- **MV1:** substream: H264
- **MV1:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 0, cadre aruncate 4
- **MV2:** RTT al ceasului: max 0 ms (eroarea offsetului e cel mult jumătate din el)
- **MV3/9:** jitter hub: 177 ms în rularea S3-9 (peste 20 ms): PC-ul hub-ului poate fi ocupat, deci cifrele clientului pot fi mai proaste decât în realitate
- **MV3/9:** ecran complet (flux principal, informativ): 20 fps, pierdute 0 %
- **MV3/9:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 9, cadre aruncate 12
- **MV3/16:** jitter hub: 342,5 ms în rularea S3-16 (peste 20 ms): PC-ul hub-ului poate fi ocupat, deci cifrele clientului pot fi mai proaste decât în realitate
- **MV3/16:** ecran complet (flux principal, informativ): 19,97 fps, pierdute 0 %
- **MV3/16:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 175, cadre aruncate 1008
- **MV7:** jitter hub: 1598 ms în rularea S4 (peste 20 ms): PC-ul hub-ului poate fi ocupat, deci cifrele clientului pot fi mai proaste decât în realitate
- **MV7:** memoria = heap-ul JS al paginii (Chromium); memoria procesului browserului nu e măsurată
- **MV7:** grila a paginat între 9 și 16 plăci: pe PC, MV7 se judecă doar cu 16 plăci (G6, R45)
- **MV7:** creștere = media ultimelor 6 min față de media a 6 min de după primul minut, ferestre care acoperă întregi ciclurile de paginare (2 min) și de ecran complet (3 min)
- **MV13:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară
- **G9:** primul cadru: mx-h265 pe fluxul principal 19 ms, mx-high51 pe fluxul principal 9 ms

**Rulări invalide** (nu intră în praguri; repetați scenariul cu «Repetă»):

- `chrome-20261117-173236-s3-4.json` (S3-4): pagina a fost ascunsă la 5 s de la început: ecranul s-a stins sau aplicația a trecut în fundal.
- `chrome-20261117-173420-s3-4.json` (S3-4): pagina a fost ascunsă la 7 s de la început: ecranul s-a stins sau aplicația a trecut în fundal.
- `chrome-20261117-173217-s3-9.json` (S3-9): pagina a fost ascunsă la 297 s de la început: ecranul s-a stins sau aplicația a trecut în fundal.
- `chrome-20261117-173228-s3-9.json` (S3-9): pagina a fost ascunsă la 2 s de la început: ecranul s-a stins sau aplicația a trecut în fundal.
- `chrome-20261117-173410-s3-9.json` (S3-9): pagina a fost ascunsă la 76 s de la început: ecranul s-a stins sau aplicația a trecut în fundal.
- `chrome-20261117-174353-s3-9.json` (S3-9): pagina a fost ascunsă la 570 s de la început: ecranul s-a stins sau aplicația a trecut în fundal.
- `chrome-20261117-175017-s3-9.json` (S3-9): pagina a fost ascunsă la 30 s de la început: ecranul s-a stins sau aplicația a trecut în fundal.

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| chrome-20261117-172540-s1.json | S1 | 60 s | folosită | 0,5 ms |
| chrome-20261117-172714-s2.json | S2 | 48 s | folosită | 2,9 ms |
| chrome-20261117-173236-s3-4.json | S3-4 | 5 s | întreruptă, exclusă | — |
| chrome-20261117-173420-s3-4.json | S3-4 | 7 s | întreruptă, exclusă | — |
| chrome-20261117-173217-s3-9.json | S3-9 | 297 s | întreruptă, exclusă | 62,6 ms, peste 20 ms |
| chrome-20261117-173228-s3-9.json | S3-9 | 2 s | întreruptă, exclusă | — |
| chrome-20261117-173410-s3-9.json | S3-9 | 76 s | întreruptă, exclusă | 47,9 ms, peste 20 ms |
| chrome-20261117-174353-s3-9.json | S3-9 | 571 s | întreruptă, exclusă | 881,8 ms, peste 20 ms |
| chrome-20261117-175017-s3-9.json | S3-9 | 30 s | întreruptă, exclusă | 55,9 ms, peste 20 ms |
| chrome-20261117-180225-s3-9.json | S3-9 | 723 s | folosită | 177 ms, peste 20 ms |
| chrome-20261117-185707-s3-16.json | S3-16 | 723 s | folosită | 342,5 ms, peste 20 ms |
| chrome-20261117-195933-s4.json | S4 | 1803 s | folosită | 1598 ms, peste 20 ms |
| chrome-20261117-215012-s7.json | S7 | 2 s | folosită | — |
| chrome-20261117-215023-s9.json | S9 | 4 s | folosită | — |
| chrome-20261117-172425-s10.json | S10 | 21 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S1 (chrome-20261117-172540-s1.json): sub: H264
- S3-4 (chrome-20261117-173236-s3-4.json): native metrics: n/a
- S3-4 (chrome-20261117-173236-s3-4.json): decoder info: n/a
- S3-4 (chrome-20261117-173236-s3-4.json): interrupted: the page was hidden 5 s into the run
- S3-4 (chrome-20261117-173420-s3-4.json): native metrics: n/a
- S3-4 (chrome-20261117-173420-s3-4.json): decoder info: n/a
- S3-4 (chrome-20261117-173420-s3-4.json): interrupted: the page was hidden 7 s into the run
- S3-9 (chrome-20261117-173217-s3-9.json): native metrics: n/a
- S3-9 (chrome-20261117-173217-s3-9.json): decoder info: n/a
- S3-9 (chrome-20261117-173217-s3-9.json): interrupted: the page was hidden 297 s into the run
- S3-9 (chrome-20261117-173228-s3-9.json): native metrics: n/a
- S3-9 (chrome-20261117-173228-s3-9.json): decoder info: n/a
- S3-9 (chrome-20261117-173228-s3-9.json): interrupted: the page was hidden 2 s into the run
- S3-9 (chrome-20261117-173410-s3-9.json): native metrics: n/a
- S3-9 (chrome-20261117-173410-s3-9.json): decoder info: n/a
- S3-9 (chrome-20261117-173410-s3-9.json): interrupted: the page was hidden 76 s into the run
- S3-9 (chrome-20261117-174353-s3-9.json): native metrics: n/a
- S3-9 (chrome-20261117-174353-s3-9.json): decoder info: n/a
- S3-9 (chrome-20261117-174353-s3-9.json): tile 3: STALLED 504 s into the grid window
- S3-9 (chrome-20261117-174353-s3-9.json): interrupted: the page was hidden 570 s into the run
- S3-9 (chrome-20261117-175017-s3-9.json): native metrics: n/a
- S3-9 (chrome-20261117-175017-s3-9.json): decoder info: n/a
- S3-9 (chrome-20261117-175017-s3-9.json): interrupted: the page was hidden 30 s into the run
- S3-9 (chrome-20261117-180225-s3-9.json): native metrics: n/a
- S3-9 (chrome-20261117-180225-s3-9.json): decoder info: n/a
- S3-16 (chrome-20261117-185707-s3-16.json): native metrics: n/a
- S3-16 (chrome-20261117-185707-s3-16.json): decoder info: n/a
- S3-16 (chrome-20261117-185707-s3-16.json): tile 0: STALLED 305 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 1: STALLED 305 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 2: STALLED 305 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 3: STALLED 306 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 4: STALLED 306 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 5: STALLED 306 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 6: STALLED 306 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 7: STALLED 306 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 8: STALLED 306 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 9: STALLED 306 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 11: STALLED 306 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 10: STALLED 306 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 12: STALLED 306 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 13: STALLED 306 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 14: STALLED 306 s into the grid window
- S3-16 (chrome-20261117-185707-s3-16.json): tile 15: STALLED 306 s into the grid window
- S4 (chrome-20261117-195933-s4.json): native metrics: n/a
- S9 (chrome-20261117-215023-s9.json): mx-h265/main: bound H265
- S9 (chrome-20261117-215023-s9.json): mx-h265: ok
- S9 (chrome-20261117-215023-s9.json): mx-high51/main: bound H264
- S9 (chrome-20261117-215023-s9.json): mx-high51: ok
- S10 (chrome-20261117-172425-s10.json): pair: host/prflx
- S10 (chrome-20261117-172425-s10.json): prompt: yes

### Web: Edge

**Dispozitiv:** Windows · Edg/158.0.0

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV1 | p90 12 ms (mediana 4 ms, max 28 ms; 20 din 20 porniri) | p90 ≤ 1,5 s (20 porniri) | Da |
| MV2 | p95 sub 99 ms (100 mostre); p95 principal 32 ms (100 mostre) | p95 < 1 s (100 mostre) | Da |
| MV3/9 | fps minim 12; pierdute max 0 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Da |
| MV13 | pereche host/prflx; cerere de permisiune: da | ambele capete host sau prflx, fără relay | Da |
| G9 | mx-h265: fallback; mx-high51: ok | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | Da |

**Note la praguri:**

- **MV1:** substream: H264
- **MV1:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 0, cadre aruncate 4
- **MV2:** RTT al ceasului: max 0 ms (eroarea offsetului e cel mult jumătate din el)
- **MV3/9:** ecran complet (flux principal, informativ): 19,95 fps, pierdute 0,4 %
- **MV3/9:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 11, cadre aruncate 18
- **MV13:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară
- **G9:** primul cadru: mx-h265 pe substream 10 ms, mx-high51 pe fluxul principal 17 ms

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| edge-20261124-113806-s1.json | S1 | 58 s | folosită | 0,5 ms |
| edge-20261124-113853-s2.json | S2 | 47 s | folosită | 3 ms |
| edge-20261124-115054-s3-9.json | S3-9 | 721 s | folosită | 17,2 ms |
| edge-20261124-115058-s9.json | S9 | 3 s | folosită | — |
| edge-20261124-113708-s10.json | S10 | 1 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S1 (edge-20261124-113806-s1.json): sub: H264
- S3-9 (edge-20261124-115054-s3-9.json): native metrics: n/a
- S3-9 (edge-20261124-115054-s3-9.json): decoder info: n/a
- S9 (edge-20261124-115058-s9.json): mx-h265/main: error CODEC_UNSUPPORTED
- S9 (edge-20261124-115058-s9.json): mx-h265/sub: bound H264
- S9 (edge-20261124-115058-s9.json): mx-h265: fallback
- S9 (edge-20261124-115058-s9.json): mx-high51/main: bound H264
- S9 (edge-20261124-115058-s9.json): mx-high51: ok
- S10 (edge-20261124-113708-s10.json): pair: host/prflx
- S10 (edge-20261124-113708-s10.json): prompt: yes

### Web: Firefox

**Dispozitiv:** Windows · Firefox/160.0

| Cod | Valoare | Prag | Trece |
| --- | --- | --- | --- |
| MV1 | p90 21 ms (mediana 8 ms, max 36 ms; 20 din 20 porniri) | p90 ≤ 1,5 s (20 porniri) | Da |
| MV2 | p95 sub 14 ms (100 mostre); principal: n/a, browserul nu oferă H.264 Main (în produs, retragere pe substream, §6.3 pasul 7) | p95 < 1 s (100 mostre) | Da |
| MV3/9 | fps minim 12; pierdute max 0 % | fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 % | Da |
| MV13 | pereche host/host; cerere de permisiune: da | ambele capete host sau prflx, fără relay | Da |
| G9 | mx-h265: fallback; mx-high51: unsupported-profile | fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed | Da |

**Note la praguri:**

- **MV1:** substream: H264, în profilul de rezervă 42e01f (R23: browserul nu oferă profilul sursei)
- **MV1:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 0, cadre aruncate 0
- **MV2:** RTT al ceasului: max 0 ms (eroarea offsetului e cel mult jumătate din el)
- **MV3/9:** jitter hub: 63,2 ms în rularea S3-9 (peste 20 ms): PC-ul hub-ului poate fi ocupat, deci cifrele clientului pot fi mai proaste decât în realitate
- **MV3/9:** ecran complet (flux principal, informativ): 0 fps, pierdute 0 %
- **MV3/9:** pierderi în rulare (informativ, fără prag; R50): RTP pierdute 0, NACK 0, PLI 9, cadre aruncate 0
- **MV13:** hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară
- **G9:** primul cadru: mx-h265 pe substream 9 ms, mx-high51 pe substream 21 ms
- **G9:** mx-h265/sub: H264, în profilul de rezervă 42e01f (R23: browserul nu oferă profilul sursei)
- **G9:** mx-high51/sub: H264, în profilul de rezervă 42e01f (R23: browserul nu oferă profilul sursei)

**Rulări:**

| Fișier | Scenariu | Durată | Stare | Jitter hub (max) |
| --- | --- | --- | --- | --- |
| firefox-20261124-112042-s1.json | S1 | 58 s | folosită | 0,5 ms |
| firefox-20261124-112107-s2.json | S2 | 25 s | folosită | 12,8 ms |
| firefox-20261124-113308-s3-9.json | S3-9 | 721 s | folosită | 63,2 ms, peste 20 ms |
| firefox-20261124-113311-s9.json | S9 | 3 s | folosită | — |
| firefox-20261124-111944-s10.json | S10 | 1 s | folosită | — |

**Note din rulări**, așa cum le-a scris aplicația:

- S1 (firefox-20261124-112042-s1.json): sub: H264, fallback profile 42e01f
- S2 (firefox-20261124-112107-s2.json): main: bind error CODEC_UNSUPPORTED
- S3-9 (firefox-20261124-113308-s3-9.json): native metrics: n/a
- S3-9 (firefox-20261124-113308-s3-9.json): decoder info: n/a
- S3-9 (firefox-20261124-113308-s3-9.json): tile 9: bind error CODEC_UNSUPPORTED
- S3-9 (firefox-20261124-113308-s3-9.json): main: NO_FIRST_FRAME 4 s into the fullscreen window
- S9 (firefox-20261124-113311-s9.json): mx-h265/main: error CODEC_UNSUPPORTED
- S9 (firefox-20261124-113311-s9.json): mx-h265/sub: bound H264, fallback profile 42e01f
- S9 (firefox-20261124-113311-s9.json): mx-h265: fallback
- S9 (firefox-20261124-113311-s9.json): mx-high51/main: error CODEC_UNSUPPORTED
- S9 (firefox-20261124-113311-s9.json): mx-high51/sub: bound H264, fallback profile 42e01f
- S9 (firefox-20261124-113311-s9.json): mx-high51: unsupported-profile
- S10 (firefox-20261124-111944-s10.json): pair: host/host
- S10 (firefox-20261124-111944-s10.json): prompt: yes

## Avertismente

- Jitter al hub-ului: rulările de mai jos au avut o fereastră de 10 s cu p99 al abaterii de trimitere de peste 20 ms. PC-ul hub-ului poate să fi fost ocupat (lucru în paralel, antivirus), deci cifrele clientului din aceste rulări pot fi mai proaste decât în realitate; repetați-le cu PC-ul liber.
  - `android-samsung-sm-a546b-20261124-163252-s4.json` (S4): jitter maxim 49,8 ms, peste 20 ms.
  - `android-samsung-sm-a546b-20261124-163951-s5.json` (S5): jitter maxim 38,3 ms, peste 20 ms.
  - `chrome-20261117-173217-s3-9.json` (S3-9): jitter maxim 62,6 ms, peste 20 ms.
  - `chrome-20261117-173410-s3-9.json` (S3-9): jitter maxim 47,9 ms, peste 20 ms.
  - `chrome-20261117-174353-s3-9.json` (S3-9): jitter maxim 881,8 ms, peste 20 ms.
  - `chrome-20261117-175017-s3-9.json` (S3-9): jitter maxim 55,9 ms, peste 20 ms.
  - `chrome-20261117-180225-s3-9.json` (S3-9): jitter maxim 177 ms, peste 20 ms.
  - `chrome-20261117-185707-s3-16.json` (S3-16): jitter maxim 342,5 ms, peste 20 ms.
  - `chrome-20261117-195933-s4.json` (S4): jitter maxim 1598 ms, peste 20 ms.
  - `firefox-20261124-113308-s3-9.json` (S3-9): jitter maxim 63,2 ms, peste 20 ms.
  - `windows-tauri-amd-ryzen-9-7950x-16-core-20261124-102206-s3-16.json` (S3-16): jitter maxim 40,3 ms, peste 20 ms.
  - `windows-tauri-amd-ryzen-9-7950x-16-core-20261124-105208-s4.json` (S4): jitter maxim 58 ms, peste 20 ms.
- Rulări invalide: 10 (întrerupte, eșuate sau la încărcător). Nu intră în praguri; motivul fiecăreia e la dispozitivul ei, sub «Rulări invalide», iar aplicația oferă «Repetă»: `android-samsung-sm-a546b-20261124-163951-s5.json`, `android-samsung-sm-a546b-20261124-165254-s6.json`, `android-samsung-sm-a546b-20261124-171225-s7.json`, `chrome-20261117-173236-s3-4.json`, `chrome-20261117-173420-s3-4.json`, `chrome-20261117-173217-s3-9.json`, `chrome-20261117-173228-s3-9.json`, `chrome-20261117-173410-s3-9.json`, `chrome-20261117-174353-s3-9.json`, `chrome-20261117-175017-s3-9.json`.

## Verdicte și decizii declanșate

| Poartă | Verdict sau rezultat |
| --- | --- |
| G5: Grila Android (AND-1) | GO provizoriu (fără iOS) |
| G6: Windows WebView2 (WIN-1) | GO |
| G7: Chrome LNA și mDNS (web) | GO |
| G9: Matricea de codecuri | AND-1: mx-h265: ok; mx-high51: ok · WIN-1: mx-h265: fallback; mx-high51: ok · Chrome: mx-h265: ok; mx-high51: ok · Edge: mx-h265: fallback; mx-high51: ok · Firefox: mx-h265: fallback; mx-high51: unsupported-profile |
| G19: Argon2id pe Android mediu (MV15) | trece: rulări max 328 ms (mediana 303 ms; 10 din 10) |

### G5: Grila Android (AND-1)

- **Verdict:** GO provizoriu (fără iOS)
- **Decizie declanșată:** Nicio decizie tehnică nouă. Shell-ul Android rămâne subțire (spike-ul și accesul pe LAN din Faza 1, T42), iar lucrul de interfață specific telefonului (pluginurile din §6.6 marcate cu Faza 4, ecrane sau gesturi doar pentru telefon) așteaptă verdictul iOS din Faza 0-bis.
- **Ce se schimbă în §2 sau în cerințe:** Nimic în §2 sau în cerințe.

### G6: Windows WebView2 (WIN-1)

- **Verdict:** GO
- **Decizie declanșată:** Nicio decizie: MV6 și MV7 trec cu 16 plăci, deci grila desktop rămâne la 16 plăci.
- **Ce se schimbă în §2 sau în cerințe:** Nimic în §2 sau în cerințe.

### G7: Chrome LNA și mDNS (web)

- **Verdict:** GO
- **Decizie declanșată:** Nicio decizie: perechile ICE din Chrome, Edge, Firefox și din cele două WebView-uri nu trec prin relay. MV12 în Chrome, cu origine publică, rămâne pentru Faza 0-bis.
- **Ce se schimbă în §2 sau în cerințe:** Nimic în §2 sau în cerințe.

### G9: Matricea de codecuri

- **Rezultat:** AND-1: mx-h265: ok; mx-high51: ok · WIN-1: mx-h265: fallback; mx-high51: ok · Chrome: mx-h265: ok; mx-high51: ok · Edge: mx-h265: fallback; mx-high51: ok · Firefox: mx-h265: fallback; mx-high51: unsupported-profile
- **Decizie declanșată:** Regula SDP și primul cadru în 4 s aleg corect pe toate căile client ale Fazei 0 (AND-1, WIN-1, Chrome, Edge, Firefox): H.265 cade pe substream cu eticheta „calitate redusă: H.265 nesuportat”, iar un profil H.264 nesuportat pe eticheta „profil video nesuportat”. Rezultatele pe cale se notează.
- **Ce se schimbă în §2 sau în cerințe:** §2.2: Decizie tehnică cu rezultatele G9 de mai sus (căile iOS rămân pentru Faza 0-bis).

### G19: Argon2id pe Android mediu (MV15)

- **Rezultat:** trece: rulări max 328 ms (mediana 303 ms; 10 din 10)
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
- `phase0-data/phase0b-chrome.csv`
- `phase0-data/phase0b-edge.csv`
- `phase0-data/phase0b-firefox.csv`
- `phase0-data/phase0b-windows-tauri.csv`

