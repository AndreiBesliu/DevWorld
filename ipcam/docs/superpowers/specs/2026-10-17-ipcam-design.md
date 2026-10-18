# Specificație de design: ipcam — aplicație pentru camere IP

## §0 Antet și rezumat pentru proprietar

| Câmp | Valoare |
|---|---|
| Data | 2026-10-17; revizuit 2026-10-18 (răspunsurile proprietarului: O12–O16, §2.1) |
| Stare | Draft pentru review |
| Cerințe confirmate | [docs/brief.md](../../brief.md) (confirmat de proprietar pe 2026-10-17) |
| Ordinea autorității | jurnalul aprobat al conversației cu proprietarul (transcris în §2.1; la diferențe, jurnalul câștigă) > `docs/research/2026-10-17-research-critique.md` > `docs/research/2026-10-17-technical-landscape.md` (recomandarea Flutter din acesta e înlocuită de A3) |
| Nume și cititori | `ipcam` e nume de lucru (numele final îl alege proprietarul, §19); cititori: proprietarul (non-dezvoltator) și agenții AI care implementează și întrețin produsul |
| Cum se citește | Pentru agenții AI: fiecare cerință are un ID stabil (ex. `HUB-03`, `CTR-07`) și se poate verifica printr-un test. Prefixele: OBJ, NOB, SUC (§1); ARH (§3); HUB (§4); CLD (§5); APP (§6); CTR (§7); FLW (§8); STO (§9); ENC (§10); SEC (§11); LEG (§12); REL (§13); TST (§14); DEL (§15); PH0–PH4 (PH0 acoperă și blocul amânat Faza 0-bis), porțile G și măsurătorile MV/MI din Faza 0 (§16); RSK (§17); OWN (§18); OPN (§19); deciziile tehnice T (§2.2). Unde jurnalul lasă o alegere deschisă, AI-ul a decis și a marcat „Decizie tehnică” cu justificarea într-un rând (deciziile transversale sunt listate în §2.2). Ce depinde de o măsurătoare din Faza 0 e scris ca **poartă**: criteriu de trecere, criteriu de picare și planul B. |

### 0.1 Rezumat pentru proprietar

1. Construim o aplicație cu care un om își vede, înregistrează și controlează camerele IP de pe telefon, calculator sau browser.
2. Un program numit **hub** rulează pe un calculator care stă pornit la utilizator (PC cu Windows, NAS, Raspberry Pi, mini PC). Doar hub-ul vorbește cu camerele.
3. Hub-ul înregistrează non-stop sau doar la mișcare și urcă înregistrările unde alege utilizatorul: disc local, NAS, Google Drive, OneDrive, S3, WebDAV, SFTP.
4. Dacă pică internetul, hub-ul păstrează totul pe disc și urcă mai târziu, fără goluri, cât timp discul local are loc; aplicația vă anunță cu cel puțin o zi înainte să se umple.
5. Serverele noastre (**cloud-ul**) sunt mici și ieftine: conturi, legarea hub-ului de cont, „centrala” care pune telefonul în legătură cu hub-ul. Video-ul și parolele camerelor nu trec prin ele.
6. Imaginea merge direct de la hub la telefon, criptată. Când rețeaua nu permite legătura directă (10–25 % din cazuri), trece printr-un releu care vede doar date criptate.
7. Aplicațiile pentru web, Android, iOS și Windows au un singur cod (TypeScript). Pe Windows, același installer pune și hub-ul.
8. Criptarea înregistrărilor e alegerea utilizatorului. Dacă o alege, cheia are patru copii de rezervă (hub, fiecare dispozitiv, contul, o cheie tipărită). Un telefon nou intră în cont și își vede hub-urile dacă știți parola contului; conturile Google, Apple sau doar cu passkey au nevoie pentru asta de o „parolă de rezervă pentru acces”, aceeași cu „parola pentru înregistrări” de la criptare (propunere, OPN-13). Dacă pierdeți singurul telefon și nu ați setat parola de rezervă, un telefon nou poate fi aprobat doar de pe alt dispozitiv de încredere sau cu codul de pe hub (acces fizic la hub).
9. Totul e construit și întreținut de agenți AI: două limbaje principale (Go și TypeScript), o descriere unică a mesajelor dintre părți și teste automate pentru tot, inclusiv video care rulează cu adevărat.
10. Primul pas e **Faza 0**, care **nu costă nimic și nu cere iPhone** (decizia dvs. din 2026-10-18). Are trei părți: detectorul de camere, pe care îl rulați o dată; testul video pe Samsung Galaxy A24-ul dvs., în Chrome/Edge și în aplicația de Windows de pe PC-ul dvs.; testul de înregistrare + upload pe PC și pe Raspberry Pi-ul dvs., cu pană de internet și cu 5 tăieri de curent făcute de dvs. pe fiecare dispozitiv (scoateți ștecherul când vă spune programul). În plus, programul repornește singur PC-ul și Pi-ul de mai multe ori, într-un interval ales de dvs., în care nu le folosiți. Faza 0 are 1–2 săptămâni de măsurători, care încep după pașii gratuiți din §18. Detectorul (Faza 0 a) poate porni imediat.
11. Tot ce ține de iOS sau costă bani, oricât de puțin (domeniu, server, conturile Apple și Google Play, semnarea codului, cheile YubiKey, avocat, asigurare etc.), e mutat în **Faza 0-bis** (§16.9). Ea pornește doar după ce aprobați un buget (până atunci, 0 €) și se face înaintea fazei care o cere. Testul pe iPhone (ajunge unul împrumutat) se face cel târziu înainte de Faza 4.
12. **Riscul amânării iOS:** dacă iPhone-ul arată târziu că varianta aleasă nu merge, aplicațiile de telefon trec pe Flutter (T29); web-ul și Windows nu sunt afectate. Ca pierderea să fie mică, păstrăm codul video și de conexiune independent de interfață și facem testul iOS cât de repede permite bugetul, înainte de orice lucru de interfață specific telefonului (RSK-27).
13. **Raspberry Pi:** hub-ul merge pe orice Pi care rulează Linux pe 64 sau 32 de biți (Pi 2, 3, 3B+, 4, 5, Zero 2 W, Pi 400/500, Compute Module 3/4/5). Aplicația arată câte camere recomandăm pentru modelul dvs. (Pi 5 ≈ 8, Pi 4 ≈ 4, celelalte ≈ 1–2) și doar avertizează dacă adăugați mai multe. Pi 1, Zero și Zero W nu sunt suportate (un singur nucleu și 512 MB), iar programul explică de ce. Se poate înregistra și pe card SD, după un avertisment pe care îl confirmați; hub-ul supraveghează cardul și vă anunță dacă dă erori, dar recomandăm un SSD.
14. Urmează Faza 1 (hub + aplicația în rețeaua locală, din aplicația de Windows și din cea de Android; web-ul pe LAN vine odată cu domeniul, în Faza 2), Faza 2 (cont și acces de oriunde), Faza 3 (Drive, OneDrive, S3, reguli), Faza 4 (aplicații în magazine). Nu promitem date; fiecare fază e împărțită în pași mici verificați automat.
15. Drive-ul dvs. personal poate fi folosit la testele manuale: aplicația vede doar fișierele create de ea. Testele automate folosesc conturi de test separate.
16. **Acum** aveți de făcut doar lucruri gratuite (§18, coloana „Acum (gratuit) / Amânat”): răspunsurile din §19, proiectul Google Cloud în modul Testing pe contul dvs. personal, înregistrarea aplicației Microsoft (doar dacă nu cere card), conturile de test, rularea detectorului și tăierile de curent. Folosim doar dispozitivele dvs.: PC-ul cu Windows 11, Galaxy A24, Raspberry Pi-ul și internetul DIGI; nu cumpărăm nimic. **Mai târziu** vin SRL-ul, conturile de dezvoltator plătite, domeniul, serverul, semnarea codului și avocatul. Repo-ul rămâne public; ce publicăm din testele dvs. (fixture-uri, rapoarte) e redactat și aprobat de dvs. (PH0-22). Ce e încă deschis (nume, monetizare, mărcile camerelor, bugetul și momentul Fazei 0-bis) e în §19. Rândurile din §18 marcate „nou — propus de AI” nu sunt în lista aprobată în conversație și au nevoie de OK-ul dvs.
17. Pe termen lung vă rămân: contactul de securitate CRA (răspuns în 24 h), păstrarea cheilor YubiKey și ceremonia anuală, semnarea cu YubiKey a release-urilor stable dacă build-ul nu e reproductibil (o dată la 2–4 săptămâni, DEL-03), aprobarea rapoartelor de fază și a fixture-urilor, un exercițiu de recuperare și emailurile de incident SEV1; restul îl fac agenții.

### 0.2 Cuprins

Secțiunile și subsecțiunile, în ordinea din document. Trimiterile din text au forma „§9.4”.

- **§0 Antet și rezumat pentru proprietar** — 0.1 Rezumat pentru proprietar · 0.2 Cuprins
- **§1 Context, obiective, non-obiective, criterii de succes** — 1.1 Obiective · 1.2 Non-obiective · 1.3 Criterii de succes
- **§2 Jurnalul deciziilor** — 2.1 Decizii aprobate de proprietar · 2.2 Decizii tehnice luate în această specificație
- **§3 Arhitectura sistemului** — 3.1 Diagramă · 3.2 Componente · 3.3 Harta datelor · 3.4 Stack și licențe · 3.5 Structura folderului `ipcam/` · 3.6 Cerințe de arhitectură
- **§4 Hub-ul** — 4.1 Principii · 4.2 Procese și integrarea go2rtc / MediaMTX · 4.3 Module · 4.4 Interfețe Go (doar granițele) · 4.5 Comportamentul înregistrării · 4.6 Catalogul SQLite (schiță) · 4.7 Platforme și rulare · 4.8 Niveluri hardware și bugete · 4.9 Structura pe disc · 4.10 Cerințe
- **§5 Cloud-ul** — 5.1 Arhitectură și module · 5.2 Conturi și autentificare (P4) · 5.3 Registrul hub-urilor și împerecherea (F1) · 5.4 Signaling și prezență · 5.5 TURN · 5.6 OAuth pentru furnizorii de stocare · 5.7 Backup de chei, drepturi, certificate LAN, telemetrie · 5.8 Modelul de date PostgreSQL (schiță) · 5.9 Suprafața API, găzduire, scalare · 5.10 Cerințe
- **§6 Aplicațiile client** — 6.1 Structura `apps/` și stack-ul · 6.2 `@ipcam/player` · 6.3 Sesiunea și conectivitatea (`@ipcam/core`) · 6.4 Ecrane MVP · 6.5 Grila și limitele de decodare · 6.6 Pluginuri native (mici, în repo, cu teste) · 6.7 Cerințe de platformă · 6.8 Limbă și accesibilitate · 6.9 Cerințe
- **§7 Contractul (Protobuf) și API-urile** — 7.1 Principii · 7.2 Pachete · 7.3 Versionare și compatibilitate · 7.4 Generarea codului · 7.5 Transporturi · 7.6 RPC pe canalul de date (framing) · 7.7 Modelul de erori · 7.8 API-ul local al hub-ului (`ipcam.hub.v1`) · 7.9 API-ul cloud (`ipcam.cloud.v1`) · 7.10 Canalul de control hub↔cloud (`ipcam.link.v1`)
- **§8 Fluxuri** — 8.1 F1 — Instalare și împerechere · 8.2 F2 — Descoperire și adăugare camere · 8.3 F3 — Live și PTZ, pe LAN și de la distanță · 8.4 F4 — Înregistrare · 8.5 F5 — Redare, timeline, miniaturi, export MP4 · 8.6 F6 — Redare din cloud fără hub (după furt sau distrugere) · 8.7 F7 — Conectarea fiecărei destinații din MVP
- **§9 Stocare și reguli** — 9.1 Interfața de driver (`hub/internal/storage`) · 9.2 Note per furnizor (din cercetare) · 9.3 Modelul de date al regulilor · 9.4 Evaluarea și rezolvarea conflictelor · 9.5 Șabloane · 9.6 Validare și previzualizare · 9.7 Ciclul de viață al unui segment · 9.8 Retenție și cote · 9.9 Comportament la căderi · 9.10 Structura folderelor pe destinație · 9.11 Catalogul pe destinații (pentru F6)
- **§10 Criptare și chei** — 10.1 Cine alege și cum · 10.2 Formatul fișierelor · 10.3 Ierarhia cheilor · 10.4 Cele patru căi de recuperare (E7) · 10.5 Dispozitive: adăugare, revocare, rotație · 10.6 Ce rămâne în clar și de ce · 10.7 Regula de criptare a catalogului (T15) · 10.8 Limitări și avertismente pentru utilizator
- **§11 Securitate** — 11.1 Bunuri protejate și modelul de amenințări · 11.2 Ce garantează „zero-knowledge” și ce nu (P3) · 11.3 Împerechere, fixarea cheii, lista de încredere, roluri · 11.4 Custodia cheilor de semnare a update-urilor · 11.5 HTTPS pe LAN cu certificat per hub (P9) · 11.6 Conturi, acces intern, blocarea aplicației, notificări · 11.7 Jurnalul de audit (P5) · 11.8 Hub: întărire, credențiale sigilate, criptarea discului (P1, P9) · 11.9 Intrări ostile și fuzzing (P7) · 11.10 Lanțul de aprovizionare · 11.11 Cerințe
- **§12 Confidențialitate și conformitate legală** — 12.1 Roluri GDPR · 12.2 Datele pe care le prelucrăm ca operator · 12.3 Funcții de confidențialitate în produs (P10) · 12.4 Specific românesc ⚖ · 12.5 Cyber Resilience Act și răspunderea pentru produs · 12.6 Magazine de aplicații (checklist la fiecare release, LEG-12) · 12.7 Lista pentru avocatul român (P12) · 12.8 Cerințe
- **§13 Fiabilitate și operare** — 13.1 Modelul de sănătate (R1) · 13.2 Watchdog și reconectare · 13.3 Alerte în aplicație (R1) · 13.4 Update automat (P6) · 13.5 Backup și migrare (P9) · 13.6 Pachetul de diagnostic și telemetria (R2) · 13.7 Specific Windows · 13.8 Disc plin și ceas · 13.9 Operarea cloud-ului · 13.10 Cerințe
- **§14 Strategia de testare** — 14.1 Piramida · 14.2 Laboratorul `e2e/` · 14.3 Camere simulate și simulatorul ONVIF · 14.4 Record-replay (R4) · 14.5 Stocare: furnizori falși, furnizori reali, conturi de test · 14.6 Scenarii obligatorii · 14.7 Matricea CI și device farm · 14.8 Criterii de merge pentru PR-urile agenților · 14.9 Cerințe
- **§15 Livrare și infrastructură** — 15.1 Artefacte · 15.2 Lanțul de release · 15.3 Canale de release · 15.4 Workflow-uri CI/CD (`.github/workflows/ipcam-*.yml`) · 15.5 Infrastructura cloud și TURN · 15.6 Modelul de cost · 15.7 Cerințe
- **§16 Faze, MVP și Faza 0** — 16.1 Fazele pe scurt · 16.2 Faza 0 (a) — Detectorul de camere (primul livrabil) · 16.3 Faza 0 (b) — Spike-ul video Android, web și Windows (decizia de stack A3) · 16.4 Faza 0 (c) — Spike-ul de înregistrare și upload · 16.5 Porțile · 16.6 Fazele 1–4 · 16.7 MVP: ce intră și ce nu · 16.8 Backlog după MVP (ordine propusă; o decide proprietarul) · 16.9 Faza 0-bis — amânată (iOS sau costuri)
- **§17 Riscuri**
- **§18 Ce are de făcut proprietarul**
- **§19 Întrebări deschise**
- **Anexa A Glosar**
- **Anexa B Surse**

## §1 Context, obiective, non-obiective, criterii de succes

**Context.** Proprietarul vrea un produs comercial, public, pentru camere IP standard (RTSP/ONVIF), cu aplicații pe web, Android, iOS și Windows și cu înregistrările păstrate la utilizator (local sau în propriul cloud: Drive, OneDrive etc.). Cercetarea arată un gol pe piață: Frigate nu are aplicații native, suport Windows și stocare în Drive/OneDrive; Agent DVR cere abonament pentru acces remote și aplicații; nimeni nu oferă un hub multi-marcă prietenos, cu aplicații pe toate platformele și stocare în Drive + OneDrive (briefs: înregistrare). Produsul e dezvoltat și întreținut **doar de agenți AI** (O6), deci arhitectura e optimizată pentru verificare automată: puține limbaje, tipuri generate dintr-un contract unic, laborator automat cu camere simulate.

### 1.1 Obiective

| ID | Obiectiv | Decizii | Cum se verifică |
|---|---|---|---|
| OBJ-01 | Un utilizator leagă mai multe camere RTSP/ONVIF la un hub și le vede live pe web, Android, iOS și Windows | O2, O4, O8.1 | E2E în laborator (§14.6) + spike-ul mobil: Android în Faza 0, iOS în Faza 0-bis (§16) |
| OBJ-02 | Hub-ul înregistrează per cameră: continuu, doar la mișcare sau deloc | O5, O8.3, F4 | E2E înregistrare + test de pană de curent |
| OBJ-03 | Utilizatorul controlează PTZ, presetări, pornire/oprire înregistrare | O8.2, F3 | E2E cu simulatorul ONVIF (PTZ round-trip) |
| OBJ-04 | Înregistrările ajung pe destinațiile alese, după reguli configurabile, fără goluri la căderi de internet | O3, S1–S5 | E2E cu furnizori falși + poarta de upload din Faza 0 |
| OBJ-05 | Acces de oriunde pe conexiune criptată, direct pe LAN | O8.5, A7, P2 | E2E LAN/remote/TURN în laborator |
| OBJ-06 | Serverele noastre nu văd niciodată video sau parole de camere | P1, A1 | ARH-03, ARH-08 |
| OBJ-07 | Criptare la alegerea utilizatorului, format deschis, patru căi de recuperare | E1–E8 | teste §10 + recuperare cu unealta `age` standard |
| OBJ-08 | Sistem construibil și întreținut de AI: Go + TypeScript, contract Protobuf unic, teste automate pentru tot | O6, A4, R3 | CI (§14, §15) |
| OBJ-09 | Abonamentele se pot adăuga fără rescriere: modelul de drepturi există din prima zi, MVP gratuit | O7 | EntitlementService (§7) răspunde cu planul „free” |
| OBJ-10 | Conformitate GDPR, CRA, magazine de aplicații | P10–P12 | §12, checklist de release (§12.6, LEG-12) |

### 1.2 Non-obiective

| ID | Ce NU face MVP-ul | Când |
|---|---|---|
| NOB-01 | Camere doar-cloud (Xiaomi, Ring, Arlo, Eufy etc.) | în afara MVP; nicio decizie pentru mai târziu (O8.1) |
| NOB-02 | Telefoanele nu înregistrează 24/7; hub pe Android (telefon vechi, TV box) | hub Android după MVP |
| NOB-03 | Notificări push la mișcare; partajare cu familia/angajații (modelul de roluri există, interfața nu) | versiunea următoare |
| NOB-04 | Detecție de mișcare pe hub (pixel-diff) | v1.1 |
| NOB-05 | Detecție AI persoane/vehicule; Dropbox, Box, pCloud, rclone; conversia înregistrărilor vechi la schimbarea criptării; setări avansate de imagine; audio bidirecțional | după MVP |
| NOB-06 | Recunoaștere facială (GDPR Art. 9, AI Act) | niciodată |
| NOB-07 | Facturare, abonamente, plăți | după decizia de monetizare |
| NOB-08 | Port forwarding, UPnP, tuneluri inverse, VPN mesh; video prin serverele noastre în operare normală (TURN e doar releu criptat de rezervă) | niciodată |
| NOB-09 | Hardware vândut de noi (hub-ul rulează pe dispozitivul utilizatorului) | nu e planificat |

### 1.3 Criterii de succes

| ID | Criteriu (O9) | Cum se măsoară | Prag de trecere |
|---|---|---|---|
| SUC-01 | Instalare → primul live | Test de utilizabilitate cu câte ≥ 5 persoane non-tehnice pe Windows și pe Docker/NAS, cu camere din laborator care au RTSP/ONVIF activ sau pași ghidați pentru marca lor; cronometru de la pornirea installer-ului / comenzii Docker până la primul cadru afișat; pe Windows, cu avertismentul SmartScreen inclus dacă apare (PH4-03) | pe fiecare platformă: ≥ 4 din 5 participanți sub 10 minute, fără ajutor, niciunul peste 15 minute |
| SUC-02 | Latență live pe LAN | Glass-to-glass: cameră simulată cu ora arsă în imagine vs. ora afișării (`requestVideoFrameCallback`) în laborator; pe dispozitive reale, cronometru filmat | p95 < 1 s |
| SUC-03 | Latență live remote | Același test prin rețea cu 4G românesc și prin TURN-TLS/443 | p95 < 3 s |
| SUC-04 | Fără goluri la căderi de internet | Pană simulată de 6 h, apoi revenire; Drive și OneDrive | 0 segmente lipsă, 0 duplicate, hash verificat pentru fiecare obiect, cât timp discul local a avut loc |
| SUC-05 | Pierdere la pană de curent (F4) | Opriri brutale automate, cel puțin 40 per sistem de operare (kill -9 și reset fără scrierea cache-ului, în laborator și pe dispozitivele proprietarului), plus 5 tăieri reale de curent făcute de mână de proprietar pe PC-ul lui cu Windows 11 și 5 pe Raspberry Pi-ul lui, în Faza 0 (MI1, T43) | pe fiecare set de rulări, inclusiv pe fiecare set de 5 tăieri reale: median ≤ 1 s, maxim ≤ 2 s pierdut; catalogul se reconciliază singur. Excepție: la fluxurile criptate local sub 512 kbps, maximul e max(1 parte, 64 KiB ÷ bitrate), iar aplicația spune asta (§4.5, HUB-07) |
| SUC-06 | Nimic GPL/AGPL livrat | Verificarea licențelor pe SBOM-ul fiecărui release | 0 componente interzise |
| SUC-07 | Fiecare schimbare e verificată automat | Fiecare PR trece unit + E2E laborator + licențe; după aprobarea OWN-35, GitHub refuză merge-ul fără `ipcam-gate` verde (§14.8) | 100 % din PR-urile îmbinate |

## §2 Jurnalul deciziilor

Surse: **conv.** = conversația cu proprietarul din 2026-10-17 (jurnal aprobat); **conv. 2026-10-18** = răspunsurile proprietarului din 2026-10-18 (decizii aprobate O12–O16); **land** = `docs/research/2026-10-17-technical-landscape.md`; **crit** = `docs/research/2026-10-17-research-critique.md`; **panel** = `docs/research/2026-10-17-client-stack-judge-panel.md`; **briefs** = `docs/research/2026-10-17-research-briefs.json`.

### 2.1 Decizii aprobate de proprietar

| ID | Decizie | Motiv | Sursă/dată |
|---|---|---|---|
| O1 | Produs comercial, public: App Store, Google Play, web, Windows | Decizia proprietarului | conv. 2026-10-17; brief |
| O2 | Un utilizator leagă mai multe camere IP: le vede live, le înregistrează, le controlează | Nucleul produsului | conv.; brief |
| O3 | Înregistrările merg unde alege utilizatorul: Google Drive, OneDrive, alți furnizori, local | Video-ul rămâne la utilizator | conv.; brief |
| O4 | Clienți pe web, Android, iOS, Windows | Acoperirea pieței | conv.; brief |
| O5 | 24/7 doar pe un hub, pe un dispozitiv pe care utilizatorul îl are (PC Windows, NAS/Docker, Raspberry Pi, mini PC); hub Android mai târziu; telefoanele nu înregistrează 24/7 | iOS suspendă aplicațiile în fundal; Android 15+ limitează serviciile la 6 h/24 h | conv.; land §1.3; crit #7 |
| O6 | Dezvoltare și întreținere doar de agenți AI; stack pentru fiabilitate, tipare stricte, puține limbaje, teste + CI pentru tot, depanare fără om | Nu există dezvoltator uman | conv. |
| O7 | Monetizarea se decide mai târziu; MVP gratuit; model de drepturi (entitlements) pregătit, fără facturare în MVP | Abonamente fără rescriere | conv. |
| O8.1 | Camere standard RTSP/ONVIF (Hikvision, Dahua, Reolink, Uniview, Tapo cu RTSP…); camerele doar-cloud nu intră în MVP | Acestea nu expun RTSP/ONVIF | conv.; land K4 |
| O8.2 | Control în MVP: PTZ (mișcare, zoom), presetări, pornire/oprire înregistrare; setări avansate de imagine mai târziu | Scopul MVP | conv. |
| O8.3 | Per cameră: continuu / doar la mișcare (pre-roll 5–10 s, post-roll 15–20 s) / oprit; evenimente ONVIF în MVP; pixel-diff pe hub în v1.1; AI mai târziu; segmente de câteva minute; retenție configurabilă | Cea mai ieftină sursă de mișcare în MVP | conv.; briefs: înregistrare |
| O8.4 | Lista de stocare MVP, extinsă ulterior în S4 | — | conv. |
| O8.5 | Acces de oriunde pe conexiune criptată; direct pe LAN | — | conv. |
| O8.6 | Hub în MVP: Windows + Docker/Linux (x64, arm64); hub Android mai târziu | — | conv.; **modificat de O16** (se adaugă linux/arm/v7) |
| O8.7 | Piața: România + UE; interfață română + engleză; GDPR | — | conv. |
| O8.8 | MVP: un proprietar per hub; partajare și push la mișcare în versiunea următoare; modelul de roluri se proiectează acum | Fără rescriere ulterioară | conv. |
| O9 | Succes: instalare → live < 10 min; latență < 1 s LAN, < 3 s remote; înregistrări în Drive/OneDrive fără goluri | Măsura reușitei (§1.3) | conv.; brief |
| O10 | Proprietarul are camere IP de diverse modele pentru teste (mărcile le spune detectorul) | Laborator real | conv. |
| O11 | Drive-ul personal al proprietarului poate fi folosit la teste manuale/spike; aplicația are propriul client OAuth (proiect Google Cloud creat de proprietar); CI folosește conturi de test dedicate Google + Microsoft, cu refresh token în secretele CI | `drive.file`: aplicația vede doar fișierele ei; conectorul Drive al chat-ului nu e autorizarea aplicației | conv. (mesaj în timpul scrierii spec-ului); **modificat de O13** (în Faza 0, proiectul e în modul Testing, pe contul personal; Production vine odată cu domeniul) |
| O12 | Repo-ul DevWorld rămâne **public** deocamdată. Regula de redactare: fixture-urile, rapoartele și datele din Faza 0 și Faza 0-bis ajung în repo doar redactate după §16.2.3, verificate automat și aprobate de proprietar în PR (PH0-22) | Decizia proprietarului; CI gratuit pe repo public (OWN-18) | conv. 2026-10-18 (închide OPN-06) |
| O13 | **Faza 0 la cost zero și fără iOS.** Citat: „Lasă tot ce ține de iOS sau ce presupune costuri pentru mai târziu.” Tot ce ține de iOS și tot ce costă bani, inclusiv sume mici (domeniu, VPS, taxele Apple și Play, device farm, semnarea codului, YubiKey, KMS, VM-uri, Entra P1, priza inteligentă, telefoane de test, N100, avocat, asigurare, pentest, marcă), se amână până când proprietarul aprobă un buget. Elementele amânate rulează în blocul „Faza 0-bis” (§16.9), înaintea fazei care le cere; testul iOS rulează cel târziu înainte de începutul Fazei 4. Bugetul e 0 € până decide proprietarul (OPN-09) | Decizia proprietarului | conv. 2026-10-18 |
| O14 | Dispozitivele proprietarului: un PC cu Windows 11, un Raspberry Pi (modelul îl citește hub-ul, iar detectorul îl semnalează), un Samsung Galaxy A24 și internet acasă de la DIGI. Nu are iPhone; unul se poate împrumuta mai târziu. Faza 0 folosește doar aceste dispozitive, fără cumpărări | Matricea de test din §16.3 și §16.4 | conv. 2026-10-18 (închide OPN-07) |
| O15 | SRL-ul se înființează mai târziu. Întâi se face munca ce nu cere firmă (Fazele 0–2; în Faza 3, doar verificarea publisher-ului Microsoft cere firmă); conturile și verificările care cer firmă (D-U-N-S, Apple și Play ca organizație, publisher Microsoft, varianta de semnare Windows pentru organizații) așteaptă SRL-ul | Decizia proprietarului | conv. 2026-10-18 (închide OPN-05: „mai târziu”) |
| O16 | **Raspberry Pi.** Proprietarul vrea hub-ul pe toate modelele. Traducerea aprobată: suportat = orice Pi care rulează Linux arm64 sau armv7 (Pi 2 v1.2, Pi 3, 3B+, 4, 5, Zero 2 W, Pi 400/500, CM3/4/5), cu un număr **recomandat** de camere per model, afișat de aplicație (estimări, confirmate de HUB-32 pe Pi-ul proprietarului): Pi 5 ≈ 8, Pi 4 ≈ 4, Pi 3/3B+/Zero 2 W/Pi 2 v1.2 ≈ 1–2; peste el apare un avertisment, niciodată un refuz. Nesuportate: plăcile armv6 (Pi 1, Zero, Zero W), fiindcă un nucleu și 512 MB nu pot înregistra și transmite fiabil; aplicația explică asta. Înregistrarea pe card SD e **permisă**, cu avertisment clar despre uzură, confirmare explicită, recomandarea unui SSD și supravegherea cardului (erori SD și I/O) cu alerte; înlocuiește refuzul anterior. Se adaugă o imagine Docker linux/arm/v7 lângă amd64 și arm64 | Hub pe orice Pi, fără promisiuni false de capacitate | conv. 2026-10-18 (traducere explicată proprietarului de AI-ul principal) |
| A1 | Trei părți: hub (Go, local), cloud subțire (Go + PostgreSQL, UE), clienți (un cod TS); video P2P WebRTC hub↔client, TURN doar ca rezervă criptată (10–25 %); cloud = conturi, împerechere, signaling, prezență, credențiale TURN (Cloudflare, apoi coturn/pion-turn în UE), push (mai târziu), broker OAuth unde e nevoie, bloburi criptate de backup chei | Cost mic, confidențialitate, merge prin NAT | conv.; land §2.1, §2.4 |
| A2 | Hub din componente permisive, după interfețe proprii: go2rtc (MIT, fork vendored), MediaMTX (MIT) sau recorder propriu gortsplib/mediacommon, kerberos-io/onvif (MIT), Pion (MIT), drivere proprii, rclone (MIT) mai târziu; zero GPL/AGPL; allowlist de licențe în CI (MIT/BSD/Apache/ISC/MPL/LGPL dinamic) | Maturitate, licențe comerciale; go2rtc fără release din ian. 2026 | conv.; crit #1–2 |
| A3 | Clienți = opțiunea B „web-first TypeScript”: SPA React (și PWA) în Capacitor (Android, iOS) și Tauri/WebView2 (Windows; installer-ul pune și hub-ul); video prin WebRTC/MSE nativ, player adaptat din video-rtc.js (MIT); HLS(fMP4) pentru înregistrări/iOS; insule native subțiri; plan B: mobil în Flutter dacă pică spike-ul din Faza 0 | Mentenanță AI B=8 (A=5, C=3); UX C=8 (A=6,5, B=6); risc B=7,5 (A=5, C=3); codul testat ≈ codul livrat | conv.; panel; **modificat de O13** (verdictul GO/NO-GO se împarte: Android, web și Windows în Faza 0; iOS în Faza 0-bis, cel târziu înainte de Faza 4) |
| A4 | Limbaje: Go + TypeScript (+ Swift/Kotlin/Rust subțiri); contract Protobuf → tipuri Go + TS; TS strict + noUncheckedIndexedAccess; validare la runtime la granițe | Puține limbaje, tipare stricte | conv.; panel |
| A5 | Structura `ipcam/`: hub/, cloud/, apps/, contract/, e2e/, tools/, docs/ | Un loc clar per componentă | conv. |
| A6 | Substream H.264 = baza (~640×360, 10–15 fps, keyframe = fps, fără smart codec/B-frames, setat cu permisiune); fluxul principal înregistrat ca atare; H.265 doar la clienții care îl oferă în SDP și decodează primul cadru; grilă pe substream (telefon: 4 per pagină, max 9), ecran complet pe fluxul principal | H.264 e singurul codec redat peste tot | conv.; land §1.1; crit C6 |
| A7 | WebRTC peste tot; un PeerConnection per sesiune de vizualizare cu toate track-urile + canal de date fiabil pentru RPC; hub-ul ține un WebSocket de ieșire spre cloud; fără port forwarding, UPnP, tuneluri inverse, VPN mesh | Un singur transport; mai puține alocări TURN | conv.; crit lipsă #10 |
| F1 | Instalare: installer Windows sau o comandă Docker; hub-ul arată QR; aplicația: cont → Adaugă hub → QR (sau cod în stil RFC 8628, limitat); cheia hub-ului fixată la împerechere | Cloud-ul nu poate impersona hub-ul (aplicații native; vezi P3) | conv. |
| F2 | Adăugare camere: scanare LAN, marcă/model, manual IP/RTSP; parola camerei doar pe hub, sigilată; pași per marcă dacă ONVIF/RTSP e oprit; verificări automate (ceas, fluxuri, codecuri, PTZ real); substream H.264 cu permisiune; avertismente de securitate; utilizator ONVIF cu privilegii mici | Compatibilitate reală pe mărci | conv.; land K1–K3 |
| F3 | Live și PTZ: direct pe LAN, remote prin WebRTC (cloud doar signaling); grilă pe substream, atingere → fluxul principal; doar controalele PTZ reale | — | conv. |
| F4 | Înregistrare: continuu / mișcare / oprit; pierdere la pană de curent ≈ 1 s (părți fMP4 + fsync); retenție per cameră (zile sau GB); curățare de urgență cea mai veche întâi, fără a șterge cel mai nou sau neurcat fără politică | Fără pierderi | conv.; crit C12 |
| F5 | Redare: timeline per cameră cu marcaje și miniaturi; derulare; export clip MP4 în MVP; din cea mai rapidă locație: local → NAS → cloud | — | conv. |
| F6 | Redare doar din cloud când hub-ul e furat/distrus: hub-ul urcă și catalogul (criptat când se folosește criptarea) la fiecare destinație | Cazul spargerii | conv. |
| S1 | Destinații nelimitate + reguli: camere × tip conținut (24/7, evenimente, miniaturi, exporturi) × calitate × program × una sau mai multe destinații; per destinație în regulă: retenție, criptare, politică la plin; upload: limită de bandă, ferestre orare, copiere vs mutare | „Configurabil până la cel mai mic detaliu” | conv. |
| S2 | Șabloane pentru non-tehnici (ex. „Simplu: 24/7 local, evenimente în cloud”) + editor avansat; șabloanele se pot edita | — | conv. |
| S3 | Discul local e mereu spool; outbox persistent, upload reluabil, verificare prin mărime + hash furnizor; nimic pierdut la căderi cât timp e loc local; avertismente de cotă | Criteriul „fără goluri” | conv. |
| S4 | Furnizori MVP: disc local, NAS (SMB), Google Drive (doar `drive.file`), OneDrive (personal + business), S3-compatibil, WebDAV/Nextcloud, SFTP; mai târziu Dropbox, Box, pCloud, rclone; o interfață de driver cu capabilități | `drive.file` evită evaluarea CASA | conv.; land §1.5 |
| S5 | „Conectează Google Drive” → login Google standard → aplicația vede doar folderul ei | — | conv. |
| E1 | La adăugarea destinației aplicația întreabă: „Criptat (recomandat)” / „Necriptat (poți deschide fișierele direct)”; implicit per destinație | Alegere informată | conv. |
| E2 | Criptarea e și opțiune a fiecărei reguli (cameră, tip conținut, tip eveniment, program, destinație); regula bate implicitul; la conflict „criptarea câștigă” | Nimic marcat criptat nu ajunge necriptat | conv. |
| E3 | Se aplică tuturor destinațiilor, inclusiv discului local al hub-ului (cheie sigilată TPM/DPAPI) | Furtul discului | conv. |
| E4 | Înregistrările criptate au lacăt în aplicație | — | conv. |
| E5 | Schimbarea setării se aplică înregistrărilor noi; conversia celor vechi după MVP | — | conv. |
| E6 | Format deschis, recuperabil fără aplicația noastră: `age` (filippo.io/age, BSD-3; cheie aleatoare per fișier; STREAM în bucăți de 64 KiB); seek-ul se verifică în Faza 0 | rclone-crypt = o singură cheie derivată din parolă | conv.; crit C11 |
| E7 | Patru căi de recuperare, toate active implicit, afișate doar cu criptare: hub; fiecare dispozitiv logat; contul (criptat pe client cu parola; resetarea prin email nu îl deblochează); cheie de recuperare salvată/tipărită; cei cu Google/Apple setează o „parolă pentru înregistrări” | Footage pierdut doar dacă se pierd toate patru | conv. |
| E8 | Motivele (conturi sparte, partajare accidentală, scanare la furnizor, răspunderea noastră, GDPR Art. 32/34) și costurile (fișiere nedeschise direct, risc de cheie, CPU mic) explicate și acceptate | Decizie informată | conv. |
| P1 | Serverele noastre nu văd video și parole de camere; doar hub-ul vorbește cu camerele; fără port forwarding/UPnP; hub-ul face doar conexiuni de ieșire | Lecțiile Ring, Verkada, Wyze | conv.; land R13 |
| P2 | Video remote criptat end-to-end (DTLS-SRTP); identitatea hub-ului fixată la împerecherea prin QR | — | conv. |
| P3 | „Zero-knowledge” doar pentru aplicațiile native; web-ul și canalul de update depind de serverele/cheia noastră; cheie de semnare offline/HSM, build-uri semnate și reproductibile; marketing doar cu afirmații literal adevărate | Lecția Eufy | conv.; crit C16 |
| P4 | Conturi: email + parolă + MFA sau passkeys; Google/Apple sign-in; protecție la credential stuffing; blocare biometrică a aplicației | — | conv. |
| P5 | Jurnal de audit: vizionări live, redări, exporturi, mișcări PTZ, schimbări de setări | Trasabilitate | conv. |
| P6 | Update-uri automate semnate, cu rollback; canal beta, apoi stable | Siguranță, CRA | conv. |
| P7 | Intrările de la camere sunt ostile: parsare memory-safe, fuzzing | CVE-uri istorice | conv. |
| P8 | Notificările nu conțin imagini implicit | Fără video prin Apple/Google | conv. |
| P9 | Ghid de criptare a discului hub-ului (BitLocker/LUKS), credențiale sigilate cu TPM; migrare/backup hub; HTTPS pe LAN cu certificat public per hub (subdomeniu per hub, DNS-01) | Furtul hub-ului; context securizat în browser | conv.; crit lipsă #3, #7, #8; **modificat de O13** (certificatul public per hub vine odată cu domeniul; până atunci, pe LAN, shell-urile native fixează cheia autosemnată a hub-ului, T42) |
| P10 | GDPR/RO: retenție per cameră (acasă scurt; business ~30 zile, Legea 190/2018), audio oprit implicit, avertisment spațiu public (Ryneš) + măști, generator de afiș „zonă supravegheată video”, ștergere cont + export date, fără recunoaștere facială, șablon DPA; suntem operator pentru date cont, telemetrie, loguri ICE | Conformitate | conv.; land §1.6; crit C17 |
| P11 | CRA: raportare vulnerabilități din 11.09.2026 (security.txt, politică, runbook 24 h/72 h către DNSC/ENISA) înainte de prima beta publică; din 11.12.2027 marcaj CE, SBOM per release, ≥ 5 ani de update-uri; hub probabil Clasa I; SBOM + update-uri semnate din prima zi; asigurare de răspundere pentru produs | Obligații legale | conv.; crit #10, C9 |
| P12 | Pentru avocat: valorile de retenție, clasificarea CRA, Legea 333/2003 + HG 301/2012, politica de confidențialitate, termenii, DPA | Incertitudine juridică | conv. |
| R1 | Watchdog, reconectare camere cu backoff, sănătate per cameră (flux, evenimente, înregistrare, upload) vizibilă; alerte: cameră offline, disc aproape plin, upload blocat, cotă cloud plină | Fiabilitate | conv. |
| R2 | Pachet de diagnostic (doar loguri, fără imagini/parole) trimis de utilizator la suport | Depanare fără om | conv. |
| R3 | Teste unit per modul; laborator automat în `e2e/` (hub + camere simulate + cloud + aplicația reală în Chromium/Playwright) care verifică că video chiar rulează, PTZ, înregistrare, timeline, upload, pană de internet și de curent; matrice CI Linux/Windows/macOS/Android; device farm înainte de release; licențe, vulnerabilități, SBOM; fuzzing | Verificare automată completă | conv.; panel |
| R4 | Record-replay: detectorul înregistrează răspunsurile ONVIF/RTSP reale; simulatoarele le redau; mai târziu un hub de test la proprietar (runner self-hosted) | Ciudățeniile fiecărei mărci, reproduse automat | conv. |
| R5 | Livrare: hub = installer Windows semnat + imagine Docker multi-arch; aplicații în App Store, Google Play, web, installer Windows; cloud pe server UE cu backup și monitorizare | — | conv.; **modificat de O13 și O16** (imaginea include linux/arm/v7; semnarea și infrastructura de livrare vin din Faza 0-bis) |
| D1 | Detector rulat o dată de proprietar: găsește toate camerele (WS-Discovery + porturi 80/443, 554, 8000, 37777, 9000, 2020, 34567/8899 + amprentare) | Primul livrabil din Faza 0 | conv. |
| D2 | Fără parolă: marcă/model/firmware unde se poate; cu parolă (doar pe acel PC): raport complet (URL-uri RTSP, codecuri, rezoluții, fps, bitrate, GOP, PTZ real testat, evenimente, tipuri de autentificare, decalaj de ceas) | — | conv. |
| D3 | Avertismente: parole implicite, porturi expuse, firmware vechi | — | conv. |
| D4 | Raportul ne ajunge fără parole și imagini; schimburile ONVIF/RTSP brute (credențiale redactate) devin fixture-uri pentru simulatoare | Laborator realist | conv. |
| D5 | Codul detectorului devine modulul `cameras` al hub-ului | Nu e cod de aruncat | conv. |

Fazele 0–4 și lista „după MVP” sunt aprobate și detaliate în §16; Faza 0-bis (O13) e blocul amânat din §16.9, nu o fază nouă de produs; lista proprietarului în §18; punctele deschise (nume, monetizare, mărcile camerelor, audio bidirecțional, bugetul și momentul Fazei 0-bis) în §19.

**Rânduri modificate de deciziile din 2026-10-18.** Textul vechi rămâne ca istoric; la diferențe, rândul nou câștigă.
- **A3** (O13): verdictul GO/NO-GO se împarte. Android, web și Windows se decid în Faza 0; iOS se decide în Faza 0-bis, cel târziu înainte de Faza 4 (§16.3).
- **O8.6** (O16): hub-ul din MVP acoperă și linux/arm/v7; plăcile armv6 nu sunt suportate (T39).
- **O11** (O13): în Faza 0, proiectul Google Cloud e pe contul personal al proprietarului, în modul Testing; Production cu brand verificat vine odată cu domeniul (OWN-07).
- **P9** (O13): certificatul public per hub așteaptă domeniul; până atunci, accesul pe LAN se face din shell-urile native, care fixează cheia autosemnată a hub-ului (T42), iar T28 se mută la începutul Fazei 2.
- **R5** (O13, O16): imaginea Docker are și linux/arm/v7; semnarea (Authenticode, KMS, YubiKey) vine din Faza 0-bis.
- **D1, F4** și criteriile din §1.3 rămân; se schimbă doar procedura de test a penei de curent (SUC-05, T43).

**Notă despre `docs/brief.md`.** Brief-ul (ipoteza 4) punea S3 „după MVP”. Decizia S4, luată mai târziu în aceeași conversație, aduce S3, WebDAV/Nextcloud și SFTP în MVP. Brief-ul a fost aliniat la S4 în același PR cu această specificație. Pe 2026-10-18 a primit secțiunea „Decizii din 2026-10-18” (O12–O16).

### 2.2 Decizii tehnice luate în această specificație

Acestea sunt alegerile pe care jurnalul le lasă deschise și care privesc mai multe secțiuni. Alegerile locale sunt marcate „Decizie tehnică” în secțiunea lor. Deciziile declanșate de porțile din Faza 0 se adaugă aici (PH0-31); cele care ar schimba o decizie a proprietarului (de exemplu plafonul de plăci din A6) se scriu în §2.1, ca modificare, doar cu aprobarea lui.

| ID | Decizie tehnică | Justificare | Unde |
|---|---|---|---|
| T01 | Contract în Protobuf, gestionat cu Buf CLI (lint, verificare de compatibilitate, generare) cu pluginuri locale fixate (fără registru remote); codul generat se comite, iar CI regenerează și pică la diferențe | Un singur instrument, build-uri reproductibile offline; diferențele se văd în PR | §7.4 |
| T02 | Generare: `protoc-gen-go` + `protoc-gen-connect-go` (Go), `protoc-gen-es` v2 + Connect-ES (TS); validare semantică cu protovalidate (Go + ES) la fiecare graniță; zod doar pentru intrări care nu vin din proto (deep links, setări locale) | O singură sursă de adevăr, fără scheme scrise de mână care să diveargă | §7.4 |
| T03 | API-ul cloud folosește protocolul Connect peste HTTPS (JSON sau binar) | Merge direct din browser fără proxy; se depanează cu curl | §7.5 |
| T04 | RPC pe canalul de date = proceduri Connect transportate în `ipcam.rtc.v1.Frame`; hub-ul servește aceleași handlere connect-go printr-o punte în proces; clientul TS are un `Transport` Connect peste canalul de date | Un singur generator, aceleași handlere pe LAN HTTPS și pe WebRTC | §7.6 |
| T05 | Canalul hub↔cloud: un WebSocket TLS cu mesaje Protobuf binare `ipcam.link.v1.Envelope`; hub-ul se autentifică semnând o provocare cu cheia de identitate | Aceeași cheie ca la fixare; fără CA proprie | §7.10 |
| T06 | Model de erori: coduri Connect + `ErrorReason` (enum) + `correlation_id`; textul pentru utilizator se traduce în client (RO/EN) | Erori citibile de mașină, i18n | §7.7 |
| T07 | ID-uri UUIDv7 (text); timpi în UTC (`google.protobuf.Timestamp`) după ceasul NTP al hub-ului | ID-uri create offline pe hub, sortabile în timp | §7.3 |
| T08 | Fereastră de compatibilitate de 12 luni; schimb `ProtocolInfo` + capabilități; formatele persistente se citesc pe viață | Aplicațiile din magazine rămân în urmă; recuperarea înregistrărilor nu expiră | §7.3 |
| T09 | Monorepo: `go.work` (hub, cloud, contract/gen/go); workspace pnpm în `apps/`; detectorul e `hub/cmd/ipcam-detector` peste `hub/internal/cameras` | D5 literal: același cod | §3.5 |
| T10 | Pachetele `pkg/` din fork-ul go2rtc rulează în procesul `hubd`; MediaMTX rulează ca proces separat supervizat, ascultă doar pe 127.0.0.1 | A7 cere media + canalul de date în același PeerConnection; codul MediaMTX nu e importabil și scrierile pe disc rămân izolate | §3.2 |
| T11 | Tokenurile de stocare stau doar pe hub: login-ul se face pe dispozitiv (Authorization Code + PKCE), cu clientul OAuth al platformei (T34); verifier-ul PKCE e generat și păstrat de hub, iar aplicația îi predă doar `code` + `state` pe canalul criptat; hub-ul răscumpără și reîmprospătează; „brokerul OAuth” din cloud = pagina statică de redirect; pe web, Google Drive folosește device flow pornit de hub (T34); poartă în Faza 0 (§3.3) | Cloud-ul nu poate citi Drive-ul utilizatorului; un cod interceptat nu se poate răscumpăra fără verifier | §3.3, §8.7 |
| T12 | Endpoint-ul HTTPS al hub-ului pe LAN expune doar identitate, împerechere locală (inclusiv pagina `/pair` a unui hub fără proprietar, T36) și signaling local; restul API-ului merge numai pe canalul de date | O singură cale de autorizare, suprafață mică | §7.8 |
| T13 | Jurnalul de audit: acțiunile de pe hub în SQLite pe hub, înlănțuite prin hash; acțiunile de cont în PostgreSQL în cloud | Acțiunile pe camere nu trec prin cloud | §3.3 |
| T14 | Parola contului nu pleacă de pe dispozitiv: clientul derivă cu Argon2id o cheie de autentificare (serverul ține doar un hash al ei) și o cheie de împachetare pentru backup-ul de chei; parametrii în §10 | E7.3 cere ca serverul să nu afle parola | §3.3 |
| T15 | Copia catalogului de pe o destinație e criptată dacă destinația conține orice conținut criptat | „Criptarea câștigă” (E2) se aplică și catalogului | §3.3 |
| T16 | Backup-ul de chei din cont conține și directorul destinațiilor, fără secrete (tip, endpoint, bucket/folder) | Un telefon nou găsește destinațiile după furtul hub-ului (F6) | §3.3 |
| T17 | NAS (SMB) prin clientul SMB al sistemului: cale UNC cu credențiale sigilate pe Windows, volum montat în Docker | Fără bibliotecă SMB în Go de întreținut | §3.4 |
| T18 | Miniaturile vin din snapshot-ul camerei (ONVIF GetSnapshotUri); FFmpeg LGPL, proces separat, doar pentru AAC→Opus când utilizatorul pornește audio și ca rezervă pentru miniaturi | Fără decodor video livrat în cazul obișnuit (expunere la brevete, crit C10) | §3.4 |
| T19 | Codul QR de împerechere = link universal `https://<domeniu>/p#<PairingTicket în base64url>`; fragmentul nu ajunge la server | Camera telefonului deschide aplicația; secretul de împerechere nu atinge serverul | §7.8 |
| T20 | Fiecare fișier criptat are exact doi destinatari `age`: generația curentă a cheii de înregistrări a hub-ului (`RI_g`) și cheia de recuperare (`RK`), generată pe client | Cheia de recuperare singură, inclusiv cu unealta `age` standard, deschide orice fișier (E6, E7.4) | §10.2 |
| T21 | Criptarea pe discul local se decide la deschiderea segmentului. Dacă discul local rezolvă la „criptat” pentru acea cameră, segmentul se scrie direct `.age` (patch-ul MTX-2) și nu atinge discul în clar; copiile pentru destinațiile necriptate se decriptează din mers la upload | E3 + „criptarea câștigă”, fără plaintext temporar pe hub | §4.5, §9.7 |
| T22 | Modul „doar la mișcare”: înregistrare continuă doar într-un inel local de 1 h, din care se taie clipurile de eveniment; pre-roll implicit 10 s, post-roll implicit 20 s; substream la 12 fps | Inelul acoperă evenimentele întârziate fără păstrare continuă; pre-roll și post-roll la capătul de sus al intervalelor aprobate (O8.3); 12 fps = mijlocul intervalului 10–15 din A6 | §8.2, §8.4 |
| T23 | Un dispozitiv intră în lista de încredere a hub-ului doar prin: aprobare de pe un dispozitiv de încredere, QR sau cod de pe hub, ori dovada de backup cu cheia de înrolare `EK` (derivată din cheia de backup a contului) | Îndeplinește E7.1 („telefonul nou vede tot”) fără ca un cloud compromis să poată adăuga dispozitive | §10.5, §11.3 |
| T24 | La împerecherea prin cod, utilizatorul compară o amprentă a hub-ului de 64 de biți (16 caractere hex) | 6 cifre se pot forța de un cloud compromis în câteva secunde | §8.1, §11.3 |
| T25 | Certificatele LAN au forma `<ip-cu-liniuțe>.<dns_label>.<domeniu-hub>`; `dns_label` = 16 octeți aleatori în base32; DNS-ul nostru răspunde algoritmic, fără înregistrări per hub, doar pentru IP-uri private și etichete existente (T35) | `hub_id` (UUIDv7) ar publica ora creării în jurnalele Certificate Transparency | §5.7, §11.5 |
| T26 | Update-urile folosesc metadate TUF: rădăcina 2 din 3 pe YubiKey offline; `targets` 2 din 2 (builder GitHub + rebuilder AWS independent, octeți identici); fiecare release în jurnalul public Rekor | Cheia de update e „bijuteria coroanei” (P3, crit lipsă #6) | §11.4, §15.2 |
| T27 | Pe Windows, aplicația Tauri e actualizată de updater-ul hub-ului; dacă PC-ul nu e hub, serviciul rulează doar ca updater | Updater-ul Tauri acceptă o singură semnătură, deci nu respectă pragul din T26 | §15.3 |
| T28 | **Modificată de T42 (2026-10-18).** Trei bucăți din cloud vin înaintea restului Fazei 2: găzduirea statică a SPA-ului, modulul `certs` și autentificarea hub-ului pe `link/v1`. Inițial erau în Faza 1; acum vin la începutul Fazei 2, când există domeniul (Faza 0-bis). În Faza 1, LAN-ul merge din shell-urile native (T42), iar împerecherea e doar locală | Aplicația web are nevoie de un certificat de încredere ca să ajungă la hub pe LAN, iar certificatul cere domeniul | §16.6 |
| T29 | Un NO-GO mobil în Faza 0 (Android) sau în Faza 0-bis (iOS) mută ambele aplicații mobile pe Flutter, împreună; web-ul și Windows rămân pe opțiunea B | O singură bază de cod mobilă e mai ușor de întreținut decât trei | §16.3 |
| T30 | Cloud-ul e un monolit modular (un binar, rolurile `api`, `gateway`, `worker`); sesiuni PASETO v4 de 10 min (biblioteca `aidantwoods.dev/go-paseto`, MIT) + refresh token rotit; un dispozitiv necunoscut logat doar cu parolă confirmă un cod primit pe email | Un singur artefact de testat; protecție la credential stuffing (P4) | §5.1, §5.2 |
| T31 | Planul `free` are în MVP doar limite anti-abuz configurabile: 5 hub-uri, 32 de camere per hub, 4 sesiuni remote simultane, 100 GB relay pe lună (peste ultima, doar alertă internă) | O7: drepturile există din prima zi, fără facturare | §5.7 |
| T32 | Găzduire: Hetzner (primar în Germania, replică în Finlanda), backup la Scaleway (Franța), email prin Brevo (Franța) | Furnizori UE; backup la un al doilea furnizor | §5.9, §15.5 |
| T33 | Versiuni minime: iOS 17.1 (ManagedMediaSource), Android 10 (minSdk 29), Windows 11; Windows 10 22H2 e „suportat cu avertisment”, fiindcă update-urile de securitate pentru consumatori (inclusiv ESU) s-au oprit pe 13.10.2026 (OPN-15) | Redarea înregistrărilor în WKWebView; matrice de test rezonabilă; un produs de securitate nu recomandă un sistem fără patch-uri | §4.7, §6.7, §15.1 |
| T34 | Clienți OAuth Google per platformă, toți în același proiect: iOS (tip „iOS”, fără secret), Android (tip „Android”, cu schemă URI proprie), Windows (tip „Desktop”, redirect loopback); pe web, device flow pornit de hub (tip „TVs and Limited Input”). Secretele clienților Desktop și TV nu sunt confidențiale (așa le tratează Google) și stau în hub | Clienții de tip „Web application” cer `client_secret` la răscumpărare chiar și cu PKCE; planul A funcționează astfel fără secret în cloud | §3.3, §5.6, §8.7 |
| T35 | Zona DNS a hub-urilor stă pe un domeniu înregistrabil separat, `<domeniu-hub>`, nu sub domeniul SPA-ului; DNS-ul răspunde doar pentru IP-uri private și etichete existente; certificatele se emit doar după `ClaimHub`, cu cote per IP și ASN | Abuzul pe zona hub-urilor (limitele Let's Encrypt, phishing, liste negre) nu atinge SPA-ul și conturile | §5.7, §11.5 |
| T36 | Pe un hub fără proprietar, pagina `/pair` de pe portul 7443 arată QR-ul și codul în primele 30 min după pornire | Utilizatorul de NAS/Docker nu trebuie să caute în loguri (O9); riscul „primul care revendică” e acceptat, cu resetare locală și audit | §8.1 |
| T37 | Fiecare cont primește la creare `BK`, blob și `EK`, indiferent de criptare; la revocarea unui dispozitiv, hub-ul oprește calea (c) până la o cheie `EK` nouă | Calea (c) (E7.1, „telefonul nou vede tot”) trebuie să meargă și fără criptare, iar un dispozitiv revocat nu se poate readmite cu `BK`-ul vechi | §10.3, §10.5 |
| T38 | Backup-ul automat al hub-ului nu conține secrete (parole de camere, tokenuri și chei de stocare, `HIK` privat); ele părăsesc hub-ul doar în exportul manual protejat cu frază | F2 și P1: credențialele nu ajung la clienți sau pe destinații prin backup | §13.5 |
| T39 | **Raspberry Pi (O16).** Imaginea multi-arch are linux/amd64, linux/arm64 și linux/arm/v7 (Go cu `GOARM=7`, MediaMTX și FFmpeg LGPL pentru armv7, bază distroless static). Pe Raspberry Pi, hub-ul citește modelul din `/proc/cpuinfo` (câmpurile `Model` și `Revision`, vizibile și din container, unde Docker maschează `/sys/firmware`; `Serial` nu se citește) și îl raportează; aplicația arată numărul recomandat de camere al modelului (§4.8) și avertizează peste el, fără refuz (HUB-34). Pentru linux/arm/v6, manifestul are doar un binar mic care nu pornește hub-ul: afișează explicația în `docker logs` și pe pagina `/pair` (T36) și rămâne pornit, fără buclă de repornire. Pi 2 v1.1 (Cortex-A7) rulează și el armv7, deci intră sub aceeași regulă | Proprietarul vrea toate modelele; numărul recomandat evită promisiunile false; armv6 nu poate înregistra și transmite fiabil | §4.7, §4.8, §15.1 |
| T40 | **Card SD (O16).** Când `rec/` (implicit în `/data`) stă pe un card SD (dispozitiv `mmcblk*`, numărul major 179, citit din `/proc/self/mountinfo`), hub-ul nu scrie niciun segment până când utilizatorul confirmă explicit avertismentul despre uzură, care recomandă un SSD; confirmarea intră în audit. Cât timp înregistrează pe card, `health` urmărește erorile I/O, remontarea doar-citire și latența fsync și ridică alerta „card SD” (§13.1, §13.3) | Înlocuiește refuzul anterior: proprietarul vrea să meargă și pe card, cu riscul explicat | §4.9, §13.1, §13.3, HUB-17 |
| T41 | **Mijloacele Fazei 0 la cost zero (O13, O14).** (1) Android: APK de test (build de depanare Capacitor), instalat direct pe Galaxy A24 al proprietarului, fără cont Play. (2) Windows: build de dezvoltare Tauri, nesemnat, pe PC-ul Windows 11; avertismentul SmartScreen se acceptă pentru test. (3) Web: Chrome și Edge pe același PC; SPA-ul e servit de o unealtă de spike (`tools/spikes/`) pe `http://localhost`, care e context securizat în browser; tot ea transmite signaling-ul la hub pe LAN și verifică SPKI-ul certificatului autosemnat cu `HIK` (T42); media merge direct, prin WebRTC. (4) Google: proiect Google Cloud pe contul personal al proprietarului, în modul Testing, doar cu `drive.file`; tokenurile de 7 zile sunt acceptate la spike-uri. (5) Microsoft: înregistrarea aplicației pe un cont personal, doar dacă nu cere date de plată; altfel testele OneDrive trec în Faza 0-bis. (6) Remote (G8) și MV12 în Chrome: doar dacă o variantă de signaling + TURN merge gratuit, fără date de plată (de exemplu planurile gratuite Cloudflare: Pages pentru SPA, Workers pentru signaling, Realtime TURN); altfel trec în Faza 0-bis. (7) CI: runner-ele găzduite de GitHub, gratuite pe repo-ul public (O12). Un pas care se dovedește cu plată se oprește și trece în Faza 0-bis (PH0-32) | Proprietarul a cerut Faza 0 fără costuri și fără iOS; fiecare mijloc folosește doar dispozitivele și conturile lui | §16.3, §16.4, §16.9 |
| T42 | **LAN fără domeniu (Fazele 0–1).** Până există domeniul (cost amânat, O13), `lanapi` folosește pe 7443 un certificat autosemnat, generat pe hub. Shell-urile native (Tauri pe Windows, Android) îl acceptă doar dacă semnătura `HIK`, fixat la împerecherea locală prin QR sau din `ticket.txt` (F1.X2), acoperă `SHA-256(SPKI)`, ca în modul 2 din §11.5 (APP-23). Fără domeniu nu există nici link-ul universal din T19: în Fazele 0–1, QR-ul conține doar `PairingTicket` în base64url, citit de scanerul din aplicație. Pluginul `lan-http` intră astfel în Faza 1 pe Android și Windows; cheile de dispozitiv folosesc în Faza 1 WebCrypto ne-extractabil, prin aceeași fațadă ca `device-keys`. Web-ul pe LAN cu certificat public per hub (P9) așteaptă domeniul: T28 se mută la începutul Fazei 2 | Fără domeniu nu există certificat public; fixarea `HIK` dă aceeași garanție de identitate (§11.5) | §11.5, §16.6 |
| T43 | **Pana de curent fără priză inteligentă (O13).** Pragul statistic vine din rulările automate (kill -9 și reset fără scrierea cache-ului, în laborator și pe dispozitivele proprietarului, cel puțin 40 per sistem de operare); câte 5 tăieri reale de curent pe PC-ul și pe Pi-ul proprietarului, făcute de mână, confirmă comportamentul hardware-ului real. Momentul opririi îl dă semnalul de viață primit de controlor (§16.4) | 0 eșecuri în 40 de rulări automate dau o rată de eșec sub ≈ 7 % (95 % încredere); 10 tăieri reale cer ≈ 2,5 h de lucru de la proprietar și prind ce simularea nu acoperă (cache-ul de scriere al SSD-ului sau al cardului, NTFS la pierderea curentului) | §1.3, §4.10, §16.4 |
| T44 | **Hub-ul de test fără infrastructura de semnare.** Până la OWN-19…21 (Faza 0-bis), hub-ul de test al proprietarului rulează build-uri CI nesemnate (imaginea GHCR `:dev`, arhiva Windows din CI), instalate și actualizate manual, după instrucțiunile agentului; updater-ul A/B se testează doar în laborator, cu chei TUF de test; canalul beta nu pornește înainte de G20 | Fără chei de producție, un canal de update ar avea chei slabe; hub-ul de test nu are alți utilizatori | §15.3, §16.6 |

## §3 Arhitectura sistemului

**Pe scurt (pentru proprietar).** Sistemul are trei părți: hub-ul de lângă camere, cloud-ul nostru mic și aplicațiile. Imaginea merge direct de la hub la aplicație; cloud-ul doar le pune în legătură. Tabelul din §3.3 arată, pentru fiecare fel de date, unde stă, cum e protejat și unde nu ajunge niciodată.

### 3.1 Diagramă

```
                    +------------------- CLOUD ipcam (UE, Go + PostgreSQL) --------------------+
                    | API Connect/HTTPS: conturi, împerechere, signaling, backup chei, drepturi |
                    | Gateway WSS hub-uri: autentificare, prezență, rutare signaling            |
                    | Certificate LAN (DNS-01) | callback OAuth (static) | telemetrie | CDN web   |
                    +------^---------------------------------^-------------------------^--------+
  LA UTILIZATOR (LAN)      | WSS de ieșire (control,         | HTTPS (cont, signaling, credențiale TURN scurte)
                           | signaling)                      |                         |
  +------------------------+-----------+   WebRTC P2P:      +--+--------------------------------+
  | HUB  (serviciu Windows / Docker)   |   DTLS-SRTP video  | CLIENȚI: un cod TypeScript        |
  |  hubd: cameras, live (go2rtc pkg), |<==================>|  web/PWA (browser)                |
  |  session (Pion), catalog (SQLite), |   + canal de date  |  Android, iOS (Capacitor)         |
  |  playback, rules+outbox, storage,  |   RPC              |  Windows (Tauri/WebView2)         |
  |  crypto, cloudlink, lanapi, audit, |                    +--------------^--------------------+
  |  updater; MediaMTX (127.0.0.1)     |   rezervă (10–25 % din sesiuni): TURN Cloudflare,
  +----^-----------------------+-------+   care vede doar date criptate
       | RTSP/ONVIF (doar hub) | HTTPS (upload, tokenuri doar pe hub)
  +----+---------+   +---------v---------------------------------------------+
  | Camere IP    |   | Destinațiile utilizatorului: disc local, NAS (SMB),    |
  | RTSP / ONVIF |   | Google Drive, OneDrive, S3, WebDAV/Nextcloud, SFTP     |
  +--------------+   +--------------------------------------------------------+
```

### 3.2 Componente

| Componentă | Rulează în | Limbaj | Rol | Detalii |
|---|---|---|---|---|
| `hubd` | hub: serviciu Windows sau container Docker cu rețeaua gazdei | Go | supervizor (watchdog R1), API local, chei, pornește MediaMTX | §4 |
| `cameras` | `hubd` | Go | descoperire (WS-Discovery + porturi), amprentare, verificări, ONVIF (PTZ, presetări, evenimente), credențiale sigilate; același cod ca detectorul (D5) | §4 |
| `live` | `hubd`, pachetele `pkg/` din fork-ul go2rtc (T10) | Go | o singură conexiune upstream per flux de cameră; restream RTSP local pe 127.0.0.1 | §4 |
| `session` | `hubd`, Pion | Go | un PeerConnection per sesiune: track-urile tuturor plăcilor + canalul de date (A7); semnează răspunsul SDP cu cheia hub-ului | §4, §7.6 |
| `recorder` | proces MediaMTX separat, doar 127.0.0.1 (T10), cu patch-urile MTX-1…MTX-3 | Go (upstream) | segmente fMP4 de 5 min cu părți de 1 s și fsync, criptate la scriere când regula cere (T21) | §4.2, §4.5 |
| `playback` | `hubd` | Go | timeline HLS(fMP4) din catalog, citire din cea mai rapidă locație, decriptare, tăierea clipurilor de eveniment, export MP4, miniaturi | §4.3, §8.5 |
| `catalog` | `hubd`, SQLite | Go | segmente, evenimente, miniaturi, stare de upload per destinație, audit (T13) | §9 |
| `rules` + `outbox` | `hubd` | Go | aplică regulile S1/E2, coada persistentă de upload, retenție, curățare de urgență | §9 |
| `storage` | `hubd` | Go | interfața `Destination` + drivere: local/NAS, Drive, OneDrive, S3, WebDAV, SFTP | §9 |
| `crypto` | `hubd` | Go | criptare `age`, cheile de înregistrări, sigilare TPM/DPAPI/Linux | §10 |
| `cloudlink`, `lanapi`, `updater`, `config`, `rpc` | `hubd` | Go | WebSocket de ieșire spre cloud (§7.10); HTTPS pe LAN cu certificat per hub (autosemnat până există domeniul, T42): identitate, împerechere și signaling local (T12); update-uri semnate cu rollback (P6, T26); configurația cu revizii; handler-ele `ipcam.hub.v1` cu verificarea permisiunilor | §4.3, §15 |
| `ipcam-detector` | binar separat, rulat o dată pe un PC | Go | raport fără parole/imagini + fixture-uri pentru laborator (D1–D5) | §16 |
| `ipcam-cloud` (rolurile `api`, `gateway`, `worker`) | cloud UE | Go | `api`: Connect/HTTPS pentru clienți (conturi, dispozitive, împerechere, signaling, backup chei, drepturi, telemetrie); `gateway`: WSS pentru hub-uri (autentificare, prezență, rutare signaling, certificate LAN); `worker`: email, ACME, DNS pentru `<domeniu-hub>`, ștergeri; PostgreSQL: conturi, hub-uri, dispozitive, bloburi de backup, drepturi, audit de cont | §5 |
| TURN | Cloudflare Realtime TURN; mai târziu coturn/pion-turn în UE | — | releu criptat pentru sesiunile fără cale directă | §5 |
| CDN | static | — | aplicația web (release semnat, P3) și manifestele de update semnate | §15 |
| SPA + `@ipcam/player` | browser / WebView | TypeScript + React | toată interfața, pe toate platformele; player WebRTC/MSE (adaptat din video-rtc.js) și HLS(fMP4) pentru înregistrări | §6 |
| shell mobil | Capacitor (Android, iOS) | TS + Kotlin/Swift | pluginuri mici: PiP, permisiune rețea locală, stocare sigură, push (mai târziu) | §6 |
| shell desktop | Tauri (WebView2) | Rust < ~300 linii | fereastră, tray, deep link, starea serviciului hub; installer-ul pune și hub-ul; update-urile vin prin updater-ul hub-ului (T27) | §6 |

### 3.3 Harta datelor

| Dată | Unde trăiește | Protecție | Nu ajunge niciodată în |
|---|---|---|---|
| Credențiale camere | hub: SQLite, sigilate; exportul manual de migrare al hub-ului, criptat cu frază (§13.5); detectorul: doar în memoria PC-ului pe care rulează (D2) | sigilare = criptare cu o cheie a hub-ului protejată de TPM/DPAPI pe Windows sau de cheia sigilată pe Linux (§10) | cloud, clienți (exportul manual trece prin dispozitiv doar criptat cu fraza), destinații (inclusiv backup-ul automat, T38), telemetrie, loguri, raportul detectorului |
| Cheia de identitate a hub-ului (Ed25519, land §3.1) | privată: hub, sigilată; publică: registrul din cloud și fixată în fiecare client la împerechere | sigilare | cheia privată nu iese de pe hub (doar în exportul manual de migrare, criptat cu frază; nu intră în backup-ul automat) |
| Cheile dispozitivelor client | private: Keychain / Keystore / DPAPI / WebCrypto ne-extractabil; publice: lista de încredere a hub-ului + lista de dispozitive din cont | hardware unde există | cheia privată nu iese de pe dispozitiv |
| Cheile de înregistrări (identități `age` ale hub-ului) | (1) hub, sigilate; (2) fiecare dispozitiv de încredere, împachetate cu cheia dispozitivului; (3) cloud: blob criptat pe client cu cheia derivată din parola contului sau din „parola pentru înregistrări” (T14); (4) cheia de recuperare a utilizatorului (hârtie/fișier). Cheia aleatoare a fiecărui fișier stă în antetul lui, împachetată pentru aceste identități (T20) | E7, §10.3 | cloud în clar, destinații în clar, telemetrie |
| Video live | doar în RAM pe hub și pe client; tranzitează P2P sau prin TURN, criptat DTLS-SRTP | E2E | cloud, TURN (vede doar text cifrat), push |
| Înregistrări (segmente, clipuri de eveniment) | discul local al hub-ului (spool, mereu) + destinațiile alese prin reguli | `age` conform regulilor (E1–E3) | cloud ipcam |
| Exporturi MP4, miniaturi, snapshot-uri | hub; destinațiile cu regulă „exporturi” / „miniaturi”; dispozitivul pe care utilizatorul descarcă un export | conform regulilor | cloud ipcam, push (P8), telemetrie |
| Catalog / index | hub: SQLite (sursa principală); copie la fiecare destinație (F6), criptată dacă destinația are conținut criptat (T15); clienții țin doar cache în memorie | `age` (T15) | cloud ipcam |
| Configurație (camere fără parole, reguli, destinații fără secrete) | hub: SQLite; exportul manual și backup-ul automat (§13.5); directorul destinațiilor și în blobul de backup al contului (T16) | sigilare / blob criptat pe client | cloud în clar |
| Token Google Drive (`drive.file`) | refresh: hub, sigilat; access: RAM pe hub. Se obține cu clientul OAuth al platformei (§8.7, T34): pe iOS, Android și Windows prin cod predat hub-ului, pe web prin device flow pornit de hub. În F6 (fără hub), un token separat obținut prin login, doar pe dispozitivul care redă | sigilare | cloud (T11), backup-ul automat |
| Token OneDrive personal / business | refresh: hub, sigilat; PKCE pe dispozitiv + cod predat hub-ului pe toate platformele, inclusiv web (§8.7); la business fără device code (crit C19) | sigilare | cloud (T11), backup-ul automat |
| Chei S3, parole WebDAV/SFTP/SMB | hub, sigilate; în F6 utilizatorul le reintroduce pe dispozitiv | sigilare | cloud, telemetrie, backup-ul automat |
| Date de cont (email, hash al cheii de autentificare, factori MFA, chei publice passkey, ID-uri Google/Apple, limbă) | cloud: PostgreSQL în UE | TLS, criptare la nivel de disc, backup criptat | parola în clar, chei de înregistrări în clar |
| Sesiunea aplicației (tokenuri de cont) | access token: doar în memorie; refresh token: nativ în stocarea sigură a sistemului, web în cookie HttpOnly SameSite=Strict (panel, §5.2) | rotație, expirare | localStorage, sessionStorage |
| Registrul hub-urilor (ID, cheie publică, proprietar, prezență, versiune); drepturi (entitlements; MVP: plan „free”) | cloud: PostgreSQL; drepturile au și o copie semnată de cloud pe hub | semnătură | — |
| Credențiale TURN | emise de cloud, scurte; RAM pe client (și pe hub doar când rețeaua lui blochează UDP) | expirare | stocare persistentă |
| Jurnal de audit | acțiuni pe hub (live, redare, export, PTZ, setări): SQLite pe hub, înlănțuit prin hash, inclus în exportul manual și în backup-ul automat (§13.5); acțiuni de cont (login, împerechere, dispozitive, ștergere): PostgreSQL în cloud (T13) | hash-chain / acces restrâns | — |
| Telemetrie operațională (versiuni, coduri de eroare, tip pereche ICE, durate) și loguri ICE (IP-uri) | cloud UE; categoriile, baza legală și retenția în §12.2 | acces restrâns | imagini, nume de camere, credențiale, catalog |
| Pachet de diagnostic, rapoarte crash, raportul detectorului | create pe hub/dispozitiv/PC; trimise nouă doar la acțiunea utilizatorului (R2) sau a proprietarului (D4); fixture-urile au credențialele redactate | doar loguri / redactare | imagini, parole |

**Poartă Faza 0 — OAuth pe hub (T11, G15).** Pentru fiecare combinație furnizor × platformă (Google Drive, OneDrive personal, OneDrive business cu Conditional Access × web, Android, iOS, Windows): *trece* dacă hub-ul răscumpără codul și face refresh 7 zile la rând, inclusiv refresh-ul din ziua 7 făcut chiar de hub, fără un secret confidențial deținut de cloud; *pică* altfel. Combinația web × Google Drive nu are plan A și folosește B1 prin design (T34). Tot aici se verifică vizibilitatea între clienți: un fișier creat de clientul OAuth al hub-ului trebuie să poată fi listat și citit de clienții F6 ai aceluiași proiect Google, respectiv ai aceleiași aplicații Entra (§8.6, MI6). Plan B, în ordine: (1) doar pentru Google Drive și OneDrive personal, hub-ul rulează device flow (RFC 8628; Google îl permite cu `drive.file`; briefs: stocare), iar aplicația afișează codul și deschide pagina furnizorului; la OneDrive business nu se folosește device code (crit C19); (2) pentru combinațiile rămase, cloud-ul face schimbul de cod și refresh-ul ca proxy fără stare (planul B2 din §5.6: nu salvează și nu loghează tokenul), iar rândul din harta datelor și politica de confidențialitate se actualizează înainte de activare. Dacă Conditional Access blochează refresh-ul de pe hub, se aplică poarta FLW-23 (§8.7). **Calendar (O13, T41):** în Faza 0 rulează doar combinațiile gratuite (Google Drive × Android, Windows, web; OneDrive personal doar dacă înregistrarea aplicației nu cere date de plată), cu proiectul Google în Testing, deci refresh-ul de pe hub se verifică în zilele 1–6; combinațiile iOS, OneDrive business și refresh-ul Drive din ziua 7 cu proiectul în Production se verifică în Faza 0-bis, înainte de Faza 3 (§16.9).

### 3.4 Stack și licențe

Versiunile sunt cele verificate în cercetare (octombrie 2026). „—” = versiunea se fixează exact în `go.mod` / `pnpm-lock.yaml` la prima utilizare, după regula: ultima versiune stabilă cu cel puțin 14 zile vechime; apoi Renovate cu perioadă de așteptare (panel). Uneltele care nu se livrează (linter-e, ffmpeg cu libx264 din simulatoarele de laborator) pot avea alte licențe; verificarea de licențe (SUC-06) rulează pe SBOM-ul artefactelor livrate (§15).

| Zonă | Componentă | Versiune | Licență | Folosită în |
|---|---|---|---|---|
| Limbaj | Go (toolchain stabil curent) | — | BSD-3 | hub, cloud, detector |
| Live | go2rtc, fork vendored | bază v1.9.14 | MIT | hub |
| Înregistrare | MediaMTX (proces separat) | v1.21.2 | MIT | hub |
| Media, WebRTC | gortsplib / mediacommon; Pion WebRTC | v5.6.7 / — ; v4.2.24 | MIT | hub |
| ONVIF | kerberos-io/onvif | v1.2.3 | MIT | hub, detector |
| Bază de date hub | modernc.org/sqlite (Go pur, fără CGO) | — | BSD-3 | hub |
| Criptare | filippo.io/age; golang.org/x/crypto | — | BSD-3 | hub, cloud |
| Audio / rezervă miniaturi | FFmpeg, build LGPL, proces separat (T18) | 9.0.x | LGPL-2.1+ | hub |
| Drivere cloud | google.golang.org/api (Drive v3); client REST Microsoft Graph propriu (OneDrive); aws-sdk-go-v2 (S3) | — | BSD-3 / proprie / Apache-2.0 | hub |
| Drivere fișiere | github.com/pkg/sftp + golang.org/x/crypto/ssh (SFTP); client WebDAV propriu peste `net/http`; NAS prin clientul SMB al sistemului (T17); după MVP rclone pentru coada lungă | — ; rclone v1.75.2 | BSD-2 / BSD-3 / proprie / MIT | hub |
| Bază de date cloud | PostgreSQL (majora curentă) + pgx v5 + sqlc + goose | — | PostgreSQL / MIT | cloud |
| WebSocket, passkeys | github.com/coder/websocket; github.com/go-webauthn/webauthn | — | ISC / BSD-3 | hub, cloud |
| RPC | connectrpc.com/connect, @connectrpc/connect-web | — | Apache-2.0 | toate |
| Contract | google.golang.org/protobuf, @bufbuild/protobuf (protobuf-es v2), Buf CLI, protovalidate | — | BSD-3 / Apache-2.0 | toate |
| TURN | Cloudflare Realtime TURN ($0,05/GB după 1 TB/lună gratuit, crit #4); apoi coturn sau pion/turn | coturn 4.14+ | serviciu / BSD-3 / MIT | cloud |
| Client | TypeScript (strict), React, Vite, pnpm | — | Apache-2.0 / MIT | apps |
| Shell-uri | Capacitor (Android, iOS); Tauri 2 (Windows, WebView2) | 8.5.3; 2.12.4 | MIT; MIT / Apache-2.0 | apps/mobile, apps/desktop |
| Player | video-rtc.js adaptat din go2rtc; hls.js | — / 1.5+ | MIT / Apache-2.0 | apps/packages/player |
| Client: UI și stare | React 19, TanStack Router + Query, Zustand, Radix UI, Tailwind CSS; FormatJS (i18n) | — | MIT; BSD-3 | apps |
| Client: criptare | `age-encryption` (typage) pentru antete; `@noble/ciphers`, `@noble/hashes` pentru `agerange` (§10.2); hash-wasm (Argon2id) | 0.3.1 / — / — | BSD-3 / MIT / MIT | apps |
| Update și transparență | go-tuf v2; sigstore-go (Rekor) | — | Apache-2.0 | hub, cloud, CI |
| DNS pentru `<domeniu-hub>`; sesiuni | miekg/dns; `aidantwoods.dev/go-paseto` (PASETO v4; Decizie tehnică: bibliotecă PASETO Go întreținută, cu licență permisivă; licența se verifică în CI, SEC-20). Rezervă: fără bibliotecă potrivită, token Ed25519 propriu, definit în `contract/` | — | BSD-3 / MIT | cloud |
| Teste | Playwright, Vitest + Testing Library, `go test`, `pgregory.net/rapid`, fast-check, gofakes3, `0x524a/onvif-go` (doar laborator) | — | Apache-2.0 / MIT / BSD-3 / MPL-2.0 / MIT / MIT / MIT | CI, `e2e/` |
| Operare (nelivrate) | OpenTofu, SOPS; VictoriaMetrics, VictoriaLogs; pgBackRest; ko; Syft, osv-scanner | — | MPL-2.0; Apache-2.0; MIT; Apache-2.0; Apache-2.0 | `cloud/deploy/`, CI |

### 3.5 Structura folderului `ipcam/`

Workflow-urile CI stau la rădăcina repo-ului, în `.github/workflows/ipcam-*.yml`, cu filtre `paths: ipcam/**`.

```
ipcam/
├── CLAUDE.md, README.md      reguli pentru sesiunile AI; prezentare în română
├── go.work                   leagă modulele Go: hub, cloud, contract/gen/go (T09)
├── contract/
│   ├── proto/                fișierele .proto, sursa unică a tuturor API-urilor și formatelor (§7)
│   └── gen/                  cod Go generat și comis (modulul contract/gen/go); `contract/testdata/` ține fișierele golden (CTR-07)
├── hub/
│   ├── cmd/                  binare: ipcam-hub (hubd), ipcam-detector
│   ├── internal/             module: cameras, live, session, recorder, playback, catalog, rules, outbox, storage, crypto, rpc, config, cloudlink, lanapi, audit, health, updater (§4.3)
│   ├── third_party/          sursele vendored ale go2rtc (fork) și MediaMTX + PATCHES.md cu patch-urile noastre; ffmpeg/SHA256SUMS (§15.2)
│   └── packaging/            installer Windows (serviciu), Dockerfile multi-arch, configurare semnare
├── cloud/
│   ├── cmd/                  binare: ipcam-cloud (rolurile api, gateway, worker), ipcam-migrate
│   ├── internal/             module: accounts, auth, devices, pairing, presence, signaling, turn, certs, oauth, keybackup, entitlements, audit, telemetry (§5.1)
│   ├── migrations/           migrații SQL PostgreSQL versionate
│   └── deploy/               Docker Compose, configurare server UE, backup, monitorizare
├── apps/
│   ├── web/                  SPA React + PWA
│   ├── mobile/               shell Capacitor (android/, ios/) + pluginuri native mici
│   ├── desktop/              shell Tauri (Rust < ~300 linii), include hub-ul ca sidecar/serviciu
│   └── packages/             pachete TS partajate: api (cod generat), player, ui, i18n, core
├── e2e/
│   ├── run.sh                pornește laboratorul cu o singură comandă (TST-20)
│   ├── lab/                  docker-compose: hub + camere simulate + cloud + furnizori falși + TURN (§14.2)
│   ├── simulators/           camere RTSP ffmpeg (oră arsă, contor de cadre), simulator ONVIF (PTZ, evenimente), replay
│   ├── fixtures/             înregistrările record-replay de la camerele reale (R4), redactate
│   └── tests/                teste Playwright și scenarii: pană internet, kill -9, upload
├── tools/
│   ├── licenses/             allowlist de licențe și configurarea verificării
│   ├── scripts/              generare contract, release, SBOM, approve-release (DEL-03)
│   └── spikes/               scripturile de măsurare din Faza 0, șterse după raport (§16.1)
└── docs/
    ├── brief.md              cerințele confirmate
    ├── research/             cercetarea din 2026-10-17
    ├── superpowers/          specs/ (această specificație) și plans/ (planurile de implementare)
    ├── formats/              specificațiile publice ale fișierelor criptate și ale catalogului (E6)
    ├── legal/                afirmațiile publice permise (claims.md), șablonul DPA (§11.2, §12.3)
    ├── detector.md           ghidul detectorului pentru proprietar (§16.2)
    └── runbooks/             proceduri: raportare CRA 24 h/72 h, incidente, ceremonia cheilor, release, postmortem-uri
```

### 3.6 Cerințe de arhitectură

| ID | Cerință | Test |
|---|---|---|
| ARH-01 | Doar hub-ul deschide conexiuni către camere (P1) | În laborator, containerele cloud și client nu au rută spre subrețeaua camerelor simulate; E2E trece |
| ARH-02 | Hub-ul face doar conexiuni de ieșire spre internet; nu există cod UPnP, port forwarding sau tunel în `hubd` | Scanare de socket-uri: pe LAN ascultă doar 7443/tcp (HTTPS), 7445/udp (WebRTC) și 5353/udp (mDNS), ca în HUB-02; verificare de dependențe fără biblioteci UPnP |
| ARH-03 | Video-ul și credențialele camerelor nu trec prin componentele cloud în operare normală | Captură de pachete pe containerul cloud în E2E: zero RTP/SRTP |
| ARH-04 | Clienții vorbesc cu hub-ul doar prin WebRTC (media + canal de date); signaling prin cloud sau prin endpoint-ul LAN | Test E2E cu cloud-ul oprit: live pe LAN prin signaling local |
| ARH-05 | Fiecare motor terț (go2rtc, MediaMTX, ONVIF, SDK-uri de stocare, player) e folosit doar prin interfața noastră (`live.Gateway`, `recorder.Recorder`, `cameras.Camera`, `storage.Destination`, `Player`; §4.4, §6.2, §9.1) | Reguli de import în CI (Go: depguard; TS: `no-restricted-imports`) |
| ARH-06 | Hub-ul înregistrează și servește live pe LAN când cloud-ul nu e accesibil | E2E cu cloud-ul oprit 1 h: 0 segmente lipsă |
| ARH-07 | Fiecare componentă scrie loguri structurate JSON cu `correlation_id` și își raportează sănătatea | Test unit pe formatul logurilor; E2E citește starea |
| ARH-08 | Harta datelor (§3.3) e respectată | Test „santinelă”: după un scenariu E2E complet, parola camerei de test, numele camerei și cheia de recuperare nu apar în DB-ul cloud, loguri sau telemetrie (0 rezultate); parola camerei și tokenurile de stocare de test nu apar nici în obiectele urcate pe destinații, inclusiv în backup-ul automat (T38), decriptate cu `RI_g` și `RK` |
| ARH-09 | Niciun artefact livrat nu conține GPL/AGPL (A2); toate tipurile care trec granițe vin din `contract/` | SUC-06; CTR-01 |

## §4 Hub-ul

**Pe scurt (pentru proprietar).** Hub-ul e programul instalat lângă camere: pe un PC cu Windows sau, prin Docker, pe un mini PC, NAS ori Raspberry Pi. Doar hub-ul vorbește cu camerele. El înregistrează pe discul local, criptează dacă ați ales asta, urcă fișierele în destinațiile dvs. și trimite imaginea live la telefon, browser sau PC. Partea care scrie pe disc rulează separat, ca o problemă la scriere să nu blocheze restul. Orice cădere e repornită automat, iar golul rămas apare în timeline; nimic nu se ascunde. Hub-ul merge pe orice Raspberry Pi care rulează Linux pe 64 sau 32 de biți (Pi 2, 3, 3B+, 4, 5, Zero 2 W, Pi 400/500, CM3/4/5); aplicația arată câte camere recomandăm pentru model (Pi 5 ≈ 8, Pi 4 ≈ 4, celelalte ≈ 1–2) și doar avertizează peste. Pi 1, Zero și Zero W nu sunt suportate, iar programul explică de ce. Pe card SD se poate înregistra după un avertisment și o confirmare, iar hub-ul supraveghează cardul; recomandăm totuși un SSD, fiindcă cardul se uzează (§4.8, §4.9). Discul trebuie ales după câte zile vreți să păstrați local: o cameră obișnuită ocupă ≈ 43 GB pe zi (§4.8).

### 4.1 Principii

1. Doar hub-ul vorbește cu camerele. Spre internet deschide numai conexiuni de ieșire (P1, ARH-01, ARH-02).
2. Fiecare cameră are exact o sesiune RTSP per profil (main, sub). Live-ul, înregistrarea și MSE folosesc același flux (land §1.1 M6).
3. Fără transcodare video în MVP. Fluxul principal se înregistrează cum vine; substream-ul e H.264, setat la onboarding (A6).
4. Tot ce vine de la camere e tratat ca ostil (P7): parsare în Go, `recover` în fiecare goroutină de flux, fuzzing (§14).
5. Fișierele de pe disc sunt sursa de adevăr pentru segmente. Catalogul SQLite se poate reconstrui din ele.
6. Fiecare motor terț stă în spatele unei interfețe proprii (ARH-05).

### 4.2 Procese și integrarea go2rtc / MediaMTX

```
 Camere IP ──RTSP: 1 sesiune per profil──┐            ONVIF SOAP + snapshot HTTP
                                          ▼                     ▼
 ┌──────────── hubd (binar ipcam-hub; serviciu Windows / container Docker) ─────────────────────┐
 │ live (go2rtc pkg/: ingest, fan-out, MSE) ──în memorie──▶ session (Pion: 1 PeerConnection/sesiune)│◀═ WebRTC ═ clienți
 │ cameras · rules · outbox · storage · crypto · catalog · playback · config · audit · health      │── WSS ───▶ cloud
 │ cloudlink · lanapi · updater · supervizorul MediaMTX                                            │── HTTPS ─▶ destinații
 └───────┬──────────────────────────────────────────────────────────────────────▲─────────────┘
         │ restream RTSP 127.0.0.1 (port aleator, parolă nouă la fiecare pornire)    │ hook „segment închis” + API 127.0.0.1
         ▼                                                                           │
 ┌──── MediaMTX (proces separat; fork cu hub/third_party/PATCHES.md) ─────────────────┴────────┐
 │ citește restream-ul → fMP4 (părți 1 s, segmente 5 min), fsync pe parte, criptare age la scriere│──▶ disc: rec/…
 │ toate serverele proprii (RTSP/HLS/WebRTC/playback) sunt oprite                                │
 └──────────────────────────────────────────────────────────────────────────────────────────────┘
 ffmpeg (build LGPL): proces scurt, doar la cerere — miniaturi de rezervă; AAC→Opus când audio e pornit (T18)
```

**Decizie tehnică (confirmă și detaliază T10): go2rtc rulează ca bibliotecă în `hubd`; MediaMTX e un proces separat, supravegheat de `hubd`.**

- **go2rtc în proces:** A7 cere media tuturor plăcilor și canalul de date în același PeerConnection Pion; consumatorii `pkg/` ai go2rtc dau unitățile de acces direct din memorie, fără hop RTSP local și fără reîmpachetarea RTP-ului supradimensionat de pe RTSP/TCP. Serverul go2rtc stă în `internal/` și nu se poate importa (briefs: „Live video pipeline”); folosim doar `pkg/`, deci fork-ul rămâne mic. **Prețul:** un crash al `hubd` oprește și înregistrarea, fiindcă MediaMTX citește restream-ul din `hubd`; poarta HUB-31 îl măsoară.
- **MediaMTX în proces separat:** codul lui e tot în `internal/`; scrierea pe disc rămâne izolată și înlocuibilă; release-uri la 2–4 săptămâni (crit #2).
- **Patch-uri MediaMTX.** Fiecare are test propriu, e listat în `hub/third_party/PATCHES.md` și se rebazează la fiecare release adoptat: **MTX-1:** fsync la închiderea fiecărei părți (crit C12). **MTX-2:** scriere prin `age` STREAM când jobul are destinatari, ca discul local să nu primească video în clar când regula cere criptare (E3, T21), cu alinierea părților la bucățile de 64 KiB din §4.5. **MTX-3:** hook „segment închis” cu calea finală, peste `runOnRecordSegmentComplete`. **Plan B (HUB-30, G10):** recorder propriu pe gortsplib + mediacommon, în spatele interfeței `Recorder` (A2 permite asta).
- **Fork-ul go2rtc** (`hub/third_party/go2rtc`, bază v1.9.14): fără sursele `exec`/`echo`/`expr` și fără scrierea configurației prin API (land §3.1); serverele HTTP/WebRTC/RTSP ale go2rtc rămân oprite, iar restream-ul pentru MediaMTX e un server gortsplib din `live`, pe 127.0.0.1; adăugăm digest SHA-256 (K2).
- **Redarea, clipurile și exportul MP4 le face `playback` din `hubd`, nu MediaMTX.** Lucrează pe catalog: HLS fMP4 cu byte range-uri, decriptare din mers, remux cu mediacommon. Funcționează la fel pentru segmente locale, de pe NAS sau din cloud, criptate sau nu (F5). Tot `playback` taie clipurile de eveniment (§8.4), fiindcă MediaMTX nu are cheile private ca să citească segmentele criptate.

| Port | Unde | Modul | Scop |
|---|---|---|---|
| 7443/tcp | interfețe LAN private | `lanapi` | HTTPS cu certificat public per hub (P9, T12); până există domeniul, certificat autosemnat, acceptat doar de shell-urile native (T42); un client HTTP simplu primește 301 spre numele HTTPS. Excepție: pe un hub fără proprietar, pagina `/pair` (T36, §8.1) |
| 7445/udp | toate | `session` | ICE UDP mux: un singur port WebRTC, deci o singură regulă de firewall |
| 5353/udp | LAN | `lanapi`, `session` | mDNS: anunță `_ipcam-hub._tcp` (crit C3) și rezolvă candidații `.local` ai browserelor (crit C5) |
| aleator | 127.0.0.1 | `live`, MediaMTX | restream RTSP și API MediaMTX; credențialele se regenerează la fiecare pornire |

### 4.3 Module

Numele modulelor sunt cele din §3.5.

| Modul (`hub/internal/…`) | Responsabilitate | Depinde de |
|---|---|---|
| `cameras` | Codul detectorului (D5):<br>• descoperire: WS-Discovery, porturile din D1, amprentare;<br>• ONVIF prin kerberos-io/onvif: profiluri, PTZ probat, presetări, evenimente PullPoint (termen 10 min, reînnoire la 8 min, K3), offset de ceas, digest MD5/SHA-256, snapshot;<br>• substream H.264 cu acordul utilizatorului (A6) și avertismente de securitate (F2). | `config`, `crypto`, `health` |
| `live` | Ingest și fan-out cu `pkg/` din go2rtc: o conexiune per profil, backoff 1→60 s plus jitter (§13.2). Restream pe loopback, MSE fMP4, cache-ul ultimului GOP per flux (§8.3 pasul 6) și starea fluxului (biți/s, fps, ultimul keyframe). | `cameras`, `health` |
| `session` | Un PeerConnection Pion per sesiune (A7): sloturi video, canalul `rpc` și canalele `bulk/<id>`.<br>Regula H.265: oferit în SDP + primul cadru decodat (A6).<br>`Hello` legat de DTLS (CTR-10); lista de încredere (§10.5, §11.3). | `live`, `crypto`, `rpc` |
| `cloudlink` | Conexiune WSS de ieșire spre `wss://<domeniu-cloud>/link/v1` (§7.10): signaling, împerechere, certificat LAN, TURN, drepturi, telemetrie | `session`, `crypto` |
| `lanapi` | Endpoint-ul HTTPS de pe LAN (T12), pagina `/pair` a hub-ului fără proprietar (T36) și anunțul mDNS. Cere și reînnoiește certificatul per hub (§5.7); până există domeniul, generează un certificat autosemnat (T42). | `session`, `crypto` |
| `recorder` | Adaptor peste procesul MediaMTX: generează configurația, pornește și repornește procesul, primește hook-ul, detectează golurile | `live`, `rules`, `crypto` (doar chei publice) |
| `catalog` | SQLite (§4.6): segmente, evenimente, clipuri, miniaturi, goluri, locații per destinație, audit. Răspunde la interogările de timeline și reconciliază la pornire. | — |
| `rules` | Regulile S1/E2 ca funcții pure: plasamente (destinație, retenție, criptare, politică la plin), „criptarea câștigă”, modul doar-la-mișcare, retenție, curățare de urgență. Semantica e în §9. | `config`, `catalog` |
| `outbox` | Coada persistentă de upload și ștergere: reluare, backoff, verificare de mărime + hash (S3), limită de bandă, ferestre orare | `catalog`, `storage` |
| `storage` | Drivere `Destination` (contractul din land §3.2): local/NAS (T17), Google Drive, OneDrive, S3, WebDAV, SFTP. Copia de catalog pentru fiecare destinație (F6, T15). | `crypto` |
| `crypto` | Sigilare (TPM / DPAPI / software), cheia de identitate Ed25519, identitățile `age`, scriere age, citire aleatoare pe bucăți de 64 KiB, împachetarea cheilor pentru dispozitive. Algoritmii sunt în §10. | — |
| `playback` | HLS din catalog, `ReadRange` din cea mai rapidă locație (local → NAS → cloud, F5), decriptare, clipuri aliniate la keyframe, export MP4 (`moov` la început, scris într-o singură trecere, §8.5), miniaturi | `catalog`, `storage`, `crypto` |
| `rpc` | Handler-ele connect-go pentru `ipcam.hub.v1` (§7.8), servite prin puntea de pe canalul de date (T04). Verifică permisiunea fiecărui apel și scrie în `audit`. | modulele de domeniu |
| `config` | Starea dorită în SQLite, cu revizii. Validare protovalidate și schimbări atomice. Export/import pentru migrare (P9, `ipcam.backup.v1`). | `catalog`, `crypto` |
| `audit` | Jurnal append-only, înlănțuit prin hash (T13, P5): live, redare, export, PTZ, setări | `catalog` |
| `health` | Starea fiecărei componente și camere (flux, evenimente, înregistrare, upload), alerte (R1), metrici, verificarea ceasului, pachetul de diagnostic (R2) | — |
| `updater` | Verifică metadatele TUF (§11.4), pregătește slotul inactiv, comută, confirmă sănătatea, face rollback (P6, §13.4). Același binar e și lansator (§4.7). Pe Windows actualizează și aplicația Tauri, în sloturile din `<data>` (T27, §15.3). | `health` |

### 4.4 Interfețe Go (doar granițele)

Tipurile `pb.*` vin din `contract/` (§7). Toate metodele primesc `ctx context.Context` ca prim argument; aici e omis ca să rămână schița scurtă.

```go
package cameras // codul detectorului (D5)
type Camera interface {
	Probe() (*pb.CameraProbe, error)              // fluxuri, codec, GOP, PTZ probat, evenimente, auth, decalaj de ceas
	ApplySubstreamH264(p SubstreamProfile) error  // doar după acordul explicit (F2)
	PTZ() (PTZ, bool)                             // false dacă proba n-a confirmat PTZ (F3)
	Events() (<-chan Event, error)                // PullPoint + reînnoire; emite și EventsHealthy/Unhealthy
	Snapshot() ([]byte, error)                    // GetSnapshotUri (T18)
}
type PTZ interface {
	ContinuousMove(v Velocity, timeout time.Duration) error // Stop automat la expirare (§7.8)
	Stop() error
	Presets() ([]Preset, error); GotoPreset(id string) error; SetPreset(name string) (Preset, error); RemovePreset(id string) error
}

package live // LiveGateway din ARH-05; secretele trec doar prin memorie
type Gateway interface {
	Apply(want []StreamSpec) error; Subscribe(cam CameraID, p Profile) (Source, error) // Source: Codec(), Next() (AccessUnit, error), Close()
	RestreamURL(cam CameraID, p Profile) string; Status(cam CameraID, p Profile) StreamStatus
}
package recorder // Job: RestreamURL, segment 5 min, parte 1 s, destinatari age (doar chei publice)
type Recorder interface { Apply(jobs []Job) error; Closed() <-chan SegmentClosed; Status(cam CameraID, p Profile) RecStatus }
package rules // funcții pure; Expire nu șterge cel mai nou obiect și nici unul neurcat fără acordul regulii (F4)
type Engine interface { Place(o ObjectMeta, now time.Time) ([]Placement, error); Expire(now time.Time, inv Inventory) []Deletion }
package outbox // Enqueue rulează în aceeași tranzacție cu obiectul (S3)
type Queue interface { Enqueue(tx *sql.Tx, p Placement) error; Run() error }

package crypto // Backend(): "tpm" | "dpapi" | "software", afișat în aplicație
type Sealer interface { Seal(label string, plain []byte) ([]byte, error); Unseal(label string, sealed []byte) ([]byte, error); Backend() string }
type Recording interface {
	NewWriter(dst io.Writer, scope KeyScope) (io.WriteCloser, error)
	OpenReaderAt(src io.ReaderAt, size int64) (io.ReaderAt, int64, error) // acces aleator (E6)
}
package session // răspunsul SDP e semnat cu cheia de identitate a hub-ului
type Manager interface { Accept(offer *pb.SignalOffer) (*pb.SignalAnswer, error); Close(id SessionID, reason pb.ErrorReason) }
```

### 4.5 Comportamentul înregistrării

- **Segmente.** Părți de 1 s din `RestreamURL`. Un segment se închide la primul keyframe după 300 s sau când se schimbă codecul ori rezoluția. Fișierul `.part` se redenumește atomic, cu fsync pe director.
- **Criptare locală (E3, T21).** La deschiderea segmentului, `rules` evaluează plasamentul pe discul local pentru acea cameră și calitate, conservator: selectorul de evenimente se consideră potrivit, fiindcă evenimentele pot veni după. Dacă rezultatul e „criptat”, fișierul se scrie direct ca `.mp4.age` (MTX-2) și video-ul nu atinge discul în clar (HUB-04); copiile pentru destinațiile necriptate se decriptează din mers la upload (§9.7). La fsync-ul fiecărei părți se scriu toate bucățile age complete de 64 KiB; restul stă în RAM. MTX-2 aliniază sfârșitul unei părți la granița unei bucăți, cu o cutie fMP4 `free` de umplutură, doar când umplutura e ≤ 10 % din parte. La o pană se pierde cel mult max(1 parte, 64 KiB ÷ bitrate): ≈ 1 s la 4 Mbps, până la ≈ 2 s la 256 kbps și ≈ 4 s la 128 kbps. Pentru camerele criptate local al căror flux înregistrat e sub 512 kbps (substream, VBR de noapte, smart codec), aplicația afișează o notă despre această limită (SUC-05). MediaMTX primește doar chei publice. Dacă discul local rezolvă la „necriptat”, segmentul se scrie în clar, iar pentru țintele criptate se produce un singur `.age` la închidere (§9.7).
- **Timp.** Contează ceasul hub-ului, sincronizat NTP (T07). La fiecare 6 h, `health` îl compară cu 2 servere SNTP; peste 2 s diferență dă alertă.
- **Doar la mișcare (T22).** Înregistrarea locală e continuă, dar doar într-un inel de 1 h. `playback` taie clipurile de eveniment cu pre-roll și post-roll (§8.4); segmentele inelului se șterg la 1 h după închidere. Inelul se scrie criptat dacă oricare regulă a camerei cere criptare pe discul local.
- **Oprit.** Live-ul merge, dar nu se înregistrează. Comutarea manuală (O8.2) schimbă `recording_enabled` și intră în audit.
- **Miniaturi.** Snapshot ONVIF la începutul fiecărui segment și eveniment (T18); rezervă: ffmpeg cu `-skip_frame nokey`.
- **La pornire.** Fișierele `.part` în clar se trunchiază la ultima parte completă (`moof` + `mdat`) și se finalizează. La un `.age`, `hubd` recuperează cheia fișierului cu `RI_g`, decriptează coada, trunchiază plaintext-ul la ultima parte completă și re-criptează bucata finală cu marcajul de final, ca fișierul să rămână valid pentru `age` și să nu conțină o parte fMP4 tăiată. Coada pierdută intră în `gaps`. Fișierele se reconciliază cu baza de date. Dacă peste 50 % din segmente ar ieși „pierdute”, reconcilierea se oprește cu alertă (land §4.1 SP3).
- **Goluri.** Fiecare gol (cameră offline, repornire, disc plin, pană) are un motiv și apare în timeline.

### 4.6 Catalogul SQLite (schiță)

Catalogul folosește modernc.org/sqlite (Go pur), cu `journal_mode=WAL` și `synchronous=FULL`. Migrațiile merg numai înainte. Zilnic se face `VACUUM INTO state/backup/` și se păstrează ultimele 7 copii. ID-urile sunt UUIDv7, iar timpii sunt în ms UTC (T07).

| Tabel | Coloane principale | Rol |
|---|---|---|
| `cameras`, `camera_streams` | id, name, vendor, model, firmware, host, onvif_port, mode (continuous/motion/off), recording_enabled, audio_enabled (implicit 0), time_offset_ms, caps (pb), secret_id; per profil: codec, rezoluție, fps, gop, bitrate | camere (F2) |
| `secrets` | id, kind (camera/oauth/s3/webdav/sftp/smb), sealed_blob, sealer | toate secretele, numai sigilate |
| `segments` | id, camera_id, profile, start_ms, end_ms, bytes, codec, encrypted, key_generation, sha256, local_path, local_state (writing/complete/evicted/lost), has_event, motion_fallback (§8.4 pasul 6), `part_index` (BLOB: delte varint împachetate pentru offset, durată în ms și keyframe, ≈ 1–2 KB per segment); index pe (camera_id, profile, start_ms) | timeline, seek, byte range-uri HLS. Decizie tehnică: un rând per parte de 1 s ar însemna ≈ 500 M rânduri pe an la 16 camere |
| `events`, `gaps` | camera_id, source (onvif/manual), kind, start_ms, end_ms; goluri cu motiv | marcaje (F5), goluri |
| `clips`, `thumbnails`, `exports` | id, camera_id, interval, path, bytes, sha256, encrypted | obiecte derivate |
| `destinations`, `rules` | configurare + `secret_id`; filtre și plasamente (pb) | S1, E1–E2 |
| `placements`, `upload_sessions` | object_kind, object_id, destination_id, rule_ids, encrypt, retain_until, state (pending/uploading/verified/deleted/failed), remote_ref, remote_hash, attempts, next_attempt_at; `UploadState` serializat, offset | outbox, upload reluabil |
| `devices`, `keys`, `keyring_generations`, `enrollment_key` | account_id, platform, sign_pub, kx_pub, grant (pb), revoked_at; identități age împachetate (§10); per generație: g, stare (`HUB_ONLY` → `ON_DEVICE` → `IN_BLOB`), dispozitivele care au confirmat, `blob_version`; `EK` public + `bk_version`, sau `NONE` (§10.5) | lista de încredere, chei |
| `audit_log` | seq, ts_ms, device_id, action, target, details (pb), prev_hash, hash | P5, T13 |
| `alerts`, `settings`, `config_revisions` | kind, camera_id, raised/cleared/acked; cheie/valoare (pb); revizie, actor, diff | R1, `config` |

### 4.7 Platforme și rulare

**Același binar `ipcam-hub` e și lansator.** Alege slotul activ A/B cu semnătura verificată și pornește `hubd`. Pe Linux folosește `exec`; pe Windows îl pornește ca proces copil într-un Job Object cu kill-on-close. După 3 porniri eșuate în 10 min revine la slotul anterior. În Docker, sloturile stau în `/data`, așa că update-ul automat (P6) nu are nevoie de socket-ul Docker. **Decizie tehnică (precedența în Docker):** lansatorul rulează cea mai mare versiune, dintre cea din imagine și cea din slotul activ, a cărei semnătură TUF se verifică, și nu coboară niciodată versiunea; la prima pornire, versiunea din imagine devine slotul A. Justificare: un utilizator care trage o imagine mai nouă (sau folosește Watchtower) nu trebuie să rămână pe un slot vechi. Lansatorul însuși se actualizează doar cu imaginea (Docker) sau cu installer-ul semnat, aprobat prin UAC (Windows, §15.3).

| | Windows (serviciu) | Docker / Linux |
|---|---|---|
| Platforme | Windows 11 x64; Windows 10 22H2 x64 suportat cu avertisment (T33, OPN-15) | Linux x86-64, arm64 și armv7 (32 de biți), kernel ≥ 5.10, Docker Engine ≥ 24, cgroup v2; Raspberry Pi OS 64-bit sau 32-bit, pe modelele din §4.8 (nu pe armv6: Pi 1, Zero, Zero W, unde imaginea doar explică de ce, T39). Pe plăcile care pot rula arm64 (toate cele suportate, în afară de Pi 2 v1.1), ghidul recomandă Raspberry Pi OS 64-bit, fiindcă Docker Engine v28 e ultima versiune majoră cu pachete pentru Raspberry Pi OS 32-bit; pe 32 de biți, ghidul instalează Docker din pachetele Debian armhf (ARMv7); NAS Synology/QNAP care permit rețeaua gazdei |
| Pornire | Serviciul `ipcam-hub`, pornire automată. Recovery SCM: repornire după 5 s, de 3 ori; contorul se resetează după o zi. | `docker run -d --name ipcam-hub --network host --restart unless-stopped -v ipcam-data:/data ghcr.io/andreibesliu/ipcam-hub:stable` (sau compose) |
| Cont | Cont virtual `NT SERVICE\ipcam-hub`, nu LocalSystem | Utilizator ne-root (uid 10001), imagine distroless static |
| Sigilare | TPM prin NCrypt Platform Crypto Provider; fără TPM, DPAPI în contul serviciului | TPM dacă se dau `--device /dev/tpmrm0 --group-add <gid-ul grupului tss>` (dispozitivul e `root:tss 0660`, iar uid 10001 nu îl poate deschide doar cu `--device`); altfel cheie software, cu avertisment și ghid LUKS (P9) |
| Rețea | Installer-ul deschide 7443/tcp, 7445/udp și 5353/udp pe profilurile Private și Domain. Pe o rețea marcată Public apare un avertisment. | `--network host` e obligatoriu (WS-Discovery, mDNS, ICE). Fără multicast apare un avertisment. |
| Altele | Sleep, Windows Update, antivirus, căi lungi și NAS prin cale UNC (T17): §13.7.<br>Docker Desktop nu e suportat (crit C13).<br>Pe Windows 10 fără ESU, hub-ul afișează alerta „sistem fără update-uri de securitate” și recomandă Windows 11 sau un hub Docker (HUB-28). | `HEALTHCHECK` = `ipcam-hub healthcheck`. NAS prin volum montat (T17). |

### 4.8 Niveluri hardware și bugete

Comunitatea estimează câte camere duce fiecare tip de hub (land §3.3): Pi 5 ≈ 8–10, N100 ≈ 16–32, PC cu Windows ≈ 8–16. Tabelul ia capătul de jos al fiecărui interval. Pentru Pi 4 și modelele mai vechi, cifrele sunt estimările aprobate în O16. Poarta HUB-32 confirmă sau coboară aceste cifre; pe Raspberry Pi, ea rulează pe Pi-ul proprietarului, oricare ar fi modelul, iar Pi 5 se măsoară doar dacă e disponibil (Faza 0-bis).

**Camerele sunt o recomandare, nu o limită (O16).** Aplicația arată numărul recomandat pentru modelul hub-ului; peste el, adăugarea unei camere merge, cu un avertisment, niciodată cu un refuz (HUB-34). Rămân limite doar sesiunile simultane (HUB-26) și plafonul anti-abuz al planului `free` (T31). Singurele plăci nesuportate sunt cele armv6 din ultimul rând; un model suportat nu devine nesuportat după o poartă picată, ci primește un număr recomandat mai mic (minimum 1).

| Nivel | Exemplu | Minim | Camere recomandate în MVP (main 4 Mbps înregistrat + sub) | Sesiuni de vizualizare simultane |
|---|---|---|---|---|
| H1 | Raspberry Pi 5 (și Pi 500, CM5) + SSD USB3/NVMe | recomandat 4 GB RAM și SSD dimensionat după retenție (mai jos); hub-ul rulează pe orice variantă de memorie a modelului; card SD permis cu avertisment (T40) | ≈ 8 | 2 |
| H2 (recomandat) | Intel N100/N150 mini PC | 8 GB RAM, SSD/HDD dimensionat după retenție | 16 | 4 |
| H3 | orice PC cu Windows 11 (sau 10, cu avertisment) | 4 nuclee, 8 GB | 8 (16 cu ≥ 6 nuclee) | 3 |
| H4 | NAS x86-64/arm64 cu Docker | 4 GB RAM | 8 | 2 |
| H1b | Raspberry Pi 4 (și Pi 400, CM4), arm64 sau armv7 | recomandat SSD; orice variantă de memorie a modelului; card SD permis cu avertisment (T40) | ≈ 4 | 1 |
| H1c | Raspberry Pi 3, 3B+, 3A+, Zero 2 W, Pi 2 v1.2 (și Pi 2 v1.1, tot armv7, T39), CM3/CM3+; arm64 sau armv7 | recomandat SSD (Zero 2 W: card SD sau stocare USB); card SD permis cu avertisment (T40) | ≈ 1–2 | 1 |
| nesuportat | Raspberry Pi 1, Zero, Zero W și CM1 (armv6) | — | — : un singur nucleu și 512 MB nu pot înregistra și transmite fiabil; imaginea afișează explicația (T39) | — |

Raspberry Pi Pico e un microcontroler fără Linux, deci nu intră în discuție. Modelul exact îl citește hub-ul (T39); pe H1b și H1c, bugetele per cameră de mai jos nu sunt fixate dinainte: HUB-32 măsoară pe Pi-ul proprietarului numărul de camere la care CPU total rămâne ≤ 60 % și bugetul de RAM e respectat, iar numărul recomandat coboară la cel măsurat, dacă e mai mic.

**Discul local (Decizie tehnică).** Un flux principal de 4 Mbps ocupă ≈ 43 GB pe zi. Necesarul pentru șablonul „Simplu” (24/7 local, §9.5) e camere × zile de retenție locală × 43 GB: de exemplu 8 camere × 7 zile (`HOME_PRIVATE`) ≈ 2,4 TB, iar 4 camere × 3 zile ≈ 520 GB. Un SSD de 128 GB ajunge doar pentru sistem și un spool scurt. Același calcul se aplică pe un card SD, plus avertismentul de uzură (T40). Aplicația arată zilele realizabile pe discul ales (§9.6) și avertizează când retenția aleasă nu încape.

**Bugete de proiectare** (H.264, fără transcodare), validate în HUB-32:

| Resursă | Buget |
|---|---|
| CPU ingest + înregistrare | ≤ 3 % dintr-un nucleu per cameră pe H1; ≤ 1,5 % pe H2 |
| CPU per placă trimisă | ≤ 1 % (sub); ≤ 3 % (main) |
| CPU criptare + upload | ≤ 5 % per 10 Mbps |
| RAM | ≤ 250 MB de bază + ≤ 20 MB per cameră + ≤ 15 MB per sesiune |
| Disc | ≤ 1,03 × bitrate; ≤ 1 fsync/s per fișier deschis |
| Latență adăugată de hub | ≤ 150 ms p95 |
| Pornire la rece (boot) până la înregistrarea tuturor camerelor | ≤ 20 s; după un crash sau o ieșire a procesului `hubd`, ≤ 10 s (HUB-31, REL-02) |
| Timeline 24 h (catalog de 1 an × 16 camere) | ≤ 50 ms |

### 4.9 Structura pe disc

`<data>` este `C:\ProgramData\ipcam\` pe Windows (ACL: SYSTEM, Administrators, `NT SERVICE\ipcam-hub`; în `C:\Program Files\ipcam\` stau doar lansatorul semnat și stub-ul semnat al aplicației, §15.3, iar serviciul nu are drept de scriere acolo) și volumul `/data` în Docker. `rec/` se poate muta pe alt disc local; căile din cloud sunt în §9.

```
<data>/
├── app/{a,b}/    sloturi de update (binare verificate; pe Windows și ui/, aplicația Tauri, T27) + active.json
├── state/        catalog.db (+ -wal), backup/ · keys/ (identitate + cheie de sigilare, sigilate) · reserve.bin (512 MiB, §13.8)
│                 certs/ (certificatul LAN; cheia privată sigilată) · run/ (config MediaMTX fără parole de camere, 0600)
├── rec/<camera_id>/<main|sub>/<AAAA-LL-ZZ UTC>/<HHMMSS>_<segment_id>.mp4[.age][.part]
├── clips/<camera_id>/<AAAA-LL-ZZ>/<event_id>.mp4[.age]   · thumbs/<camera_id>/<AAAA-LL-ZZ>/<ts_ms>.jpg[.age]
├── exports/<export_id>.mp4[.age]   · tmp/ (golit la pornire; nu primește video în clar al unei camere cu disc local criptat)
├── cache/        cache-ul de redare: doar octeți .age sau octeți re-criptați cu o cheie efemeră din RAM (§8.5)
├── pairing/      ticket.txt: link-ul de împerechere, QR ASCII și codul (§8.1); pe Windows, ACL doar Administrators + utilizatorul care a instalat
└── logs/         hub-AAAAMMZZ.jsonl, rotite 10 × 20 MB
```

**Card SD (T40, HUB-17).** Când `rec/` stă pe un card SD (`mmcblk*`, de obicei la un Raspberry Pi fără SSD), hub-ul nu scrie niciun segment până când utilizatorul confirmă în aplicație avertismentul: „Cardul SD se uzează repede la înregistrare continuă și poate pierde înregistrări fără să vă anunțe. Recomandăm un SSD pe USB.” Confirmarea intră în audit. Cât timp înregistrează pe card, hub-ul supraveghează cardul (erori I/O, remontare doar-citire, latența fsync) și ridică alertele din §13.1 și §13.3.

### 4.10 Cerințe

| ID | Cerință | Test |
|---|---|---|
| HUB-01 | O singură sesiune RTSP per cameră și profil, oricâți clienți privesc, înregistrează sau cer MSE | Simulatorul numără sesiunile cu 4 vizualizări + înregistrare + MSE simultan |
| HUB-02 | Pe LAN, hub-ul ascultă doar pe 7443/tcp, 7445/udp și 5353/udp; restul doar pe 127.0.0.1 (ARH-02 + mDNS) | Scanare de socket-uri în E2E, pe Windows și Linux |
| HUB-03 | Parolele camerelor nu apar în clar pe disc, în configurația MediaMTX sau în loguri | Testul santinelă (ARH-08) pe tot `<data>` |
| HUB-04 | O cameră cu criptare locală nu scrie niciodată octeți video în clar pe disc: nici la înregistrare, nici în cache-ul de redare, în `tmp/` sau la export (§8.5) | E2E pe un volum loop: în imaginea brută se caută `ftyp`/`moof` și prefixele NAL ale fluxului de test → 0 potriviri, verificat după înregistrare, după 20 de salturi în redare, după o redare cu planul B din G13 și după un export |
| HUB-05 | Latența adăugată de hub ≤ 150 ms p95 | Laborator: ora arsă în cadru + trace pe hub |
| HUB-06 | Primul cadru pe substream după `SetView` pe LAN ≤ 1,5 s p90 (keyframe = fps, A6) | Playwright, 50 de porniri |
| HUB-07 | La pană de curent se pierd ≤ 1 s în mediană și ≤ 2 s maxim; la fluxurile criptate local sub 512 kbps, maximul e max(1 parte, 64 KiB ÷ bitrate) (§4.5); catalogul se reconciliază singur și niciun fișier nu rămâne cu o parte fMP4 tăiată (SUC-05) | 20 × kill -9 în CI; în Faza 0, setul MI1: rulări automate plus câte 5 tăieri reale de curent pe dispozitivele proprietarului (HUB-30, T43); câte o rulare la 256 kbps, cu criptare locală, în fiecare set automat din MI1 și în TST-09 verifică limita |
| HUB-08 | Fiecare gol are un motiv și apare în timeline | E2E: cameră oprită 30 s → rând în `gaps` + marcaj în UI |
| HUB-09 | Dacă peste 50 % din segmente ar ieși „pierdute”, reconcilierea se oprește cu alertă | Unit + E2E cu discul schimbat |
| HUB-10 | Doar capabilitățile PTZ probate sunt expuse; `ContinuousMove` se oprește singur la expirare | Simulator ONVIF: fără Stop de la client → Stop trimis în ≤ timeout + 200 ms |
| HUB-11 | Abonamentele ONVIF se reînnoiesc la timp; „evenimente OK” devine fals în ≤ 60 s după pierderea lor | Simulator cu expirări și limită de 5 PullPoint |
| HUB-12 | Ceas decalat > 2 s → alertă; timpii segmentelor vin de la hub | Unit cu ceas fals |
| HUB-13 | MediaMTX căzut e repornit în ≤ 3 s; camerele se reconectează cu backoff 1→60 s plus jitter | Test de haos |
| HUB-14 | Un update care nu pornește de 3 ori în 10 min revine automat la versiunea anterioară | E2E cu un build stricat, pe Windows și Docker |
| HUB-15 | Pachetul de diagnostic nu conține imagini, parole, tokenuri sau cheia de recuperare | Testul santinelă pe arhivă |
| HUB-16 | Bugetele din §4.8 sunt respectate | Benchmark nightly pe `ipcam-h1` (§14.7; doar joburi programate din `main`); o regresie > 10 % deschide automat un issue care blochează următorul release pe canalul beta |
| HUB-17 | Înregistrarea pe card SD e permisă (O16, T40): până la confirmarea explicită a avertismentului despre uzură, care recomandă un SSD, hub-ul nu scrie niciun segment pe card; confirmarea intră în audit; cât timp `rec/` e pe card, `health` urmărește erorile I/O, remontarea doar-citire și latența fsync și ridică alertele din §13.1 și §13.3 | Unit pe detecția dispozitivului de bloc (`mmcblk*`, numărul major 179, din `/proc/self/mountinfo`); E2E pe un volum marcat „card SD” prin configurația de test: 0 fișiere în `rec/` până la confirmare, apoi înregistrare și rând în audit; erori injectate cu `dm-flakey` → DEGRADAT, apoi EȘUAT și alerta „card SD” în ≤ 60 s; volum remontat doar-citire → EȘUAT; pe Pi-ul proprietarului, dacă înregistrează pe card, proba de 72 h din MI2 raportează și starea cardului |
| HUB-18 | Serviciul Windows rulează în contul virtual, cu ACL-urile din §4.9 | Runner Windows: `sc qc` + verificarea ACL |
| HUB-19 | Imaginea Docker e multi-arch și ne-root; fără rețeaua gazdei apare avertisment; lansatorul respectă precedența din §4.7 și nu coboară versiunea; TPM-ul e accesibil cu `--group-add` | CI: build amd64/arm64/arm/v7, cu testele unit arm/v7 sub QEMU user-mode (T39); intrarea linux/arm/v6, rulată sub QEMU, afișează explicația, nu pornește hub-ul și nu intră în buclă de repornire; rulare fără host network → alertă; imagine nouă + slot vechi → rulează imaginea; imagine veche + slot nou verificat → rulează slotul; container cu `/dev/tpmrm0` simulat (`root:tss 0660`) → sigilare `tpm` |
| HUB-20 | Timeline 24 h ≤ 50 ms dintr-un catalog de un an × 16 camere; `catalog.db` ≤ 2 GB | Benchmark pe un catalog sintetic de un an, cu schema din §4.6 (`part_index`) |
| HUB-21 | O modificare în jurnalul de audit e detectată | Unit: un rând schimbat → verificarea lanțului pică |
| HUB-22 | O configurație invalidă nu se aplică nici măcar parțial | Unit pe `config.Apply` cu erori injectate |
| HUB-23 | Migrarea prin exportul manual (§13.5) păstrează `hub_id`, catalogul, tokenurile și cheile; hub-ul vechi e respins când se reconectează | E2E între două containere |
| HUB-24 | Fără cloud, înregistrarea și live-ul pe LAN (prin `lanapi`) continuă (ARH-06) | E2E cu cloud-ul oprit 1 h |
| HUB-25 | Doar la mișcare: clipurile au cel puțin pre-roll-ul și post-roll-ul setate (FLW-14); segmentele inelului local se șterg la ≤ 1 h după închidere (T22) | E2E cu evenimente simulate |
| HUB-26 | Peste limita de sesiuni a nivelului hardware, o sesiune nouă primește `RESOURCE_EXHAUSTED` | E2E |
| HUB-27 | Primul cadru pe fluxul principal (ecran complet) pe LAN ≤ 1,5 s p90, cu cache-ul ultimului GOP (§8.3 pasul 6); până la primul cadru MAIN, placa arată substream-ul | Playwright, 50 de porniri, cu GOP-ul fluxului principal de 2 s și de 4 s |
| HUB-28 | Pe Windows 10, `health` ridică alerta „sistem fără update-uri de securitate”; cât timp Windows 10 e suportat (OPN-15), hub-ul trece testele de fum pe el | Unit pe detecția versiunii și a ESU; nightly pe runner-ul VM Windows 10 (§14.7): instalare, serviciu, 10 min de înregistrare |
| HUB-30 | **Poartă Faza 0 — recorderul (G10).** MediaMTX cu MTX-1 și MTX-2, după procedura MI1 (§16.4, T43): rulări automate (kill -9 și oprire fără scrierea cache-ului, câte 20 per tip de oprire și gazdă, în laborator și pe dispozitivele proprietarului; numerele exacte în MI1) și câte 5 tăieri reale de curent, făcute de mână de proprietar, pe PC-ul lui cu Windows 11 (H3) și pe Pi-ul lui (oricare model suportat; pe SSD sau, cu T40, pe card SD). Pi 5 (H1) și N100 (H2) doar dacă sunt disponibile (Faza 0-bis). Criptare locală pe jumătate din rulările automate și pe tăierile reale 1, 3 și 5. Se confirmă și că MTX-2 scrie strict secvențial, fără rescrierea fișierului | *Trece:* pe fiecare set de rulări, inclusiv pe fiecare set de 5 tăieri reale: mediana ≤ 1 s, maximul ≤ 2 s (SUC-05); reconciliere fără intervenție, 0 fișiere ilizibile, HUB-04 = 0. *Pică:* recorder propriu pe gortsplib/mediacommon în spatele `Recorder`, retestat înainte de Faza 1 |
| HUB-31 | **Poartă Faza 0 — înregistrarea depinde de `hubd` (G11).** Pe fiecare gazdă de test (PC-ul și Pi-ul proprietarului): 20 × kill -9 `hubd` + o probă de 72 h cu numărul recomandat de camere al gazdei (8 pe PC; pe Pi, numărul modelului, §4.8) | *Trece:* înregistrarea reia în ≤ 10 s de fiecare dată, cu 0 crash-uri `hubd` în 72 h. *Pică:* ingest-ul se inversează — MediaMTX trage fluxul de la cameră, iar `live` citește restream-ul MediaMTX. Se schimbă doar configurația, iar sesiunea RTSP rămâne unică. |
| HUB-32 | **Poartă Faza 0 — nivelurile hardware (G12).** Pe fiecare gazdă de test, numărul ei recomandat de camere (§4.8): 8 pe PC-ul Windows 11 al proprietarului (H3) și numărul modelului pe Pi-ul lui; 8 pe Pi 5 (H1) și 16 pe N100 (H2), doar dacă sunt disponibile (Faza 0-bis). În paralel, timp de 1 h: sesiunile live ale nivelului (§4.8), 1 sesiune de redare, 1 export și upload criptat de 10 Mbps (pe H1b și H1c, cât debitul camerelor recomandate) | *Trece:* bugetele din §4.8 sunt respectate și CPU total ≤ 60 %. *Pică:* numărul recomandat al nivelului sau al modelului coboară la numărul de camere care trece (minimum 1); niciun model suportat nu devine nesuportat (O16) |
| HUB-33 | **Poartă Faza 0 — seek în `age` pe hub (E6, G13).** 1.000 de citiri la offset-uri aleatoare; debitul de criptare și clienții sunt în ENC-08 | *Trece:* rezultatul e identic cu cel al uneltei `age` standard și fiecare citire ia ≤ 50 ms pe Pi-ul de test (și pe H1, dacă e disponibil). *Pică:* hub-ul decriptează tot segmentul în bufferul de redare din RAM (≤ 256 MiB, §8.5), nu pe disc, înainte să-l servească, cu formatul neschimbat; dacă primul cadru după salt depășește atunci 1,5 s, segmentele criptate se scurtează la 2 min |
| HUB-34 | Pe Raspberry Pi, hub-ul citește modelul din `/proc/cpuinfo` (`Model`, `Revision`; niciodată `Serial`) și îl raportează în `GetHubInfo`; aplicația afișează numărul recomandat de camere al modelului (§4.8, O16). Peste el (la un interval, ca ≈ 1–2, peste capătul de sus), o cameră nouă se adaugă, cu un avertisment care spune numărul recomandat; nu există refuz în afara plafonului anti-abuz (T31). Un model pe care hub-ul nu îl cunoaște apare cu numele citit, fără număr recomandat | Unit: fiecare model din tabelul §4.8 → numărul lui; E2E cu `/proc/cpuinfo` simulat („Raspberry Pi 3 Model B Rev 1.2”) și 3 camere → a treia e adăugată, iar avertismentul apare; model necunoscut → fără număr, fără avertisment |

## §5 Cloud-ul

**Pe scurt (pentru proprietar).** Cloud-ul nostru e mic. Ține conturile, știe ce hub e al cui, pune aplicația în legătură cu hub-ul și dă acces temporar la releul TURN. Nu vede video, parole de camere, tokenuri Drive/OneDrive (în planul de bază; dacă poarta G15 pică, vezi §5.6 B2) sau chei de înregistrări în clar. Dacă pică, hub-ul înregistrează mai departe și totul merge pe LAN. Serverele costă ≈ 10–20 €/lună la 100 de utilizatori; cu Cloudflare și email, ≈ 30–70 €/lună (§15.6).

### 5.1 Arhitectură și module

**Decizie tehnică (T30): monolit modular.** Cloud-ul e un singur binar Go, `ipcam-cloud`, cu trei roluri alese la pornire:

- `api`: Connect peste HTTPS (T03);
- `gateway`: WSS pentru hub-uri (T05);
- `worker`: email, ACME, DNS, ștergeri, agregări.

Avem un singur artefact de testat, iar rolurile se scalează separat. Baza de date e PostgreSQL, accesată prin pgx v5 + sqlc, cu migrații goose (§3.4).

| Modul (`cloud/internal/…`) | Responsabilitate | Faza |
|---|---|---|
| `auth`, `accounts` | Login cu cheie derivată pe client (T14), TOTP, passkeys, Google/Apple, sesiuni, protecție la credential stuffing; profil, export de date, ștergerea contului (P10) | 2 |
| `devices` | Dispozitivele logate, cheile lor publice, revocare (trimite `DeviceRevoked` la hub) | 2 |
| `pairing` | Coduri în stil RFC 8628, revendicare prin QR cu dovadă HMAC (T19), legarea hub → cont | 2 |
| `presence`, `signaling` | Online/offline, rutare între noduri, relay pentru ofertă/răspuns/candidați, cu autorizare per sesiune | 2 |
| `turn` | Credențiale TURN: Cloudflare acum, self-hosted mai târziu | 2 |
| `certs` | Certificat LAN per hub prin ACME DNS-01; DNS autoritar pentru `<domeniu-hub>` (§5.7) | 2, la început (T28, T42) |
| `oauth` | Pagina statică `/oauth/callback` (T11); proxy fără stare (planul B2, §5.6) doar dacă pică poarta din §3.3 | 3 |
| `keybackup` | Blobul criptat pe client (E7.3, T16) | 3 |
| `entitlements` | Planuri, drepturi, token semnat pentru hub (O7) | 2 |
| `audit`, `telemetry` | Evenimentele de securitate ale contului (T13); telemetria minimă (§12.2, §13.6) | 2 |
| `push` | Rezervat pentru după MVP (P8, §11.6); nu se construiește în MVP | — |

La începutul Fazei 2, când există domeniul (Faza 0-bis), vin înaintea restului `certs`, autentificarea hub-ului pe `link/v1` (rolul `gateway`, fără conturi) și găzduirea statică a SPA-ului (T28, modificată de T42; §16.6). În Faza 1 nu rulează niciun modul cloud.

### 5.2 Conturi și autentificare (P4)

- **Email + parolă (T14).** Clientul derivă cu Argon2id o cheie de autentificare și una de împachetare (§10.3). Serverul primește doar cheia de autentificare și păstrează un hash Argon2id al ei. Ca să nu se poată afla ce conturi există, `Prelogin` (§7.9) răspunde și pentru emailuri inexistente, cu sare și parametri deterministici (HMAC cu un secret de server peste email). Parola are minimum 10 caractere. Clientul o verifică direct la „Pwned Passwords” (`api.pwnedpasswords.com`), prin k-anonimat, cu antetul `Add-Padding`. **Decizie tehnică:** interogarea nu trece prin cloud-ul nostru, fiindcă prefixul hash-ului parolei, pus lângă hash-ul `auth_key` din baza noastră, ar ușura spargerea offline; serviciul e listat în CSP (§6.7) și în §12.2.
- **Factori.** TOTP (RFC 6238), cu 10 coduri de recuperare. Passkeys (go-webauthn, RP ID `<domeniu>`), folosite fără parolă sau ca al doilea factor; pe mobil prin `auth-native` (§6.6). Google și Sign in with Apple prin OIDC. Cloud-ul e client confidențial; aplicația folosește browserul de sistem și un cod de unică folosință legat de PKCE. Conturile Google/Apple setează „parola pentru înregistrări” (E7) abia când pornesc criptarea, dacă nu au setat-o mai devreme ca „parolă de rezervă pentru acces” (§10.3, OPN-13). Conturile fără parolă (doar passkey) primesc același tratament ca Google/Apple. Login-ul cu passkey, Google sau Apple nu deschide backup-ul de chei: aplicația cere parola contului sau „parola pentru înregistrări” abia când are nevoie de chei.
- **Credential stuffing.** Limite: 20 de încercări / 10 min per IP; 10 / 10 min per cont, apoi întârziere exponențială. **Decizie tehnică (T30, P4):** un dispozitiv necunoscut logat doar cu parolă confirmă un cod de 6 cifre primit pe email. Fiecare dispozitiv nou declanșează un email.
- **Sesiuni (Decizie tehnică, T30).** Access token PASETO v4.public, valabil 10 min, ținut doar în memorie. Refresh token opac de 256 biți, rotit la fiecare folosire. Expiră după 30 de zile de inactivitate și oricum după 90. Dacă un token rotit e refolosit, se revocă toată familia lui. Pe web stă în cookie `HttpOnly; Secure; SameSite=Strict`, limitat la ruta `RefreshSession`; pe nativ în `secure-storage`.
- **Schimbarea parametrilor KDF** (de exemplu după G19): `AccountService.ChangeKdfParams` cere autentificare recentă și primește noul hash al `auth_key`, parametrii noi și `BK` reîmpachetat cu noul `KEK`, într-o singură tranzacție.
- **Resetarea parolei** schimbă doar cheia de autentificare. Backup-ul de chei rămâne închis (E7.3) până îl reîmpachetează un dispozitiv care încă are cheia de backup (§10.4).

### 5.3 Registrul hub-urilor și împerecherea (F1)

Fluxul complet, cu pașii din aplicație, e în §8.1. Cloud-ul are aici trei roluri:

1. **Registrul.** La prima pornire, hub-ul își generează `hub_id` (UUIDv7, T07), cheia de identitate `HIK` și `dns_label` (T25), se autentifică pe `link/v1`; certificatul LAN îl primește după revendicare (§5.7). Cloud-ul păstrează cheia publică `HIK` și o compară cu cea pe care o trimite aplicația la revendicare.
2. **Revendicarea.** Pe calea QR, cloud-ul primește de la aplicație doar dovada HMAC (`ClaimHub`) și o trimite hub-ului ca `PairingClaim`; nu cunoaște secretul. Pe calea cod, cloud-ul emite codul de 8 caractere la cererea hub-ului (`PairingCodeRequest`), îl potrivește cu `ClaimHub` și trimite aplicației cheia hub-ului, pe care utilizatorul o confirmă prin amprenta de 64 de biți (T24). Codul e legat de conexiunea `link` a hub-ului, deci hub-ul nu are nevoie să interogheze periodic.
3. **Limite și proprietate.** Cel mult 5 revendicări greșite pe cont la 15 min și 20 pe IP pe oră (`PAIRING_RATE_LIMITED`). Un hub are un singur cont (O8.8). Scos din cont (`RemoveHub`), își păstrează înregistrările și revine în modul de împerechere.

### 5.4 Signaling și prezență

- **Hub:** `link/v1`, cu provocare semnată (T05) și ping la 25 s. **Decizie tehnică:** hub-ul devine offline după 75 s fără mesaje și online la prima autentificare reușită. `hubs.last_seen` se scrie cel mult o dată la 5 min.
- **Clienți:** stream-urile Connect `SignalingService` și `HubRegistryService.WatchPresence` (§7.9). **Decizie tehnică:** fiecare stream de server trimite un mesaj `Heartbeat` la cel mult 30 s, fiindcă proxy-ul Cloudflare închide conexiunile inactive după ≈ 100 s; clientul se reconectează cu backoff și reia de la `last_event_id` (CLD-23).
- **Autorizare:** `CreateSession` verifică sesiunea contului, apartenența la hub și dreptul la încă o sesiune remote, apoi transmite oferta. Clientul verifică răspunsul semnat cu cheia fixată.
- **Rutare între noduri (Decizie tehnică):** `presence` (`hub_id → nod`) + `LISTEN/NOTIFY`, fără Redis. `NOTIFY` poartă doar `{target_node, message_id}`, fiindcă PostgreSQL limitează payload-ul la 8.000 de octeți, iar o ofertă SDP cu 10 m-line-uri are 15–30 KB. Mesajul stă într-un tabel `UNLOGGED` `signal_relay(id uuid, hub_id, payload bytea, expires_at)`, pe care nodul țintă îl citește și îl șterge (CLD-21).
- **Limite:** mesaj ≤ 64 KiB; 50 de mesaje/s per conexiune.
- **Loguri:** fără SDP sau candidați, doar tipul perechii ICE; IP-urile apar doar în logurile de acces (§12.2).

### 5.5 TURN

| Aspect | Decizie |
|---|---|
| Furnizor | Cloudflare Realtime TURN: $0,05/GB după 1 TB/lună gratuit, facturat doar pe traficul edge → client (crit #4) |
| Când trecem pe self-hosted | Peste 2 TB/lună de trafic relay, două luni la rând (land §3.1). Atunci rulăm coturn 4.14+ sau pion/turn pe 2 VPS din UE, la 2 furnizori, cu Cloudflare ca rezervă. |
| Abstracție | `TurnProvider.Mint(ctx, subject, ttl) (IceServers, error)`, cu implementările `cloudflare` și `restsecret` (coturn `use-auth-secret`) |
| Credențiale | Una per sesiune, TTL 12 h (**Decizie tehnică**; Cloudflare permite până la 48 h). Sesiunile mai lungi primesc credențiale noi și fac ICE restart. |
| Porturi | UDP 3478, TCP 3478, TLS 443 |
| Hub | Cere TURN doar dacă proba STUN eșuează (§7.10) |
| Self-hosted | Respinge peer-ii privați, ULA și link-local (anti-SSRF) |
| Consum | Clientul raportează octeții trecuți prin perechea relay, fără IP-uri. Se adună per cont pentru `relay_gb_month`. |

### 5.6 OAuth pentru furnizorii de stocare

Tokenurile stau doar pe hub (T11). Poarta care decide între planuri și ordinea planurilor B sunt în §3.3.

| Furnizor | Planul A (T11) | Dacă poarta pică |
|---|---|---|
| Google Drive (`drive.file`) | iOS, Android, Windows: login cu PKCE pe dispozitiv, cu clientul OAuth al platformei (T34, tabelul din §8.7); verifier-ul e generat și păstrat de hub, iar aplicația predă doar `code` + `state` pe canalul de date. Web: fără plan A; hub-ul rulează device flow (B1 prin design, T34) | B1: device flow pornit de hub (Google îl permite cu `drive.file`; briefs: stocare) |
| OneDrive personal | PKCE pe dispozitiv pe toate platformele, inclusiv web, cu redirect-ul înregistrat ca „Mobile and desktop applications” (§8.7); `/oauth/callback` doar predă codul aplicației | B1: device code flow pornit de hub |
| OneDrive business | ca OneDrive personal: PKCE pe dispozitiv, codul ajunge la hub (crit C19) | B2; device code nu se folosește (crit C19). Dacă Conditional Access blochează refresh-ul de pe hub: poarta FLW-23 (§8.7) |
| S3, WebDAV, SFTP, SMB | Fără OAuth: cheile ajung la hub pe canalul de date | — |
| Dropbox, Box (după MVP) | Dropbox: PKCE pe telefon → hub. Box are secret confidențial și refresh de unică folosință → B2 | — |

**B2 = proxy fără stare.**

- Hub-ul trimite pe `link/v1` mesajele `OAuthExchange` și `OAuthRefresh`, rezervate în §7.10 și activate doar odată cu B2.
- Cloud-ul adaugă `client_secret`, apelează furnizorul și trimite hub-ului răspunsul criptat cu cheia lui.
- Tokenul nu atinge discul, baza de date sau logurile.
- Limită: 60 de cereri pe oră per hub.
- Harta datelor (§3.3) și politica de confidențialitate se actualizează înainte de activare.

**F6.** Fără hub, aplicația se loghează direct la furnizor, cu un client al aceluiași proiect Google sau al aceleiași aplicații Entra. Tokenul rămâne pe dispozitiv (§8.6). Faptul că acest client vede fișierele create de clientul hub-ului se verifică în G15 (MI6); dacă nu le vede, F6 folosește tipul de client al hub-ului (de exemplu device flow pe dispozitivul care redă), iar rezultatul se scrie în §2.2.

### 5.7 Backup de chei, drepturi, certificate LAN, telemetrie

- **Backup de chei (E7.3).** `PutBackup`/`GetBackup` (§7.9) primesc un blob opac `ipcam.backup.v1`: maximum 64 KiB, cu `kind` (`account_password` sau `recordings_password`), parametrii KDF și sarea; scriere cu CAS pe versiune; se păstrează ultimele 3 versiuni; **Decizie tehnică:** citirea cere o autentificare din ultimele 5 min, maximum 10 citiri pe zi, cu email la fiecare citire.
- **Drepturi (O7).** Tabelele `plans` (limite în JSONB) și `entitlements` (cont, plan, sursă `free`/`promo`; `app_store`/`play`/`web` rezervate; perioadă de valabilitate). **Decizie tehnică:** planul `free` are în MVP doar limite anti-abuz, schimbabile din configurare: `max_hubs` = 5, `max_cameras_per_hub` = 32, `max_remote_sessions` = 4, `relay_gb_month` = 100. Peste ultima limită apare doar o alertă internă. `EntitlementsToken` e semnat Ed25519 și valabil 7 zile. Hub-ul îl primește prin `EntitlementsUpdate`, îl verifică offline și are 30 de zile de grație. Drepturile se verifică la împerechere, la `CreateSession` și la emiterea credențialelor TURN. Funcțiile locale nu depind de ele.
- **Certificate LAN (P9, T25, T35).** Numele au forma `<ip-cu-liniuțe>.<dns_label>.<domeniu-hub>` (ex. `192-168-1-10.k3m9….<domeniu-hub>`), cu IP-ul codat în nume, ca la Plex; `dns_label` are 16 octeți aleatori în base32, nu `hub_id` (§11.5). **Decizie tehnică (T35):** `<domeniu-hub>` e un domeniu înregistrabil separat de `<domeniu>` (cel al SPA-ului), delegat DNS-ului nostru (`worker`, miekg/dns, 2 VPS). DNS-ul servește `_acme-challenge` și răspunde algoritmic doar pentru adrese IPv4 din RFC 1918, 100.64.0.0/10 și 169.254.0.0/16 și doar pentru valorile `dns_label` existente în `hubs.dns_label`; orice altă cerere primește NXDOMAIN. Așa zona nu devine un rezolvitor wildcard deschis, bun pentru phishing, iar o eventuală listă neagră pe zona hub-urilor nu atinge SPA-ul. Nu există înregistrări per hub și nu se stochează IP-uri LAN. Hub-ul trimite un CSR pentru `*.<dns_label>.<domeniu-hub>`. **Emiterea e limitată (Decizie tehnică):** doar pentru hub-uri revendicate (`ClaimHub`; înainte să existe `ClaimHub`, la începutul Fazei 2, doar hub-urile de test de pe o listă permisă), al căror link e autentificat de cel puțin 10 min, cu cote per IP public și per ASN (de exemplu 3 hub-uri noi per IP pe zi); la `rateLimited` de la Let's Encrypt, cererile intră într-o coadă cu backoff. Justificare: altfel oricine generează chei, se prezintă pe `link/v1` ca hub și consumă limita Let's Encrypt de 50 de certificate noi per domeniu înregistrat la 7 zile. Cloud-ul face ACME DNS-01 la Let's Encrypt (cu cerere de limite mai mari), cu Google Trust Services ca rezervă. Cheia privată rămâne pe hub, iar reînnoirea are loc la 2/3 din durata de viață. Regulile CAA, monitorul CT și cererea Public Suffix List sunt în §11.5. Dacă routerul filtrează răspunsurile DNS cu IP privat (protecție la DNS rebinding), se pierde doar signaling-ul local fără cloud; media pe LAN merge în continuare prin candidații ICE locali (RSK-25).
- **Telemetrie minimă.** Hub-ul trimite zilnic, prin `TelemetryBatch`, versiunea, OS-ul și arhitectura, nivelul hardware, numărul de camere pe intervale, marca/modelul/firmware-ul fără IP, MAC sau serie, contoare de erori și upload, tipurile de perechi ICE, crash-urile și rollback-urile. Clienții nu folosesc SDK-uri de analytics. Categoriile (A obligatorie, B cu opt-out), temeiurile și retenția sunt în §12.2; conținutul telemetriei clienților e în §13.6.

### 5.8 Modelul de date PostgreSQL (schiță)

| Tabel | Coloane principale |
|---|---|
| `accounts` | id, email (citext, unic), email_verified_at, locale, created_at, disabled_at, delete_after |
| `password_credentials` | account_id, kdf_params, client_salt, server_hash (Argon2id peste cheia de autentificare) |
| `totp_factors`, `recovery_codes`, `email_codes` | secret criptat cu cheia aplicației, confirmed_at; hash-uri, used_at; scop, code_hash, expires_at, attempts |
| `webauthn_credentials`, `federated_identities` | credential_id, public_key, sign_count, aaguid, last_used_at; provider (google/apple), subject |
| `refresh_tokens` | id, family_id, account_id, device_id, hash, issued_at, last_used_at, rotated_at, revoked_at |
| `devices` | id, account_id, platform, name, sign_pub, kx_pub, app_version, last_seen, revoked_at |
| `hubs`, `hub_memberships` | id (UUIDv7), identity_pub, dns_label, epoch, owner_account_id, name, version, channel, last_seen; hub_id, account_id, role (în MVP doar owner; rolurile sunt în §11.3) |
| `pairing_codes`, `presence` | user_code_hash, hub_id, expires_at, attempts, claimed_by; hub_id, gateway_node, connected_at |
| `key_backups` | account_id, version, kind, kdf_params, salt, blob, wrapped_bk, bk_for_rk, created_at (ultimele 3; §10.3) |
| `plans`, `entitlements`, `turn_usage_monthly` | §5.7; account_id, month, relay_bytes |
| `lan_certs`, `acme_challenges` | hub_id, serial, not_after; name, txt_value, expires_at |
| `security_events` | account_id, kind, device_id, ip_trunc (/24 sau /48), ts (T13); `audit_anchors`: hub_id, seq, hash, ts (§11.7) |
| `telemetry_hub_daily`, `telemetry_sessions_daily` | agregate fără IP-uri |
| `jobs` | kind (email/ștergere/export/acme), payload, run_at, attempts, state (coada rolului `worker`) |
| `signal_relay` (UNLOGGED) | id, hub_id, payload, expires_at: mesajele de signaling dintre nodurile gateway (§5.4) |

### 5.9 Suprafața API, găzduire, scalare

Serviciile Connect sunt cele din §7.9. Domenii:

- `<domeniu>`: SPA-ul și linkurile universale (`/p`, `/oauth/callback`, `/oauth/f6`, `apple-app-site-association`, `assetlinks.json`);
- `<domeniu-cloud>` = `cloud.<domeniu>`: Connect, `/link/v1`, `security.txt`;
- `<domeniu-hub>`: domeniu înregistrabil separat, delegat DNS-ului nostru (T35).

```
 Cloudflare: DNS <domeniu>, CDN pentru SPA, WAF + rate-limit pe cloud.<domeniu>, TURN
   <domeniu> (SPA static, release semnat)   cloud.<domeniu> ──▶ LB ──▶ ipcam-cloud --role=api,gateway (VM 1, VM 2)
   ns1/ns2.<domeniu-hub> ──▶ ipcam-cloud --role=worker      PostgreSQL primar (VM 1) ──▶ replică (VM 2, altă locație)
   Backup: pgBackRest (WAL continuu + complet zilnic) ──▶ object storage la alt furnizor UE
```

**Decizie tehnică (T32) — găzduire:**

- **Servere:** Hetzner, cu primarul în Germania (Falkenstein/Nürnberg) și replica în Finlanda (Helsinki).
- **Backup:** Scaleway Object Storage (Paris), un al doilea furnizor UE.
- **Email:** Brevo (FR), prin SMTP.
- **Ținte:** RPO ≤ 5 min, RTO ≤ 4 h, disponibilitate 99,5 %/lună (§13.9).

| | 100 de utilizatori | 10.000 de utilizatori |
|---|---|---|
| Calcul | 1 VM (4 vCPU / 8 GB): toate rolurile + PostgreSQL | 2 VM api+gateway, PostgreSQL primar + replică, DNS pe aceleași VM-uri |
| Backend + PostgreSQL + backup | 10–20 €/lună | 150–400 €/lună |

Modelul complet de cost (TURN la uz normal și intens, Cloudflare, email, costuri fixe) e în §15.6. Surse: land §2.1; crit #4, #12.

### 5.10 Cerințe

| ID | Cerință | Test |
|---|---|---|
| CLD-01 | Cloud-ul nu stochează video, credențiale de camere, tokenuri de stocare, chei de înregistrări în clar sau parola contului | Testul santinelă ARH-08 + test de schemă cu o listă de coloane interzise |
| CLD-02 | La login, pe rețea trece doar cheia de autentificare derivată | E2E cu proxy de captură |
| CLD-03 | Limitele de încercări și codul pe email pentru dispozitive noi funcționează | E2E: 11 încercări greșite în 10 min → a 11-a primește `resource_exhausted` cu `retry_after` crescător; dispozitiv nou fără factor → cod pe email |
| CLD-04 | TOTP, passkeys, codurile de recuperare, Google și Apple funcționează | E2E cu OIDC fals și autentificator WebAuthn virtual (Playwright) |
| CLD-05 | Reutilizarea unui refresh token rotit revocă familia lui | Unit + E2E |
| CLD-06 | Codul de împerechere: 8 caractere, 10 min, unică folosință, limitele din §5.3 | Unit + test de ghicire sub încărcare |
| CLD-07 | Autentificarea hub-ului respinge replay-ul și semnăturile greșite | Unit |
| CLD-08 | Prezență: offline în ≤ 75 s, online în ≤ 5 s | E2E cu rețeaua tăiată |
| CLD-09 | Relay de signaling cu p99 ≤ 150 ms la 10.000 de hub-uri conectate | Test de încărcare (CLD-30) |
| CLD-10 | Un ne-membru sau un cont fără drept primește `PERMISSION_DENIED`, iar oferta lui nu ajunge la hub | E2E |
| CLD-11 | Credențialele TURN expiră în ≤ 12 h; serverul self-hosted refuză peer-ii privați | Integrare cu coturn în laborator |
| CLD-12 | Pagina de callback și proxy-ul OAuth nu scriu nicăieri tokenuri sau coduri | Testul santinelă cu tokenuri marcate |
| CLD-13 | Blobul de backup e opac pentru server; scrierea folosește CAS pe versiune; citirea cere autentificare recentă | Unit + E2E |
| CLD-14 | Tokenul de drepturi se verifică offline, cu grație de 30 de zile | Unit pe hub cu ceas fals |
| CLD-15 | Certificatul LAN se emite din CSR și se reînnoiește; cheia privată nu ajunge în cloud | Integrare cu un server ACME de test (Pebble, doar în CI) |
| CLD-16 | Telemetria nu conține IP-uri, nume de camere sau imagini; dezactivarea e respectată | Testul santinelă + unit |
| CLD-17 | Ștergerea contului: acces blocat imediat; hub-urile primesc `AccountDeleted`; date șterse în ≤ 30 de zile, iar din backup-uri în ≤ 35 | E2E + verificarea jobului |
| CLD-18 | Exportul datelor contului (JSON) e gata în ≤ 1 h | E2E |
| CLD-19 | Restaurarea din backup se verifică automat în fiecare săptămână | Job de restaurare (§13.9, §15.5) |
| CLD-20 | Baza de date, backup-urile și logurile stau în UE | Revizuirea configurației de deploy (§15.5) + lista sub-procesatorilor (§12.2) |
| CLD-21 | Signaling-ul între noduri merge cu mesaje mari (§5.4) | O ofertă SDP de 30 KB, trimisă între două noduri gateway, ajunge intactă, cu p99 ≤ 150 ms |
| CLD-22 | Certificatele LAN și DNS-ul `<domeniu-hub>` respectă restricțiile din §5.7 | Integrare cu Pebble: hub nerevendicat, link autentificat < 10 min sau peste cota per IP → refuz; `rateLimited` → coadă cu backoff; DNS: IP public, etichetă inexistentă → NXDOMAIN |
| CLD-23 | Stream-urile de server trimit `Heartbeat` la ≤ 30 s și se reiau de la `last_event_id` | 120 s fără evenimente, în spatele unui proxy cu timeout de inactivitate de 100 s → stream-ul rămâne deschis; o conexiune tăiată se reia fără evenimente pierdute |
| CLD-30 | **Poartă Faza 2 — încărcare (G22).** 10.000 de hub-uri simulate + 2.000 de clienți pe un nod gateway (2 vCPU / 4 GB) | *Trece:* CLD-08 și CLD-09 respectate, CPU ≤ 70 %. *Pică:* 2 noduri gateway din start, cu rutarea din §5.4, apoi retest |

## §6 Aplicațiile client

**Pe scurt (pentru proprietar).** Aplicația are un singur cod TypeScript. Rulează în browser și e ambalată ca aplicație de Android și iOS (Capacitor) și de Windows (Tauri). Installer-ul de Windows instalează și hub-ul. Codul specific fiecărei platforme e mic și stă în același repo. Pe telefon se văd 4 camere pe pagină (maximum 9), pe calculator până la 16.

### 6.1 Structura `apps/` și stack-ul

```
apps/
├── web/            SPA React + PWA (Vite), intrarea comună pentru toate platformele
├── mobile/         Capacitor 8: android/, ios/, capacitor.config.ts (fără server.url); plugins/ = pluginurile native (§6.6)
├── desktop/        Tauri 2: src-tauri/ (Rust < 300 linii: fereastră, tray, deep link, starea serviciului hub), installer NSIS; update-urile vin prin hub (T27)
└── packages/
    ├── api/        @ipcam/api: cod generat (protobuf-es v2 + Connect-ES), transport peste canalul de date (T04), retry (CTR-14)
    ├── player/     @ipcam/player: modul fără framework (§6.2), adaptat din video-rtc.js (MIT, atribuire în NOTICE)
    ├── core/       sesiune WebRTC + signaling, fixarea cheii hub-ului, chei de dispozitiv, stare, F6 (citire destinații + age), fațada `platform`
    ├── ui/         componente accesibile, grilă, timeline, joystick PTZ
    └── i18n/       cataloage ICU ro/en + verificări
```

**Decizie tehnică:** bibliotecile client (React 19, TanStack Router + Query, Zustand, Radix UI, Tailwind CSS, FormatJS, hls.js, typage `age-encryption`, hash-wasm pentru Argon2id) și licențele lor sunt în §3.4.

**Rezervă documentată pentru shell-ul desktop (panel).** Dacă Tauri devine nepotrivit (proiect abandonat, problemă WebView2 fără ocolire), aplicația Windows trece pe Electron sau pe un shell WebView2 scris în C#. SPA-ul nu se schimbă; se rescriu doar comenzile shell-ului (§6.6) și installer-ul, ceea ce înseamnă săptămâni, nu o rescriere.

TypeScript rulează cu `strict`, `noUncheckedIndexedAccess` și `exactOptionalPropertyTypes`; `any` e interzis. pnpm instalează doar versiuni publicate de cel puțin 7 zile (§11.10).

### 6.2 `@ipcam/player`

```ts
export type Transport = 'webrtc' | 'mse' | 'hls' | 'snapshot';
export interface PlayerSource { kind: 'live' | 'recording'; cameraId: string; profile: 'main' | 'sub'; from?: Date }
export interface MediaLink {                       // furnizat de @ipcam/core; playerul nu știe dacă e cloud sau LAN
  webrtcTrack(slot: number): Promise<MediaStreamTrack>;
  byteStream(call: 'mse' | 'segment', req: Uint8Array): Promise<ReadableStream<Uint8Array>>; // canale bulk
}
export interface Player {
  attach(video: HTMLVideoElement, link: MediaLink): void;
  play(src: PlayerSource, prefer?: Transport[]): Promise<void>;
  seek(t: Date): Promise<void>;                    // doar pentru înregistrări
  stop(): void;
  readonly stats: { transport: Transport; codec: string; framesDecoded: number; latencyMs?: number; hardware?: boolean };
  on(e: 'first-frame' | 'transport-changed' | 'error', cb: (d: unknown) => void): () => void;
}
```

| Conținut | Ordinea de rezervă | Când se trece la treapta următoare |
|---|---|---|
| Live | 1. WebRTC (slot în PeerConnection-ul sesiunii)<br>2. MSE pe un canal `bulk` (fMP4 de la `live`; pe iOS, ManagedMediaSource)<br>3. snapshot la 1 cadru/s | Primul cadru nu apare în 4 s, `framesDecoded` stă pe loc 3 s sau codecul nu se poate negocia |
| Înregistrări | 1. HLS(fMP4) cu hls.js pe MSE/MMS, cu loader peste `ReadRange` (CTR-12)<br>2. pe iOS fără MMS: HLS nativ prin `hls-proxy` (APP-31)<br>3. track WebRTC la viteză 1× (planul B din CTR-12) | MSE/MMS lipsește sau o poartă pică |
| H.265 (A6) | Se cere doar dacă SDP-ul îl oferă | Primul cadru nu apare în 4 s → H.264; rezultatul se reține pentru dispozitiv |

### 6.3 Sesiunea și conectivitatea (`@ipcam/core`)

- **Sloturi fixe.** Oferta inițială are N transceivere `recvonly` (mărimea paginii + 1 pentru ecranul complet) și canalul `rpc`. **Decizie tehnică:** `SetView` leagă camerele de sloturi prin `replaceTrack`, fără SDP nou; fiecare legare are un `binding_epoch`, iar placa afișează camera nouă abia după evenimentul `SlotBound` (§8.3 pasul 6). `Negotiate` (§7.8) se folosește doar când e nevoie de sloturi în plus sau de alt codec.
- **Cheia hub-ului.** Clientul verifică răspunsul semnat cu cheia fixată. Dacă nu se potrivește, apare o eroare blocantă, fără reîncercare.
- **Rețea.** Pe LAN, ICE alege candidații host. La trecerea Wi-Fi ↔ 4G se face ICE restart, cu credențiale TURN noi.
- **LAN fără internet.** Aplicațiile native găsesc hub-ul prin mDNS (`_ipcam-hub._tcp`) sau după ultimul nume cunoscut, apoi fac signaling prin `LanService`, prin pluginul `lan-http` (§6.6, §11.5). Pe web merge doar din PWA-ul din cache, cu permisiunea Chrome Local Network Access, cerută la gestul „Conectează local”, și doar cât timp numele LAN al hub-ului (§5.7) se mai rezolvă din cache-ul DNS; altfel rămân aplicațiile native.
- **Inactivitate (Decizie tehnică, din cauza costului TURN).** Pe o cale relay, după 10 min fără interacțiune, aplicația întreabă „Mai urmăriți?”. Fără răspuns în 60 s, sloturile se eliberează. Pe LAN nu există limită. În fundal, sloturile se eliberează în ≤ 1 s, în afară de PiP; după 60 s fără nicio placă vizibilă, sesiunea la distanță se închide (§8.3 X5).

#### 6.3.1 Mașina de stări a sesiunii (Decizie tehnică)

`@ipcam/core` implementează sesiunea ca reducer pur `(stare, eveniment) → (stare, efecte)`, cu teste de tabel pe ceas fals (APP-21). Planul Flutter (§16.3) reia aceleași cazuri în lista de paritate. Justificare: comportamentele de conectare sunt răspândite în §6.3 și §8.3, iar două implementări ar alege tranziții diferite. Motivul de eșec e un `ErrorReason` (§7.7) sau un cod local al clientului (`SIGNATURE_INVALID`, `ICE_FAILED`, `DTLS_FAILED`).

| Stare | Intră când | Iese spre | Timer | Motiv la eșec |
|---|---|---|---|---|
| `IDLE` | aplicația cere un hub | `SIGNALING` | — | — |
| `SIGNALING{lan, cloud}` | oferta pleacă în paralel pe LAN și prin cloud (§8.3 pasul 1) | `ICE_CHECKING` la primul răspuns cu semnătură validă; `FAILED` | `GetIdentity` pe LAN: 300 ms; cloud: 10 s | `HUB_OFFLINE`, `PROTOCOL_TOO_OLD`, `ENTITLEMENT_LIMIT`; `SIGNATURE_INVALID` (fără reîncercare, FLW-12) |
| `ICE_CHECKING` | răspuns verificat | `DTLS`; `RELAY_RETRY` | 8 s | — |
| `RELAY_RETRY` | ICE eșuat în 8 s; o singură dată pe sesiune | `DTLS`; `FAILED` | 8 s, doar TURN-TLS/443 (`iceTransportPolicy: "relay"`) | `ICE_FAILED` |
| `DTLS` | pereche ICE aleasă | `HELLO`; `FAILED` | 5 s | `DTLS_FAILED` |
| `HELLO` | canalul `rpc` e deschis | `ACTIVE`; `FAILED` | 15 s | `DEVICE_NOT_TRUSTED`, `PROTOCOL_TOO_OLD`, `SIGNATURE_INVALID` |
| `ACTIVE` | `Hello` reușit | `RESTARTING` (schimbarea rețelei sau credențiale TURN expirate); `SUSPENDED` (fundal fără PiP); `CLOSED` | „Mai urmăriți?” după 10 min pe relay, apoi 60 s (§6.3) | — |
| `RESTARTING` | ICE restart cu credențiale TURN noi | `ACTIVE`; `FAILED` | 10 s | `ICE_FAILED` |
| `SUSPENDED` | aplicația în fundal fără PiP | `ACTIVE` la revenire (≤ 3 s, MV9); `CLOSED` | sloturi eliberate în ≤ 1 s; închidere după 60 s | — |
| `FAILED(motiv)` | oricare stare de mai sus | `SIGNALING`, la acțiunea utilizatorului sau cu backoff 0,5 → 30 s (§13.2); niciodată după `SIGNATURE_INVALID` | — | textul motivului (CTR-13) |
| `CLOSED` | închidere de către utilizator, `SUSPENDED` expirat, 60 s fără plăci vizibile | `IDLE` | — | — |

### 6.4 Ecrane MVP

| # | Ecran | Scop |
|---|---|---|
| 1 | Bun venit, cont, autentificare, recuperare | email + parolă, passkey, Google, Apple, TOTP, cod pe email (P4); resetare, cu avertismentul E7.3 |
| 2 | Hub-uri | lista, starea online și starea fixării cheii (§11.3), „Adaugă hub” (QR sau cod + compararea amprentei de 16 caractere, T24) |
| 3 | Descoperire camere | rezultatul scanării LAN cu marcă/model; adăugare manuală după IP sau URL RTSP (F2) |
| 4 | Asistent cameră | credențiale (merg doar la hub); pași per marcă; verificări cu bife verzi; acord pentru substream H.264; avertismente de securitate; avertisment pentru spațiu public și măști; audio oprit (P10) |
| 5 | Grilă live | pagini de plăci pe substream, starea fiecărei camere |
| 6 | Live pe ecran complet | fluxul principal; PTZ (joystick, zoom, presetări) doar dacă e suportat; pornire/oprire înregistrare (O8.2); PiP |
| 7 | Timeline și evenimente | per cameră: marcaje, miniaturi, goluri, lacăt (E4), sursa redării, derulare; lista evenimentelor de mișcare |
| 8 | Export | interval, progres, descărcare sau partajare MP4 (F5) |
| 9 | Destinații | adăugare cu „Criptat (recomandat)” / „Necriptat” (E1); conectare Drive, OneDrive, S3, WebDAV, SFTP, NAS; cote; coada de upload |
| 10 | Reguli de stocare | șabloane (S2) + editor avansat; butonul „De ce?” arată `ExplainRules`; retenția reală realizabilă pe fiecare destinație (§9.6) |
| 11 | Setări cameră | mod de înregistrare (implicit continuu, §8.2), pre-roll și post-roll, retenție cu presetări GDPR (P10) și zilele realizabile pe discul local (§9.6). În modul „la mișcare”, textul: „În modul «la mișcare», ultima oră se păstrează temporar pe hub, ca să putem salva secundele de dinaintea mișcării; apoi se șterge automat.” |
| 12 | Sănătate și alerte | per cameră: flux, evenimente, înregistrare, upload; alertele R1 |
| 13 | Chei și recuperare | cele 4 căi (E7), cheia de recuperare, „parola pentru înregistrări”; apare doar dacă se folosește criptarea |
| 14 | Dispozitive și cont | dispozitivele contului și lista de încredere a hub-ului, cu revocare; profil, factori, export de date, ștergerea contului (P10, Apple 5.1.1(v)) |
| 15 | Jurnal de audit | P5 |
| 16 | Setări hub | canal de update, pachet de diagnostic (R2), migrare (P9), protecția cheilor (TPM/DPAPI/software) |
| 17 | Afiș „Zonă supravegheată video” | generator PDF/PNG (P10) |
| 18 | Redare fără hub | F6: alegerea destinației, login la furnizor, timeline din catalogul de acolo |
| 19 | Setări aplicație, Despre | limbă, blocare biometrică, temă, „Economisire date” (implicit oprită, §8.3); licențele open-source |

### 6.5 Grila și limitele de decodare

| Dispozitiv | Plăci pe pagină (substream) | Maxim | Ecran complet |
|---|---|---|---|
| Telefon | 4 | 9 (A6) | 1 flux principal; celelalte plăci se opresc |
| Tabletă | 9 | 9 | 1 flux principal |
| Desktop și web | 9 | 16 (land §6 D8) | 1 flux principal |

- Paginile ascunse și plăcile ieșite din ecran (IntersectionObserver) își eliberează slotul.
- O placă fără primul cadru sau fără decodor liber trece pe snapshot (1 cadru/s) și arată un indicator.
- La încălzire, grila coboară la 4 plăci: pe iOS când `thermalState` ≥ serious, pe Android de la nivelul termic severe în sus.
- Capacitatea de decodare hardware intră în telemetria de categoria B, dacă utilizatorul nu a oprit-o (§12.2, §13.6). Se citește cu `navigator.mediaCapabilities.decodingInfo({type: 'webrtc', video: {contentType: 'video/H264', width, height, bitrate, framerate}})` (`powerEfficient`), per codec și rezoluție, sau din semnalele native (Android `MediaCodecInfo.isHardwareAccelerated`, iOS `VTIsHardwareDecodeSupported`). Decizie tehnică: Chrome expune `decoderImplementation` din `getStats` doar paginilor care capturează cameră sau ecran, iar aplicația nu capturează.

### 6.6 Pluginuri native (mici, în repo, cu teste)

Pe lângă acestea folosim pluginurile oficiale `@capacitor/app`, `barcode-scanner` (QR), `share`, `filesystem`, `haptics`, `status-bar` și `splash-screen`.

| Plugin | iOS | Android | Windows (comandă Tauri) | Faza |
|---|---|---|---|---|
| `secure-storage` | Keychain, `AfterFirstUnlockThisDeviceOnly` | Keystore AES-GCM (hardware unde există) | DPAPI | 2 |
| `local-network` | declanșează și verifică permisiunea (NWBrowser pe `_ipcam-hub._tcp`); întoarce endpoint-urile mDNS | `ACCESS_LOCAL_NETWORK` (targetSdk 37), NsdManager | ultimul nume cunoscut | 4 |
| `pip` | `allowsPictureInPictureMediaPlayback`, sesiune audio „playback”, PiP pe `<video>`; planul B: `AVPlayer` + `AVPictureInPictureController` pe HLS live prin `hls-proxy` (§16.3) | PiP la nivel de Activity (raport de aspect, acțiuni) | — | 4 |
| `biometric-lock` | LocalAuthentication | BiometricPrompt | Windows Hello | 4 |
| `auth-native` | passkeys (ASAuthorization), Sign in with Apple, ASWebAuthenticationSession | Credential Manager, Custom Tabs | deep link + browserul sistemului | 2/4 |
| `hls-proxy` | HTTP pe loopback, care servește HLS din octeții primiți de la JS; doar dacă pică APP-31 sau PiP-ul din WKWebView (§16.3) | — | — | 4 |
| `device-keys` | `SecKeyCreateRandomKey` cu `kSecAttrTokenIDSecureEnclave` | Keystore EC P-256, StrongBox cu rezervă TEE | CNG, Microsoft Platform Crypto Provider | 2 |
| `lan-http` | `URLSession` cu delegat care expune cheia publică (SPKI) a certificatului | `HttpsURLConnection`/OkHttp cu `TrustManager` propriu | client HTTPS în Rust (rustls) | 1 (Android, Windows; T42), 2 (iOS) |
| `push` | după MVP (P8, §11.6) | după MVP | după MVP | — |

**Decizie tehnică (`device-keys`).** Cheile `DK` și `LWK` din §10.3 cer chei hardware, pe care `secure-storage` nu le oferă (doar păstrează octeți). API-ul comun: `generate(alias)`; `publicKey(alias)` → SPKI; `sign(alias, data)` → semnătură IEEE-P1363 `r‖s` (convertită din DER, ca la WebCrypto); `wrap(alias, data)` și `unwrap(alias, data)` cu AES-256-GCM, pentru `LWK`. Pe web, aceeași fațadă folosește WebCrypto ne-extractabil. **`lan-http`** face cererile `LanService` și întoarce, pe lângă răspuns, SPKI-ul certificatului, ca aplicațiile native să lege TLS de `HIK` (§11.5). Comenzile Tauri pentru ambele intră în bugetul Rust < ~300 de linii (A3); dacă nu încap, pe Windows `device-keys` folosește WebCrypto ne-extractabil, iar verificarea LAN folosește modul comun din §11.5.

Fiecare plugin are cel mult 400 de linii per platformă. Tot codul Swift + Kotlin + Rust rămâne ≤ 10 % din codul client. Peste 15–20 %, avantajul opțiunii B dispare (panel).

### 6.7 Cerințe de platformă

| Platformă | Cerințe |
|---|---|
| iOS (minim 17.1, pentru ManagedMediaSource) | **Rețea locală:** `NSLocalNetworkUsageDescription` (RO/EN); `NSBonjourServices = ["_ipcam-hub._tcp"]`, fără entitlement de multicast (crit C3).<br>**Cameră:** `NSCameraUsageDescription` pentru QR.<br>**ATS:** `NSAllowsArbitraryLoads = NO`; `NSAllowsLocalNetworking = YES` doar dacă se activează `hls-proxy`.<br>**Fundal:** `UIBackgroundModes: audio` doar pentru PiP (Apple 2.5.4).<br>**Confidențialitate:** `PrivacyInfo.xcprivacy` pentru aplicație și fiecare plugin, cu `NSPrivacyTracking = false`.<br>**Cont:** ștergere în aplicație (Apple 5.1.1(v)); Sign in with Apple (Apple 4.8).<br>**Domenii:** Associated Domains `applinks:` + `webcredentials:<domeniu>`.<br>**App Review:** cont demo + hub demo permanent (Apple 2.1, §12.6).<br>**Export:** declarație pentru criptare standard (§12.6). |
| Android (minSdk 29) | **targetSdk:** 36 acum. La 37 (estimat aug. 2027) se adaugă `ACCESS_LOCAL_NETWORK`, cu ecran explicativ; se testează de acum pe Android 16, prin opt-in.<br>**Fundal (Decizie tehnică):** fără serviciu foreground în MVP, cu o excepție: exportul. Pe API 34+ exportul e un *user-initiated data transfer job*, cu notificare; pe API 29–33, unde aceste joburi nu există, e un worker WorkManager de lungă durată, cu notificare în prim-plan de tip FGS `dataSync`, declarat în formularul FGS din Play (§12.6). În fundal rulează PiP. Tipul FGS `camera` nu se folosește niciodată.<br>**Trafic în clar (Decizie tehnică):** `cleartextTrafficPermitted=false`, fără excepții RFC 1918. Hub-ul are HTTPS cu certificat public (în Fazele 0–1, autosemnat, verificat prin `lan-http` și `HIK`, T42), iar Android nu poate limita excepțiile la intervale de IP.<br>**WebView:** Android System WebView ≥ 120; altfel aplicația cere actualizarea.<br>**Permisiuni:** `INTERNET`, `ACCESS_NETWORK_STATE`, `CAMERA`, `USE_BIOMETRIC`, `RUN_USER_INITIATED_JOBS` (API 34+), `POST_NOTIFICATIONS` (API 33+), `FOREGROUND_SERVICE` și `FOREGROUND_SERVICE_DATA_SYNC` (doar pentru exportul pe API 29–33).<br>**Play:** App Links verificate; Data safety; URL web pentru ștergerea contului; poziționare ca „supraveghere a proprietății” (politica de stalkerware). |
| Windows | **Bază:** Tauri 2 + WebView2 Evergreen, cu bootstrapper. Windows 11 e platforma principală; pe Windows 10 22H2, aplicația și hub-ul afișează avertismentul din §4.7 (T33, OPN-15). WebView2 primește update-uri pe Windows 10 cel puțin până în oct. 2028.<br>**Installer NSIS** cu bifa „Acest PC e hub-ul meu”, bifată implicit (O5):<br>• instalează hub-ul (`externalBin`), serviciul și regulile de firewall (§4.7);<br>• la dezinstalare întreabă dacă păstrează înregistrările;<br>• nu coboară niciodată un hub actualizat.<br>**Update:** aplicația e actualizată de updater-ul hub-ului, nu de plugin-ul updater al Tauri. Binarele aplicației stau în sloturile `<data>\app\{a,b}\ui\`, verificate TUF; stub-ul semnat din `C:\Program Files\ipcam\` verifică hash-ul țintei TUF înainte de `CreateProcess` (§15.3). Dacă bifa e scoasă, serviciul `ipcam-hub` rulează doar ca updater, fără camere și fără porturi deschise (T27, §15.3).<br>**Semnare:** Authenticode pe toate binarele (§15.1, §18), din Faza 0-bis (OWN-19); până atunci, build-urile de test sunt nesemnate (T41, T44).<br>**Restricții:** capabilități Tauri minime; nicio pagină remote. |
| Web | **Livrare:** SPA static pe `https://<domeniu>`, cu asset-uri imutabile, SRI și manifest de release semnat (P3, §15.1). Hub-ul nu servește SPA-ul (T12).<br>**CSP:** `default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; connect-src 'self' https://cloud.<domeniu> https://*.<domeniu-hub>:7443 https://api.pwnedpasswords.com <API-urile F6>; img-src 'self' blob: data:; media-src 'self' blob:; worker-src 'self' blob:; frame-ancestors 'none'; base-uri 'none'; require-trusted-types-for 'script'`.<br>**F6 pe web (Decizie tehnică):** `<API-urile F6>` = Drive (`https://www.googleapis.com`), OneDrive (`https://graph.microsoft.com` plus gazdele de descărcare `https://*.files.1drv.com`, `https://*.microsoftpersonalcontent.com`, `https://*.sharepoint.com`) și S3 la AWS, B2, R2 și Wasabi. Endpoint-urile S3 proprii, WebDAV, SFTP și NAS merg doar din aplicațiile native (§8.6).<br>**LAN:** signaling prin cloud; dacă LNA blochează candidații locali, se folosește srflx sau relay (APP-32).<br>**Browsere:** Chrome/Edge ultimele 2 versiuni, Safari ≥ 17.1, Firefox ultimele 2 + ESR (doar H.264).<br>**PWA:** doar app shell.<br>**Tokenuri și chei:** nimic în `localStorage`; cheile de dispozitiv sunt WebCrypto ne-extractabile, în IndexedDB (§10.3). După login, aplicația cere `navigator.storage.persist()`. Safari poate șterge totuși datele după 7 zile fără interacțiune; atunci aplicația pornește explicit reînrolarea browserului (§10.8). |
| Toate WebView-urile | Originile sunt fixate: `server.iosScheme` și `server.androidScheme` în `capacitor.config.ts` (`capacitor://localhost`, `https://localhost`) și schema din configurația Tauri (`http://tauri.localhost`); lista CORS din §11.5 le conține exact. Asset-uri incluse în pachet; nu se încarcă HTML de la hub; deep link-urile se validează pe o listă permisă și nu se execută automat; depanarea WebView e oprită în release (panel). |

### 6.8 Limbă și accesibilitate

- **i18n.** Toate textele stau în cataloagele ICU `ro` și `en`. Româna e implicită pe dispozitivele setate în română. Datele și orele se formatează cu `Intl`, pe 24 de ore. ESLint interzice textele scrise direct în JSX. CI pică dacă lipsește o cheie sau traducerea unui `ErrorReason` (CTR-13). Pseudo-localizarea (texte cu 40 % mai lungi) se verifică vizual.
- **Accesibilitate:** WCAG 2.2 AA, cu European Accessibility Act în vedere (crit lipsă #14). Fiecare ecran trece prin axe-core. Totul merge de la tastatură, inclusiv PTZ (săgeți și +/−). Elementele au etichete pentru cititorul de ecran, de exemplu „Cameră Hol, live, înregistrează”. Contrast ≥ 4,5:1; ținte de atingere ≥ 44 pt; text mărit la 200 % fără pierderi; „reduce motion” respectat. VoiceOver și TalkBack se verifică pe dispozitiv (§14.7).

### 6.9 Cerințe

| ID | Cerință | Test |
|---|---|---|
| APP-01 | O singură bază de cod TS; cod nativ ≤ 10 %; Rust < 300 de linii; fiecare plugin ≤ 400 de linii per platformă | Contor de linii în CI; o depășire blochează PR-ul |
| APP-02 | Fiecare treaptă din ordinea de rezervă (§6.2) funcționează | E2E cu defecte forțate (codec blocat, MSE dezactivat) |
| APP-03 | Live-ul chiar rulează în Chromium și Firefox (blocant); în WebKit rezultatul se raportează, fără să blocheze (panel): `framesDecoded` crește, iar latența p95 pe LAN e < 1 s (SUC-02) | Playwright în laborator, pe x64, cu ora arsă în cadru; browserele au H.264 (§14.7) |
| APP-04 | Limitele din §6.5 sunt respectate; plăcile ascunse își eliberează slotul | Unit + E2E pe statisticile sesiunii de pe hub |
| APP-05 | Regula H.265: SDP + primul cadru în 4 s, altfel H.264; regula de profil H.264 din §8.3 pasul 7 | CI: ramura de rezervă (H.265 neoferit sau nedecodat → H.264) și cazul Firefox × flux principal High → substream cu eticheta „profil video nesuportat”. Ramura pozitivă H.265 rulează pe WIN-1 (cu GPU) și pe device farm, fiindcă runner-ele CI nu au decodor H.265 hardware |
| APP-06 | O semnătură greșită sau o cheie de hub schimbată produc o eroare blocantă | E2E cu un hub fals |
| APP-07 | Tokenurile nu ajung în `localStorage`, `sessionStorage` sau IndexedDB în clar | Regulă de lint + scanarea stocării după E2E |
| APP-08 | CSP-ul din §6.7 e servit; fără `eval` și fără scripturi inline | E2E pe antete + o încălcare provocată intenționat |
| APP-09 | Deep link-urile din afara listei permise sunt ignorate | Unit + fuzzing pe parser |
| APP-10 | Traducerile RO și EN sunt complete | CI (§6.8) |
| APP-11 | axe găsește 0 probleme serious sau critical; PTZ se poate controla complet de la tastatură | Playwright |
| APP-12 | În aplicațiile native, live-ul pe LAN merge cu cloud-ul oprit | E2E pe emulator Android și simulator iOS |
| APP-13 | Blocarea biometrică se activează după 5 min în fundal (configurabil) | Device farm |
| APP-14 | Ștergerea contului e disponibilă în aplicație și la un URL web | E2E |
| APP-15 | Fiecare înregistrare criptată are lacătul afișat (E4) | E2E cu o regulă de criptare |
| APP-16 | F6: cu hub-ul oprit, timeline-ul se reconstruiește din catalogul destinației, iar înregistrarea se redă decriptată pe dispozitiv | E2E cu S3 fals și Drive simulat, cu și fără criptare |
| APP-17 | Installer-ul Windows instalează serviciul hub și nu îl trece niciodată pe o versiune mai veche; serviciul nu are drept de scriere în `C:\Program Files\ipcam\`, iar stub-ul refuză un binar de aplicație al cărui hash diferă de ținta TUF | CI pe runner Windows: verificarea ACL pe ambele căi; binar alterat în slot → pornire refuzată |
| APP-18 | În fundal, sloturile se eliberează în ≤ 1 s (în afară de PiP) | Teste pe dispozitiv + statisticile hub-ului |
| APP-19 | Preflight-ul CORS reușește din fiecare origine din lista §11.5 și pică din oricare alta | E2E: câte un preflight per origine, spre `lanapi` și spre cloud |
| APP-20 | `device-keys` dă chei publice SPKI și semnături P-256 IEEE-P1363 identice ca format pe toate platformele | Teste Go care verifică fixture-uri de semnături produse pe fiecare platformă; rulare pe device farm |
| APP-21 | Sesiunea din `@ipcam/core` urmează tabelul din §6.3.1 | Teste de tabel pe reducer, cu ceas fals: fiecare tranziție și fiecare timer |
| APP-22 | `@ipcam/player`, `@ipcam/core` și `@ipcam/api` nu depind de React sau de alt framework de UI și nici direct de Capacitor sau Tauri (doar prin fațada `platform`), ca un verdict iOS târziu să atingă doar shell-ul mobil (RSK-27) | Regula ESLint `no-restricted-imports` în CI pe cele trei pachete; o dependență interzisă blochează PR-ul |
| APP-23 | În Fazele 0–1, fără domeniu, shell-urile Tauri și Android acceptă certificatul autosemnat al hub-ului doar dacă semnătura `HIK` fixat la împerecherea locală acoperă `SHA-256(SPKI)` (T42, §11.5) | E2E pe emulatorul Android și pe runner-ul Windows: SPKI semnat corect → conectare; alt certificat sau semnătură greșită → refuz fără reîncercare |
| APP-30 | **Poartă Faza 0 — spike mobil (A3, Faza 0 (b), G2–G6).** Matricea de dispozitive, măsurătorile MV1–MV15 și regula GO/NO-GO sunt în §16.3. Pe scurt:<br>• iPhone real (Faza 0-bis, cel târziu înainte de Faza 4, O13): WKWebView WebRTC prin TURN-TLS/443 pe operator mobil românesc; grilă de 4 plăci, 10 min; PiP în fundal, 5 min; HLS fMP4.<br>• Android: Galaxy A24 al proprietarului (AND-1, O14), grilă de 9 plăci pe substream, cu decodare hardware confirmată și temperatură acceptabilă după 10 min (Faza 0 (b) aprobată), plus grila de 4 plăci.<br>• WebView2 pe PC-ul Windows 11 al proprietarului: 16 plăci. | *Trece:* măsurătorile critice din §16.3, pe Android în Faza 0 și pe iOS în Faza 0-bis. *Pică pe mobil:* ambele aplicații mobile trec pe Flutter (T29); web-ul și Windows rămân pe B. *Pe AND-1 pică grila de 9 plăci, dar trece cea de 4:* verdict „GO condiționat”; plafonul pe telefon (A6) se schimbă doar cu aprobarea proprietarului (§16.3). *Pică grila de 16 pe WebView2:* grila desktop coboară la 9 (Decizie tehnică, §6.5) |
| APP-31 | **Poartă Faza 0-bis — înregistrări pe iOS (G4, O13).** hls.js pe ManagedMediaSource în WKWebView, iOS 17.1+, derulare pe 10 min de înregistrare | *Trece:* redarea merge, iar un salt durează ≤ 1,5 s. *Pică:* pluginul `hls-proxy` + `NSAllowsLocalNetworking = YES` |
| APP-32 | **Poartă Faza 0 — Chrome LNA pe LAN (crit C5, G7).** Web de pe o origine publică (`<domeniu>`; în Faza 0, o origine gratuită, dacă T41 o permite, altfel în Faza 0-bis) către un hub din același LAN, pe Chrome stabil | *Trece:* perechea ICE aleasă nu e relay (host, prflx sau srflx), cu sau fără prompt. *Pică:* web-ul folosește relay pe LAN și recomandă aplicația nativă; costul intră în §15.6 |

## §7 Contractul (Protobuf) și API-urile

**Pe scurt (pentru proprietar).** Toate mesajele dintre hub, cloud și aplicații sunt descrise într-un singur loc, `contract/`. Din această descriere se generează automat cod Go și TypeScript, deci părțile nu se pot contrazice fără ca testele să pice. Aici sunt regulile de versionare, formatul erorilor și lista serviciilor.

### 7.1 Principii

- **CTR-01.** Orice mesaj care trece o graniță (client↔hub, client↔cloud, hub↔cloud, fișiere scrise pe destinații, raportul detectorului) e definit în `contract/proto`. În `apps/`, doar pachetul `@ipcam/api` vorbește cu rețeaua; regula ESLint `no-restricted-imports`/`no-restricted-syntax` pică build-ul la `fetch`/`JSON.parse` direct spre API-urile noastre.
- **CTR-02.** Fiecare PR rulează `buf lint` (set STANDARD) și `buf breaking` față de `main`; orice eroare blochează PR-ul.
- **CTR-03.** Codul generat e comis (T01); CI rulează generarea și pică dacă `git diff` nu e gol.

### 7.2 Pachete

| Pachet (în `contract/proto/ipcam/<domeniu>/v<N>/`) | Conținut | Folosit de |
|---|---|---|
| `ipcam.common.v1` | ID-uri, `TimeRange`, paginare, `ProtocolInfo`, `Status` (cod Connect + `ErrorInfo`), `ErrorInfo` + `ErrorReason`, `Permission` + `Grant`, `PublicKey`, `Entitlements`, `PairingTicket` | toate |
| `ipcam.hub.v1` | API-ul local al hub-ului (§7.8) | hub (server), clienți |
| `ipcam.rtc.v1` | framing-ul canalului de date (§7.6) | hub, clienți |
| `ipcam.cloud.v1` | API-ul cloud pentru clienți (§7.9) | cloud (server), clienți |
| `ipcam.link.v1` | canalul de control hub↔cloud (§7.10) | hub, cloud |
| `ipcam.catalog.v1` | catalogul scris pe destinații (F6, §9.11) | hub (scrie), clienți (citesc fără hub) |
| `ipcam.backup.v1` | conținutul blobului de backup de chei (E7.3, T16, §10.3) și exportul de migrare al hub-ului (P9, §13.5) | hub, clienți |
| `ipcam.detector.v1` | raportul detectorului (D2–D4, §16.2.2) | detector, unelte interne |

### 7.3 Versionare și compatibilitate

- **CTR-04.** Numerele de câmp nu se refolosesc; câmpurile scoase devin `reserved` (număr și nume). Fiecare enum începe cu `<NUME>_UNSPECIFIED = 0`. Valorile de enum necunoscute se tratează ca `UNSPECIFIED`, iar câmpurile necunoscute se ignoră fără eroare.
- **CTR-05.** În `v1` se fac doar schimbări aditive. O schimbare incompatibilă creează pachetul `v2` alături de `v1`; serverul le servește pe amândouă cât ține fereastra de compatibilitate.
- **CTR-06.** Fereastra de compatibilitate e de **12 luni** (T08): hub-ul și cloud-ul acceptă orice client publicat în ultimele 12 luni, iar cloud-ul acceptă orice hub din aceeași perioadă. Fiecare parte trimite `ProtocolInfo` (în `Hello`, în `HubHello` și în antetul `ipcam-protocol` al apelurilor cloud). Sub `min_peer_api_level` răspunsul e `failed_precondition` cu motivul `PROTOCOL_TOO_OLD`, iar aplicația cere update. Funcțiile opționale se anunță prin `features`, nu prin comparații de versiune. Test: matrice CI cu hub-ul curent × artefactele de aplicație publicate în ultimele 12 luni, și invers.
- **CTR-07.** Formatele persistente (`catalog.v1`, `backup.v1`, `detector.v1`) au antet cu `format` și `format_version`. Cititorii suportă **toate** versiunile scrise vreodată, fiindcă recuperarea înregistrărilor nu expiră. Specificațiile lor sunt publice în `docs/formats/` (E6); raportul detectorului se scrie ca JSON (protojson), ca proprietarul să verifice singur că nu conține parole. Test: fișiere golden per versiune în `contract/testdata/`, citite de testele Go și TS.
- `ProtocolInfo { string component = 1 /* "hub", "cloud", "app-web", "app-android", "app-ios", "app-windows" */; string build_version = 2 /* semver */; uint32 api_level = 3 /* +1 la fiecare release care schimbă contractul */; uint32 min_peer_api_level = 4; repeated string features = 5 /* ex. "ptz.v1", "playback.hls.v1", "encryption.age.v1" */ }`.
- ID-urile sunt UUIDv7 în text; timpii sunt `google.protobuf.Timestamp` UTC după ceasul NTP al hub-ului; duratele sunt `google.protobuf.Duration`; mărimile sunt `int64` octeți (T07).

### 7.4 Generarea codului

| Pas | Unealtă | Ieșire |
|---|---|---|
| Lint + compatibilitate | `buf lint`, `buf breaking --against` ramura `main` | blochează PR-ul |
| Go | `protoc-gen-go` + `protoc-gen-connect-go`, managed mode pentru `go_package` | `contract/gen/go/ipcam/...` (modulul `github.com/AndreiBesliu/DevWorld/ipcam/contract/gen/go`) |
| TypeScript | `protoc-gen-es` v2 (`target=ts`); Connect-ES v2 folosește descriptorii generați | `apps/packages/api/src/gen/` (pachetul `@ipcam/api`) |

Validarea folosește adnotări protovalidate în `.proto`, aplicate la runtime de `protovalidate-go` și `@bufbuild/protovalidate`. Pluginurile sunt fixate local (Go: `tools/go.mod`; TS: `devDependencies`), fără registrul remote Buf (T01). O singură comandă, `tools/scripts/gen-contract.sh`, face lint, verificarea de compatibilitate și generarea; workflow-ul `ipcam-contract.yml` o rulează la fiecare PR care atinge `ipcam/contract/**`.

- **CTR-08.** Fiecare server validează cererea cu protovalidate înainte de logica de business (interceptor), iar fiecare client validează răspunsul la graniță. Test: fuzzing cu mesaje invalide → `invalid_argument`, niciodată panică sau crash.

### 7.5 Transporturi

| Legătură | Transport | Autentificare | Serviciile |
|---|---|---|---|
| Client → cloud | Connect peste HTTPS (T03), binar în aplicații, JSON pentru depanare | access token PASETO ținut în memorie; refresh token în cookie HttpOnly limitat la `RefreshSession` (web) sau în stocarea sigură (nativ), §5.2 | `ipcam.cloud.v1` |
| Client ↔ hub | canal de date WebRTC `rpc` + canale `bulk/<call_id>` (§7.6) | `Hello` legat de sesiunea DTLS (CTR-10) | `ipcam.hub.v1` |
| Client → hub pe LAN, fără cloud | Connect peste HTTPS pe LAN, certificat public per hub (P9, T25); în Fazele 0–1, fără domeniu, certificat autosemnat acceptat doar de shell-urile native (T42) | dovada de împerechere sau semnătura dispozitivului; identitatea hub-ului se verifică prin `GetIdentity` (§11.5) | doar `ipcam.hub.v1.LanService` (T12) |
| Hub → cloud | WebSocket TLS de ieșire, mesaje `ipcam.link.v1.Envelope` binare (T05) | provocare semnată cu cheia de identitate a hub-ului | `ipcam.link.v1` |

### 7.6 RPC pe canalul de date (framing)

Clientul deschide canalul de date `rpc` (fiabil, ordonat, subprotocol `ipcam.rtc.v1`) în același PeerConnection cu video-ul (A7). Fiecare mesaj SCTP conține exact un `Frame` Protobuf binar. Un apel e adresat prin numele procedurii Connect, iar hub-ul îl transmite handler-ului connect-go generat (T04). `open` deschide apelul; `message` poartă un mesaj al apelului (cerere sau răspuns) sau un fragment din el; `close` e ultimul frame al hub-ului; `cancel` = clientul renunță, iar hub-ul oprește lucrul și închide canalul bulk; `half_close` = clientul nu mai trimite mesaje.

```proto
// contract/proto/ipcam/rtc/v1/frame.proto
message Frame {
  uint64 call_id = 1;   // alocat de client, crescător, unic în sesiune
  oneof body { CallOpen open = 2; bytes message = 3; CallClose close = 4; Cancel cancel = 5; HalfClose half_close = 6; }
  bool continued = 7;   // true = urmează alt fragment al aceluiași mesaj
}
message CallOpen {
  string procedure = 1;              // ex. "/ipcam.hub.v1.PtzService/ContinuousMove"
  map<string, string> metadata = 2;  // correlation-id, idempotency-key, accept-language
  google.protobuf.Duration timeout = 3;
}
message CallClose { ipcam.common.v1.Status status = 1; /* OK sau eroare (7.7) */ int64 bulk_bytes = 2; }
```

| Regulă | Valoare |
|---|---|
| Mărimea unui mesaj SCTP | min(64 KiB, `a=max-message-size` negociat); mesajele mai mari se fragmentează cu `continued = true` |
| Mesaj maxim pe `rpc` | 4 MiB; peste această limită datele merg pe un canal bulk |
| Apeluri simultane per sesiune | 32; al 33-lea primește `resource_exhausted` |
| Canale bulk | hub-ul deschide `bulk/<call_id>` (fiabil, ordonat) pentru redare, export, miniaturi, diagnostic; maxim 4 simultan; închiderea canalului marchează sfârșitul datelor |
| Contrapresiune | emițătorul se oprește la `bufferedAmount` > 1 MiB și reia sub 256 KiB (`bufferedamountlow`) |
| Timeout și liveness | 15 s pentru apeluri unare; 2 s pentru PTZ; stream-urile țin cât sesiunea; fără ping aplicativ: ICE consent freshness (RFC 7675) |
| Conținutul lui `message` (Decizie tehnică) | protobuf-ul binar al tipului de intrare sau de ieșire al metodei, fără anvelopa Connect de 5 octeți și fără compresie |
| Metadate și stare | `CallOpen.metadata` = antetele cererii Connect; `CallClose.status` = codul Connect + `ErrorInfo` (§7.7); trailerele stream-urilor intră tot în `CallClose` |
| Puntea de pe hub | sintetizează o cerere în protocolul Connect (`application/proto` la apelurile unare, `application/connect+proto` la stream-uri) către un `http.Handler` din memorie, printr-un pipe; handler-ele connect-go rămân cele generate |
| Clientul TS | implementează interfața `Transport` din `@connectrpc/connect` pentru apeluri unare, server-stream, client-stream și bidi, după aceleași reguli |

- **CTR-09.** Limitele și regulile de codare din tabel sunt aplicate de ambele capete. Test: unit pe punte (fragmentare, 33 de apeluri, `Cancel`) + fuzzing pe decodarea `Frame` (P7 se aplică și intrărilor noastre) + vectori de octeți golden în `contract/testdata/rtc/` (unar, server-stream, client-stream, bidi, eroare), citiți de testele Go și TS.
- **CTR-10.** Primul apel al sesiunii e `/ipcam.hub.v1.SessionService/Hello`. Orice alt apel înainte primește `unauthenticated`, iar hub-ul închide sesiunea. Ambele părți semnează transcriptul `"ipcam-rtc-v1" ‖ rol ‖ session_id ‖ amprenta DTLS client ‖ amprenta DTLS hub ‖ client_nonce`: clientul cu cheia dispozitivului, hub-ul cu cheia de identitate fixată la împerechere. Astfel autentificarea e legată de canalul criptat (algoritmi: §10; reguli de încredere: §11). Test: E2E cu semnătură greșită, cu dispozitiv revocat și cu amprentă DTLS schimbată → sesiune închisă.
- **CTR-11.** Apelurile care modifică starea acceptă `idempotency-key`; hub-ul și cloud-ul păstrează rezultatul 24 h și răspund identic la repetare. Test: apel repetat după o deconectare simulată.
- **CTR-12 (poartă Faza 0 — redare prin canalul de date, crit lipsă #11, G18).** Pe un fișier înregistrat de 4 Mbps: *trece* dacă un canal bulk susține ≥ 8 Mbps pe LAN și ≥ 5 Mbps prin TURN-TLS/443 la RTT de 50 ms, iar un salt în timeline afișează primul cadru în ≤ 1,5 s; *pică* altfel. Plan B: hub-ul redă înregistrarea ca track WebRTC în aceeași sesiune (viteză 1×, saltul se face prin RPC), iar HLS peste canalul de date rămâne doar pe căile care trec poarta.

### 7.7 Modelul de erori

Erorile sunt erori Connect: un cod standard (`invalid_argument`, `not_found`, `permission_denied`, `unauthenticated`, `failed_precondition`, `resource_exhausted`, `unavailable`, `deadline_exceeded`, `internal` etc.) plus un detaliu `ipcam.common.v1.ErrorInfo` (T06). Pe canalul de date, același conținut vine în `CallClose.status`.

```proto
message ErrorInfo {
  ErrorReason reason = 1;                   // stabil, citit de mașină
  string correlation_id = 2;                // UUIDv7, apare în logurile tuturor părților
  google.protobuf.Duration retry_after = 3; // doar pentru erorile cu reîncercare
  map<string, string> params = 4;           // valori pentru mesajul tradus (ex. "camera_id")
}
enum ErrorReason {  // intervale per domeniu: 100 sesiune, 1000 camere, 2000 stocare, 3000 chei, 4000 hub/împerechere, 5000 drepturi
  ERROR_REASON_UNSPECIFIED = 0; PROTOCOL_TOO_OLD = 100; DEVICE_NOT_TRUSTED = 101; PERMISSION_NOT_GRANTED = 102;
  CAMERA_UNREACHABLE = 1000; CAMERA_AUTH_FAILED = 1001; CAMERA_TIME_SKEW = 1002; CAMERA_ONVIF_DISABLED = 1003;
  CAMERA_SESSION_LIMIT = 1004; PTZ_NOT_SUPPORTED = 1100; DISK_NEARLY_FULL = 2000; STORAGE_AUTH_EXPIRED = 2100;
  STORAGE_QUOTA_FULL = 2101; STORAGE_RATE_LIMITED = 2102; ENCRYPTION_KEY_MISSING = 3000; HUB_OFFLINE = 4000;
  PAIRING_EXPIRED = 4100; PAIRING_RATE_LIMITED = 4101; CONFIG_CONFLICT = 4200; ENTITLEMENT_LIMIT = 5000;
}
```

- **CTR-13.** Fiecare eroare are cod, `reason` și `correlation_id`; serverele nu trimit text pentru utilizator. Clientul afișează textul din cheia i18n a motivului (RO și EN), iar pentru un motiv necunoscut afișează textul generic al codului. Test: CI enumeră `ErrorReason` și pică dacă lipsește o traducere RO sau EN.
- **CTR-14.** Se reîncearcă automat, cu backoff și respectând `retry_after`, doar `unavailable`, `resource_exhausted` și `aborted`, și doar apelurile idempotente sau cu `idempotency-key`. Excepție: `aborted` cu `CONFIG_CONFLICT` nu se reîncearcă; clientul recitește configurația (CTR-17). Test: unit pe politica de reîncercare a clientului TS și Go.

### 7.8 API-ul local al hub-ului (`ipcam.hub.v1`)

Servit pe canalul de date (§7.6); `LanService` e servit doar pe HTTPS în LAN (T12). Permisiunile sunt atomice (`Permission`: `LIVE_VIEW`, `PLAYBACK`, `EXPORT`, `PTZ`, `RECORDING_CONTROL`, `CAMERA_ADMIN`, `STORAGE_ADMIN`, `DEVICE_ADMIN`, `AUDIT_READ`); rolurile din §11.3 sunt seturi de permisiuni. În MVP există un singur grant, proprietarul, cu toate permisiunile (O8.8).

| Serviciu | RPC principale | Rol | Faza |
|---|---|---|---|
| `SessionService` | `Hello` | versiuni, autentificare legată de DTLS, `Grant`-ul dispozitivului; câmpul opțional `enrollment_proof` (semnătura `EK` peste transcript, plus `bk_version`) admite un dispozitiv nou prin dovada de backup (§10.5); răspunsul aduce rotațiile `IdentityRotation` nevăzute de client | 1 (`enrollment_proof`: 3) |
| `HubService` | `GetHubInfo`, `WatchHealth`, `WatchAlerts`, `GetSettings`, `UpdateSettings`, `CreateDiagnosticBundle`, `GetUpdateStatus`, `ApplyUpdate`, `ExportHubBackup`, `ImportHubBackup` | sănătate per cameră și alerte (R1), diagnostic (R2), update (P6), migrare (P9) | 1 (backup: 3) |
| `DiscoveryService` | `StartScan` (stream), `GetVendorGuide` | scanare LAN, marcă/model, pași per marcă când ONVIF/RTSP e oprit (F2) | 1 |
| `CameraService` | `AddCamera`, `ProbeCamera`, `ApplyStreamProfile`, `ListCameras`, `GetCamera`, `UpdateCamera`, `RemoveCamera`, `GetSnapshot`, `GetSecurityWarnings` | adăugare, verificări (ceas, fluxuri, codecuri, PTZ real), substream H.264 cu permisiune (A6), avertismente | 1 |
| `LiveService` | `SetView`, `Negotiate`, `WatchLiveEvents` (stream) | alege plăcile (cameră × main/sub) și renegociază SDP în aceeași sesiune (A7); `SetView` întoarce `binding_epoch` per slot; stream-ul aduce `SlotBound` și `QualityChanged` (§8.3) | 1 |
| `PtzService` | `GetPtzCapabilities`, `ContinuousMove`, `Stop`, `ListPresets`, `GotoPreset`, `SetPreset`, `RemovePreset` | doar capabilitățile reale (F3); `ContinuousMove` are `timeout` (implicit 1 s) după care hub-ul oprește singur mișcarea | 1 |
| `RecordingService` | `GetRecordingConfig`, `SetRecordingMode` | continuu / mișcare / oprit, pre-roll, post-roll (O8.3) | 1 |
| `CatalogService` | `GetTimeline`, `ListEvents`, `GetThumbnail` | segmente, evenimente, miniaturi, lacăt (E4), locații (F5) | 1 |
| `PlaybackService` | `OpenPlayback`, `GetPlaylist`, `ReadRange`, `ClosePlayback` | HLS(fMP4) din cea mai rapidă locație; octeții pe canal bulk (CTR-12) | 1 |
| `ExportService` | `CreateExport`, `WatchExport`, `DownloadExport` | export MP4 (F5) | 1 |
| `StorageService` | `ListDestinations`, `AddDestination`, `TestDestination`, `RemoveDestination`, `BeginOAuth`, `CompleteOAuth`, `ListRules`, `PutRule`, `DeleteRule`, `ListTemplates`, `ApplyTemplate`, `ExplainRules`, `GetUploadSettings`, `SetUploadSettings`, `GetQueueStatus` | S1–S5, E1–E2; `ExplainRules` arată ce reguli se aplică unei combinații și dacă rezultatul e criptat; `BeginOAuth` întoarce URL-ul de autorizare și `redirect_uri`-ul exact, pe care hub-ul îl repetă la răscumpărare, sau, la device flow, `verification_url` + `user_code` (§8.7) | 1 (local), 3 (restul) |
| `KeyService` | `GetEncryptionStatus`, `RegisterRecoveryRecipient`, `WrapKeyringForDevice`, `RestoreKeyring`, `RotateRecordingKey`, `SetEnrollmentKey`, `AckKeyring`, `AckBlob` | cele patru căi de recuperare (E7, §10.4); `RegisterRecoveryRecipient` primește doar cheia publică `RK`, generată pe client; rotațiile și stările generațiilor din §10.5; `SetEnrollmentKey{ek_pub, bk_version, sig_by_trusted_DK}` instalează `EK` nouă, doar cu `DEVICE_ADMIN` | 3 |
| `DeviceService` | `ListTrustedDevices`, `ApproveDevice`, `RevokeDevice` | lista de încredere a hub-ului; granturi | 2 |
| `AuditService` | `ListAuditEvents` | jurnalul de audit (P5) | 1 |
| `LanService` (doar LAN HTTPS) | `GetIdentity`, `PairDevice`, `Offer`, `Trickle` | identitate (semnătura `HIK` peste nonce-ul clientului și `hub_id`; în aplicațiile native și peste SPKI-ul certificatului TLS, §11.5), împerechere locală, signaling când cloud-ul nu e accesibil | 1 |

Codul QR de împerechere (T19) conține un `ipcam.common.v1.PairingTicket`: `hub_id`, cheia publică a hub-ului, un secret de împerechere de 128 biți, expirarea, adresa cloud-ului și indiciile LAN (`dns_label` și IP-urile locale). Aplicația fixează cheia din QR; cloud-ul primește doar o dovadă HMAC calculată cu secretul (§7.10). Fluxul complet e în §8.1.

- **CTR-17 (Decizie tehnică).** Fiecare cerere care modifică configurația hub-ului (`UpdateSettings`, `UpdateCamera`, `RemoveCamera`, `AddDestination`, `RemoveDestination`, `PutRule`, `DeleteRule`, `ApplyTemplate`, `SetUploadSettings`, `SetRecordingMode` etc.) poartă `expected_config_revision`. La nepotrivire, răspunsul e `aborted` cu `CONFIG_CONFLICT`, iar răspunsurile reușite întorc revizia nouă (`config_revisions`, §4.6). Justificare: două dispozitive sau două tab-uri nu se mai suprascriu în tăcere. Test: doi clienți modifică aceeași regulă; al doilea primește `CONFIG_CONFLICT` și nu schimbă nimic.

### 7.9 API-ul cloud (`ipcam.cloud.v1`)

| Serviciu | RPC principale | Rol | Faza |
|---|---|---|---|
| `AccountService`, `ClientDeviceService` | `Prelogin` (sarea și parametrii KDF, §10.3), `Register`, `Login`, `RefreshSession`, `Logout`, `BeginPasskey`, `FinishPasskey`, `SignInWithProvider`, `EnrollTotp`, `ConfirmTotp`, `ChangePassword`, `ChangeKdfParams`, `RequestPasswordReset`, `CompletePasswordReset`, `DeleteAccount`, `ExportAccountData`, `ListAccountEvents`; `ClientDeviceService`: `ListDevices`, `RevokeDevice` | P4, P10; dispozitivele logate în cont; parola nu ajunge la server (T14) | 2 |
| `HubRegistryService` | `ClaimHub`, `ListHubs`, `WatchPresence`, `RenameHub`, `RemoveHub` | împerechere (F1, §5.3), prezență; `RemoveHub` face cloud-ul să refuze link-ul hub-ului (§8.6) | 2 |
| `SignalingService` | `CreateSession`, `WatchSession`, `SendCandidate`, `CloseSession` | ofertă/răspuns/candidați ICE prin cloud; serverele ICE cu credențiale TURN scurte | 2 |
| `KeyBackupService` | `GetBackup`, `PutBackup` | blobul criptat pe client (E7.3, T16), cu versiune pentru scrieri concurente | 3 |
| `EntitlementService` | `GetEntitlements` | planul contului; în MVP mereu „free” (O7) | 2 |
| `TelemetryService` | `Report` | telemetrie operațională (§12.2, §13.6) | 2 |
| Rute HTTP fără Protobuf | pe `<domeniu>`: `/p`, `/oauth/callback` și `/oauth/f6`; pe `<domeniu>` și `cloud.<domeniu>`: `/.well-known/security.txt`; pe CDN: metadatele TUF și feed-ul de vulnerabilități | link-ul universal de împerechere (T19); pagina statică de redirect OAuth (T11, §8.7); contact de securitate (P11, §12.5); update-uri semnate (§11.4, §15.1) | 2 (cer domeniul; inițial 1–2, T42) |

### 7.10 Canalul de control hub↔cloud (`ipcam.link.v1`)

Hub-ul deschide `wss://<domeniu-cloud>/link/v1` și trimite mesaje `Envelope { id, in_reply_to, oneof body }`. Reconectarea folosește backoff exponențial cu jitter, de la 1 s la 60 s. Intervalele de heartbeat și pragurile de prezență sunt în §5.4.

| Grup | Mesaje | Direcție | Rol |
|---|---|---|---|
| Autentificare | `HubHello`, `AuthChallenge`, `HubAuth`, `AuthOk` | hub ↔ cloud | hub-ul semnează provocarea cu cheia de identitate și trimite `hub_epoch` (cloud-ul acceptă doar epoca cea mai nouă, §13.5); primește ora serverului și drepturile |
| Prezență, audit | `Heartbeat`, `HubStatus` | ambele / hub → cloud | online/offline, versiune, număr de camere cu probleme (fără nume), decalajul ceasului; `audit_head` (`seq`, `hash`) zilnic, ca ancoră a jurnalului de audit (§11.7) |
| Signaling | `SignalOffer`, `SignalAnswer`, `IceCandidate`, `SignalClose` | ambele | relay pentru sesiunile WebRTC; răspunsul SDP e semnat de hub |
| Împerechere | `PairingCodeRequest`, `PairingCode`, `PairingClaim`, `PairingResult` | ambele | cod în stil RFC 8628 și revendicare prin QR (cloud-ul vede doar dovada HMAC) |
| Dispozitive, cont, identitate | `DeviceRevoked`, `AccountDeleted`, `HubRemoved`; `IdentityRotation` | cloud → hub; hub → cloud | revocare pornită din cont; ștergerea contului (§12.3); hub scos din cont; rotația `HIK`, semnată cu ambele chei (§10.5) |
| Certificate LAN, TURN | `CertificateRequest`, `CertificateIssued`, `IceServersRequest`, `IceServers` | hub → cloud → hub | CSR → certificat public per hub prin DNS-01, cheia privată rămâne pe hub (P9); servere TURN doar când rețeaua hub-ului blochează UDP |
| Drepturi, update | `EntitlementsUpdate`, `UpdateNotice` | cloud → hub | drepturi = token semnat de cloud, verificat offline de hub (§5.7); update = anunță o versiune, iar hub-ul verifică singur metadatele TUF (P6, §11.4) |
| Verificări | `PortCheckRequest`, `PortCheckResult` | hub → cloud → hub | testul TCP al porturilor camerelor expuse spre internet, făcut din cloud spre IP-ul public al link-ului și spre adresele IPv6 globale ale camerelor (§8.2) |
| OAuth B2 (rezervat) | `OAuthExchange`, `OAuthRefresh` | hub → cloud → hub | proxy fără stare; acceptate doar dacă planul B2 e activ (§5.6) |
| Telemetrie, erori | `TelemetryBatch`, `LinkError` | hub → cloud; ambele | telemetrie (§12.2, §13.6); erori cu același `ErrorInfo` (§7.7) |

- **CTR-15.** Cloud-ul acceptă pe canalul `link` doar mesajele din tabel; orice alt tip închide conexiunea cu `LinkError` și se loghează. Test: fuzzing pe decodarea `Envelope` și test E2E cu mesaj necunoscut.
- **CTR-16.** Niciun mesaj `link.v1` nu are câmpuri pentru credențiale de camere, chei de înregistrări în clar, imagini sau nume de camere. Test: regulă de lint pe `.proto` (listă de nume interzise) + testul santinelă ARH-08.

## §8 Fluxuri

**Pe scurt (pentru proprietar).** Aici sunt pașii fiecărei acțiuni importante, așa cum îi trăiește utilizatorul și cum îi execută sistemul: instalarea, adăugarea camerelor, live-ul, înregistrarea, redarea, redarea fără hub și conectarea fiecărui tip de stocare. Pentru fiecare pas spunem și ce se întâmplă când ceva nu merge.

Convenții: fiecare flux are componentele implicate, pașii numerotați și ramurile de eșec (X1, X2…; din alte secțiuni se citează „F1.X2”, ca să nu se confunde cu deciziile E1–E8). RPC-urile sunt cele din §7.8–§7.10. Cerințele `FLW-xx` se testează automat în laboratorul din §14.2, dacă nu scrie altfel. Deciziile locale sunt marcate „Decizie tehnică”.

### 8.1 F1 — Instalare și împerechere

**Componente:** installer Windows sau imaginea Docker (§15.1), `hubd` (`crypto`, `cloudlink`, `lanapi`), cloud (`pairing`, `devices`, `presence`), aplicația (shell + `@ipcam/api`).

1. **Instalare.** Pe Windows, installer-ul semnat pune serviciul hub și aplicația Tauri, deschide în firewall 7443/tcp, 7445/udp și 5353/udp (§4.7) și exclude folderul de înregistrări din scanarea Defender (land R11, §13.7). Pe Docker e o singură comandă: `docker run -d --name ipcam-hub --network host --restart unless-stopped -v ipcam-data:/data ghcr.io/andreibesliu/ipcam-hub:stable`; înregistrările pot sta pe alt disc cu `-v <disc>:/data/rec`. Rețeaua gazdei e obligatorie pentru WS-Discovery, mDNS și ICE (land §3.1).
2. **Prima pornire.** Hub-ul generează cheia de identitate Ed25519 (`HIK`, §10.3) și o sigilează, alege `dns_label` (T25), deschide WSS spre cloud (`HubHello` → provocare semnată, §7.10) și, după revendicare, cere certificatul LAN (DNS-01, §5.7).
3. **Tichetul.** Hub-ul produce un `PairingTicket` (T19): secret de 128 biți, valabil 10 min, de unică folosință, rotit automat. Îl scrie în `<data>/pairing/ticket.txt` (§4.9): pe Windows, aplicația Tauri de pe același PC îl citește de acolo (ACL: Administratori și utilizatorul care a instalat) și afișează QR-ul; pe Docker, QR-ul ASCII și codul apar și în `docker logs ipcam-hub` și în `/data/pairing/ticket.txt`. **Decizie tehnică (T36):** cât timp hub-ul nu are proprietar, și doar în primele 30 min după pornire, pagina `/pair` de pe portul 7443 arată QR-ul și codul. Utilizatorul o deschide tastând `http://<IP-ul dispozitivului>:7443/pair` (de exemplu pe un NAS sau pe un PC Windows fără ecran, instalat cu `/HUB_ONLY=1`); comanda Docker și installer-ul afișează adresa. Pagina e statică, fără JS de aplicație, servită pe HTTP simplu, fiindcă certificatul LAN vine abia după revendicare (§5.7). După revendicare sau după 30 min întoarce 404; fereastra se redeschide la fiecare repornire a hub-ului și după „Resetare proprietar” (X3), cât timp hub-ul nu are proprietar. Justificare: un utilizator de NAS nu trebuie să caute QR-ul în loguri (O9). Risc acceptat: în fereastră, primul dispozitiv din LAN care revendică hub-ul câștigă; proprietarul observă imediat (hub-ul nu apare la el), îl recuperează cu „Resetare proprietar” (X3), iar revendicarea intră în audit. O cheie substituită pe drum e prinsă de cloud la pasul 6 (X4).
4. **În aplicație.** Utilizatorul își face cont (§5.2, §11.6), apasă „Adaugă hub” și scanează QR-ul (merge și camera sistemului, care deschide link-ul universal).
5. **Fixarea cheii și revendicarea.** Aplicația fixează `hub_public_key` din QR, își generează cheia de dispozitiv `DK` (§10.3) și apelează `HubRegistryService.ClaimHub(hub_id, dk_public, nonce, proof)`, cu `proof = HMAC-SHA256(pairing_secret, "ipcam-claim-v1" ‖ hub_id ‖ account_id ‖ dk_public ‖ nonce)`. Cloud-ul nu primește niciodată secretul.
6. **Verificarea.** Cloud-ul compară cheia înregistrată de hub la `HubHello` cu cea trimisă de aplicație, apoi trimite `PairingClaim` pe link. Hub-ul verifică HMAC-ul, consumă tichetul, leagă proprietarul, adaugă `DK` în lista de încredere cu grantul de proprietar și răspunde `PairingResult` semnat cu `HIK`.
7. **Confirmarea.** Aplicația verifică semnătura cu cheia fixată, deschide prima sesiune WebRTC (F3) cu `Hello` (CTR-10) și afișează „Hub conectat ✓”. Imediat după, întreabă cum se păstrează înregistrările pe discul hub-ului: „Criptat (recomandat)” sau „Necriptat”, fără opțiune preselectată (ENC-01, E1, E3); la „Criptat” urmează asistentul „Configurare criptare” (§10.1). Abia după răspuns pornește F2; până atunci hub-ul nu scrie niciun segment (FLW-27).
8. **Varianta cu cod** (când QR-ul nu se poate scana). Hub-ul trimite `PairingCodeRequest`, iar cloud-ul emite un cod de 8 caractere din alfabetul RFC 8628 `BCDFGHJKLMNPQRSTVWXZ`, în forma `XXXX-XXXX`, valabil 10 min, de unică folosință. Hub-ul afișează codul lângă amprenta sa: primele 16 caractere hex din SHA-256(`HIK` public), în grupe de 4.
9. Utilizatorul tastează codul. Aplicația primește cheia hub-ului **prin cloud** și afișează amprenta pentru comparare (T24). Fixarea rămâne în starea `CLOUD_ONLY`, cu bandă galbenă „identitate neverificată”, până când utilizatorul confirmă că amprenta e aceeași (`USER_CONFIRMED`) sau aplicația ajunge în același LAN cu hub-ul și verifică automat cheia (`LAN_VERIFIED`, după regulile din §11.5). Stările și limitarea de securitate a acestei variante sunt în §11.3.

**Ramuri de eșec.**
- **X1. Tichet sau cod expirat:** `PAIRING_EXPIRED`; aplicația cere scanarea noului QR.
- **X2. Hub-ul nu e conectat la cloud:** pe același LAN, aplicația folosește indiciile LAN din QR și `LanService.PairDevice` cu aceeași dovadă HMAC. Live-ul pe LAN merge imediat, iar revendicarea în cont se finalizează automat la prima reconectare a hub-ului.
- **X3. Hub-ul aparține altui cont:** `failed_precondition`, fiindcă în MVP există un singur proprietar (O8.8). **Decizie tehnică:** schimbarea proprietarului cere o acțiune locală pe hub („Resetare proprietar” din aplicația Windows sau `docker exec ipcam-hub ipcam-hub reset-owner`), care golește lista de dispozitive și păstrează înregistrările și cheile de înregistrări; acțiunea intră în audit. Acoperă hub-urile vândute sau un cont pierdut; proprietarul o confirmă (OPN-11).
- **X4. Cheia din cloud diferă de cea din QR:** fluxul se oprește cu „Identitatea hub-ului nu corespunde”, se scrie un eveniment de securitate în audit (§11.7) și nu există buton de continuare.
- **X5. Ceasul hub-ului e decalat cu mai mult de 5 min:** decalajul apare în `HubStatus`, iar aplicația afișează ghidul NTP.

| ID | Cerință | Test |
|---|---|---|
| FLW-01 | Tichetul are 128 biți, expiră în 10 min și se poate folosi o singură dată | E2E: a doua revendicare cu același tichet e respinsă; după 10 min, `PAIRING_EXPIRED` |
| FLW-02 | `pairing_secret` nu ajunge în cloud | Santinela ARH-08 caută secretul în baza de date, loguri și telemetrie: 0 rezultate |
| FLW-03 | Dacă cloud-ul substituie cheia hub-ului, împerecherea prin QR eșuează (X4) | E2E cu un cloud de test care înlocuiește cheia: aplicația nativă și cea web refuză |
| FLW-04 | Limitele codului din §5.3 se aplică pe tot fluxul: după depășire, `PAIRING_RATE_LIMITED` cu `retry_after` | Test de integrare pe cloud (CLD-06) |
| FLW-05 | De la instalare la live: sub 10 min (O9) | Laborator: de la pornirea hub-ului la primul cadru ≤ 60 s, automat. Faza 4: SUC-01 (TST-26, G23) |
| FLW-27 | Niciun segment nu se scrie înainte ca alegerea de criptare pentru discul local să fie salvată (E1, E3, ENC-01) | E2E: hub nou, cameră adăugată direct prin API înainte de alegere → 0 fișiere în `rec/`; după alegere, primul segment are starea aleasă |
| FLW-28 | Pagina `/pair` apare doar pe un hub fără proprietar și doar în primele 30 min după pornire; revendicarea prin ea intră în audit (T36) | E2E: hub nou → pagina are QR; după revendicare sau după 30 min (ceas fals) → 404; repornire fără proprietar → pagina revine |

### 8.2 F2 — Descoperire și adăugare camere

**Componente:** `cameras` (`DiscoveryService`, `CameraService`), `live`, MediaMTX, asistentul din aplicație, feed-ul semnat de vulnerabilități de pe CDN (§11.4, §15.1).

1. **Scanarea.** `DiscoveryService.StartScan` pornește trei lucruri: o sondă WS-Discovery; scanarea porturilor tipice (80/443, 554, 8000, 37777, 9000, 2020, 34567/8899; D1) pe subrețelele locale ale hub-ului, limitată la /24 per interfață; amprentarea (OUI-ul MAC, bannere HTTP, `Server` din RTSP `OPTIONS`, ONVIF `GetSystemDateAndTime` fără autentificare). Rezultatele vin pe stream: IP, marcă, model, firmware (când se pot afla), starea ONVIF și RTSP.
2. **Credențialele.** Utilizatorul alege o cameră și introduce user/parolă. Datele merg doar pe canalul de date spre hub, care le sigilează (P1). Aplicația recomandă un utilizator ONVIF cu drepturi reduse și deschide ghidul mărcii.
3. **Verificările automate.** `CameraService.ProbeCamera` rulează verificările de mai jos și le afișează ca bife verzi sau ca avertismente cu acțiune. **Decizie tehnică (O9):** live-ul camerei pornește imediat după autentificarea reușită (primul rând din tabel), pe fluxul găsit; celelalte verificări rulează în fundal, iar întrebările din pasul 8 apar după primul cadru, înainte de pornirea înregistrării.

| Verificare | Cum | Verde | Altfel |
|---|---|---|---|
| Autentificare | WS-UsernameToken, apoi digest MD5/SHA-256, apoi basic | login reușit | `CAMERA_AUTH_FAILED`; după 3 eșecuri, pauză de 60 s, ca să nu se blocheze contul camerei |
| Ceas | `GetSystemDateAndTime` | decalaj ≤ 5 s | decalajul se memorează ca offset pentru WS-UsernameToken; se propune NTP pe cameră, cu permisiune |
| Fluxuri | `GetProfiles`, `GetStreamUri`, RTSP `DESCRIBE` și 10 s de analiză (codec, rezoluție, fps, GOP, B-frames, bitrate) | main și sub găsite | fără substream, grila folosește fluxul principal, cu avertisment de bandă |
| Substream | analiza de mai sus | H.264, GOP = fps (keyframe la 1 s, toleranță ±1 cadru), fără B-frames, fără smart codec | propunerea de la pasul 4 |
| Evenimente | `CreatePullPointSubscription` și topic de mișcare; opțional „Treceți prin fața camerei” | abonare reușită, eveniment primit | modul „la mișcare” e indisponibil pentru cameră până la detecția pe hub din v1.1 |
| PTZ real | `GetNodes`/`GetConfigurations`, apoi, cu permisiune („camera se va mișca puțin”), `ContinuousMove` 300 ms + `Stop`, comparând `GetStatus` sau snapshot-ul, apoi revenire | mișcare confirmată pe fiecare axă | doar axele, zoom-ul și presetările confirmate apar în F3 |
| Securitate | pasul 5 | fără probleme | avertismente |

4. **Substream-ul H.264 (A6).** Aplicația arată configurația actuală lângă cea propusă: H.264 Main, ~640×360 (cea mai apropiată rezoluție suportată), 12 fps (Decizie tehnică T22: mijlocul intervalului 10–15 din A6), GOP = fps, plafon ~512 kbps, fără smart codec și fără B-frames. Doar după „Aplică” se apelează `ApplyStreamProfile` cu `consent=true`, apoi fluxul se recitește și se reanalizează. Pentru fluxul principal (înregistrat cum e), aplicația doar avertizează despre GOP > 2 s sau smart codec și oferă separat corecția într-o atingere, tot cu permisiune. Dacă ONVIF refuză schimbarea, se deschide ghidul mărcii pentru pașii manuali din interfața web a camerei.
5. **Avertismente de securitate** (`GetSecurityWarnings`):
   - **Parolă implicită:** hub-ul compară local parola introdusă cu lista de parole implicite ale mărcii, inclusă în hub, fără încercări suplimentare de login. Avertizează și pentru `admin` cu parolă sub 8 caractere.
   - **Porturi expuse:** hub-ul cere cloud-ului (`PortCheckRequest`, §7.10) un test TCP-connect spre IP-ul public al link-ului său, doar pe porturile camerelor găsite, fără conținut; rezultatul nu se stochează (§12.2). Testul nu rulează dacă IP-ul e în 100.64.0.0/10 (CGNAT). **Decizie tehnică:** hub-ul adaugă în cerere și adresele IPv6 globale ale camerelor (2000::/3, luate din tabela de vecini), pe care cloud-ul le testează pe IPv6, fiindcă pe multe linii de fibră din România o cameră cu IPv6 global e accesibilă direct, fără NAT; o potrivire produce avertismentul „cameră accesibilă direct prin IPv6”. Hub-ul semnalează și UPnP-ul activ pe cameră, unde API-ul mărcii îl expune. Decizie tehnică: singura cale de a vedea expunerea din afară fără UPnP pe hub (ARH-02).
   - **Firmware vulnerabil:** marca, modelul și firmware-ul se caută în feed-ul semnat; rezultatul are CVE-ul și link-ul de update al producătorului.
6. **Ghidurile pe mărci** (`GetVendorGuide`). Hub-ul spune ce ghid se aplică (marcă, problemă, variantă de firmware); textul RO/EN, cu capturi, e inclus în aplicație. Acoperire MVP: Hikvision (ONVIF oprit implicit de la firmware 5.5.0, plus utilizatorul ONVIF), Dahua, Reolink (porturi RTSP/ONVIF), Uniview, Tapo („Camera Account”, „Third-Party Compatibility”), EZVIZ („LAN Live View”), Imou și un ghid generic. Subiecte: ONVIF oprit, RTSP oprit, cont separat de cameră, creare utilizator, substream manual, NTP.
7. **Adăugare manuală** (mereu disponibilă). După IP și port: aceleași verificări. După URL RTSP: doar flux, fără ONVIF, deci live și înregistrare continuă; PTZ și modul „la mișcare” nu există în MVP pentru astfel de camere, iar aplicația spune de ce.
8. **Finalizare.** Utilizatorul dă un nume camerei. Modul implicit de înregistrare e `CONTINUOUS`; se schimbă în ecranul 11 (Setări cameră). Urmează solicitările de confidențialitate din §12.3: „Ce vede camera?”, audio oprit implicit, avertismentul pentru spațiul public, presetarea de retenție.
9. **Primul ecran după primul live:** „Unde și cum păstrăm înregistrările?”, cu șablonul „Simplu” și „Conectează Google Drive / OneDrive” sau „Mai târziu”. Întrebarea de criptare pentru discul local s-a pus deja la finalul F1 (pasul 7), deci niciun segment nu există fără o alegere explicită (FLW-27).

**Ramuri de eșec.** **X1.** Camera nu apare (alt VLAN, multicast blocat): aplicația propune adăugarea manuală. **X2.** ONVIF sau RTSP oprit (`CAMERA_ONVIF_DISABLED`): se deschide ghidul mărcii, iar verificarea se reia la „Am activat”. **X3.** Limita de sesiuni RTSP (`CAMERA_SESSION_LIMIT`, land M6): aplicația cere închiderea altui NVR sau a altei aplicații care citește camera. **X4.** Cameră doar H.265, fără substream configurabil: avertisment că placa poate rămâne neagră pe unii clienți; în MVP nu se transcodează. **X5.** Ceasul nu se poate corecta: offset-ul rămâne activ; dacă abonarea la evenimente eșuează, camera rămâne pe modul „continuu”, cu alertă.

| ID | Cerință | Test |
|---|---|---|
| FLW-06 | Scanarea găsește toate camerele simulate din laborator, cu ONVIF pornit și oprit, în ≤ 30 s pe un /24 | E2E |
| FLW-07 | Credențialele camerei nu apar în niciun răspuns RPC și nu ajung în cloud | ARH-08 + test că `ListCameras`/`GetCamera` nu conțin câmpul de parolă |
| FLW-08 | Profilul de flux se schimbă doar cu `consent=true`; după aplicare, analiza confirmă H.264, GOP = fps și lipsa B-frames | E2E cu simulatorul ONVIF; un substream cu GOP de 2 s la 12 fps nu e marcat verde |
| FLW-09 | Interfața afișează doar capabilitățile PTZ confirmate | Simulator care anunță zoom dar nu-l execută → butonul de zoom e ascuns |

### 8.3 F3 — Live și PTZ, pe LAN și de la distanță

**Componente:** aplicația (`@ipcam/player`, shell), cloud (`signaling`, `turn`), `hubd` (`session` Pion, `live` go2rtc, `cameras`), TURN Cloudflare.

1. **Calea de semnalizare.** Aplicația pornește în paralel două încercări: (a) signaling prin cloud, `SignalingService.CreateSession(hub_id, offer)`; (b) dacă are un indiciu LAN, `LanService.Offer` pe `https://<ip-cu-liniuțe>.<dns_label>.<domeniu-hub>:7443` (certificat public, nume rezolvat algoritmic de DNS-ul nostru, §5.7; în Fazele 0–1, fără domeniu, `https://<IP>:7443` cu certificat autosemnat, doar din shell-urile native, T42), cu timeout de 300 ms pentru `GetIdentity`, care dovedește cheia `HIK` (§11.5). Prima care răspunde câștigă. Decizie tehnică: pe LAN, primul cadru vine mai repede și nu depinde de cloud. Pe web, (b) poate declanșa promptul Chrome Local Network Access; dacă e refuzat, rămâne (a).
2. **Cloud-ul** verifică faptul că dispozitivul aparține proprietarului și nu e revocat, atașează `iceServers` (STUN Cloudflare; TURN pe UDP/3478, TCP/3478 și TLS/443, cu credențiale scurte de 12 h, §5.5) și transmite `SignalOffer` pe link.
3. **Hub-ul** creează răspunsul SDP și semnează `sha256(offer) ‖ answer ‖ session_id` cu `HIK`. Aplicația verifică semnătura cu cheia fixată; dacă nu se potrivește, sesiunea e refuzată fără reîncercare (FLW-12).
4. **ICE.** Candidații sunt host (browserele îi ascund ca `.local`; Pion rezolvă mDNS, crit C5), srflx și relay (doar la client). Hub-ul folosește numai STUN și primește TURN doar dacă rețeaua lui blochează UDP (§3.3, §5.5). Dacă ICE nu reușește în 8 s, clientul reia o singură dată cu `iceTransportPolicy: "relay"`, doar pe TURN-TLS/443.
5. **Autentificarea sesiunii.** Se deschide canalul de date `rpc`, apoi `Hello` (CTR-10, semnături legate de amprentele DTLS), apoi `LiveService.SetView`.
6. **Un singur PeerConnection** (A7, crit lipsă #10). Clientul pre-alocă transceivere video recvonly: mărimea paginii de grilă (§6.5) plus unul pentru ecranul complet (§6.3). `SetView([{slot, camera_id, quality}])` leagă sloturile de camere. Hub-ul schimbă sursa cu `ReplaceTrack`. **Decizie tehnică (contractul sloturilor):** `SetView` întoarce un `binding_epoch` per slot, iar hub-ul trimite pe `WatchLiveEvents` mesajul `SlotBound{slot, camera_id, epoch, first_rtp_timestamp}`; clientul arată placa nouă (eticheta, PTZ) abia când `requestVideoFrameCallback` raportează `rtpTimestamp ≥ first_rtp_timestamp`, ca să nu atribuie camerei noi cadrele rămase în jitter buffer. Hub-ul rescrie timestamp-urile RTP (90 kHz, monotone) și numerele de secvență per emițător, ca să rămână continue la schimbarea sursei. `live` ține în cache ultimul GOP al fiecărui flux: un abonat nou pornește de la keyframe-ul din cache, iar cadrele din cache se trimit în rafală, cu timestamp-uri comprimate, ca decodorul să ajungă la cadrul curent fără latență în plus (HUB-06, HUB-27). Paginarea grilei nu renegociază SDP, iar plăcile ieșite din ecran se detașează (fără RTP). Se renegociază (`LiveService.Negotiate`) doar când se schimbă numărul de sloturi sau codecul. Decizie tehnică (§6.3): pre-alocarea evită o rundă SDP la fiecare pagină.
7. **Ecran complet.** O atingere pe placă face `SetView` cu slotul de ecran complet = camera pe `MAIN`. Dacă fluxul principal e H.265, se negociază H.265 doar când oferta SDP a clientului îl conține (A6). Dacă primul cadru nu apare în 4 s (§6.2, APP-05), clientul revine la substream-ul H.264 cu eticheta „calitate redusă: H.265 nesuportat” și ține minte alegerea pentru perechea dispozitiv × cameră. Ecranul complet pornește pe fluxul principal (A6); placa arată substream-ul până la primul cadru MAIN (HUB-27). În setări există opțiunea „Economisire date” (implicit oprită), care pornește ecranul complet pe substream pe date mobile sau pe relay, cu butonul „HD”. **Profilul H.264 (Decizie tehnică).** Pentru fiecare slot, hub-ul alege din ofertă payload type-ul H.264 al cărui `profile_idc` se potrivește cu SPS-ul camerei (High → `64xxxx`, Main → `4dxxxx`) și răspunde cu `level-asymmetry-allowed=1`. Dacă oferta nu are un profil compatibil cu fluxul principal, ecranul complet rămâne pe substream, cu eticheta „profil video nesuportat”. Substream-ul (fără B-frames, A6) pleacă în cel mai apropiat profil oferit; G9 verifică decodarea pe toți clienții, iar dacă pică pe unul (de exemplu Firefox, care oferă doar Constrained Baseline), onboarding-ul propune profilul Baseline pentru substream.
8. **PTZ și presetări** (doar ce a confirmat F2). La apăsare continuă, aplicația trimite `PtzService.ContinuousMove(velocity, timeout=1 s)` la fiecare 500 ms, iar la eliberare trimite `Stop`. Presetările folosesc `ListPresets`, `GotoPreset`, `SetPreset`, `RemovePreset`. Fiecare comandă intră în jurnalul de audit (P5).
9. **Plafonul de bandă.** Hub-ul limitează totalul live trimis la distanță la 80 % din uplink-ul măsurat. Peste plafon, cererile noi de flux principal primesc substream-ul și mesajul „bandă insuficientă”. „Uplink-ul măsurat” e estimarea GCC de la pasul 10, adunată pe sesiunile la distanță.
10. **Controlul congestiei (Decizie tehnică).** Hub-ul nu transcodează, deci se adaptează doar prin alegerea fluxurilor. Interceptoarele Pion active: răspuns NACK (buffer de 512 pachete per emițător), TWCC cu estimatorul GCC și rapoarte RTCP. La PLI/FIR, hub-ul retrimite keyframe-ul din cache-ul GOP și cadrele de după el, ca la un abonat nou, fiindcă nu poate cere camerei un IDR. Scara de coborâre: dacă estimarea rămâne 3 s sub 1,2 × suma debitelor sloturilor, ecranul complet trece de pe MAIN pe SUB, apoi plăcile de grilă peste 4 devin snapshot; revenirea se face după 10 s cu estimarea peste prag. Fiecare schimbare ajunge la client ca `QualityChanged`, cu un mesaj pe placă. Justificare: pe 4G sau TURN, un debit fix cu pierderi înseamnă înghețări până la următorul keyframe (2–10 s pe fluxul principal) și latență în creștere (SUC-03).

**Ramuri de eșec.**
- **X1. Hub offline:** `HUB_OFFLINE`; aplicația arată ultima oră la care a văzut hub-ul și oferă F6.
- **X2. Cameră deconectată:** placa arată „Camera offline” și ultima captură; hub-ul se reconectează cu backoff de la 1 s la 60 s, cu jitter (R1, HUB-13, §13.2).
- **X3. ICE eșuează și pe relay:** mesajul „Conexiune imposibilă” vine cu tipul rețelei; telemetria de categoria B primește tipurile de candidați, fără IP-uri (§13.6).
- **X4. Canalul de date cade în timpul mișcării:** hub-ul oprește singur camera la expirarea `timeout`-ului.
- **X5. Aplicația ajunge în fundal fără PiP:** sloturile se eliberează în ≤ 1 s, iar sesiunea la distanță se închide după 60 s fără nicio placă vizibilă (§6.3). Decizie tehnică: nu consumăm TURN și baterie degeaba.

| ID | Cerință | Test |
|---|---|---|
| FLW-10 | Latență imagine-la-ecran p95 < 1 s pe LAN și < 3 s de la distanță prin TURN, cu RTT emulat de 100 ms (O9, SUC-02, SUC-03) | Laborator: ceas ars în imagine, 100 de porniri per cale (TST-02) |
| FLW-11 | Un singur PeerConnection pe sesiune; schimbarea paginii nu creează altul și nu renegociază | `getStats`: un transport, N track-uri; numărăm `negotiationneeded` |
| FLW-12 | Un răspuns SDP cu semnătură invalidă închide sesiunea | E2E cu un cloud de test care modifică răspunsul |
| FLW-13 | PTZ: dacă clientul dispare în mijlocul mișcării, camera primește `Stop` în ≤ 1,2 s | Simulatorul ONVIF măsoară timpul până la `Stop` |
| FLW-29 | Sub congestie, latența remote rămâne în prag, iar imaginea își revine după pierderi (pasul 10) | Laborator cu netem la 2 Mbps și 2 % pierderi: remote p95 < 3 s; după o rafală de pierderi, imaginea își revine în ≤ 1 GOP; `QualityChanged` ajunge la client |
| FLW-30 | Contractul sloturilor (pasul 6): nicio placă nu arată cadrele camerei vechi sub eticheta celei noi; timestamp-urile RTP rămân monotone la schimbarea sursei | E2E: 50 de schimbări de pagină, cu banda arsă în cadru comparată cu `camera_id` din `SlotBound` |

### 8.4 F4 — Înregistrare

**Componente:** `live` (o singură conexiune upstream per flux), MediaMTX (`recorder`), `cameras` (evenimente), `catalog`, `rules`/`outbox` (§9), `crypto` (§10).

1. **Lanțul.** Camera → `live` (go2rtc) → `rtsp://127.0.0.1` → MediaMTX. Segmentele sunt fMP4 de **5 minute**, cu părți de **1 s** și fsync la închiderea fiecărei părți (crit C12; patch-ul MTX-1, §4.2); când discul local rezolvă la „criptat”, se scriu direct `.age` (T21, §4.5). Decizie tehnică (land §3.6): pierderea la crash depinde de mărimea părții, iar 5 minute înseamnă de 5 ori mai puține fișiere pe Drive decât 1 minut. Fluxul principal se înregistrează mereu; substream-ul doar dacă o regulă cere calitatea `SUB` pentru acea cameră (§9.3).
2. **Indexarea.** La hook-ul de segment închis (MTX-3), `hubd` indexează segmentul în SQLite: cameră, calitate, început și sfârșit după ceasul NTP al hub-ului (nu al camerei), offset-urile keyframe-urilor, codec, octeți, SHA-256. Apoi îl predă motorului de reguli (§9.4).
3. **Modurile** (`RecordingService.SetRecordingMode`, O8.3):

| Mod | Ce produce hub-ul | Ce se păstrează |
|---|---|---|
| `CONTINUOUS` | segmente continue; clipuri de eveniment dacă o regulă cere `EVENT_CLIP` | după reguli |
| `MOTION` | segmente continue doar într-un **inel local de 1 h**, plus clipuri de eveniment | doar clipurile și miniaturile; restul inelului se șterge după 1 h (Decizie tehnică T22: acoperă tăierea clipului și evenimentele întârziate) |
| `OFF` | nimic; live-ul merge în continuare | — |

4. **Clipul de eveniment** acoperă `[start − pre_roll, end + post_roll]`, cu pre-roll între 5 și 10 s (implicit 10) și post-roll între 15 și 20 s (implicit 20). Decizie tehnică (T22): valorile implicite sunt capătul de sus al intervalelor aprobate, ca să nu ratăm nimic. Începutul se aliniază la keyframe-ul anterior, deci pre-roll-ul efectiv e cel puțin cel setat. Un eveniment care pornește înainte să se termine post-roll-ul prelungește același clip. **Decizie tehnică (clipuri progresive):** clipul nu așteaptă sfârșitul evenimentului. Primul sub-clip, `[start − pre_roll, start + 30 s]`, intră în coadă imediat ce e disponibil, cu prioritatea cea mai mare; urmează câte un sub-clip de 60 s la fiecare 60 s cât ține evenimentul, iar ultimul include post-roll-ul. Toate au același `event_id`, iar timeline-ul și F6 le arată ca un singur clip. Justificare: la o spargere, primele secunde ajung în cloud chiar dacă hub-ul e distrus în timpul evenimentului. Sub-clipurile le taie modulul `playback` din `hubd`, prin stream copy din segmentele inelului (inclusiv din segmentul încă deschis, verificat în G14), decriptate din mers dacă e cazul (§4.2); dacă o regulă cere criptare pentru clip, el se scrie direct `.age`.
5. **Miniaturile:** câte una per segment (la început) și una per eveniment (la start + 1 s), din `GetSnapshotUri`, cu FFmpeg ca rezervă (T18).
6. **Siguranța evenimentelor.** Abonamentele PullPoint au termen de 10 min și se reînnoiesc la 8 min (land K3); indicatorul „evenimente OK” se vede per cameră. În modul `MOTION`, dacă evenimentele tac peste 5 min fără reînnoire reușită, hub-ul **păstrează segmentele continue** cu retenția locală până își revin evenimentele și trimite o alertă. Decizie tehnică: nimic nu se pierde pentru că au tăcut evenimentele. Aceste segmente devin artefacte `CONTINUOUS` marcate `motion_fallback = true`: stau doar pe discul local, cu retenția implicită a discului, apar în timeline, iar regulile `CONTINUOUS` spre alte destinații le ignoră, în afară de cele cu `include_motion_fallback = true` (§9.3). **Ora evenimentului (Decizie tehnică):** hub-ul citește evenimentele cu `PullMessages` (`Timeout=PT5S`, `MessageLimit=32`) în buclă; ora de start = `UtcTime` al camerei + `time_offset_ms`, limitată la intervalul [recepție − 10 s, recepție], ca un ceas de cameră greșit să nu mute pre-roll-ul.
7. **Pană de curent.** La pornire, hub-ul reconciliază folderul cu catalogul (detaliile, inclusiv pentru fișierele `.age`, sunt în §4.5). Golurile se scriu explicit în catalog (`gap`, cu motivul: cameră offline, hub oprit, repornire, disc plin), ca timeline-ul să le arate. Ținta e o pierdere de ~1 s (SUC-05); poarta e HUB-30 (G10).
8. **Pornire și oprire din aplicație:** `SetRecordingMode`, cu audit (P5). Retenția și curățarea de urgență sunt în §9.8.

| ID | Cerință | Test |
|---|---|---|
| FLW-14 | Clipul, reunit din sub-clipuri, conține cel puțin pre-roll-ul și post-roll-ul setate; primul sub-clip pleacă imediat (pasul 4) | Eveniment simulat la t: clipul începe la ≤ t − pre și se termină la ≥ sfârșit + post; hub oprit cu kill -9 la 40 s după start → primul sub-clip e `VERIFIED` pe destinația falsă; simulator cu ceasul decalat cu 300 s → clipul are tot pre-roll-ul |
| FLW-15 | Pierderea la o pană de curent respectă SUC-05 (mediana ≤ 1 s, maximul ≤ 2 s) și catalogul se reconciliază fără intervenție; poarta, hardware-ul și planul B sunt în HUB-30 (G10) | Faza 0 (c), procedura MI1 (§16.4); în CI, TST-08 și TST-09 |
| FLW-16 | Dacă evenimentele tac peste 5 min în modul `MOTION`, segmentele continue se păstrează și apare o alertă; implicit nu se urcă nimic din ele (`motion_fallback`) | Simulatorul ONVIF oprește evenimentele; destinația cloud falsă, cu o regulă `CONTINUOUS`, nu primește niciun segment `motion_fallback` |

### 8.5 F5 — Redare, timeline, miniaturi, export MP4

**Componente:** `CatalogService`, `PlaybackService`, `ExportService`, `storage` (drivere), `crypto`, `@ipcam/player` (hls.js + loader pe canalul de date; pe iOS, calea din §6.2).

1. **Timeline-ul.** `CatalogService.GetTimeline(camera, interval)` întoarce segmentele (cu locațiile verificate și lacătul per copie, E4), evenimentele, golurile și miniaturile. La tragerea pe timeline se arată miniatura cea mai apropiată (`GetThumbnail`, din cache).
2. **Pornirea redării.** La eliberare, `PlaybackService.OpenPlayback(camera, t, quality)`: hub-ul construiește din catalog un playlist HLS fMP4 (`EXT-X-MAP` + byte-range).
3. **Alegerea sursei** (F5, S3): prima copie `VERIFIED` în ordinea discul hub-ului → NAS → S3/WebDAV/SFTP → OneDrive → Google Drive. Decizie tehnică: Drive e ultimul fiindcă o descărcare costă 200 de unități de cotă (land S2). La S3, WebDAV, SFTP și OneDrive, hub-ul citește blocuri de 4 MiB prin `OpenRange`. La Drive face o descărcare secvențială a segmentului; un salt departe în segment pornește a doua cerere `Range` de la offset (maxim 2 descărcări per segment). Datele trec printr-un cache de redare LRU pe disc, de min(2 GiB, 5 % din disc). **Decizie tehnică (fără plaintext pe disc):** cache-ul ține octeții `.age` originali; un segment în clar intră în cache în clar doar dacă discul local al camerei rezolvă la „necriptat”, altfel e re-criptat cu o cheie ChaCha20-Poly1305 efemeră, ținută în RAM și regenerată la fiecare pornire `hubd`. Plaintext-ul există doar într-un buffer în RAM de cel mult 256 MiB; planul B din G13 decriptează tot aici (HUB-04).
4. **Copiile criptate** se decriptează cu `age.DecryptReaderAt` (§10.2), cu cheia fișierului ținută în cache după primul antet. Octeții ajung la client prin `ReadRange` pe un canal bulk (CTR-12).
5. **Viteze:** 1× și 2×. Decizie tehnică: plafonul de debit al canalului de date (CTR-12) nu susține mai mult pentru fluxul principal.
6. **Export MP4.** Utilizatorul alege intervalul. `ExportService.CreateExport` concatenează în stream copy (început aliniat la keyframe), cu progres prin `WatchExport`. **Decizie tehnică:** `moov` se construiește în memorie din tabelele de mostre din catalog și se scrie primul, într-o singură trecere, fără fișier temporar; când exportul moștenește criptarea (ENC-16), fluxul trece direct prin `age`, deci `tmp/` nu primește video în clar al unei camere criptate. Exportul pornește doar dacă discul are cel puțin 1,1 × mărimea estimată. `DownloadExport` trimite fișierul pe canal bulk, iar aplicația îl salvează prin dialogul platformei (Capacitor Filesystem/Share, descărcare în browser, dialog Tauri). Exportul devine artefact `EXPORT` și urmează regulile (§9.3), cu moștenirea criptării (ENC-16). Se scrie în audit.

**Ramuri de eșec.** **X1.** Segment șters peste tot: timeline-ul arată „șters (retenție)”. **X2.** Sursa nu răspunde: hub-ul trece la următoarea locație; dacă nu mai e niciuna, `unavailable` cu motivul. **X3.** Lipsește cheia (`ENCRYPTION_KEY_MISSING`): aplicația deschide recuperarea (§10.4). **X4.** Înregistrare H.265 pe un client fără decodor: „Descarcă MP4 și redă-l cu un player care suportă H.265”; în MVP nu se transcodează. **X5.** Hub offline: aplicația oferă F6.

| ID | Cerință | Test |
|---|---|---|
| FLW-17 | Primul cadru după un salt: ≤ 1,5 s din discul local (CTR-12) și ≤ 5 s dintr-o destinație cloud falsă limitată la 20 Mbps | E2E |
| FLW-18 | Se alege cea mai rapidă sursă disponibilă | Segment șters local → redat din NAS; NAS oprit → redat din S3 fals |
| FLW-19 | Exportul trece de `ffprobe` fără erori, durata e în ±1 s (aliniere la keyframe), `moov` e la început | E2E |

### 8.6 F6 — Redare din cloud fără hub (după furt sau distrugere)

**Componente:** aplicația (cititoare de destinații în TS, `agerange` din §10.2, player), cloud (`KeyBackupService`, `HubRegistryService`), destinațiile utilizatorului. Hub-ul **nu** participă.

1. **Pornirea.** Aplicația oferă „Redă din cloud” când hub-ul e offline. „Hub-ul meu a fost furat sau distrus” pornește același flux plus pașii de securitate de la pasul 7.
2. **Cheile.** Keyring-ul și directorul destinațiilor (T16) se iau, în ordine: din copia dispozitivului (calea 2, §10.4), din blobul din cont (calea 3: parola contului sau „parola pentru înregistrări”) sau cu cheia de recuperare (calea 4: QR sau text), care deschide și blobul (§10.4).
3. **Accesul la destinații.** Pentru fiecare destinație, utilizatorul se autentifică **pe acest dispozitiv**: Google Drive (`drive.file`, client al aceluiași proiect Google; faptul că vede fișierele hub-ului se verifică în G15) și OneDrive prin PKCE (pe web, cu redirect-ul `/oauth/f6`, §8.7); la S3 și WebDAV reintroduce cheia sau parola. Tokenurile rămân doar în stocarea sigură a dispozitivului (§3.3).
4. **Catalogul.** Aplicația descarcă `.ipcam/<hub_id>/catalog/head.cat[.age]` și zilele cerute (§9.11), decriptează, verifică semnătura cu `HIK` (fixată sau din blob) și versiunile (§9.11; un fișier de zi mai vechi decât intrarea lui din `head` apare ca „catalog vechi”) și construiește timeline-ul. Fișierele urcate după ultima scriere a catalogului se găsesc listând folderul zilei (listarea e permisă doar aici și la reconciliere); apar ca „neindexate”, cu ora luată din nume.
5. **Redarea.** Aplicația citește intervale (link presemnat S3, `downloadUrl` OneDrive, `Range` + bearer la Drive), decriptează pe bucăți de 64 KiB și le dă player-ului prin loader-ul hls.js. Exportul MP4 se face local, pe dispozitiv.
6. **Ce merge unde:**

| Destinație | Web | Android, iOS, Windows |
|---|---|---|
| Google Drive, OneDrive | da | da |
| S3 | doar la AWS, B2, R2 și Wasabi (lista din CSP, §6.7) și doar dacă bucket-ul are CORS pentru originea aplicației (aplicația afișează JSON-ul) | da (HTTP nativ) |
| WebDAV, S3 pe endpoint propriu | nu (CSP, §6.7) | da (HTTP nativ) |
| SFTP, NAS (SMB) | nu | nu; se folosește pasul 8 |
| Discul hub-ului furat | pierdut | pierdut |

7. **Securitate după furt** (lista din aplicație): scoate hub-ul din cont (`RemoveHub`), iar cloud-ul îi refuză link-ul și le spune clienților să nu mai aibă încredere în el; revocă accesul aplicației din contul Google/Microsoft și reconectează destinația din telefon; rotește cheile S3 și parolele WebDAV/SFTP și șterge cheia SSH a hub-ului; schimbă parolele camerelor; descarcă imediat clipurile importante, fiindcă regulile de ciclu de viață S3 continuă să șteargă.
8. **Hub nou.** Instalezi hub-ul (F1) și alegi „Restaurează din cont” sau „Restaurează din backup-ul automat” (§13.5). Keyring-ul vine de la un dispozitiv de încredere sau din blob (`RestoreKeyring`), destinațiile se reconectează, iar hub-ul importă cataloagele, deci timeline-ul vechi reapare. Backup-ul automat nu conține secrete (T38): parolele camerelor se reintroduc, iar destinațiile se reconectează. Hub-ul nou are `HIK` nou și o generație nouă de chei de înregistrări (§10.5).

| ID | Cerință | Test |
|---|---|---|
| FLW-20 | Cu hub-ul oprit, aplicația web și cea Android reconstruiesc timeline-ul din Drive, OneDrive și S3 falși și redau un segment criptat, separat cu fiecare din căile 2, 3 și 4 | E2E (3 rulări) |
| FLW-21 | Un catalog modificat pe destinație sau înlocuit cu o versiune mai veche e respins (§9.11) | E2E: un octet alterat → „catalog nesigur”; un fișier de zi mai vechi, cu semnătură validă, pus în locul celui curent → „catalog vechi”; în ambele cazuri redarea se face doar din listare, marcată |

### 8.7 F7 — Conectarea fiecărei destinații din MVP

**Componente:** aplicația, `StorageService` pe hub, driverele (§9.1), cloud-ul (doar pagina statică `/oauth/callback`), furnizorii.

**Decizie (T11, detaliată aici): codul OAuth se predă de pe telefon hub-ului (PKCE). Nu folosim broker de tokenuri în cloud.**

| Criteriu | Broker de tokenuri în cloud | Predarea codului către hub (ales) |
|---|---|---|
| Unde stau refresh token-urile | în baza noastră, pentru toți utilizatorii | doar pe hub-ul fiecărui utilizator, sigilate |
| Breșă în cloud-ul nostru | acces la Drive/OneDrive-ul tuturor | nicio cheie de stocare expusă (în planul de bază; dacă poarta G15 pică, vezi §5.6 B2) |
| Cloud-ul nostru e căzut | upload-urile se opresc (refresh centralizat), contrar O9 | upload-urile continuă |
| GDPR / încredere | devenim procesator cu acces la video | rămânem fără acces la video (P1) |
| Cost | configurare OAuth mai simplă | client OAuth per platformă și poarta din Faza 0 (§3.3) |

Detaliu la T11: `code_verifier`-ul PKCE e generat și păstrat **de hub**, în RAM, 10 min. Aplicația primește doar URL-ul de autorizare (cu `code_challenge` S256 și `state`) și trimite înapoi `code` + `state`. Un cod interceptat, inclusiv din logurile CDN-ului de pe calea de callback web, nu se poate răscumpăra fără verifier. Pe `/oauth/callback`, logarea query string-ului e dezactivată și se setează `Referrer-Policy: no-referrer`.

**Clienți OAuth Google per platformă (T34).** Toți sunt în același proiect Google Cloud. Tipul de client contează: un client „Web application” cere `client_secret` la răscumpărare chiar și cu PKCE, iar la clienții Android noi schemele URI proprii sunt oprite implicit din 2023.

| Platformă | Tip de client Google | Redirect | Cine răscumpără |
|---|---|---|---|
| iOS | „iOS”, fără secret | schema inversă a ID-ului de client (`com.googleusercontent.apps.<id>:/oauth`) | hub-ul, cu `client_id`, `code_verifier` și exact același `redirect_uri` |
| Android | „Android”, cu „Custom URI scheme” activat din setările avansate | schema proprie a aplicației | hub-ul, ca la iOS; dacă Google refuză schema proprie, Android folosește B1 |
| Windows (Tauri) | „Desktop” | loopback `http://127.0.0.1:<port aleator>/`, ascultat de shell-ul Tauri (în bugetul Rust, A3) | hub-ul, cu secretul ne-confidențial al clientului Desktop, inclus în hub |
| Web | — (fără plan A) | — | hub-ul rulează device flow (RFC 8628) cu un client „TVs and Limited Input” (`drive.file` e permis), al cărui secret ne-confidențial e inclus în hub; aplicația arată `verification_url` și `user_code` |

`BeginOAuth` întoarce și `redirect_uri`-ul exact, pe care hub-ul îl repetă la răscumpărare (FLW-31).

**Google Drive** (`drive.file` și atât, S4):

1. **Pornirea.** Destinații → Adaugă → Google Drive; întrebarea de criptare (ENC-01); „Conectează Google Drive”.
2. **URL-ul de autorizare.** `StorageService.BeginOAuth{provider: GOOGLE_DRIVE, client_platform}` întoarce URL-ul cu `client_id`-ul platformei (toți clienții OAuth sunt în **același** proiect Google Cloud, land §3.2), `scope=https://www.googleapis.com/auth/drive.file`, `access_type=offline`, `prompt=consent`, `code_challenge`, `state` și `redirect_uri`-ul exact; pe web întoarce în schimb `verification_url` și `user_code`, din device flow-ul pornit de hub.
3. **Autentificarea.** Se deschide suprafața de autentificare a sistemului (`ASWebAuthenticationSession` pe iOS, Custom Tabs pe Android, browserul de sistem pe Windows; pe web, pagina Google de device flow, într-un tab nou). Utilizatorul se loghează la Google și vede consimțământul „doar fișierele create de aplicație” (S5).
4. **Redirect-ul.** Aplicațiile native primesc redirect-ul cerut de tipul lor de client (tabelul de mai sus). Pe web nu există redirect pentru Google: hub-ul interoghează endpoint-ul de token al device flow-ului până când utilizatorul confirmă.
5. **Răscumpărarea.** `CompleteOAuth{state, code}`: hub-ul verifică `state`, răscumpără codul cu verifier-ul și `redirect_uri`-ul exact și sigilează refresh token-ul. Pe web, hub-ul primește tokenurile direct din device flow.
6. **Pregătirea destinației.** Hub-ul creează folderul `ipcam` în rădăcina My Drive (și ține minte ID-ul) și scrie `.ipcam/<hub_id>/README.txt` (§9.10). `TestDestination` scrie 1 MiB, citește un interval, șterge și citește `about.storageQuota`. Aplicația afișează ✓, spațiul liber și capabilitățile.
7. **Ramuri de eșec.** Anulare sau `access_denied`: nu se salvează nimic. `invalid_grant` la răscumpărare (cod expirat): se reia de la pasul 2. `invalid_grant` la refresh (acces revocat, 6 luni de neutilizare, limita de 100 de tokenuri, aplicație în modul Testing cu tokenuri de 7 zile, S1): destinația trece în `BLOCKED_AUTH`, coada crește local, iar alerta spune „Reconectează Google Drive”. Dacă poarta din §3.3 (G15) pică pe o platformă, acea platformă folosește planul B de acolo, în ordine: device flow pe hub (Google îl permite cu `drive.file`), apoi schimb de cod prin cloud ca proxy fără stare (B2, §5.6).
8. **Teste (O11).** Testele manuale și spike-urile pot folosi Drive-ul personal al proprietarului: cu `drive.file`, aplicația vede doar fișierele create de ea. Clientul OAuth e al proiectului Google Cloud creat de proprietar (§18), nu conectorul Drive al sesiunii de chat. Testele automate din CI folosesc conturi de test dedicate (Google și Microsoft), cu refresh token-urile ca secrete CI (§14.5).

**OneDrive.** Același tipar: client public Entra cu publisher verificat, PKCE fără secret, pe toate platformele, inclusiv web. **Decizie tehnică:** `https://<domeniu>/oauth/callback` e înregistrat pe platforma „Mobile and desktop applications” (`publicClient.redirectUris`, `allowPublicClient = true`), cu `response_mode=query`; pagina statică de pe aceeași origine predă `code` și `state` SPA-ului prin `BroadcastChannel`, fără să le trimită la server. Platforma „Single-page application” nu se folosește niciodată pentru coduri răscumpărate de hub: Entra cere atunci antetul `Origin` (`AADSTS9002327`), iar refresh token-urile SPA expiră fix după 24 h, deci refresh-ul de pe hub ar muri a doua zi. Un redirect separat, `/oauth/f6`, pe platforma SPA, servește doar tokenurile F6 care rămân în browser (§8.6). Destinația apare ca două intrări separate, ca tipul de cont să fie clar înainte de login (Decizie tehnică):

| | OneDrive personal | OneDrive pentru firme (Microsoft 365) |
|---|---|---|
| Autoritate și scope | `consumers`; `Files.ReadWrite.AppFolder offline_access` (preview, crit #9) | `organizations`; `Files.ReadWrite offline_access`. G16/MI7 verifică dacă `Files.ReadWrite.AppFolder` merge și pe conturile de firmă; dacă da, se folosește acesta |
| Text pentru utilizator | — | „La OneDrive pentru firme, aplicația cere acces la tot OneDrive-ul, dar scrie doar în /ipcam.” |
| Folder | `/special/approot` (`Apps/<nume aplicație>`) | `/ipcam`, creat de hub |
| Ștergere | coș de reciclare care ocupă cotă (`quota.deleted`) | `permanentDelete` |
| Ramuri specifice (crit C19) | — | Device-code flow **nu** se folosește (multe tenant-uri îl blochează prin Conditional Access). Consimțământ de administrator (`AADSTS65001`, `AADSTS90094`): „Administratorul IT trebuie să aprobe aplicația”, plus link-ul `https://login.microsoftonline.com/<tenant>/adminconsent?client_id=…`. Conditional Access (`AADSTS53003` și înrudite): login-ul pe telefon poate reuși, dar refresh-ul de pe hub (alt IP, dispozitiv necunoscut) e refuzat; destinația trece în `BLOCKED_AUTH`, cu explicație și cu soluțiile „excepție de la administrator” sau „altă destinație” |

**FLW-23 (poartă Faza 0-bis, G16: tenantul Entra ID P1 costă, O13; înainte de Faza 3).** Tenant de test cu trei politici CA: „MFA obligatoriu”, „blochează device-code”, „dispozitiv conform”. *Trece* dacă primele două permit refresh 7 zile de pe hub, iar a treia produce mesajul documentat. *Pică* dacă una dintre primele două blochează refresh-ul. Tot aici se încearcă `Files.ReadWrite.AppFolder` pe un cont de firmă. Plan B: OneDrive business se marchează „necesită excepție CA” în ghid, iar S3 sau NAS devin recomandarea pentru firme.

**Celelalte destinații:**

| Destinație | Ce introduce utilizatorul | Ce verifică hub-ul (`TestDestination`) | Refuzuri și ramuri |
|---|---|---|---|
| S3-compatibil | endpoint, regiune, bucket, chei, path-style; presetări AWS, B2, Wasabi, R2, Hetzner, „altul”. Aplicația recomandă bucket dedicat și cheie limitată la el (arată JSON-ul de politică) | PutObject mic; multipart cu 2 părți egale (R2); GetObject `Range`; DeleteObject; `GetBucketVersioning`; lifecycle (propune regula de rezervă din §9.8 sau arată instrucțiunile); CORS (doar pentru F6 pe web) | versionare activă → avertisment că retenția nu eliberează spațiu; Wasabi cu retenție sub minimul facturat (30/90 zile) → avertisment de cost |
| NAS (SMB), T17 | Windows: cale UNC `\\nas\share\folder` + credențiale sigilate (`WNetAddConnection2` în serviciu). Docker: share montat pe gazdă; aplicația afișează fragmentul exact `docker run -v`/compose | scriere, citire, ștergere, spațiu liber | doar SMB1 → refuzat (nesigur); coș pe share (ex. Synology `#recycle`) → avertisment; cale indisponibilă → `UNREACHABLE`, coada crește local |
| WebDAV / Nextcloud | URL (Nextcloud: `…/remote.php/dav/files/<user>/`), utilizator, parolă de aplicație | detectează Nextcloud (`status.php`) → upload pe bucăți v2, reluabil; altfel `PUT .part` + `MOVE`; cota prin `quota-available-bytes` (RFC 4331) | HTTP fără TLS spre adrese care nu sunt private → refuzat |
| SFTP | host, port, user; recomandat cheie Ed25519 generată de hub (aplicația arată linia pentru `authorized_keys`), altfel parolă | scriere, redenumire, citire, ștergere, `statvfs@openssh.com`; amprenta host key-ului se afișează și se fixează | host key schimbat ulterior → `BLOCKED_AUTH` + alertă de posibil atac (FLW-25) |
| Disc local | volumele hub-ului (Windows) sau căile montate (Docker) | scriere, fsync, spațiu liber | discul hub-ului există mereu ca destinație și ca spool (S3) |

| ID | Cerință | Test |
|---|---|---|
| FLW-22 | Verifier-ul PKCE nu părăsește hub-ul; un cod fără verifier nu se poate răscumpăra; tokenurile de stocare nu ajung în cloud | Furnizor OAuth fals: răscumpărare fără verifier → `invalid_grant`; santinela ARH-08 cu refresh token de test |
| FLW-24 | Probarea S3 detectează versionarea, lipsa permisiunii de lifecycle și cerința R2 de părți egale; verificarea nu interpretează ETag-ul final (§9.2) | Fake S3 configurat pe fiecare caz; ETag final care nu e MD5-of-MD5 (SSE-KMS) → upload acceptat |
| FLW-25 | La SFTP, un host key schimbat blochează destinația și dă alertă | Server SFTP de test cu cheia rotită |
| FLW-26 | Erorile Microsoft de consimțământ și CA ajung la utilizator ca mesajele documentate | Furnizor fals care întoarce `AADSTS65001`, `AADSTS53003`, `AADSTS9002326` și `AADSTS9002327` (ultimele două: configurare greșită a platformei, raportată ca eroare internă cu `correlation_id`) |
| FLW-31 | Fiecare platformă folosește tipul de client Google din tabelul T34; hub-ul repetă exact `redirect_uri`-ul întors de `BeginOAuth` | Furnizor OAuth fals strict: client „Web” fără secret → refuz; `redirect_uri` diferit → `invalid_grant`; web × Google → device flow |

## §9 Stocare și reguli

**Pe scurt (pentru proprietar).** Utilizatorul adaugă oricâte destinații și spune, prin reguli, ce merge unde: ce camere, ce fel de conținut, ce calitate, în ce program, cât timp se păstrează și dacă se criptează. Discul hub-ului e mereu zona tampon, deci o pană de internet nu pierde nimic cât timp e loc. Pentru începători există șabloane, iar butonul „De ce?” arată exact ce regulă se aplică.

### 9.1 Interfața de driver (`hub/internal/storage`)

Fiecare destinație e un driver în spatele interfeței `Destination` (ARH-05). Restul hub-ului lucrează **doar** cu capabilitățile declarate. Un furnizor nou se adaugă scriind un driver care trece suita de conformitate (STO-01), fără schimbări în reguli, outbox sau UI.

```go
type Destination interface {
    Kind() Kind                     // LOCAL, NAS_SMB, GOOGLE_DRIVE, ONEDRIVE_PERSONAL, ONEDRIVE_BUSINESS, S3, WEBDAV, SFTP
    Capabilities() Caps
    Probe(ctx context.Context) (ProbeReport, error)  // scrie/citește/șterge, cotă, permisiuni, avertismente
    BeginUpload(ctx context.Context, key ObjectKey, size int64, meta Meta) (UploadState, error)
    PutChunk(ctx context.Context, st *UploadState, off int64, r io.Reader, n int64) error
    Resume(ctx context.Context, st *UploadState) (nextOffset int64, err error) // detectează și sesiunile deja complete
    Commit(ctx context.Context, st *UploadState) (ObjectRef, error)            // ID furnizor, mărime, hash furnizor
    Abort(ctx context.Context, st *UploadState) error
    Stat(ctx context.Context, ref ObjectRef) (ObjectInfo, error)
    OpenRange(ctx context.Context, ref ObjectRef, off, n int64) (io.ReadCloser, error)
    PlaybackURL(ctx context.Context, ref ObjectRef, ttl time.Duration) (string, error) // sau ErrProxyRequired
    Delete(ctx context.Context, ref ObjectRef) (DeleteOutcome, error)                  // PERMANENT sau RECYCLED
    Quota(ctx context.Context) (Quota, error)       // total, folosit, liber, în coș; Unknown pentru S3
    PutSmall(ctx context.Context, key ObjectKey, data []byte) (ObjectRef, error)       // catalog, README; înlocuire atomică
    List(ctx context.Context, prefix ObjectKey) iter.Seq2[ObjectInfo, error]          // doar reconciliere și import
}

type Caps struct {
    Resumable       ResumeKind // SESSION_URI | UPLOAD_URL | MULTIPART | OFFSET_WRITE | DAV_CHUNKED | NONE
    ChunkAlign      int64      // 256 KiB Drive, 320 KiB OneDrive, 0 = oricare
    PartSize        int64      // mărimea fixă aleasă de driver (9.2)
    MaxRequest      int64      // < 60 MiB OneDrive
    EqualParts      bool       // R2: toate părțile, mai puțin ultima, egale
    MaxObject       int64
    Playback        LinkKind   // PRESIGNED | PREAUTH_SHORT | PROXY_ONLY | LOCAL_FILE
    RangeRead       bool
    Delete          DeleteKind // PERMANENT | RECYCLE_BIN
    Quota           QuotaKind  // ACCOUNT | FILESYSTEM | NONE
    Hashes          []HashKind // MD5, QUICKXOR, SHA256, ETAG_MULTIPART; gol = verificare prin re-citire
    ServerLifecycle bool       // S3/R2/B2: expirare pe server
    DailyUploadCap  int64      // Drive: 750 GiB
    CreatesPerSec   float64    // Drive: 2
}
```

`UploadState` se serializează în SQLite după fiecare bucată confirmată: sesiunea Drive, `uploadUrl` Graph, `uploadId` S3 cu ETag-urile părților, offset-ul SFTP/SMB sau ID-ul încărcării Nextcloud. Erorile driverului sunt tipizate (`ErrAuthExpired`, `ErrQuotaFull`, `ErrRateLimited{RetryAfter}`, `ErrDailyCap`, `ErrSessionExpired`, `ErrNotFound`, `ErrTransient`, `ErrPermanent`) și se mapează pe motivele 2100–2102 din §7.7. Bucățile se citesc direct din fișierul din spool, deci nu se țin în RAM. Pentru F6, clienții au un cititor numai-citire cu aceeași semantică, `RemoteReader { stat, readRange, list, playbackUrl }`, implementat pentru Drive, OneDrive, S3 și WebDAV în `apps/packages/core/storage`.

### 9.2 Note per furnizor (din cercetare)

| Furnizor | Upload și reluare | Verificare | Redare | Ștergere și cotă | Limite și note |
|---|---|---|---|---|---|
| Disc local | `.part`, fsync, rename | SHA-256 | fișier local | permanentă; `statfs` | spool-ul e mereu aici (S3) |
| NAS (SMB) | `.part` + offset + rename | SHA-256 la re-citirea completă (LAN) | proxy prin hub | permanentă (atenție la coșul NAS-ului); spațiul liber al volumului | T17 |
| Google Drive | sesiune resumable (URI valabil o săptămână); bucăți de 16 MiB (multiplu de 256 KiB); reluare cu `PUT bytes */size` | `md5Checksum` + mărime | **fără link direct**: proxy prin hub, sau token propriu pe client în F6 | `files.delete` e permanent; `about.storageQuota` | 750 GiB/zi pe utilizator → pauză 24 h; ~2 fișiere create/s; cote în unități (list 100, download 200, get 5); pragul de facturare de 400M unități/zi e **per proiect**, comun tuturor utilizatorilor → niciun `files.list` în operarea normală, cache la redare; 100 de refresh tokens pe cont și client (land S1–S2; briefs: stocare) |
| OneDrive personal | sesiune de upload; fragmente de 10 MiB (32 × 320 KiB, < 60 MiB), în ordine, **fără** antet `Authorization` pe `uploadUrl`; reluare cu `GET uploadUrl` | `quickXorHash` + mărime | `downloadUrl` pre-autentificat (minute, Range, CORS) | coș de reciclare care **ocupă cotă** (`quota.deleted`) | `Retry-After` la 429; < 100k elemente per folder |
| OneDrive business | ca la personal | ca la personal | ca la personal | `permanentDelete` | 50 GB/h ingress per utilizator; consimțământ și CA (§8.7) |
| S3 / B2 / Wasabi / R2 | multipart cu **părți egale de 16 MiB** (minim 5 MiB, maxim 10.000 de părți); PUT simplu sub 16 MiB; reluare cu `uploadId` + `ListParts` | per parte: ETag-ul din răspunsul `UploadPart` comparat cu MD5-ul local (fără SSE-KMS) sau checksum-ul părții; obiectul final: `HeadObject` (mărime) + lista de părți păstrată; formatul ETag-ului final nu se interpretează niciodată; `ChecksumAlgorithm=SHA256` doar unde probarea arată suport | URL presemnat cu TTL de 10 min, Range | permanentă (avertisment dacă bucket-ul are versionare); fără cotă | lifecycle de rezervă + `AbortIncompleteMultipartUpload` 7 zile; R2 cere părți egale; Wasabi facturează minimum 30/90 de zile; MinIO nu se livrează (AGPL, arhivat). **Decizie tehnică:** la presetările non-AWS, `RequestChecksumCalculation` și `ResponseChecksumValidation` = `WhenRequired`, fiindcă aws-sdk-go-v2 trimite implicit checksum-uri CRC din 2025, iar unele servicii compatibile S3 le tratează greșit |
| WebDAV / Nextcloud | Nextcloud: chunking v2 cu bucăți de 10 MiB, reluabil; generic: `PUT .part` + `MOVE`, nereluabil | Nextcloud: `OC-Checksum: MD5` + mărime; generic: mărime + re-citire eșantion | proxy prin hub; nativ în F6 | `DELETE` (Nextcloud mută în coș: `RECYCLE_BIN`); RFC 4331 | doar HTTPS spre adrese publice |
| SFTP | `.part` + scriere la offset, punct de control la 8 MiB; `posix-rename@openssh.com` | mărime + re-citire eșantion | proxy prin hub | permanentă; `statvfs@openssh.com` dacă serverul îl suportă | host key fixat |

**Re-citirea eșantion** (pentru drivere fără hash de la furnizor) compară SHA-256 pe trei blocuri de 64 KiB: primul, ultimul și unul aleator. Decizie tehnică: verificarea nu dublează traficul de upload.

### 9.3 Modelul de date al regulilor

Totul stă în SQLite pe hub și se expune prin `StorageService` (§7.8). Tipurile sunt în `ipcam.hub.v1`; schița fixează granița.

```proto
message Destination {
  string id = 1; DestinationKind kind = 2; string display_name = 3;
  DestinationConfig config = 4;            // endpoint, bucket, folder, root ID; fără secrete (secretele sunt sigilate pe hub)
  EncryptionChoice encryption_default = 5; // ENCRYPTED | PLAINTEXT; UNSPECIFIED e refuzat la salvare (ENC-01)
  RetentionPolicy default_retention = 6; WhenFull default_when_full = 7;
  int64 capacity_cap_bytes = 8;            // plafonul nostru pe această destinație; 0 = cota furnizorului
  UploadWindowSet upload_windows = 9; int64 bandwidth_limit_kbps = 10; // suprascriu setările globale
  DestinationState state = 11;             // OK, BLOCKED_AUTH, QUOTA_FULL, UNREACHABLE, PAUSED_DAILY_CAP, DISABLED
}
message Rule {
  string id = 1; string name = 2; bool enabled = 3; uint32 order = 4; // ordinea servește doar la afișare și departajare
  CameraSelector cameras = 5;          // all = true (include camerele adăugate ulterior) sau o listă de ID-uri
  repeated ContentType content = 6;    // CONTINUOUS, EVENT_CLIP, THUMBNAIL, EXPORT
  repeated EventType events = 7;       // ANY, MOTION; PERSON și VEHICLE rezervate pentru AI (după MVP)
  Quality quality = 8;                 // MAIN, SUB, ANY
  Schedule schedule = 9;               // ALWAYS sau ferestre săptămânale în fusul orar IANA al hub-ului
  repeated Target targets = 10;        // cel puțin una
  AfterUpload after_upload = 11;       // COPY = păstrează local după retenția locală; MOVE = local evictabil după verificare
  bool include_motion_fallback = 12;   // implicit false: segmentele motion_fallback rămân locale (§8.4 pasul 6)
}
message Target {
  string destination_id = 1;
  EncryptionOverride encryption = 2;   // INHERIT | ENCRYPT | PLAINTEXT
  RetentionPolicy retention = 3;       // max_age_days și/sau max_bytes; nesetat = moștenește destinația
  WhenFull when_full = 4;              // INHERIT | DELETE_OLDEST | PAUSE_KEEP_LOCAL | ALERT_ONLY
}
message UploadSettings { int64 bandwidth_limit_kbps = 1; UploadWindowSet windows = 2; uint32 parallel_per_destination = 3; /* implicit 2 */ }
```

Camera are în plus `retention_ceiling_days`: plafonul legal din presetările din §12.3, aplicat pe **toate** destinațiile.

**Ora de vară (Decizie tehnică).** Ferestrele orare se evaluează pe ora locală de perete, în fusul hub-ului. O oră locală inexistentă (03:00–04:00 în ultima duminică din martie, Europe/Bucharest) se mută înainte; o oră ambiguă (03:00–04:00 în ultima duminică din octombrie) se potrivește cu ambele apariții. Aceeași regulă se aplică ferestrei de instalare a update-urilor (§13.4). Test: STO-15.

### 9.4 Evaluarea și rezolvarea conflictelor

Intrarea e un **artefact** (segment, clip de eveniment, miniatură, export) cu atributele `camera_id`, `content_type`, `event_types[]`, `quality`, `start_time`. O singură funcție pură, `rules.Evaluate(artifact, rules, destinations) → Plan`, e folosită de outbox, de `ExplainRules` și de previzualizarea din UI (STO-05).

1. **Potrivirea.** O regulă activă se potrivește dacă **toți** selectorii se potrivesc. Programul se verifică după `start_time`, în fusul orar al hub-ului. Pentru `EVENT_CLIP` și `THUMBNAIL` de eveniment, selectorul de evenimente se potrivește dacă intersecția nu e vidă.
2. **Reuniunea.** Planul reunește țintele tuturor regulilor potrivite, grupate pe destinație (o singură copie per destinație).
3. **Conflictele:**

| Atribut | Regula | De ce |
|---|---|---|
| Criptare | `ENCRYPT` dacă **oricare** țintă potrivită pentru acea destinație rezolvă la `ENCRYPT` (ținta, altfel default-ul destinației) | „Criptarea câștigă” (E2) |
| Retenție în zile | maximul dintre țintele potrivite, plafonat de `retention_ceiling_days` al camerei | nimic nu se șterge mai devreme decât a cerut vreo regulă; plafonul legal câștigă |
| Retenție în GB și clasa copiei (Decizie tehnică) | **Clasa** copiei = ținta potrivită cu cel mai mare `max_age_days` (nesetat = ∞); la egalitate, regula cu `order` mai mic, apoi ținta cu indexul mai mic. Copia e eligibilă la ștergere când `now > start + max_age_days` al clasei ei sau când octeții clasei depășesc `max_bytes` al clasei (cele mai vechi primele), mereu plafonat de `retention_ceiling_days` | contabilitate fără dublări; o singură definiție pentru zile și GB (STO-14) |
| Când e plin | se aplică politica clasei; `DELETE_OLDEST` șterge doar copii din clase care au tot `DELETE_OLDEST`; dacă nu ajunge, se comportă ca `PAUSE_KEEP_LOCAL` | nu se șterge ce altă regulă a cerut să fie păstrat |
| Copiere sau mutare | dacă vreo regulă potrivită spune `COPY` sau are țintă pe discul local, copia locală rămâne | păstrarea locală câștigă |

4. **Derivatele moștenesc.** Miniatura unui segment sau eveniment criptat pe destinația D se criptează pe D. Exportul făcut din înregistrări criptate se criptează pe orice destinație (ENC-16).
5. **Ce nu se potrivește cu nicio regulă** rămâne pe discul local, cu retenția implicită a discului. Nimic nu se șterge doar pentru că lipsește o regulă; previzualizarea arată acest rând ca „Regula implicită”.
6. **Modul de înregistrare decide ce se produce; regulile decid unde merge.** O cameră în `MOTION` nu produce artefacte `CONTINUOUS` rutabile (inelul de 1 h din F4 nu se rutează). Excepția sunt segmentele `motion_fallback` (§8.4 pasul 6), rutate doar de regulile cu `include_motion_fallback = true`.
7. **Schimbările de reguli** se aplică artefactelor create după salvare (E5); cele deja planificate își păstrează planul. Excepția e scăderea retenției, care se aplică imediat, după confirmarea din §9.6.

### 9.5 Șabloane

Șabloanele creează reguli obișnuite, editabile după aplicare (S2). Retenția vine din presetarea camerei (§12.3); testele folosesc presetarea `HOME_PRIVATE`.

| Șablon | Reguli create |
|---|---|
| **Simplu: 24/7 local, evenimente în cloud** (implicit) | R1: toate camerele, `CONTINUOUS`, `MAIN`, mereu → disc local. R2: toate camerele, `EVENT_CLIP` + `THUMBNAIL`, `ANY` → prima destinație cloud adăugată, cu criptarea după default-ul ei; regula stă dezactivată, cu „Adaugă o destinație cloud”, până există una. R3: `EXPORT` → disc local, fără ștergere automată (exporturile nu se șterg singure, recording brief) |
| **Doar local** | R1 și R3, plus evenimentele pe discul local |
| **Redundant** | R1 + R3, plus `CONTINUOUS` și evenimente spre fiecare destinație cloud aleasă; validarea arată banda necesară (§9.6) |

### 9.6 Validare și previzualizare

`ExplainRules` primește o combinație (cameră, conținut, eveniment, calitate, oră) sau „toate combinațiile”. Pentru fiecare destinație întoarce: criptat (lacăt), retenția efectivă, copiere sau mutare, regulile care au produs rândul, volumul estimat pe zi și zilele realizabile pe capacitatea destinației; la destinațiile cu coș (`RECYCLE_BIN`), calculul adaugă o rezervă de 30 de zile × volumul zilnic (§9.8 pasul 7). UI-ul (ecranele 10 și 11) afișează o matrice camere × tip de conținut, cu destinațiile și lacătele în celule, plus „Banda necesară: X Mbps din Y măsurați” și retenția reală realizabilă.

| Severitate | Condiție |
|---|---|
| Eroare (nu se salvează) | regulă fără ținte; țintă spre o destinație ștearsă; ferestre orare invalide; destinație fără alegere de criptare |
| Avertisment | volumul estimat pentru retenția aleasă depășește discul local: „retenția reală ≈ X zile”; peste 750 GiB/zi spre Drive pe un cont Google; volum planificat > 80 % din uplink × orele ferestrei de upload; retenție peste plafonul camerei (se plafonează); Wasabi cu retenție sub durata minimă; OneDrive personal (spațiul se eliberează abia după golirea coșului); camere al căror conținut ajunge doar pe disc local; S3 cu versionare activă |
| Confirmare explicită | o schimbare care ar șterge imediat peste 10 % din copiile unei destinații; dialogul arată numărul și GB-ii |

### 9.7 Ciclul de viață al unui segment

```
 artefact:  [înregistrat] ──rutare (9.4)──► [criptare, dacă vreo țintă cere] ──► copii per destinație
 copie:     [în coadă] ──worker──► [se urcă] ──Commit──► [urcat] ──hash OK──► [verificat] ──retenție──► [expiră] ──► [șters]
                ▲  │                  │  └─eroare tranzitorie──► [backoff 1 s → 5 min, jitter] ──┐
                │  └─în afara ferestrei──► [așteaptă fereastra]                                    │
                └──────────────────────────────────────────────────────────────────────────────────┘
            blocări care așteaptă o acțiune: [BLOCKED_AUTH] [QUOTA_FULL] [PAUSED_DAILY_CAP] [UNREACHABLE]
 local:     [prezent] ──toate copiile non-locale verificate ȘI (fără țintă locală SAU retenția locală expirată)──► [evictabil] ──curățare──► [evacuat]
```

- **Criptarea se face o singură dată per artefact (T21).** Același fișier `.age` merge pe toate țintele criptate. Dacă discul local rezolvă la „criptat”, segmentul e scris criptat încă de la înregistrare (§4.5) și devine copia locală; copiile pentru țintele necriptate se produc prin decriptare din mers, direct în upload, fără fișier în clar pe disc. Dacă discul local rezolvă la „necriptat”, segmentul local rămâne în clar, iar `.age`-ul pentru țintele criptate se produce la închiderea segmentului și stă în spool până se verifică acele copii.
- **Fără duplicate.** Cheia obiectului e deterministă (§9.10), iar `Resume` recunoaște sesiunile deja finalizate (Drive întoarce 200/201 la `bytes */size`; la S3, `HeadObject` după `Complete`).
- **Ordinea în coadă:** `EVENT_CLIP` și `THUMBNAIL`, apoi `CONTINUOUS`, apoi `EXPORT`; în fiecare clasă, cele mai vechi primele. Decizie tehnică: după un furt contează mai întâi evenimentele, iar ordinea cronologică evită golurile.

### 9.8 Retenție și cote

1. **Bucla de retenție** rulează la 10 min per destinație și la presiune de spațiu. Șterge copiile `verificat` mai vechi decât retenția clasei lor și, pentru plafoanele în GB, pe cele mai vechi din clasă, folosind `Delete` conform capabilităților. Pe OneDrive personal, spațiul liber se calculează din `quota.remaining`, iar `quota.deleted` se afișează separat.
2. **Frâna de siguranță.** O trecere care ar șterge peste 50 % din copiile unei destinații sau peste 1.000 de obiecte, fără o schimbare confirmată de utilizator, se oprește și cere confirmare (land SP3).
3. **Ceasul.** Dacă ceasul NTP al hub-ului sare înainte cu peste 24 h față de ceasul monoton, retenția se suspendă până la confirmare.
4. **Cotele** se citesc la 15 min și înaintea fiecărui upload de peste 1 GiB. Alerta „aproape plin” apare la 90 %. La 98 %, sau la un 403/507 de cotă depășită, se aplică politica clasei (la destinațiile cu coș, pasul 7): `DELETE_OLDEST` șterge cele mai vechi copii eligibile; `PAUSE_KEEP_LOCAL` trece destinația în `QUOTA_FULL` și păstrează copiile local; `ALERT_ONLY` alertează la fiecare oră, iar upload-urile reîncearcă.
5. **S3/R2/B2.** Hub-ul propune o regulă de lifecycle de rezervă pe prefixul lui: retenția maximă de pe destinație + 1 zi, plus `AbortIncompleteMultipartUpload` 7 zile. Retenția merge astfel și cu hub-ul oprit; ștergerea exactă rămâne la hub.
6. **Curățarea de urgență a discului local** pornește la spațiu liber < max(5 %, 10 GiB) și se oprește la > max(8 %, 15 GiB). Decizie tehnică: praguri sigure și pe un SSD de 128 GB. Ordinea e fixă, documentată în UI, cu alertă la fiecare treaptă: (1) copiile `evictabil`, cele mai vechi primele; (2) copiile locale deja verificate în altă parte; (3) segmentele `CONTINUOUS` neurcate, cele mai vechi primele; (4) clipurile de eveniment neurcate. **Ultima oră a fiecărei camere nu se șterge niciodată** (F4: nu ștergem cele mai noi). **Decizie tehnică (F4: nimic neurcat nu se șterge fără politică):** treptele (3) și (4) rulează doar dacă politica „când e plin” a discului local e `DELETE_OLDEST` (implicită în șabloane și afișată la configurare); cu `PAUSE_KEEP_LOCAL` sau `ALERT_ONLY`, după treapta (2) înregistrarea se oprește cu gol `disk_full` și alertă (§13.8).
7. **Destinații cu coș (`Caps.Delete = RECYCLE_BIN`: OneDrive personal, Nextcloud) (Decizie tehnică).** O ștergere nu se socotește niciodată spațiu eliberat. Retenția șterge doar ce cere contabilitatea din catalog (vârsta și plafoanele în GB ale claselor), niciodată „până scade cota”. La cotă ≥ 98 % cu octeți în coș, destinația trece în `QUOTA_FULL` cu comportamentul `PAUSE_KEEP_LOCAL`, oricare ar fi politica, iar alerta spune: „Spațiul se eliberează după golirea coșului (max. 30 de zile).” Justificare: Graph nu are API de golire a coșului pe OneDrive personal (`permanentDelete` există doar la business), deci o buclă „șterge cele mai vechi” ar șterge tot mai multe copii fără să elibereze nimic.

### 9.9 Comportament la căderi

| Situație | Comportament |
|---|---|
| Fără internet | Înregistrarea continuă local, iar coada crește. Worker-ii reîncearcă cu backoff exponențial cu jitter, de la 1 s la 5 min. Hub-ul calculează „timp până la disc plin” și alertează sub 24 h |
| Cloud-ul nostru e căzut | Upload-urile nu sunt afectate: tokenurile și refresh-ul sunt pe hub (T11) |
| Furnizorul răspunde 429 sau 5xx | Backoff per destinație, cu respectarea `Retry-After`; celelalte destinații merg mai departe |
| Plafonul zilnic Drive | `PAUSED_DAILY_CAP` 24 h, apoi o probă |
| Autentificare expirată | `BLOCKED_AUTH`, alertă, coada crește |
| Hub repornit sau kill -9 | `UploadState` se reia de la ultimul offset confirmat, nu de la 0 (la WebDAV generic, fișierul se reia de la capăt) |
| Sesiune expirată (Drive după o săptămână, R2 după 7 zile) | Sesiune nouă de la 0; încărcarea incompletă veche se abandonează |

### 9.10 Structura folderelor pe destinație

Rădăcina e folderul aplicației: `ipcam/` pe Drive, `approot` pe OneDrive personal, `/ipcam` pe business, prefixul ales pe S3, WebDAV, SFTP și NAS.

```
<rădăcină>/
├── .ipcam/<hub_id>/README.txt                          text simplu: ce e folderul, formatul age, cum decriptezi cu `age -d -i`
├── .ipcam/<hub_id>/catalog/head.cat[.age]              §9.11
├── .ipcam/<hub_id>/catalog/days/2026-10-17.cat[.age]
├── e/<hub_id>/<camera_id>/2026-10-17/20261017T081500Z_cont_main.mp4.age     fișiere criptate: doar ID-uri opace
└── <NumeHub>/<NumeCameră>/2026-10-17/20261017T081500Z_eveniment_main.mp4    fișiere necriptate: nume lizibile
```

Decizie tehnică: fișierele criptate nu au niciodată nume de hub sau cameră în cale. Fișierele necriptate au, fiindcă utilizatorul le-a ales tocmai ca să le deschidă direct. Numele se curăță de caracterele interzise pe Windows/SMB. Folderele zilnice țin sub 100k elementele per folder (OneDrive) și împart listările din F6.

### 9.11 Catalogul pe destinații (pentru F6)

Formatul e `ipcam.catalog.v1` (§7.2): binar, cu antet de format și versiune (CTR-07), semnat Ed25519 cu `HIK`, cu semnătura în interior.

- **`head`:** ID-ul hub-ului, cheia lui publică, camerele (ID, nume, codec), generațiile de chei, o versiune monotonă proprie, pentru fiecare zi perechea `(versiune, sha256)` și ora ultimei actualizări.
- **Fiecare `day`:** `{hub_id, zi, versiune (monotonă), intrări}`, cu intrările = artefactele **prezente pe acea destinație**: ID, cameră, tip, calitate, început și sfârșit, mărime, hash, cale sau ID la furnizor, `encrypted` și generația de cheie, evenimente, goluri.

Scrierea se face prin `PutSmall`, cu înlocuire atomică. Fișierul zilei se rescrie la cel mult 5 min după apariția unei copii verificate noi și la închiderea zilei. **Decizie tehnică (versiuni):** `head` se rescrie în același job din outbox, imediat după fiecare scriere a unui fișier de zi, și când se schimbă camerele sau cheile. Cititorul acceptă un fișier de zi dacă semnătura e validă și versiunea lui e ≥ cea din `head`; o versiune mai mică apare ca „catalog vechi”, iar cititorul trece pe listare. Clientul ține minte cea mai mare versiune de `head` văzută per destinație și respinge una mai mică. Justificare: semnătura singură ar permite cuiva cu acces la cont să pună la loc un fișier vechi, semnat valid, și să ascundă evenimente recente. După retenție se rescrie ziua afectată, iar o zi rămasă goală se șterge. Criptarea urmează T15 (§10.7). Cloud-ul ipcam nu ține niciun catalog.

| ID | Cerință | Test |
|---|---|---|
| STO-01 | Fiecare driver trece suita comună de conformitate: kill la offset aleator → reluare fără duplicate, hash verificat, Range, semantica de ștergere, cotă | `go test ./storage/conformance` pe furnizori falși în CI; nightly pe conturi reale de test, inclusiv Wasabi și un bucket AWS cu SSE-KMS |
| STO-02 (poartă Faza 0, G14; OneDrive personal în Faza 0-bis dacă T41 nu îl permite, OneDrive business mereu în Faza 0-bis) | Pană de internet simulată 6 h în timp ce se urcă spre Drive și OneDrive. *Trece* cu 0 segmente lipsă, 0 duplicate și toate hash-urile verificate (SUC-04); *pică* altfel. Plan B: se corectează outbox-ul înainte de Faza 3. Tot aici se măsoară unitățile de cotă Drive per segment, eliberarea spațiului pe OneDrive personal după ștergere și citirea de către `playback` a părților deja scrise dintr-un segment încă deschis (clip de eveniment, redare „acum”); dacă citirea eșuează, segmentele scad la 2 min (land §3.6) | Faza 0 (c), procedura MI5 (§16.4) |
| STO-03 | Nimic neurcat nu se șterge în afara treptelor (3)–(4) din §9.8 pasul 6 și doar cu politica `DELETE_OLDEST` a discului local; ultima oră a fiecărei camere rămâne | Disc plin simulat în laborator, cu fiecare politică |
| STO-04 | „Criptarea câștigă” pentru orice combinație de reguli | Test pe proprietăți: 10.000 de seturi aleatoare de reguli → dacă o țintă potrivită cere `ENCRYPT`, planul e criptat |
| STO-05 | Previzualizarea, `ExplainRules` și outbox-ul dau același plan | Toate trei apelează `rules.Evaluate`; teste golden |
| STO-06 | Erorile și avertismentele din §9.6 apar exact în condițiile lor | Teste de tabel |
| STO-07 | Drive: zero `files.list` în operarea normală, ≤ 2 creări/s, pauză la plafonul zilnic | Drive fals care numără apelurile |
| STO-08 | OneDrive: fragmente multiplu de 320 KiB și < 60 MiB, fără `Authorization` pe `uploadUrl`, `Retry-After` respectat | Graph fals strict |
| STO-09 | S3: părți egale, lifecycle propus, `AbortIncomplete` setat; la presetările non-AWS, checksum-uri doar când sunt cerute | Fake S3 în modul R2 strict; fake care respinge antetele `x-amz-checksum-*` și trailerele `aws-chunked` neașteptate |
| STO-10 | Frâna de siguranță și suspendarea retenției la salt de ceas funcționează | Test cu ceas fals |
| STO-11 | Catalogul de pe destinație e actualizat în ≤ 5 min după o copie verificată, semnat, criptat conform T15 (ENC-15), iar `head` are noua versiune a zilei (§9.11) | E2E |
| STO-12 | Limita de bandă e respectată în ±10 % pe ferestre de 60 s; în afara ferestrelor nu se urcă nimic | E2E cu shaper |
| STO-13 | Alertele de cotă la 90 % și politicile „când e plin” se comportă ca în §9.8, inclusiv la destinațiile cu coș (pasul 7) | Furnizor fals cu cotă reglabilă; `fake-graph` cu coș care ocupă cotă: bucla nu șterge mai mult decât cere politica, iar destinația trece în `QUOTA_FULL` |
| STO-14 | Clasa copiei și eligibilitatea la ștergere urmează exact definiția din §9.4 | Teste pe proprietăți (rapid) cu ținte care amestecă `max_age_days` și `max_bytes`, plus cazuri golden în `ExplainRules` |
| STO-15 | Ferestrele orare și fereastra de update respectă regula orei de vară (§9.3) | Teste pe proprietăți pe tranzițiile Europe/Bucharest din martie și octombrie, pentru `rules.Evaluate` și pentru fereastra de instalare |

## §10 Criptare și chei

**Pe scurt (pentru proprietar).** Criptarea e alegerea utilizatorului, pe fiecare destinație și pe fiecare regulă; dacă două reguli se contrazic, criptarea câștigă. Folosim formatul deschis `age`, deci înregistrările se pot deschide și fără aplicația noastră, cu cheia de recuperare. Cheile au patru copii de rezervă, iar înregistrările se pierd doar dacă se pierd toate patru.

### 10.1 Cine alege și cum

- **ENC-01.** La adăugarea oricărei destinații, inclusiv discul local, aplicația întreabă explicit: „**Criptat (recomandat)**: fișierele se pot deschide doar cu aplicația sau cu cheia ta de recuperare” sau „**Necriptat**: poți deschide fișierele direct în Drive/OneDrive”. Nicio opțiune nu e preselectată și nu se poate salva fără alegere (E1). Test: UI + `invalid_argument` pentru `UNSPECIFIED`.
- **ENC-02.** Fiecare regulă poate suprascrie criptarea pe țintă, cu aceleași filtre: cameră, tip de conținut, tip de eveniment, program, destinație. Conflictele urmează „criptarea câștigă” (E2), ca în §9.4. Test: STO-04.
- **ENC-03.** Discul local al hub-ului e tratat ca orice destinație (E3): când rezolvă la „criptat”, segmentele, clipurile, miniaturile și inelul din modul „doar la mișcare” se scriu direct `.age` (T21, §4.5), deci video-ul nu atinge discul în clar. Test: HUB-04.
- **ENC-04.** Fiecare copie din catalog are `encrypted` și `key_generation`. Aplicația arată lacătul pe orice înregistrare a cărei copie redată sau listată e criptată (E4). Test: Playwright verifică iconița.
- **ENC-05.** O schimbare de setare se aplică doar înregistrărilor noi (E5). Ecranul arată câte înregistrări existente rămân în vechea stare; conversia lor vine după MVP. Test: E2E.
- **Configurarea criptării, o dată per cont.** Prima dată când o destinație sau o regulă cere criptare, salvarea așteaptă asistentul „Configurare criptare”: (0) dacă contul nu are TOTP sau passkey, asistentul cere activarea unuia (SEC-10), iar fără el criptarea nu se poate porni; (1) se generează cheia de recuperare și utilizatorul confirmă că a salvat-o (§10.4); (2) utilizatorii Google, Apple și cei doar cu passkey își setează „parola pentru înregistrări”, dacă nu au setat-o deja ca parolă de rezervă pentru acces (§10.3); (3) hub-ul generează `RI_1` și o distribuie dispozitivelor. Astfel nu există niciun fișier criptat fără toate căile de recuperare.

### 10.2 Formatul fișierelor

**age v1** (specificație deschisă C2SP; `filippo.io/age` v1.3.2 din 2026-08-29, BSD-3, verificat pe proxy.golang.org). Fiecare fișier are o cheie aleatoare de 128 biți, împachetată în antet pentru exact doi destinatari X25519: generația curentă `RI_g` și `RK`. Conținutul e criptat STREAM ChaCha20-Poly1305 pe bucăți de 64 KiB, fiecare cu tag de 16 octeți. Fișierele sunt binare (nu armored), cu extensia `.age`.

**De ce X25519, nu hibridul post-cuantic.** Decizie tehnică (completează T20): X25519 se decriptează cu orice `age` ≥ v1.0 și cu `rage`. Hibridul ML-KEM-768 (`age1pq`, v1.3+) nu se poate amesteca în același fișier cu destinatari clasici. Înregistrările se păstrează zile sau luni, deci riscul „stochează acum, decriptează mai târziu” e mic. Trecerea la hibrid se face la o rotație de generație (§10.5), cu cheie de recuperare nouă.

**Seek.** Bucata *i* a plaintext-ului începe în fișierul criptat la `len(antet) + 16 + i·(65536+16)`, iar nonce-ul e contorul *i* pe 11 octeți plus flagul „ultima bucată”. Deci orice interval se decriptează fără restul fișierului.
- **Go (hub):** `age.DecryptReaderAt(src, size, ids…)`, existent în v1.3.2. Poate fi apelat concurent, ține în cache ultima bucată și autentifică mărimea prin bucata finală. Cheia fișierului se ține în cache cu `ExtractHeader`/`DecryptHeader` + `NewInjectedFileKeyIdentity`, ca X25519 să nu se repete la fiecare interval.
- **TS (clienți, F6):** `age-encryption` (typage) v0.3.1 (BSD-3, peste @noble/*, MIT) decriptează antetul (`decryptHeader`) și acceptă chei X25519 WebCrypto ne-extractabile, dar decriptează doar ca flux. De aceea scriem `apps/packages/core/agerange` (~150 de linii peste `@noble/ciphers` și `@noble/hashes`), care decriptează intervale de bucăți.

- **ENC-06.** Fișierele noastre se decriptează cu `age` CLI (v1.0 și ultima versiune) și cu `rage`, folosind doar fișierul cheii de recuperare. Test în CI.
- **ENC-07.** `agerange` (TS) și `DecryptReaderAt` (Go) dau aceiași octeți ca decriptarea în flux pentru 10.000 de intervale aleatoare și trec vectorii de test C2SP CCTV age. Test: golden între limbaje.
- **ENC-08 (poartă Faza 0 (b) și (c), G13).** *Trece* dacă: (1) pe Pi-ul de test (modelul proprietarului; Pi 5 doar dacă e disponibil, Faza 0-bis), debitul de criptare al hub-ului e ≥ 50 MB/s pe Pi 5 și, pe celelalte modele, ≥ 10 × debitul total al camerelor recomandate ale modelului (≥ 20 MB/s pe Pi 4, ≥ 10 MB/s pe H1c), adică cel mult ≈ 10 % dintr-un nucleu; un model nemăsurat își păstrează estimarea din §4.8; (2) un salt într-un segment criptat de 5 min la 4 Mbps, servit de hub, dă primul cadru în ≤ 1,5 s; (3) `agerange` decriptează ≥ 12 Mbps (3× tempo real la 4 Mbps) în WebView-ul lui AND-1 (Faza 0) și în WKWebView pe iOS-1 (Faza 0-bis, O13); (4) ENC-06 trece. *Pică* altfel. Plan B, cu formatul **neschimbat**: pentru (1), numărul recomandat al modelului coboară până când pragul se respectă (minimum 1), iar modelul rămâne suportat (O16); pentru (2), hub-ul decriptează tot segmentul în bufferul de redare din RAM (§8.5) înainte să-l servească; pentru (3), F6 descarcă și decriptează segmentul întreg înainte de redare pe acea platformă. Pornirea e mai lentă, dar funcționează.

### 10.3 Ierarhia cheilor

| Cheie | Algoritm | Generată pe | Unde stă | Rol |
|---|---|---|---|---|
| `HIK`, identitatea hub-ului | Ed25519 | hub, la instalare | privată: sigilată pe hub (§4, §11); publică: în cloud, fixată pe clienți, în blob | semnează SDP, `PairingResult`, catalogul, rotațiile |
| `DK`, cheia dispozitivului | ECDSA P-256 | fiecare client | Secure Enclave / StrongBox-TEE / TPM prin CNG / WebCrypto ne-extractabil (IndexedDB), prin pluginul `device-keys` (§6.6) | `Hello` (CTR-10), cererile de înrolare. Decizie tehnică: P-256 e singura curbă disponibilă pe toate aceste suporturi hardware |
| `LWK`, cheia locală de împachetare | AES-256-GCM | fiecare client | Keychain / Keystore / DPAPI / WebCrypto ne-extractabil | criptează pe dispozitiv keyring-ul și `BK` |
| `RI_g`, identitatea de înregistrări (generația *g*) | age X25519 | hub | hub (sigilată), keyring-ul dispozitivelor, blob | destinatar al fiecărui fișier nou |
| `RK`, cheia de recuperare | age X25519 | **clientul**, la configurarea criptării | privată: doar la utilizator; publică: pe hub-uri și în blob | al doilea destinatar al fiecărui fișier; deschide și `BK` |
| Cheia fișierului | 128 biți aleatori | hub, per fișier | doar în antetul fișierului, împachetată pentru `RI_g` și `RK` | conținutul |
| `BK`, cheia de backup, cu versiunea `bk_version` | 256 biți aleatori | primul client al contului, la crearea contului, indiferent de criptare (T37) | dispozitive (sub `LWK`); în cloud doar împachetată | criptează blobul din cont |
| `KEK` | Argon2id → HKDF-SHA-256 | derivată pe client, la nevoie | nicăieri (efemeră) | împachetează `BK` în cont |
| `EK`, cheia de înrolare | Ed25519 din `HKDF(BK, "ipcam/enroll/v1")` | derivată din `BK`-ul curent | publică: pe fiecare hub, instalată cu `KeyService.SetEnrollmentKey` după împerechere și după fiecare rotație a `BK` | dovedește că un dispozitiv nou cunoaște `BK` (§10.5) |

**KDF-ul (T14).** Pentru conturile cu email și parolă: `master = Argon2id(parola, salt, m=64 MiB, t=3, p=4)`, din care `auth_key = HKDF(master, "ipcam/auth/v1")` (serverul păstrează doar un hash al ei, §5) și `KEK = HKDF(master, "ipcam/kek/v1")`. Pentru conturile Google/Apple, același calcul pe „parola pentru înregistrări” dă doar `KEK`. Sarea și parametrii se cer înainte de login; pentru un email necunoscut, cloud-ul întoarce o sare falsă deterministă (HMAC cu un secret de server), ca să nu se poată afla ce conturi există. Implementare: `golang.org/x/crypto/argon2` și `hash-wasm` (MIT) în clienți.

**`BK` pentru fiecare cont (Decizie tehnică, T37).** Fiecare cont primește la creare `BK`, blobul și `EK`, chiar dacă nu folosește criptarea, fiindcă și fără criptare calea (c) din §10.5 face ca un telefon nou să se logheze și să-și vadă hub-urile (E7.1, O8.5). La conturile cu email și parolă, `BK` se împachetează cu `KEK`-ul derivat din parolă, fără niciun pas în plus. La conturile Google, Apple și doar cu passkey, aceeași parolă are două roluri: „parola pentru înregistrări” din E7 și parola de rezervă pentru acces. **Propunere care cere aprobarea proprietarului (OPN-13):** aplicația o oferă încă de la onboarding, sub numele „parolă de rezervă pentru acces”, cu „Mai târziu” și un avertisment; dacă e setată, asistentul de criptare nu o mai cere. Până o setează, `BK` stă doar pe dispozitive (sub `LWK`) și, dacă există, împachetat pentru `RK`. În Faza 2 nu există încă `keybackup` (Faza 3); conturile create atunci primesc `BK` la primul login după release-ul Fazei 3.

**Blobul** `ipcam.backup.v1.KeyBackup` conține keyring-urile (toate generațiile `RI_g`, per hub), cheile publice `HIK`, `RK` public și directorul destinațiilor, fără secrete (T16). Cloud-ul (`KeyBackupService`, cu versiune pentru scrieri concurente) stochează: blobul criptat XChaCha20-Poly1305 sub `BK` (AAD = `account_id ‖ versiune`), `BK` împachetat cu `KEK`, `BK` criptat age pentru `RK` și parametrii KDF. `RK` privat nu intră niciodată în blob, altfel căile 3 și 4 ar deveni una singură.

- **ENC-09.** Nicio cheie privată sau secretă (`RI_g`, `BK`, `KEK`, `LWK`, `RK`, parola) nu ajunge în clar în cloud, loguri sau telemetrie. Cloud-ul primește doar blobul criptat, `BK` împachetat și hash-ul lui `auth_key`. Test: santinela ARH-08 cu valori de test pentru fiecare cheie.
- **ENC-10 (poartă Faza 0, G19).** Derivarea Argon2id durează ≤ 3 s în WebView-ul unui Android mediu. *Pică* → parametrii impliciți scad la `m=46 MiB, t=1, p=1` (pragul OWASP). Parametrii stau lângă blob și se schimbă per cont cu `AccountService.ChangeKdfParams` (§5.2), care înlocuiește în aceeași tranzacție hash-ul `auth_key` și `BK`-ul împachetat, fără altă migrare.

### 10.4 Cele patru căi de recuperare (E7)

Toate patru sunt active implicit și apar în aplicație doar dacă utilizatorul folosește criptarea. Înregistrările se pierd numai dacă se pierd toate patru.

| # | Calea | Cum se folosește | Ce o strică |
|---|---|---|---|
| 1 | Pe hub | `RI_g` sigilat; un telefon nou primește keyring-ul de la hub după înrolare (§10.5) | hub furat sau distrus |
| 2 | Pe fiecare dispozitiv logat | `WrapKeyringForDevice`: dispozitivul trimite un destinatar X25519 efemer, hub-ul întoarce keyring-ul ca fișier age, iar dispozitivul îl păstrează sub `LWK` | toate dispozitivele pierdute. Un browser nu se numără ca o copie sigură, fiindcă datele lui se pot șterge |
| 3 | În cont | parola contului sau „parola pentru înregistrări” → `KEK` → `BK` → blob. Un reset de parolă prin email **nu** deschide blobul; la următorul login pe un dispozitiv care are `BK`, aplicația reîmpachetează `BK` cu noua parolă | parola uitată și nicio altă cale |
| 4 | Cheia de recuperare | fișierul sau foaia tipărită: decriptează direct orice fișier (`age -d -i`) și deschide `BK`, deci tot blobul | foaia pierdută |

**Cheia de recuperare.** Textul e identitatea age `AGE-SECRET-KEY-1…` (74 de caractere bech32, fără diferență între majuscule și minuscule), afișată în grupe de câte 6. Foaia PDF are codul QR, instrucțiunile de decriptare fără aplicația noastră și data. Fișierul `ipcam-cheie-recuperare.txt` e în formatul de identitate age (cu comentarii `#`), deci merge direct cu `age -d -i`. Configurarea nu se termină până când utilizatorul scanează QR-ul de pe foaie sau tastează ultimele 8 caractere. `RK` se generează pe client; hub-ul primește doar destinatarul public (`KeyService.RegisterRecoveryRecipient` poartă doar cheia publică).

- **ENC-11.** `RK` privat nu ajunge pe hub, în cloud sau în telemetrie. Singura excepție e restaurarea explicită a unui hub nou doar cu cheia de recuperare, când nu există blob. Test: santinela ARH-08 cu `RK` de test.
- **ENC-12.** Fiecare cale, singură, ajunge ca să decriptezi un segment și catalogul. Test: 4 rulări E2E, fiecare cu celelalte trei șterse; contul de test are al doilea factor activ, cum cere SEC-10.

### 10.5 Dispozitive: adăugare, revocare, rotație

**Adăugare.** Un dispozitiv intră în lista de încredere a hub-ului pe una din trei căi: (a) aprobare dintr-un dispozitiv de încredere (`DeviceService.ApproveDevice`); (b) QR sau cod de pe hub, ca în F1; (c) **dovada de backup**: dispozitivul care a deschis blobul (calea 3 sau 4) semnează transcriptul `Hello` și cu `EK` și trimite `bk_version`, iar hub-ul îl verifică cu `EK` public; orice altă versiune decât cea curentă e respinsă cu `DEVICE_NOT_TRUSTED`. Calea (c) îndeplinește E7.1 („telefonul nou se loghează și vede tot”) fără ca cloud-ul să poată adăuga dispozitive, fiindcă nu cunoaște parola. La conturile Google, Apple sau doar cu passkey care nu și-au setat parola de rezervă pentru acces (§10.3), calea (c) merge doar cu cheia de recuperare, dacă există; altfel rămân (a) și (b). În toate cazurile, `HIK` vine din blob sau din QR, niciodată doar din cloud.

**Revocare** (`RevokeDevice` din aplicație sau `DeviceRevoked` din cont). În aceeași tranzacție SQLite în care scoate `DK` din listă, hub-ul pune `enrollment_key = NONE`, deci calea (c) e oprită până la o cheie nouă. Apoi închide sesiunile, creează **imediat** `RI_{g+1}` pentru fișierele noi, o distribuie dispozitivelor rămase la următoarea sesiune și intră în starea `BK_ROTATION_REQUIRED`. Dispozitivul revocat cunoștea `BK`, deci aplicația cere parola ca să rotească `BK` și blobul; după `PutBackup`, clientul care a rotit apelează `KeyService.SetEnrollmentKey{ek_pub, bk_version, sig_by_trusted_DK}` pe fiecare hub (doar cu `DEVICE_ADMIN`). Până atunci aplicația arată un banner permanent: „Admiterea prin parolă e oprită până confirmați parola.” Fișierele vechi nu se re-criptează (E5).

**Starea generațiilor (Decizie tehnică).** Pentru fiecare generație, hub-ul ține `KeyringGeneration{g, stare}`: `HUB_ONLY` → `ON_DEVICE(device_ids)` → `IN_BLOB(blob_version)` (§4.6). Clientul trimite `KeyService.AckKeyring{g, device_id}` după ce a păstrat keyring-ul primit prin `WrapKeyringForDevice` și `KeyService.AckBlob{g, blob_version}` după `PutBackup`. La fiecare început de sesiune, dacă hub-ul raportează o generație sub `IN_BLOB`, aplicația rulează sarcina „sincronizare chei” (actualizează blobul, apoi `AckBlob`). Justificare: hub-ul nu poate scrie blobul, fiindcă nu are `BK`, deci un client o face, iar starea arată cine și când.

**Rotația voluntară** (din setări, la migrarea hub-ului sau la trecerea la hibrid) folosește `RI_{g+1}` pentru fișiere noi doar după ce generația a ajuns în `ON_DEVICE` (un dispozitiv a confirmat primirea) sau în `IN_BLOB`. Până atunci rămâne `RI_g`.

**Rotația `HIK`.** Mesajul `IdentityRotation{old, new, sig_old, sig_new}`, semnat cu ambele chei, ajunge la clienți pe sesiunea autentificată și în registrul din cloud. Un hub furat nu se rotește: se scoate din cont și se împerechează unul nou (§8.6, pasul 8).

- **ENC-13.** După revocare, un fișier nou nu se poate decripta cu keyring-ul dispozitivului revocat, iar o dovadă `EK` derivată din `BK`-ul vechi e respinsă. Test: E2E care verifică și tranzițiile `enrollment_key = NONE` → `BK_ROTATION_REQUIRED` → `EK` nou.
- **ENC-14.** O rotație voluntară nu produce fișiere criptate pentru o generație care există doar pe hub; stările trec `HUB_ONLY` → `ON_DEVICE` → `IN_BLOB` doar prin `AckKeyring`/`AckBlob`. Test: E2E cu dispozitivele offline.
- **ENC-18.** Backup-ul automat (§13.5) nu conține secrete, iar keyring-ul unui dispozitiv revocat nu deschide niciun backup automat scris după revocare. Test: E2E: revocare → backup nou, scris imediat pentru `RI_{g+1}`; keyring-ul revocat nu îl deschide; santinela ARH-08 pe conținutul decriptat.

### 10.6 Ce rămâne în clar și de ce

| Metadată | Unde | De ce rămâne în clar |
|---|---|---|
| Căile fișierelor criptate: ID opac de hub și cameră, data, ora de start, tipul, calitatea | destinațiile | retenția și ștergerea fără decriptare, lifecycle S3 pe prefix, listarea din F6, găsirea după oră fără aplicația noastră (E6). Dezvăluie **când** au fost evenimente și câte fișiere există |
| Mărimea fișierelor, orele de upload | furnizorul | inevitabil |
| Antetul age | fișierul | destinatarii X25519 sunt anonimi; antetul nu dezvăluie cheile |
| `README.txt` | `.ipcam/<hub_id>/` | text generic, fără date personale; explică formatul (E6) |
| Sarea și parametrii KDF, mărimea și versiunea blobului | cloud | necesare înainte de decriptare |
| Catalogul local al hub-ului (SQLite), fără credențiale | discul hub-ului | hub-ul îl folosește permanent, iar o cheie pe aceeași mașină n-ar proteja nimic în plus; protecția vine din criptarea discului (BitLocker/LUKS, P9) |

### 10.7 Regula de criptare a catalogului (T15)

- **ENC-15.** Copia catalogului de pe destinația D e criptată (`.cat.age`, pentru `RI_g` și `RK`) dacă D are default „criptat” sau conține cel puțin un fișier criptat. Odată criptat pe D, catalogul nu mai revine în clar. Toate fișierele de catalog, criptate sau nu, sunt semnate cu `HIK` (FLW-21). Test: E2E pe ambele ramuri.
- **ENC-16.** Miniaturile și exporturile moștenesc criptarea (§9.4, pasul 4). Fișierul exportat și descărcat pe dispozitiv e un MP4 necriptat, iar aplicația spune asta. Test: E2E.

### 10.8 Limitări și avertismente pentru utilizator

Textele de mai jos (RO; EN în `i18n`) apar în asistentul de criptare și în „Despre criptare”; cele despre hub, telefonul nou și browser apar și în ecranele 13, 14 și 16 (§6.4). **ENC-17.** Toate textele există în ambele limbi. Test: CI enumeră cheile și pică dacă lipsește o traducere.

| Limitare | Text afișat |
|---|---|
| Fișierele nu se deschid direct la furnizor | „Fișierele criptate nu se pot previzualiza în Drive/OneDrive. Le poți reda în aplicație sau exporta oricând ca MP4.” |
| Pierderea tuturor căilor | „Dacă pierzi hub-ul, toate dispozitivele, parola și cheia de recuperare, nimeni (nici noi) nu mai poate deschide înregistrările.” |
| Resetarea parolei | „Resetarea parolei prin email nu deschide backup-ul cheilor. Îl refacem automat când te loghezi pe un dispozitiv care are deja cheile.” |
| Disc local necriptat | „Ai ales «Necriptat» pentru discul hub-ului: înregistrările stau acolo necriptate, chiar și cele care se urcă criptat în cloud. Activează BitLocker/LUKS sau alege «Criptat».” |
| Metadate vizibile | „Furnizorul vede orele și mărimile fișierelor, nu și conținutul.” |
| Doar pentru înregistrările noi | „Schimbarea se aplică înregistrărilor noi. Cele existente rămân cum erau.” |
| Revocarea | „Un dispozitiv revocat nu mai poate deschide înregistrările noi. Pe cele vechi nu le re-criptăm.” |
| Aplicația web | „Pe web, codul aplicației vine de pe serverele noastre. Aplicațiile instalate oferă garanția mai puternică.” (P3, §11.2) |
| Exportul | „Fișierul MP4 descărcat nu e criptat.” |
| Hub fără TPM și fără disc criptat (afișat când `Sealer.Backend()` = `software` sau la DPAPI fără criptarea discului, §11.8) | „Hub-ul dvs. nu are TPM și nici disc criptat. Dacă e furat, hoțul poate obține cheile și accesul la Drive, OneDrive sau S3 și poate citi înregistrările de acolo până revocați accesul (pașii din «Hub-ul meu a fost furat»). Activați BitLocker/LUKS sau folosiți un dispozitiv cu TPM.” |
| Telefon nou, fără parolă de rezervă (§10.3, OPN-13) | „Dacă pierdeți singurul telefon și nu ați setat parola de rezervă, un telefon nou poate fi aprobat doar de pe alt dispozitiv de încredere sau cu codul de pe hub (acces fizic la hub).” |
| Browserul ca dispozitiv (§6.7) | „Browserul poate șterge cheile acestui dispozitiv (de exemplu Safari, după 7 zile fără utilizare). Atunci browserul trebuie aprobat din nou; înregistrările nu se pierd.” |
| Costul | „Criptarea folosește puțin procesor: câteva procente dintr-un nucleu de Raspberry Pi pentru numărul recomandat de camere.” Cifra afișată e cea măsurată de ENC-08 pentru modelul hub-ului (pe Pi 5, 32 Mbps față de pragul de ≥ 50 MB/s înseamnă cel mult 8 % dintr-un nucleu; pe celelalte modele, pragul din ENC-08 ține costul sub ≈ 10 %) |

## §11 Securitate

**Pe scurt (pentru proprietar).** Am pornit de la întrebarea „cine ar vrea să vadă camerele și pe ce drum ar încerca?”; răspunsul e tabelul din §11.1. Regulile de bază: serverele noastre nu văd video și nici parolele camerelor; hub-ul nu primește conexiuni din internet; telefonul recunoaște hub-ul după o cheie fixată la împerechere; un update al hub-ului are nevoie de două semnături independente și apare într-un jurnal public; o parolă furată nu ajunge pentru a vedea camerele dacă aveți MFA sau passkey. Public spunem doar ce e literal adevărat (§11.2).

### 11.1 Bunuri protejate și modelul de amenințări

Bunuri: **B1** video live · **B2** înregistrări (hub și destinații) · **B3** credențialele camerelor · **B4** tokenuri și chei de stocare · **B5** cheile de înregistrări (`RI_g`, `RK`, `BK`, §10.3) · **B6** identitatea hub-ului `HIK` · **B7** cheile dispozitivelor `DK` · **B8** contul ipcam · **B9** integritatea auditului · **B10** cheile de semnare a update-urilor · **B11** metadate (IP-uri, prezență, orele vizionărilor) · **B12** rețeaua locală a utilizatorului · **B13** continuitatea înregistrării.

| ID | Actor | Bunuri | Calea de atac | Controale | Risc rămas |
|---|---|---|---|---|---|
| AM-01 | Atacator din internet (scanare în masă) | B1–B3, B12 | porturi deschise spre hub sau camere; API-ul go2rtc (CVE-2024-29192); UPnP | fără porturi de intrare și fără UPnP (SEC-01); sidecar-uri doar pe 127.0.0.1, surse `exec`/`echo`/`expr` eliminate (§4.2); verificarea porturilor expuse ale camerelor (SEC-22) | camere deschise de router din alte motive: le semnalăm, nu le putem închide |
| AM-02 | Dispozitiv compromis din LAN | B1, B3, B12 | atac pe 7443/7445; mDNS falsificat; ascultarea RTSP-ului camerelor | `LanService` minim (T12); `Hello` legat de DTLS + lista de încredere (CTR-10); mDNS dă doar indicii, identitatea se verifică cu `HIK` (SEC-09); digest în loc de basic; ghid VLAN | RTSP-ul camerei e necriptat pe LAN (limita protocolului) → VLAN; în primele 30 min ale unui hub nou, un dispozitiv din LAN îl poate revendica înaintea proprietarului (T36): se vede imediat și se repară cu resetarea locală (F1.X3) |
| AM-03 | Cameră ostilă sau compromisă | hub (B3–B6) | RTSP/SDP/SOAP/HTTP malformate, fluxuri uriașe, ceas fals | parsere în Go fără `unsafe`, `recover` per flux, limite (SEC-16), fuzzing (SEC-15); hub-ul nu execută nimic din ce primește | camera își poate trimite singură video-ul la producător; doar blocarea ieșirii ei în internet oprește asta |
| AM-04 | Hoț care ia hub-ul | B2–B6 | citirea discului | sigilare TPM/DPAPI (SEC-02); criptare locală (E3); ghid BitLocker/LUKS (SEC-18); scoaterea hub-ului din cont și rotații (§8.6 pasul 7) | cu sigilare `software` sau DPAPI fără criptarea discului, hoțul obține `RI_g`, parolele camerelor și tokenurile de stocare: citește tot de pe hub și poate citi destinațiile până se revocă accesul (§8.6 pasul 7); aplicația afișează asta (§10.8) |
| AM-05 | Cloud ipcam compromis sau angajat rău-intenționat | B1, B2, B5–B8, B11 | se dă drept hub, modifică SDP, adaugă un dispozitiv, citește backup-uri, cere tokenuri | cheie fixată + răspuns semnat (FLW-03, FLW-12); admitere doar pe căile din §11.3 (SEC-03); backup criptat pe client (§10); tokenuri de stocare doar pe hub (T11); fără acces permanent la producție (SEC-11) | aplicația web (§11.2); metadate; varianta cu cod înainte de verificare (SEC-04); oprirea serviciului |
| AM-06 | Cineva cu contul Google/Microsoft al utilizatorului | B2 | descarcă sau șterge înregistrări | criptare recomandată (E1); copii redundante prin reguli (S1) | fișierele necriptate se pot citi; ștergerea se compensează doar prin copii |
| AM-07 | Credential stuffing, phishing | B8 → B1 | login cu parola furată, apoi admitere prin dovada de backup (§10.5 c) | Argon2id pe client (fiecare încercare costă), limite (CLD-03), cod pe email pentru dispozitiv nou, MFA/passkey, `GetBackup` cu al doilea factor (SEC-10), alertă la admitere (SEC-13) | parolă + email furate, fără MFA → acces; de aceea cerem MFA când se configurează criptarea |
| AM-08 | Telefon furat sau pierdut | B1, B5, B7 | deschide aplicația | blocare biometrică (SEC-12); chei în Keychain/Keystore; revocare din alt dispozitiv sau din cont → rotație `RI` (§10.5) | telefon deblocat, cu blocarea aplicației oprită |
| AM-09 | Membru al casei, fost partener | B1, B2 | păstrează un dispozitiv de încredere ca să urmărească pe ascuns | lista dispozitivelor e vizibilă; auditul arată cine a privit și când (SEC-13); email la fiecare dispozitiv nou; revocare | proprietarul însuși abuzează: îl acoperă termenii și avertismentele legale (§12), nu tehnica |
| AM-10 | Atac pe lanțul de aprovizionare | toate | pachet npm/Go/crate compromis; GitHub Action modificată | SEC-19, SEC-20; build reproductibil (DEL-03) | pachet compromis care trece de perioada de așteptare și de scanere |
| AM-11 | Atac pe build sau pe semnare | B10 → toate hub-urile | livrarea unui update rău | prag 2 din 2 între builder și rebuilder independenți; rădăcină offline; jurnal public; hub-ul refuză metadatele expirate sau nejurnalizate (SEC-07, SEC-08) | cod rău-intenționat intrat în `main`: îl reduc recenzia dublă (§14.8) și transparența |
| AM-12 | Actor statal (ex. GRU 26165, CISA AA25-141A) | B1, B12 | exploatarea camerelor expuse și a parolelor implicite | avertismente la onboarding (SEC-22); fără port forwarding; patch critic în ≤ 72 h (SEC-21) | 0-day țintit |
| AM-13 | Releu TURN, operator de rețea | B1, B11 | interceptarea traficului | DTLS-SRTP cap la cap; credențiale TURN scurte (§5.5) | analiza traficului: cât și când |
| AM-14 | Conținut ostil în client | B7, B8 | deep link, XSS, catalog falsificat pe destinație | CSP strict + Trusted Types (APP-08); listă permisă de deep link-uri (APP-09); catalog semnat (FLW-21); tokenuri doar în stocarea sigură (APP-07) | — |

### 11.2 Ce garantează „zero-knowledge” și ce nu (P3)

| Situație | Ce rămâne adevărat | Ce nu e garantat | Atenuare |
|---|---|---|---|
| Aplicații instalate (Android, iOS, Windows) cu cheia hub-ului fixată | Un cloud compromis nu vede live-ul și înregistrările, nu adaugă dispozitive, nu se poate da drept hub | Metadate: orele vizionărilor; IP-urile ajung la TURN | — |
| Aplicația web | Ca mai sus, **cât timp** codul servit e cel publicat | Un server sau CDN compromis poate servi JS care fură chei la următoarea încărcare | release semnat și jurnalizat public (DEL-04); textul din §10.8; recomandăm aplicația instalată |
| Canalul de update (hub, Windows) | Un update cere 2 semnături independente și o intrare în jurnalul public | Cine controlează simultan codul din `main` și ambele chei poate livra cod rău tuturor | §11.4, DEL-03, DEL-04 |
| Magazine (iOS, Android) | Semnătura și review-ul Apple/Google | Un cont de dezvoltator compromis poate publica o versiune | 2FA hardware pe conturile de magazin; Play App Signing |
| Hub furat | Înregistrările de pe destinații rămân protejate dacă hub-ul are TPM sau disc criptat (BitLocker/LUKS) | Cu sigilare `software` sau DPAPI fără criptarea discului, hoțul poate obține `RI_g` și tokenurile și poate citi destinațiile până revocați accesul (§8.6 pasul 7) | TPM, BitLocker/LUKS (§11.8); textul din §10.8 |
| Destinație necriptată (alegerea utilizatorului, E1) | — | Furnizorul și oricine are acces la cont văd fișierele | alegere informată (E8) |

**Afirmații publice permise** (singurele, păstrate în `docs/legal/claims.md`, fiecare cu cerința sau testul care o dovedește, SEC-06, și verificate de avocat): „Video-ul nu trece prin serverele noastre; când e nevoie de releu, acesta vede doar date criptate.” (ARH-03) · „Parolele camerelor sunt păstrate criptat pe hub-ul dvs. și nu ajung pe serverele noastre.” (HUB-03, CLD-01, ARH-08) · „Înregistrările criptate se deschid cu cheile dvs.; noi nu păstrăm aceste chei în clar, ci cel mult o copie de rezervă criptată pe dispozitivul dvs., cu parola dvs. Aplicațiile instalate și update-urile hub-ului sunt semnate; fiecare versiune a hub-ului, a aplicației Windows și a aplicației web apare într-un jurnal public.” (ENC-09, DEL-04, SEC-07) · „În aplicațiile instalate, live-ul e criptat cap la cap.” (CTR-10, FLW-12) Sunt interzise „zero-knowledge” fără precizări, „nimeni nu poate vedea” și „doar local” (lecția Eufy, crit C16).

### 11.3 Împerechere, fixarea cheii, lista de încredere, roluri

- **Admiterea unui dispozitiv** în lista de încredere a hub-ului are exact trei căi (T23, §10.5): (a) aprobare de pe un dispozitiv de încredere cu `DEVICE_ADMIN`; (b) QR sau cod de pe hub (F1); (c) dovada de backup cu `EK`, pentru versiunea curentă a `BK` (după o revocare, calea (c) e oprită până la o `EK` nouă). Cloud-ul singur nu poate folosi niciuna: nu cunoaște `pairing_secret`, `BK` și nici cheile dispozitivelor.
- **Stările fixării** (afișate la „Hub-uri”): `QR_VERIFIED` (cheia din QR); `CLOUD_ONLY` (varianta cu cod, §8.1 pasul 9, cu bandă galbenă „identitate neverificată”); `USER_CONFIRMED` (amprenta comparată de utilizator); `LAN_VERIFIED` (verificare automată la primul contact pe LAN, după regulile din §11.5).
- **Amprenta comparată de utilizator are cel puțin 64 de biți** (16 caractere hex, §8.1; T24). Decizie tehnică: 6 cifre (20 de biți) se pot forța de un cloud compromis în câteva secunde, generând chei până se potrivesc. Șase cifre ar fi acceptabile doar ca SAS cu angajament (hub-ul publică `H(nonce_hub)` înainte să primească `nonce_app`); MVP-ul folosește amprenta de 64 de biți. Cât timp fixarea e `CLOUD_ONLY`, un cloud compromis care substituie cheia în fereastra codului poate impersona hub-ul; riscul rămas e în AM-05.
- **Rolurile sunt seturi de permisiuni** (`Permission`, §7.8): `owner` = toate, singurul care transferă sau scoate hub-ul; `admin` = toate, fără transferul proprietății; `operator` = `LIVE_VIEW`, `PLAYBACK`, `EXPORT`, `PTZ`, `RECORDING_CONTROL`; `viewer` = `LIVE_VIEW`, `PLAYBACK`; `live_viewer` = doar `LIVE_VIEW`. Un grant are rolul, lista de camere (sau „toate”) și, opțional, `expires_at`. În MVP există doar `owner` (O8.8), dar fiecare RPC verifică deja permisiunea.

### 11.4 Custodia cheilor de semnare a update-urilor

Cheia de update e „bijuteria coroanei” (crit lipsă #6): cine o deține poate citi înregistrările oricui. **Decizie tehnică (T26):** metadatele de update urmează The Update Framework (`github.com/theupdateframework/go-tuf/v2`, Apache-2.0). Toate cheile sunt ECDSA P-256, suportat de toate HSM-urile de mai jos.

| Rol TUF | Ce semnează | Chei și prag | Unde stau | Expirarea metadatelor |
|---|---|---|---|---|
| `root` | cheile celorlalte roluri | 2 din 3 | 3 YubiKey 5 (PIV): proprietar (acasă), proprietar (seif bancar), persoană de încredere sau avocat | 365 de zile; ceremonie anuală scrisă în `docs/runbooks/key-ceremony.md` |
| `targets` | hash-urile artefactelor | **2 din 2**: (1) builder = GitHub Actions, cheie în Google Cloud KMS (HSM, `europe-west3`), accesată prin OIDC doar din `ipcam-release.yml` pe tag-uri `ipcam-v*`; (2) rebuilder = AWS CodeBuild (`eu-central-1`), cont AWS separat, cheie AWS KMS | HSM-uri cloud, la doi furnizori diferiți | 180 de zile; re-semnare lunară automată, tot 2 din 2 |
| delegat `feeds` | feed-ul de vulnerabilități (F2) | 1 (KMS) | GCP KMS | 7 zile |
| `snapshot`, `timestamp` | prospețimea | 1 (online) | KMS, folosit de rolul `worker` al cloud-ului | 7 zile / 1 zi |

- **Rebuilder-ul semnează doar dacă build-ul lui, făcut independent din același tag, dă octeți identici** (DEL-03). Dacă poarta G20 pică, a doua semnătură vine tot din AWS KMS, dar după verificarea unei sesiuni de agent separate, iar canalul stable cere și semnătura proprietarului pe YubiKey (DEL-03). La Windows se compară digestul Authenticode al PE-ului, care exclude semnătura. **Transparență.** Fiecare versiune `targets` intră în Sigstore Rekor (`sigstore-go`, Apache-2.0). Hub-ul cere dovada de includere și o verifică offline, cu cheia publică Rekor livrată prin rădăcina noastră TUF. Un update trimis țintit unui singur utilizator devine astfel vizibil public. `ipcam-monitor.yml` caută orar intrări semnate cu cheile noastre care nu corespund unui release cunoscut.
- **Ce acoperă:** hub-ul, aplicația Windows (prin updater-ul hub-ului, §15.3), binarele din imaginea Docker, feed-ul și web-ul (la web doar jurnalizare, fiindcă browserul nu verifică; §11.2). Aplicațiile mobile le semnează Apple/Google. Un cloud compromis (cheile `snapshot`/`timestamp`) poate cel mult îngheța update-urile: hub-ul refuză metadatele expirate și orice versiune mai mică decât cea văzută ultima dată.

### 11.5 HTTPS pe LAN cu certificat per hub (P9)

- **Emiterea** e cea din §5.7: CSR pentru un wildcard per hub, ACME DNS-01 făcut de cloud, doar pentru hub-uri revendicate și în cotele din §5.7 (T35), cheia privată sigilată pe hub. **Decizie tehnică (T25): eticheta DNS e `dns_label`, 16 octeți aleatori în base32, nu `hub_id`.** `hub_id` e UUIDv7, conține ora creării, iar numele din certificate apar în jurnalele publice Certificate Transparency (CT).
- **Clientul nu se bazează doar pe certificat (Decizie tehnică: două moduri de verificare).** JavaScript-ul din browser, din Capacitor și din WebView2 nu poate citi certificatul TLS al serverului, deci legarea se face astfel. (1) **Pe toate platformele:** `GetIdentity` întoarce `Sign_HIK("ipcam-lan-id-v1" ‖ nonce_client ‖ hub_id)`, iar starea `LAN_VERIFIED` se setează abia după ce sesiunea WebRTC care urmează are răspunsul SDP semnat și `Hello` (CTR-10) verificate cu `HIK` fixat; amprentele DTLS sunt vizibile din JS, deci canalul e legat de hub. (2) **În aplicațiile native** (Capacitor, Tauri): `LanService` trece prin pluginul `lan-http` (§6.6), care expune SPKI-ul certificatului; semnătura `HIK` acoperă și `SHA-256(SPKI)`, iar un certificat expirat e acceptat dacă și numai dacă această semnătură se verifică. Așa un hub fără internet, cu certificatul neînnoit (Let's Encrypt scurtează durata spre 45 de zile), rămâne accesibil pe LAN din aplicațiile native. Un certificat emis greșit nu ajunge pentru a impersona hub-ul.
- **Fără domeniu (Fazele 0–1, T42).** Până există `<domeniu-hub>`, `lanapi` folosește un certificat autosemnat, generat pe hub. Shell-urile native îl acceptă doar dacă semnătura `HIK`, fixat la împerecherea locală (QR sau `ticket.txt`), acoperă `SHA-256(SPKI)`, după aceeași regulă ca la un certificat expirat (modul 2). În acest interval, browserul nu se conectează la hub pe LAN, în afară de unealta de spike de pe `http://localhost` din Faza 0 (T41). Test: APP-23.
- **Restricții:** CAA pe `<domeniu-hub>` permite doar Let's Encrypt și Google Trust Services, cu `accounturi`; `ipcam-monitor.yml` caută orar în CT certificate `*.<domeniu-hub>` care nu provin din `worker`; hub-ul acceptă doar antete `Host` cu propriile nume (anti DNS rebinding). **CORS (Decizie tehnică):** `lanapi` și cloud-ul acceptă exact originile `https://<domeniu>`, `https://beta.<domeniu>`, `capacitor://localhost` (iOS), `https://localhost` (Android), `http://tauri.localhost` (plus `https://tauri.localhost`, dacă se activează `useHttpsScheme`); schemele sunt fixate în configurația shell-urilor (§6.7), iar APP-19 testează câte un preflight per origine.
- **Volumul de certificate.** Cerem includerea `<domeniu-hub>` în Public Suffix List (ca `plex.direct`), ca limitele Let's Encrypt să se numere per hub. Până atunci: limite mărite la cerere și Google Trust Services ca a doua CA (§5.7).

### 11.6 Conturi, acces intern, blocarea aplicației, notificări

- **Conturi (P4)** după §5.2 (Argon2id pe client, limite, cod pe email pentru dispozitiv nou, Pwned Passwords cu k-anonimat, TOTP, passkeys, Google/Apple). În plus: aplicația propune un passkey la onboarding; la configurarea criptării cere un al doilea factor, fiindcă de acolo parola deschide blobul (AM-07); `GetBackup`, `DeleteAccount`, `ChangePassword` și dezactivarea MFA cer autentificare din ultimele 5 min, cu al doilea factor dacă există; Cloudflare WAF limitează `Login` și `Prelogin` per IP și ASN.
- **Fără acces permanent la producție (lecțiile Ring și Verkada).** Agenții AI nu primesc credențiale de producție pentru baza de date sau SSH; schimbă producția doar prin pipeline (DEL-08). Accesul de urgență („break-glass”) folosește o cheie SSH pe YubiKey-ul proprietarului, valabilă 8 h; fiecare folosire se jurnalizează și trimite email proprietarului.
- **Blocarea aplicației (P4, APP-13).** E propusă bifată la finalul onboarding-ului. Cere biometrie sau codul dispozitivului la pornire și după 5 min în fundal (0/1/5/15 min, configurabil). Blocată, aplicația închide sesiunile și nu afișează nimic. În lista de aplicații recente conținutul e ascuns: pe iOS cu un strat opac la `willResignActive`, pe Android 13+ (API 33) cu `setRecentsScreenshotEnabled(false)`, iar pe API 29–32 cu `FLAG_SECURE` doar cât timp aplicația e blocată. Decizie tehnică: în rest fără `FLAG_SECURE`, ca utilizatorul să poată face capturi de ecran. Pe web, blocarea opțională cere passkey sau parolă după 15 min de inactivitate.
- **Notificări (P8; push vine după MVP).** Regula se fixează de acum: payload-ul push e doar un blob criptat de hub pentru acel dispozitiv, decriptat de Notification Service Extension (iOS) sau de handler-ul FCM (Android). Nu conține niciodată imagini sau nume de camere în clar. Miniatura se aduce după atingere, prin sesiunea WebRTC (crit C14).

### 11.7 Jurnalul de audit (P5)

- **Conținut** (`audit_log` pe hub, înlănțuit prin hash, T13). Fiecare rând are `seq`, `ts`, `device_id`, rol, acțiune, țintă, detalii, rezultat și `correlation_id`. Acțiuni: pornirea și oprirea unei vizionări live per cameră (cu durata); redare (cameră, interval); export; PTZ (un rând per gest, cu direcțiile și durata, nu câte unul la 500 ms); presetări; modul de înregistrare; setări (diff fără secrete); camere, destinații, reguli și criptare; admiterea (calea a/b/c) și revocarea dispozitivelor; cheia de recuperare și împachetarea keyring-ului; update și rollback; diagnostic; backup și import; `Hello` eșuat; cheie nepotrivită. Nu conține niciodată imagini, parole, tokenuri sau chei.
- **Retenție (Decizie tehnică):** 365 de zile pe hub, configurabil între 90 și 730. Ștergerea pe loturi scrie un rând `TRUNCATE` cu ultimul hash eliminat, deci lanțul rămâne verificabil. `security_events` din cloud: 13 luni. Citirea cere `AUDIT_READ`; exportul e JSON sau CSV. La firme, jurnalul conține date despre angajați (§12.3).
- **Ancorare (Decizie tehnică).** Hub-ul pune zilnic `audit_head` (`seq`, `hash`) în `HubStatus`, ca schimbare aditivă în `link.v1` (CTR-05). Cloud-ul păstrează ancorele 400 de zile, iar aplicația verifică lanțul față de ele. O rescriere a jurnalului pe hub se detectează.

### 11.8 Hub: întărire, credențiale sigilate, criptarea discului (P1, P9)

- **Sigilare.** Windows: TPM 2.0 (NCrypt Platform Crypto Provider), cu DPAPI în contul serviciului ca rezervă. Linux: TPM (`/dev/tpmrm0`) dacă e dat containerului; altfel cheie software în `state/keys` (0600). `Sealer.Backend()` apare în aplicație. La `software`, și la DPAPI fără criptarea discului, aplicația afișează textul „Hub fără TPM și fără disc criptat” din §10.8.
- **Windows:** cont virtual `NT SERVICE\ipcam-hub`, ACL-urile din §4.9, reguli de firewall doar pe profilurile Private și Domain; lansatorul verifică semnătura TUF a slotului înainte de `exec` (§4.7). **Docker:** utilizator 10001, `--read-only` cu `/data` și `/tmp` scriibile, `--cap-drop ALL`, `--security-opt no-new-privileges`, profilul seccomp implicit, imagine distroless fără shell; `--network host` e un risc acceptat, fiindcă discovery și ICE au nevoie de el.
- **Rețea.** Hub-ul ascultă doar pe 7443/tcp, 7445/udp și 5353/udp (HUB-02). Ieșirile, documentate pentru firewall-uri: cloud, STUN/TURN, furnizorii de stocare, NTP, CDN-ul de update. Ghidul „rețea pentru camere” recomandă un VLAN fără ieșire în internet, accesibil doar hub-ului.
- **Criptarea discului.** Hub-ul raportează starea: BitLocker sau „Device encryption” pe Windows Home (citită de installer și reverificată la update); dm-crypt pe Linux, dacă se vede din container. Fără criptare, aplicația deschide ghidul BitLocker/LUKS. Pe Pi fără TPM, ghidul spune deschis că LUKS cu deblocare automată nu protejează la furt.

### 11.9 Intrări ostile și fuzzing (P7)

| Țintă (Go, `go test -fuzz`) | Pachet | Corpus inițial |
|---|---|---|
| RTSP (răspunsuri, antete, interleaved), SDP, depachetizare RTP H.264/H.265/AAC | `hub/internal/live`, adaptoarele gortsplib/mediacommon | fixture-urile R4 + capturi din simulatoare |
| ONVIF SOAP (Device, Media, PTZ, Events), WS-Discovery, bannere HTTP, digest/WWW-Authenticate | `hub/internal/cameras` | fixture-urile R4 |
| fMP4 (`moof`/`mdat`) la reconciliere și remux | `recorder`, `playback` | segmente din laborator, inclusiv trunchiate |
| `rtc.v1.Frame`, `link.v1.Envelope`, catalog și backup de pe destinații, antet `age` | `session`, `cloudlink`, `catalog`, `crypto` | fișiere golden (CTR-07) |

În TS se folosesc teste de proprietăți fast-check (MIT) pentru deep link-uri, `agerange` și citirea catalogului. ClusterFuzzLite (Apache-2.0) rulează 60 s per țintă atinsă de PR și 30 min per țintă nightly. Orice crash devine fișier în `testdata/fuzz/` și test de regresie înainte de merge.

### 11.10 Lanțul de aprovizionare

- **Lockfile-uri comise** (`go.sum`, `pnpm-lock.yaml`, `Cargo.lock`, Gradle, `Package.resolved`) și instalări înghețate (`--frozen-lockfile`, `-mod=readonly`). **Perioade de așteptare:** pnpm `minimumReleaseAge` = 7 zile (§6.1); Renovate propune doar versiuni de cel puțin 14 zile (§3.4), grupate; merge automat doar pentru patch-uri, cu CI verde.
- **CI:** GitHub Actions fixate prin SHA; `permissions:` minime; OIDC în loc de secrete lungi; secrete doar în mediile protejate `release`, `providers`, `deploy`.
- **Scanare:** `govulncheck` (cu graf de apeluri), osv-scanner (Apache-2.0) pe toate lockfile-urile, `cargo-deny`, `pnpm audit signatures`; SBOM CycloneDX 1.6 per artefact (Syft, Apache-2.0), verificat cu allowlist-ul din `tools/licenses/` (SEC-20).
- **Fork-uri vendored (go2rtc, MediaMTX):** un job săptămânal compară cu upstream și citește advisory-urile; patch-urile sunt listate în `hub/third_party/PATCHES.md`.

### 11.11 Cerințe

| ID | Cerință | Test |
|---|---|---|
| SEC-01 | Hub-ul nu deschide porturi spre internet, nu are cod UPnP sau NAT-PMP și face doar conexiuni de ieșire (P1) | ARH-02 + scanare externă a IP-ului public al laboratorului: 0 porturi |
| SEC-02 | Credențialele camerelor și tokenurile sunt sigilate; backend-ul apare în aplicație; `software` produce avertismentul din §11.8 | HUB-03, FLW-07; unit pe fiecare backend |
| SEC-03 | Un dispozitiv intră în lista de încredere doar pe căile a/b/c | Cloud de test rău-intenționat: `PairingClaim` fără secret, aprobare falsă, `EK` greșit → toate respinse; un dispozitiv revocat care prezintă o dovadă derivată din `BK`-ul vechi → respins |
| SEC-04 | Starea fixării e afișată; amprenta are ≥ 64 de biți; `CLOUD_ONLY` → `LAN_VERIFIED` automat la primul contact pe LAN | E2E: varianta cu cod + hub pe LAN |
| SEC-05 | Fiecare RPC verifică permisiunea; rolurile sunt cele din §11.3 | Test generat: fiecare RPC × fiecare rol → permis sau `PERMISSION_NOT_GRANTED` după tabel |
| SEC-06 | Textele din aplicație, de pe site și din magazine folosesc doar afirmațiile din §11.2; fiecare frază din `docs/legal/claims.md` citează cerința sau testul care o dovedește | CI caută frazele interzise în i18n, metadatele de magazin și site: 0 rezultate; CI pică dacă o frază din `claims.md` nu are citare |
| SEC-07 | Hub-ul refuză metadatele TUF sub prag, expirate, cu versiune mai mică sau fără dovadă Rekor | Depozite TUF construite pentru fiecare caz → refuz + alertă |
| SEC-08 | Custodia din §11.4: rădăcină offline 2 din 3, `targets` 2 din 2 la doi furnizori, ceremonie scrisă | Exercițiu anual de rotație într-un depozit de staging |
| SEC-09 | Verificarea LAN din §11.5: pe toate platformele, `LAN_VERIFIED` doar după SDP semnat + `Hello` verificate cu `HIK`; în aplicațiile native, semnătura `HIK` acoperă SPKI-ul, iar un certificat expirat e acceptat doar cu semnătura validă; `Host` străin → 421; CAA și monitorul CT active | E2E web: certificat valid dar alt `HIK` → `LAN_VERIFIED` nu se setează și sesiunea e refuzată; E2E nativ: certificat expirat + semnătură validă → acceptat, certificat expirat + semnătură greșită → refuz; test DNS rebinding |
| SEC-10 | Autentificare recentă + al doilea factor pentru `GetBackup`, `DeleteAccount`, `ChangePassword`, oprirea MFA; MFA cerut la configurarea criptării | E2E cu autentificator WebAuthn virtual |
| SEC-11 | Niciun secret de producție în CI în afara mediilor `deploy`/`release`; break-glass jurnalizat, cu email | Audit al configurației + exercițiu trimestrial |
| SEC-12 | Blocarea aplicației și ascunderea în lista de aplicații recente | Device farm (iOS, Android) |
| SEC-13 | Auditul are acțiunile, retenția și ancorarea din §11.7; admiterea prin `EK` alertează toate dispozitivele, iar alerta deosebește „browser reînrolat” de „dispozitiv nou” | HUB-21; E2E: rând șters pe hub → aplicația semnalează nepotrivirea cu ancora |
| SEC-14 | Payload-urile push nu conțin imagini și nici text în clar (după MVP) | Test pe serializarea payload-ului |
| SEC-15 | Țintele de fuzzing din §11.9 există și rulează pe PR și nightly | CI pică dacă lipsește o țintă din listă |
| SEC-16 | Limite pentru intrările de la camere: antet RTSP ≤ 16 KiB, SDP ≤ 64 KiB, răspuns SOAP ≤ 1 MiB, unitate de acces ≤ 8 MiB, timeout 10 s per cerere | Fixture-uri supradimensionate → eroare tipizată, fără creștere de memorie |
| SEC-17 | Configurația serviciului și a containerului din §11.8 | HUB-18, HUB-19 + `docker inspect` în CI |
| SEC-18 | Starea criptării discului e afișată, cu ghidul potrivit | Unit pe detecție + E2E pe text |
| SEC-19 | Lockfile-uri, perioade de așteptare, Actions fixate prin SHA; vulnerabilitățile High/Critical accesibile blochează merge-ul și release-ul | `ipcam-supply-chain.yml` cu dependență vulnerabilă injectată → roșu |
| SEC-20 | În artefactele livrate există doar licențe din allowlist: MIT, BSD-2/3, Apache-2.0, ISC, MPL-2.0, Zlib, 0BSD, Unicode, CC0, public domain; LGPL doar ca proces separat (FFmpeg). Decizie tehnică: Zlib, 0BSD, Unicode, CC0 și domeniul public sunt licențe permisive echivalente, adăugate față de lista din A2; LGPL e restrâns la proces separat, ca să evităm legarea dinamică pe mobil și pe Windows | SUC-06; dependență GPL injectată → roșu |
| SEC-21 | Termene de remediere: vulnerabilitate critică sau exploatată activ ≤ 72 h până la stable; High ≤ 14 zile; Medium ≤ 90 de zile; divulgare coordonată la 90 de zile | Registru de vulnerabilități cu termene + exercițiu pe runbook-ul CRA |
| SEC-22 | Avertismentele de la onboarding: parolă implicită, porturi expuse (inclusiv camere accesibile prin IPv6 global), firmware cu CVE | §8.2 + feed de test; cameră simulată cu IPv6 global și port deschis → avertisment |
| SEC-23 | Recomandare (decizia e a proprietarului, OPN-14): pentest extern pe hub, cloud și aplicații înainte de beta publică (MVP, Faza 4), apoi anual; dacă OPN-14 = obligatoriu, problemele High+ se închid înainte de beta publică | Raportul pentestului + issue-urile închise (doar dacă OPN-14 = obligatoriu) |

## §12 Confidențialitate și conformitate legală

**Pe scurt (pentru proprietar).** Pentru datele de cont, telemetrie și loguri suntem „operator” GDPR. Video-ul nu îl stocăm și nu îl putem vedea; când trece prin releul TURN, e criptat. Pentru imagini răspunde utilizatorul; pentru clienții firme suntem persoană împuternicită pentru releu și semnalizare, cu contract DPA. Produsul vine de la început cu presetări de retenție, audio oprit, avertisment pentru stradă și vecini, generator de afiș, export și ștergere de cont și fără recunoaștere facială. Cyber Resilience Act cere deja un proces de raportare a vulnerabilităților, care trebuie să existe înainte de prima beta publică. Punctele marcate ⚖ le verifică avocatul român (lista din §12.7).

### 12.1 Roluri GDPR

| Date | Rolul nostru | Rolul utilizatorului |
|---|---|---|
| Cont, autentificare, evenimente de securitate, telemetrie, loguri de acces și ICE (IP-uri), suport | **operator** (crit C17), cu politică de confidențialitate și registru de prelucrări proprii | persoană vizată |
| Video, înregistrări, catalog, audit pe hub | nu le prelucrăm: nu avem acces (P1, §3.3) | operator; exceptat ca „uz casnic” doar dacă nu filmează spațiu public (Recital 18, CJEU C-212/13 Ryneš) ⚖ |
| Clienți firme: signaling, releul TURN al fluxului criptat, backup-ul criptat de chei, conturile invitate (după MVP) | **persoană împuternicită**, cu DPA și listă de sub-procesatori ⚖ | operator |

### 12.2 Datele pe care le prelucrăm ca operator

| Date | Scop | Temei | Retenție |
|---|---|---|---|
| Cont: email, hash-ul cheii de autentificare, factori, passkeys, ID-uri Google/Apple, limbă | furnizarea serviciului | art. 6(1)(b) | până la ștergere; apoi ≤ 30 de zile, iar din backup-uri ≤ 35 (CLD-17) |
| `security_events` (IP trunchiat /24 sau /48) | securitatea contului | art. 6(1)(f) | 13 luni |
| Loguri de acces HTTP/WSS, cu IP complet | anti-abuz, depanare | art. 6(1)(f) | **14 zile** (Decizie tehnică) |
| ICE/TURN: la noi doar tipul perechii, fără IP; la Cloudflare, IP-urile clienților releului | releu, cost | art. 6(1)(b) | la noi: agregate 13 luni; la Cloudflare: conform DPA-ului lor ⚖ |
| Telemetrie **A**. Hub: versiune, OS/arhitectură, prezență, starea update-ului și rollback. Clienți: versiunea aplicației și octeții trecuți prin relay, per sesiune (pentru `relay_gb_month`, §5.5) | funcționarea serviciului, limitele planului | art. 6(1)(b) | brut 30 de zile, agregate 13 luni |
| Telemetrie **B**. Hub: nivel hardware, camere pe intervale, marcă/model/firmware, contoare de erori și upload, tipul perechii ICE. Clienți: tipul perechii ICE, timpul până la primul cadru, RTT, capacitatea de decodare hardware (§6.5), versiunea WebView (§13.6) | fiabilitate, bugetul TURN | art. 6(1)(f), cu opt-out | idem |
| Rapoarte de crash din aplicații | stabilitate | consimțământ (opt-in) | 90 de zile |
| Pachete de diagnostic trimise de utilizator, corespondență de suport | suport | art. 6(1)(b) | 180 de zile de la închiderea cazului |
| Blob de backup chei, registrul hub-urilor, drepturi, uz TURN lunar | serviciu, limite de plan | art. 6(1)(b)/(f) | până la ștergere; uz TURN 13 luni |
| Testul de porturi expuse (§8.2): IP-ul public al link-ului hub-ului și porturile testate | securitatea camerelor utilizatorului (F2) | art. 6(1)(b) | nestocat; rezultatul merge doar la hub (în logurile de acces, 14 zile, apare doar cererea) |
| Ancorele jurnalului de audit al hub-ului (`seq`, `hash`, fără conținut) | detectarea rescrierii auditului (§11.7) | art. 6(1)(f) | 400 de zile |
| Verificarea parolei la Pwned Passwords (§5.2): dispozitivul trimite direct la `api.pwnedpasswords.com` primele 5 caractere hex ale hash-ului SHA-1, iar serviciul vede IP-ul dispozitivului | protecția conturilor (P4) | art. 6(1)(f) | la noi nimic; la furnizor, după politica lui ⚖ |

**Decizie tehnică (ePrivacy, Legea 506/2004 art. 4(5)) ⚖:** categoria B rămâne opt-out până la avizul avocatului. Dacă avocatul cere consimțământ, flag-ul `telemetry_b_default` devine `false`, adică o alegere explicită, nebifată, la onboarding, fără altă schimbare de cod.

**Sub-procesatori** (listă publică; orice schimbare se anunță cu 30 de zile înainte): Hetzner Online GmbH (servere, PostgreSQL; DE, FI) · Scaleway SAS (backup criptat; FR) · Cloudflare, Inc. (DNS, CDN, WAF, TURN; UE + transfer în SUA prin DPF + SCC ⚖) · Brevo SAS (email; FR). Google LLC și AWS EMEA SARL țin doar cheile KMS de semnare, fără date personale (UE). Google și Apple sunt operatori independenți pentru login, nu sub-procesatori. Pwned Passwords (Have I Been Pwned) primește direct de la dispozitiv doar cererea k-anonimă, fără date de cont ⚖.

### 12.3 Funcții de confidențialitate în produs (P10)

**Presetări de retenție per cameră.** Asistentul întreabă „Ce vede camera?”. Presetarea aleasă dă retenția implicită și `retention_ceiling_days`, aplicat pe toate destinațiile (§9.3, §9.4). Valorile sunt Decizie tehnică, marcate ⚖.

| Presetare | Când | Continuu | Evenimente | Plafon | Ce mai face aplicația |
|---|---|---|---|---|---|
| `HOME_PRIVATE` „Acasă — doar proprietatea mea” | interior, curte proprie | 7 zile | 14 zile | fără | — |
| `HOME_PUBLIC` „Acasă — vede și stradă, vecini, spații comune” | Ryneš; asociații de proprietari (CJEU C-708/18) | 3 zile | 14 zile | 30 de zile | cere afișul, arată ghidul de măști |
| `BUSINESS_RO` „Firmă (România)” | angajați, clienți | 30 de zile | 30 de zile | 30 de zile (Legea 190/2018) | sub 20 de zile avertizează pentru Legea 333/2003 |
| `CUSTOM` „Personalizat” | utilizatori avansați | ales | ales | fără | text de responsabilitate |

- **Exporturile MP4 nu au plafon:** sunt acțiuni explicite ale utilizatorului (de exemplu probe la un incident), iar aplicația afișează asta. La un export dintr-o cameră `BUSINESS_RO`, aplicația afișează în plus: „Păstrați exportul doar cât e necesar (ex. incident documentat).” ⚖ (§12.7).
- **Audio oprit implicit** (`audio_enabled = 0`). Pornirea cere confirmare după textul despre Codul penal art. 226 și GDPR și intră în audit. Audio bidirecțional vine după MVP.
- **Spațiu public.** La `HOME_PUBLIC` și `BUSINESS_RO`, aplicația arată ghidul mărcii pentru măștile de confidențialitate (setate în interfața camerei) și o captură de verificare. Hub-ul nu modifică singur imaginea.
- **Generator de afiș și notă de informare** (ecranul 17, §6.4): PDF A4/A5 și PNG, RO/EN, cu pictogramă proprie și primul strat EDPB 3/2019 (operator și contact, scop, retenție, drepturi, unde e informarea completă). Varianta pentru firme adaugă temeiul legal și nota pentru angajați (Legea 190/2018 art. 5). Textele sunt revizuite de avocat ⚖.
- **Export de date și ștergerea contului.** `ExportAccountData` dă un JSON în ≤ 1 h (CLD-18). Ștergerea e în aplicație și la un URL web (APP-14, Apple 5.1.1(v), Play). Înainte de ștergere, aplicația explică faptul că înregistrările de pe hub și de pe destinații sunt ale utilizatorului, rămân acolo, și arată cum se șterg (dezinstalare cu „șterge înregistrările”, folderul de pe destinație). Cererile de acces ale persoanelor filmate le rezolvă utilizatorul (termen: o lună, GDPR art. 12(3)), cu exportul MP4 existent.
- **DPA pentru firme:** `docs/legal/dpa-template.md` (RO/EN), cu anexa de măsuri tehnice (din §10, §11) și lista sub-procesatorilor ⚖.
- **Fără recunoaștere facială, niciodată** (NOB-06; GDPR art. 9; AI Act Anexa III pct. 1(a), identificare biometrică la distanță, risc ridicat, și art. 5(1)(e) pentru bazele de date de fețe ⚖). Detecția de persoane și vehicule de după MVP va rula doar pe hub și va produce doar clase, fără identitate și fără șabloane biometrice (LEG-07).

### 12.4 Specific românesc ⚖

| Act | Ce cere (după cercetare; încredere medie sau mică) | Ce face produsul |
|---|---|---|
| Legea 190/2018 art. 5 | Monitorizarea angajaților doar cu informare prealabilă, consultarea sindicatului sau a reprezentanților, dovada că mijloacele mai puțin intruzive n-au mers; retenție ≤ 30 de zile, cu excepții justificate | `BUSINESS_RO`, plafonul de 30 de zile, nota pentru angajați |
| Legea 333/2003 + HG 301/2012 | La unitățile obligate la pază: analiză de risc, instalatori licențiați, cerințe minime, retenție minimă ≈ 20 de zile (încredere mică) | avertisment sub 20 de zile; nu certificăm conformitatea instalării |
| Codul penal art. 226 | Imagini sau sunet din locuință, fără drept; înregistrarea convorbirilor private | audio oprit, avertismente, clauză în termeni |
| ANSPDCP, Decizia 174/2018 | DPIA pentru monitorizarea pe scară largă a spațiilor publice | text de ghid în varianta pentru firme a generatorului |
| Legea 506/2004 | Accesul la informații de pe echipamentul terminal | §12.2, telemetria categoria B |

### 12.5 Cyber Resilience Act și răspunderea pentru produs

CRA (Regulamentul (UE) 2024/2847) se aplică și MVP-ului gratuit, fiindcă e pus la dispoziție în cadrul unei activități comerciale (crit C9). Intră în domeniu hub-ul, aplicațiile și cloud-ul, ca prelucrare la distanță a produsului ⚖.

| Data | Obligație | Ce facem |
|---|---|---|
| 11.09.2026 (în vigoare) | Art. 14: pentru o vulnerabilitate exploatată activ sau un incident grav: avertizare timpurie în 24 h, notificare în 72 h, raport final (14 zile după corecție, respectiv o lună pentru incident), prin platforma unică ENISA către DNSC; informarea utilizatorilor afectați ⚖ | `docs/runbooks/cra-reporting.md`, `/.well-known/security.txt`, politica de divulgare, cont pe platforma ENISA (dacă nu e disponibilă, direct la DNSC); exercițiu „la masă” înainte de prima beta publică (LEG-09) |
| 11.06.2026 | Organismele notificate devin operaționale | — |
| 11.12.2027 | Cerințele esențiale din Anexa I, documentație tehnică, evaluarea conformității, declarația UE, marcaj CE, SBOM, suport ≥ 5 ani, update-uri de securitate gratuite și disponibile ≥ 10 ani, punct unic de contact ⚖ | Hub-ul e tratat ca **probabil Clasa I** (Anexa III, „smart home… security cameras”, plus descrierea tehnică pentru software de control central, crit C9). Planul A: modulul A cu standarde armonizate aplicate integral, urmărite trimestrial. Planul B: organism notificat (module B+C), bugetat în §18. SBOM și update-uri semnate există din prima zi (DEL-05, §11.4) |

- **Perioada de suport:** ≥ 5 ani de la lansarea v1; data de sfârșit apare în aplicație și pe site (LEG-11). Documentația tehnică și SBOM-urile se păstrează 10 ani. **Directiva (UE) 2024/2853** (transpunere până la 9.12.2026): software-ul e produs, lipsa update-urilor de securitate e defect, pierderea datelor consumatorilor e daună, iar răspunderea nu se poate exclude prin termeni. Răspundem cu asigurare (§18), cu ingineria „fără goluri” (§9, §13) și cu evidențe de release păstrate 10 ani ⚖.

### 12.6 Magazine de aplicații (checklist la fiecare release, LEG-12)

| Regulă | Cerință | Cum o îndeplinim |
|---|---|---|
| Apple 2.1 | demo funcțional | cont demo + **hub demo permanent** pe un VPS al nostru, cu camere simulate (scenă fără persoane), folosit și de device farm (§14.7) |
| Apple 2.5.4 | moduri de fundal doar pentru scopul lor | doar `audio`, pentru PiP (§6.7) |
| Apple 3.1.1 | cumpărăturile digitale prin IAP | MVP gratuit, fără prompturi de plată; IAP, plata alternativă în UE sau link-out (crit C1) se decid odată cu monetizarea (O7) |
| Apple 4.2 | funcționalitate minimă, nu un „site ambalat” | PiP nativ, blocare biometrică, rețea locală, QR, share sheet, haptics, UI care merge offline. Decizie tehnică: o trimitere TestFlight externă (cu review Apple), ca probă, imediat ce login-ul și accesul remote la hub-ul demo funcționează (după Faza 2) și există contul Apple din Faza 0-bis (O13), cel târziu înainte de Faza 4 (G21). Justificare: App Review cere un demo funcțional (Apple 2.1), deci cont și acces remote, care nu există la finalul Fazei 1; panelul cere proba înainte să crească funcțiile |
| Apple 4.8, 5.1.1(i), 5.1.1(v); App Privacy | Sign in with Apple; politică de confidențialitate; ștergerea contului în aplicație; etichete și `PrivacyInfo.xcprivacy` | §6.4, §6.7; etichetele se generează din §12.2, fără tracking |
| Export de criptare | declarația din App Store Connect | criptare standard „mass market” (Reg. UE 2021/821, EAR SUA, autoclasificare) ⚖ |
| Google Play: stalkerware și supraveghere | fără monitorizarea ascunsă a persoanelor | poziționare „supravegherea proprietății”; aplicația și hub-ul sunt vizibile; iconița nu se ascunde |
| Google Play: Data safety, URL de ștergere, target API | declarații corecte | target 36 de acum; 37 estimat în aug. 2027 (§6.7) |
| Google Play: tipuri FGS | declarație cu video demo | MVP are un singur FGS, `dataSync`, pentru exportul pe Android 10–13 (API 29–33, §6.7), declarat cu video demo; orice alt FGS cere declarația înainte de release |
| Android developer verification | orice APK din afara Play trebuie înregistrat (global din 2027) | cont de organizație (SRL); niciun APK în afara Play în MVP; hub-ul Android de mai târziu se înregistrează. APK-ul de test instalat de proprietar pe propriul telefon în Fazele 0–1 (T41) nu e distribuție; dacă regulile de verificare ajung să-l blocheze, testul respectiv trece în Faza 0-bis (OWN-32) |

### 12.7 Lista pentru avocatul român (P12)

1. Valorile din §12.3: retenția presetărilor (inclusiv evenimentele de 14 zile la `HOME_PRIVATE`), plafonul de 30 de zile și minimul de ≈ 20 de zile din Legea 333/2003 + HG 301/2012; exceptarea „uz casnic” și avertismentul pentru spațiul public (Ryneš, C-708/18); inelul de 1 h din modul „la mișcare” (păstrare continuă temporară pe hub, care îi privește și pe vecinii filmați); exporturile fără plafon la camerele `BUSINESS_RO`.
2. Obligațiile clienților firme (Legea 190/2018 art. 5), nota pentru angajați, textele afișului; Codul penal art. 226 în textele pentru audio și în termeni.
3. Rolurile din §12.1, DPA-ul, sub-procesatorii, transferul către Cloudflare (DPF/SCC) și interogarea directă Pwned Passwords; politica de confidențialitate, termenii, temeiurile din §12.2, testul de interes legitim pentru telemetria B și Legea 506/2004.
4. CRA: domeniul (MVP gratuit), Clasa I, ruta de evaluare, textul perioadei de suport, runbook-ul art. 14; transpunerea Directivei 2024/2853 și asigurarea.
5. Clasificarea la export a criptării; afirmațiile publice din §11.2; aplicabilitatea European Accessibility Act (crit lipsă #14); temeiurile pentru interzicerea recunoașterii faciale (GDPR art. 9; AI Act Anexa III pct. 1(a) și art. 5(1)(e)).

### 12.8 Cerințe

| ID | Cerință | Test |
|---|---|---|
| LEG-01 | Presetările din §12.3 setează retenția și plafonul; plafonul se aplică pe toate destinațiile, în afara exporturilor; un export dintr-o cameră `BUSINESS_RO` afișează avertismentul din §12.3 | Unit pe `rules` + STO-06 |
| LEG-02 | Audio oprit implicit; pornirea cere confirmarea textului și intră în audit | E2E |
| LEG-03 | „Ce vede camera?” e obligatoriu la adăugare; `HOME_PUBLIC`/`BUSINESS_RO` cer afișul și arată ghidul de măști | E2E pe asistent |
| LEG-04 | Afișul are toate câmpurile primului strat; varianta pentru firme nu se generează fără identitatea operatorului | Unit + snapshot PDF |
| LEG-05 | Exportul și ștergerea contului, ca în CLD-17, CLD-18, APP-14, plus textul despre înregistrările care rămân la utilizator | testele CLD-17, CLD-18, APP-14 + E2E pe text |
| LEG-06 | DPA-ul și lista sub-procesatorilor sunt publicate; schimbările se anunță cu 30 de zile înainte | Checklist de release |
| LEG-07 | Nicio bibliotecă sau model de recunoaștere facială în artefacte | Denylist în verificarea SBOM (ex. `face_recognition`, `insightface`, `dlib`, modele `*face*`) |
| LEG-08 | Politica de confidențialitate e accesibilă din aplicație și din magazine; consimțământul pentru crash-uri e opt-in, revocabil și înregistrat | E2E + revizuirea metadatelor |
| LEG-09 | **Poartă înainte de prima beta publică:** runbook CRA, `security.txt`, politica de divulgare, cont ENISA/DNSC | *Trece:* exercițiul „la masă” produce în timp avertizarea de 24 h și notificarea de 72 h. *Pică:* beta publică nu pornește |
| LEG-10 | SBOM-ul fiecărui release e semnat și păstrat ≥ 10 ani | DEL-05 + politica de retenție a bucket-ului |
| LEG-11 | Data de sfârșit a suportului apare în aplicație și pe site | E2E |
| LEG-12 | Checklist-ul din §12.6 e bifat la fiecare release de magazin | Pas obligatoriu în `ipcam-release.yml` (checklist semnat) |
| LEG-13 | **Poartă înainte de prima beta publică:** avizul scris al avocatului pe punctele din §12.7 | *Trece:* aviz fără blocaje. *Pică:* punctele indicate se corectează înainte de beta publică |
| LEG-14 | Telemetria respectă categoriile A/B și flag-ul `telemetry_b_default` | Santinela ARH-08 + unit |

## §13 Fiabilitate și operare

**Pe scurt (pentru proprietar).** Hub-ul se supraveghează singur: repornește ce se blochează, reconectează camerele și vă spune în aplicație ce nu merge. Se actualizează doar dacă noua versiune e sănătoasă; altfel revine automat la cea veche. Mutarea pe alt PC se face cu un fișier de backup. Pentru suport trimiteți un pachet de loguri, fără imagini și fără parole.

### 13.1 Modelul de sănătate (R1)

Starea hub-ului e cea mai proastă stare a componentelor lui. Se vede prin `HubService.WatchHealth`, iar aplicația o actualizează în ≤ 5 s de la schimbare.

| Componentă | Semnale | OK | DEGRADAT | EȘUAT |
|---|---|---|---|---|
| Flux cameră (per profil) | biți/s, fps, vârsta ultimului keyframe, reconectări | fps ≥ 80 % din nominal; keyframe < 3 × GOP | fps < 80 % sau > 3 reconectări pe oră | fără date > 10 s |
| Evenimente | PullPoint activ, ultima reînnoire | reînnoit la timp | o reînnoire eșuată | fără abonament > 5 min (FLW-16) |
| Înregistrare | ultima parte scrisă, ultimul segment închis, goluri | parte < 3 s; segment < 330 s | gol < 60 s în ultima oră | nicio parte > 10 s, în modurile care înregistrează |
| Upload (per destinație) | cea mai veche copie în așteptare, starea (§9.7) | < 1 h | > 1 h sau 429/5xx repetat | `BLOCKED_AUTH`, `QUOTA_FULL` sau > 24 h |
| Disc | spațiu liber, timp până la plin, erori I/O | > 24 h până la plin | < 24 h | curățare de urgență sau înregistrare oprită |
| Card SD (doar când `rec/` e pe card, T40) | erori I/O (`EIO`, `EROFS`), remontare doar-citire, latența fsync | 0 erori I/O în 24 h și fsync p99 ≤ 500 ms pe 10 min | o eroare I/O în 24 h sau fsync p99 > 500 ms pe 10 min | 3 erori I/O în 60 s sau volum remontat doar-citire |
| Hub | CPU, RAM, decalajul ceasului, link cloud, MediaMTX, certificat LAN, sigilare | în bugetele §4.8 | CPU > 85 % 5 min; ceas > 2 s; link căzut > 5 min; certificat < 14 zile | MediaMTX în buclă de crash; certificat expirat |

### 13.2 Watchdog și reconectare

- **În proces:** fiecare modul `hubd` rulează sub un supervizor, cu heartbeat la 5 s. Un modul fără heartbeat 30 s e anulat și repornit; o panică e prinsă și repornește modulul (backoff 1→30 s) cu un raport de crash. 5 reporniri în 10 min trec modulul în EȘUAT, cu alertă, fără buclă infinită.
- **Procesul:** un watchdog separat verifică bucla principală. Dacă ea nu avansează 60 s, procesul iese cu codul 3 și e repornit de SCM (Windows), de politica de restart Docker sau de lansatorul A/B (§4.7). Înregistrarea reia în ≤ 10 s (HUB-31); pragul de ≤ 20 s rămâne doar pentru pornirea la rece (§4.8).
- **Backoff (full jitter):** cameră RTSP 1 → 60 s (HUB-13, §8.3 X2); abonament ONVIF reînnoit la 8 min, reabonare 5 s → 5 min; link cloud 1 → 60 s (§7.10); upload 1 s → 5 min, cu `Retry-After` (§9.9); client → hub 0,5 → 30 s, cu ICE restart la schimbarea rețelei (§6.3).

### 13.3 Alerte în aplicație (R1)

Fiecare alertă are `kind`, cameră sau destinație, `raised`, `cleared` și `acked` (§4.6). Se ridică o singură dată (fără duplicate) și se închide după 2 min de stare bună, ca să nu clipească.

| Alertă | Se ridică la | Acțiunea propusă |
|---|---|---|
| Cameră offline | fără date > 60 s | verificați alimentarea și rețeaua; ultima captură |
| Evenimente oprite | > 5 min (FLW-16) | modul trece temporar pe continuu |
| Înregistrare oprită | nicio parte > 10 s | detalii + pachet de diagnostic |
| Disc aproape plin / curățare de urgență | < 24 h până la plin sau < 10 % liber / la fiecare treaptă din §9.8, pasul 6 | reguli, retenție, alt disc |
| Card SD cu probleme (T40) | rândul „Card SD” din §13.1 trece în DEGRADAT sau EȘUAT | copiați clipurile importante și mutați înregistrarea pe un SSD (ghidul din aplicație) |
| Upload blocat | cea mai veche copie > 24 h, cu destinația accesibilă | detalii coadă |
| Reconectare necesară | `BLOCKED_AUTH` | „Reconectează Google Drive/OneDrive…” |
| Cotă cloud | ≥ 90 %; plin | politica „când e plin” |
| Ceas decalat | > 2 s | ghid NTP |
| Update eșuat sau rollback | la eveniment | nimic de făcut; raportat automat |
| Sistem fără update-uri de securitate | Windows 10 fără ESU (HUB-28) | Windows 11 sau un hub Docker |
| Protecție slabă a cheilor; certificat LAN | sigilare `software` sau disc necriptat; reînnoire eșuată și < 14 zile până la expirare | ghidul din §11.8; detalii |

### 13.4 Update automat (P6)

- **Descoperire.** `UpdateNotice` pe link și interogarea CDN-ului la 6 h (metadate TUF, §11.4), deci update-ul merge și fără cloud-ul nostru. Canale per hub: `stable` (implicit) și `beta`. Trecerea de pe beta pe stable așteaptă un stable ≥ versiunea curentă, fără downgrade.
- **Etapizare (Decizie tehnică).** Hub-ul intră în release dacă `H(hub_id ‖ versiune) mod 100 < rollout_percent`. Pe stable, procentul crește 5 % → 25 % (+24 h) → 100 % (+48 h). Creșterea se oprește automat la rollback > 2 % sau la bucle de crash > 1 % în cohortă (telemetria A).
- **Fereastra de instalare:** implicit 03:00–05:00, ora hub-ului (cu regula orei de vară din §9.3), configurabilă; amânare de cel mult 2 h dacă rulează un export sau o sesiune live. `urgent = true` (SEC-21) sare peste etape și se instalează în ≤ 72 h de la release.
- **Verificarea de sănătate după comutare** (completează HUB-14). În ≤ 10 min trebuie să fie adevărate toate: `hubd` și MediaMTX rulează, catalogul se deschide, fiecare cameră care înregistra are o parte nouă în ≤ 120 s, link-ul cloud e refăcut (dacă exista). Altfel hub-ul revine la slotul anterior și raportează. **Baza de date:** migrațiile versiunii N sunt doar aditive față de N-1; cele distructive vin abia în N+1, după ce N e stable. Înainte de migrare se face `VACUUM INTO state/backup/pre-<ver>.db`.
- **Clienți.** Mobil: magazinele. Web: versiunea nouă la următoarea pornire. Sub `min_peer_api_level`, aplicația cere update (CTR-06). Pe Windows, aplicația e actualizată de updater-ul hub-ului (§15.3).

### 13.5 Backup și migrare (P9)

- **Exportul manual** (`ExportHubBackup`, `ipcam.backup.v1`) conține configurația, secretele (resigilate pentru export), `HIK`, keyring-ul, lista de încredere, catalogul, jurnalul de audit și `hub_epoch`. E criptat `age` cu o frază de cel puțin 12 caractere. Decizie tehnică: în `age`, un destinatar cu frază trebuie să fie singurul din fișier.
- **Migrarea pe un PC nou.** Se instalează hub-ul și se alege „Restaurează din backup”. Importul face `hub_epoch + 1`, iar cloud-ul acceptă doar epoca cea mai nouă, deci hub-ul vechi e respins (HUB-23). Hub-ul cere un certificat LAN nou. Folderul `rec/` se mută separat (disc sau copiere), iar importul remapează calea (`rec_root`).
- **Backup automat (Decizie tehnică, T38).** Săptămânal, pe prima destinație cu criptare activă, se urcă doar configurația fără secrete, catalogul, lista de încredere și jurnalul de audit, criptate `age` pentru `RI_g` + `RK` (fără frază, deci nesupravegheat); se păstrează ultimele 4. La revocarea unui dispozitiv se scrie imediat un backup nou, pentru `RI_{g+1}` (ENC-18). Parolele camerelor, tokenurile și cheile de stocare și cheia privată `HIK` **nu** intră în backup-ul automat: ele părăsesc hub-ul doar în exportul manual protejat cu frază (P9). Se restaurează cu oricare cale din E7; după restaurare, utilizatorul reintroduce parolele camerelor și reconectează destinațiile, iar hub-ul are `HIK` nou (§8.6 pasul 8). Fără destinație criptată, backup-ul rămâne local, iar aplicația recomandă una. Dacă discul hub-ului moare: hub nou → „Restaurează din cont” (§8.6 pasul 8) sau din acest backup. Justificare: orice dispozitiv care are `RI_g`, inclusiv aplicația web, al cărei cod vine de pe serverele noastre (P3), ar putea altfel citi toate parolele camerelor; F2 și P1 cer ca ele să rămână pe hub.

### 13.6 Pachetul de diagnostic și telemetria (R2)

- **Pachetul** (`CreateDiagnosticBundle`, ZIP ≤ 20 MB) conține: logurile JSONL pe 7 zile, redactate la sursă (câmpuri tipizate, denylist, URL-uri cu credențiale mascate); `health.json`, `versions.json`; configurația fără secrete, cu camerele și destinațiile redenumite `cam-1`, `dst-1`… și IP-urile publice mascate; `ExplainRules` pe toate combinațiile; crash-urile; statistici de catalog; din audit doar numărătorile. Utilizatorul vede lista fișierelor, apoi îl trimite prin share sheet sau email la suport (HUB-15).
- **Telemetria.** Pentru hub: conținutul din §5.7 și categoriile A/B din §12.2. Clienții trimit zilnic, agregat per sesiune: în categoria A, versiunea aplicației și octeții trecuți prin relay; în categoria B (cu opt-out), **tipul perechii ICE** (local × remote ∈ {host, srflx, prflx, relay} × {udp, tcp, tls}), timpul până la primul cadru și RTT-ul pe intervale, capacitatea de decodare hardware (§6.5) și versiunea WebView. Rapoartele de crash sunt opt-in. ID-ul de instalare e aleator și se rotește la 30 de zile; nu se trimit IP-uri. Ponderea relay-ului alimentează DEL-11.

### 13.7 Specific Windows

| Problemă | Tratare |
|---|---|
| Serviciul nu vede unitățile mapate | NAS doar prin cale UNC + credențiale sigilate (T17), `WNetAddConnection2`; unitățile mapate sunt respinse cu explicație |
| Reporniri Windows Update | gol cu motivul `reboot` (§4.7); pornire automată fără întârziere; aplicația explică setarea „ore active” din Windows Update (Windows nu repornește în ele) și arată golurile `reboot` în timeline |
| Sleep, hibernare | cu acordul utilizatorului: fără sleep pe alimentare de la priză și fără suspendare selectivă USB pentru discul de înregistrare; resume (`PBT_APMRESUMEAUTOMATIC`) → gol cu motivul `sleep`; laptop pe baterie → avertisment |
| Antivirus | Defender exclude doar `rec\` (`Add-MpPreference`); alt antivirus detectat (WMI `SecurityCenter2`) → ghid; `ERROR_SHARING_VIOLATION` → reîncercare 5 × 200 ms, apoi alertă |
| Căi lungi | prefix `\\?\` pe căile de înregistrare |
| Windows 10 22H2 | suportat cu avertisment (T33, OPN-15): alerta din §13.3 și recomandarea Windows 11 sau a unui hub Docker; testele de fum rulează nightly pe un VM Windows 10 (§14.7, HUB-28) |

### 13.8 Disc plin și ceas

- **Disc plin.** Curățarea de urgență urmează §9.8, pasul 6, inclusiv regula pentru copiile neurcate. Decizie tehnică: un fișier rezervă de 512 MiB (`state/reserve.bin`, prealocat). Dacă după curățare rămân < 1 GiB, înregistrarea se oprește (gol `disk_full`, alertă) și rezerva se șterge, ca SQLite/WAL și logurile să se închidă curat; se refăce peste 15 GiB liberi. 3 erori I/O în 60 s pe `rec/` → alertă „disc defect”. Pe un card SD se aplică în plus pragurile din rândul „Card SD” (§13.1, T40).
- **Ceas.** NTP-ul sistemului, comparat la 6 h cu 2 servere SNTP și cu ora din `AuthOk` (§7.10); peste 2 s → alertă (HUB-12). Duratele segmentelor vin din ceasul monoton. Un salt al ceasului de perete > 2 s pune un marcaj `time_adjust` în catalog; un salt > 24 h suspendă retenția (§9.8). Pe Pi fără RTC, până la sincronizarea NTP (`STA_UNSYNC`), segmentele se marchează `time_uncertain` și se corectează după sincronizare, prin offset-ul monoton.

### 13.9 Operarea cloud-ului

- **Ținte:** disponibilitate 99,5 % pe lună pentru API și link; signaling p99 ≤ 150 ms (CLD-09); RPO ≤ 5 min, RTO ≤ 4 h (§5.9, T32); restaurare testată săptămânal (CLD-19). **Incidente:** SEV1 = cloud căzut, breșă sau scurgere de date; SEV2 = funcție degradată; SEV3 = restul. O alertă deschide automat un issue GitHub `ipcam-incident`, preluat de o sesiune de agent; la SEV1 proprietarul primește și email. Breșele de date personale merg la ANSPDCP în 72 h (`docs/runbooks/incident.md`), iar cazurile CRA urmează §12.5. Fiecare SEV1/SEV2 are postmortem în `docs/runbooks/postmortems/`.

### 13.10 Cerințe

| ID | Cerință | Test |
|---|---|---|
| REL-01 | Stările din §13.1 se calculează și ajung în aplicație în ≤ 5 s | E2E cu defecte injectate pe fiecare rând |
| REL-02 | Un modul blocat se repornește în ≤ 30 s; un proces blocat iese, iar înregistrarea reia în ≤ 10 s | Haos: goroutină blocată, `SIGSTOP` pe MediaMTX |
| REL-03 | Backoff-urile din §13.2, cu jitter | Unit cu ceas fals |
| REL-04 | Alertele din §13.3: praguri, fără duplicate, închidere după 2 min | Teste de tabel pe `health` |
| REL-05 | Update: etapizare, fereastră, verificarea de sănătate, rollback în ≤ 10 min; migrațiile N compatibile cu N-1 | TST-14 + versiunea N-1 pe catalogul N |
| REL-06 | Backup manual și automat. Restaurarea din exportul manual pe alt container păstrează tot, inclusiv parolele camerelor (HUB-23); restaurarea din backup-ul automat păstrează configurația, catalogul, lista de încredere și auditul, fără secrete (T38) | E2E pe ambele căi; santinela ARH-08: parola camerei de test nu apare în niciun obiect urcat pe destinații |
| REL-07 | Pachetul de diagnostic respectă §13.6 | HUB-15 (santinela) |
| REL-08 | Telemetria clienților are tipul perechii ICE, fără IP-uri | CLD-16 |
| REL-09 | Tratările Windows din §13.7 | Runner Windows: share SMB local prin UNC, sleep simulat, fișier blocat |
| REL-10 | Disc plin: rezerva, oprirea sub 1 GiB, catalog consistent | E2E pe volum mic |
| REL-11 | Ceas: `time_adjust`, `time_uncertain`, suspendarea retenției | Unit + E2E cu `faketime` |
| REL-12 | Ținte cloud, restaurare săptămânală, issue automat la alertă | CLD-19; alertă de test → issue creat |

## §14 Strategia de testare

**Pe scurt (pentru proprietar).** Fiecare schimbare trece automat prin teste cu camere simulate care trimit video real, cu ora și un contor de cadre „arse” în imagine. Așa măsurăm întârzierea, pierderile la pană de curent și redarea. Agenții pot îmbina o schimbare doar cu toate testele verzi și cu capturi de ecran ca dovadă. Drive-ul dvs. personal e folosit doar la teste manuale; testele automate folosesc conturi de test separate.

### 14.1 Piramida

| Nivel | Ce acoperă | Unelte | Când | Durată |
|---|---|---|---|---|
| Unit și proprietăți | logică pură, parsere, `rules.Evaluate`, componente UI | `go test -race`, `pgregory.net/rapid` (MPL-2.0), Vitest + Testing Library, fast-check | fiecare PR | ≤ 5 min |
| Contract | `buf lint`/`breaking`, golden-uri CTR-07, compatibilitatea CTR-06 | buf | PR; matricea CTR-06 nightly | ≤ 2 min |
| Integrare | drivere de stocare (STO-01), cloud cu PostgreSQL real, Pebble (ACME), coturn, simulatorul ONVIF | `go test` + containere | PR pe zona atinsă | ≤ 10 min |
| E2E în laborator | hub + cloud + camere simulate + aplicația reală | Docker Compose, Playwright | fiecare PR | ≤ 25 min |
| Platformă | emulator Android, simulator iOS (din Faza 0-bis, §14.7), WebView2 | §14.7 | PR pe path-urile lor | ≤ 30 min |
| Lung și haos | pană de 6 h, 20 × reset VM, stabilitate 72 h, furnizori reali, fuzzing lung, benchmark-uri | §14.7 | nightly / săptămânal | — |
| Dispozitive reale | device farm; hub-ul de test la proprietar | Firebase Test Lab, BrowserStack | înainte de fiecare release | — |
| Oameni | instalare → live (SUC-01, FLW-05) | câte 5 persoane non-tehnice pe Windows și pe Docker/NAS | Faza 4 | — |

Acoperire (Decizie tehnică): ≥ 80 % din linii în `hub/internal/*`, `cloud/internal/*` și `apps/packages/{core,player,api}`. Un PR nu poate scădea cu mai mult de 1 punct acoperirea unui pachet pe care îl atinge.

### 14.2 Laboratorul `e2e/`

```
┌─ wan (10.90.0.0/24) ──────────────────────────────────────────────────────────────┐
│ cloud (api, gateway, worker) · postgres · pebble · dns-hub · coturn                │
│ fake-gdrive · fake-graph · fake-s3 · webdav · sftp · toxiproxy (pene, latență)     │
│ client-remote: Playwright Chromium/Firefox/WebKit, cu netem (RTT, pierderi)        │
└──────────────▲─────────────────────────────────────────────────────────────────────┘
               │ router-sim: NAT cone | NAT simetric (forțează TURN) | UDP blocat (TURN-TLS/443)
┌─ lan (192.168.50.0/24) ─┴───────────────────────────────────────────────────────────┐
│ hub (imaginea livrată, neschimbată) · client-lan (Playwright) · smb (Samba, doar test)│
└──────────────▲─────────────────────────────────────────────────────────────────────┘
               │ a doua interfață a hub-ului
┌─ camnet (192.168.60.0/24, fără rută spre wan sau lan; ARH-01) ─────────────────────┐
│ camsim-1…N (ffmpeg → RTSP) · onvif-sim · replay-<marcă>-<model> (fixture-uri R4)    │
└────────────────────────────────────────────────────────────────────────────────────┘
```

Toate containerele rulează pe aceeași gazdă, deci au același ceas: latența se măsoară exact, fără sincronizare NTP. Uneltele GPL/AGPL din laborator (ffmpeg cu libx264/libx265, Samba) nu se livrează; SUC-06 verifică doar SBOM-ul artefactelor livrate.

### 14.3 Camere simulate și simulatorul ONVIF

- **`camsim`** (Go, `e2e/simulators/`) pornește ffmpeg cu `testsrc2` ca fundal și îi trimite prin pipe un overlay generat la fiecare cadru; ffmpeg compune, codează și publică pe un server RTSP de laborator. Fiecare cadru are o bandă de 2 × 48 de blocuri alb-negru cu ora (ms), contorul de cadre și poziția PTZ, plus aceleași valori ca text (`drawtext`). Testul citește banda din pixeli în `requestVideoFrameCallback`.
- **Variante:** H.264 main + sub (bază); H.265 main; H.264 cu B-frames; „smart codec” (GOP 10 s, keyframe variabil); 4K High@5.1; audio AAC și G.711; limită de 2 sesiuni RTSP; doar digest SHA-256; ceas decalat cu 300 s; deconectare la fiecare N minute; doar RTSP/TCP.
- **`onvif-sim`** extinde serverul virtual din `0x524a/onvif-go` (MIT, doar în laborator): Device, Media, Media2, PTZ, Events, Imaging. Starea PTZ mută scena desenată de `camsim`, deci un round-trip PTZ se vede în cadrul decodat. Ciudățenii activabile: `MoveStatus` care nu se actualizează (Hikvision); fără `RelativeMove` (Reolink); `MaxPullPoints = 5` și abonamente scurse; evenimente care tac; UsernameToken respins la decalaj de ceas; ONVIF oprit (port închis); zoom anunțat dar neexecutat (FLW-09).

### 14.4 Record-replay (R4)

Detectorul scrie fiecare schimb ONVIF/RTSP în `e2e/fixtures/<marcă>/<model>/<firmware>/`, ca transcript cu credențialele redactate și nonce-urile și orele normalizate; fixture-ul nu conține imagini (D4). `replay-*` potrivește cererile după operație și corpul normalizat. O cerere necunoscută întoarce 500, iar testul pică și o listează. Media e sintetizată de `camsim` cu parametrii din SDP-ul înregistrat (profil, rezoluție, fps, GOP). Fiecare fixture rulează suita „onboarding + live + PTZ + evenimente” la fiecare PR care atinge `hub/internal/cameras`.

### 14.5 Stocare: furnizori falși, furnizori reali, conturi de test

- **Falși (în PR):** `fake-gdrive` (upload reluabil, unități de cotă, plafonul de 750 GiB/zi, 403 `rateLimitExceeded`); `fake-graph` (regula de 320 KiB, `Retry-After`, coș de reciclare cu cotă); `fake-s3` pe `gofakes3` (MIT), cu mod strict R2; WebDAV (`golang.org/x/net/webdav`); SFTP (`pkg/sftp`); Samba. **Reali (nightly):** Google Drive, OneDrive personal, OneDrive business (tenant de test cu politicile Conditional Access din FLW-23), Backblaze B2, Cloudflare R2, Wasabi și un bucket AWS S3 cu SSE-KMS; conturile care cer plată sau date de plată (tenantul de firmă, B2, R2, Wasabi, AWS) se deschid doar după aprobarea bugetului, înainte de Faza 3 (Faza 0-bis, §16.9). După fiecare rulare, un job șterge folderele de test (sub 1 GB/zi).
- **Conturi (O11).** Drive-ul personal al proprietarului se folosește doar la spike-uri și teste manuale, prin clientul OAuth al proiectului Google Cloud al proprietarului. Datorită `drive.file`, aplicația vede doar fișierele create de ea. Nu se folosește niciodată în CI, iar tokenurile lui nu ajung în repo. CI folosește conturi Google și Microsoft dedicate, cu refresh token-urile ca secrete în mediul GitHub `providers`, disponibil doar pe `main` și pe joburile programate; PR-urile din fork-uri nu le primesc. Decizie tehnică: `ipcam-monitor.yml` reîmprospătează tokenurile săptămânal și rescrie secretele printr-o GitHub App cu drept doar asupra secretelor repo-ului, fiindcă refresh token-urile Microsoft expiră la 90 de zile. Cât timp proiectul Google e în modul Testing (T41), refresh token-urile Google expiră oricum după 7 zile, deci nightly-ul pe Drive real pornește după trecerea în Production (OWN-07, înainte de Faza 3).
- **Hub-ul de test la proprietar (mai târziu, R4).** Runner self-hosted `ipcam-home` (Pi-ul proprietarului, O14; N100 sau Pi 5, dacă se cumpără), cu camerele reale într-un VLAN izolat. Nightly: probă, 60 s de live cu `framesDecoded`, 10 min de înregistrare, PTZ doar pe camerele marcate „se poate mișca”. Primește doar joburi programate de pe `main`, niciodată cod din PR-uri.

### 14.6 Scenarii obligatorii

| ID | Scenariu | Aserțiune măsurabilă | PR / nightly |
|---|---|---|---|
| TST-01 | Primul cadru pe LAN | p90 ≤ 1,5 s (HUB-06 pe substream, HUB-27 pe fluxul principal), p50 ≤ 0,8 s | 10 porniri / 50 |
| TST-02 | Latență | LAN p95 < 1 s; remote prin TURN-TLS/443 cu RTT 100 ms, p95 < 3 s (FLW-10, SUC-02/03) | 20 / 100 |
| TST-03 | Video chiar rulează | 9 plăci, 10 min: `framesDecoded` crește în fiecare fereastră de 2 s; rata ≥ 90 % din fps nominal; `framesDropped` ≤ 2 %; contorul din bandă fără salturi > 1 s | 2 min / 10 min |
| TST-04 | Round-trip PTZ | comandă → starea simulatorului ≤ 150 ms p95; poziția vizibilă în cadrul decodat ≤ 1 s p95; `Stop` la dispariția clientului ≤ 1,2 s (FLW-13); doar capabilitățile confirmate (FLW-09) | ambele |
| TST-05 | Timeline și salt | 20 de salturi: primul cadru ≤ 1,5 s local și ≤ 5 s din cloud fals la 20 Mbps (FLW-17); ora din bandă în ±1 s față de țintă | ambele |
| TST-06 | Upload verificat | după 30 min, fiecare plasament `VERIFIED` are obiect cu aceeași mărime și același hash; 0 duplicate; 0 `files.list` (STO-07) | ambele |
| TST-07 | Pană de internet | 0 goluri în catalog în afara celor injectate; coada se golește după revenire; 0 segmente lipsă la destinații; hash-uri verificate (SUC-04) | 15 min / 6 h |
| TST-08 | `kill -9` (pană de curent) | pierdere măsurată cu contorul de cadre: mediana ≤ 1 s, maximul ≤ 2 s; catalogul se reconciliază singur (HUB-07) | 3× / 20× |
| TST-09 | Reset de VM | Decizie tehnică: hub-ul rulează într-o VM QEMU/KVM, iar `system_reset` pierde și cache-ul de pagini, mai aproape de o pană reală decât `kill -9`. Același prag ca TST-08; o rulare din 20 folosește un flux de 256 kbps cu criptare locală și limita din HUB-07; la eșec se deschide un issue legat de HUB-30 | — / 20× |
| TST-10 | Cloud oprit | live pe LAN + înregistrare 1 h, 0 goluri (ARH-04, HUB-24, APP-12) | ambele |
| TST-11 | Atac din cloud | cheie substituită, SDP modificat, revendicare fără secret, dispozitiv fals → toate respinse (FLW-03, FLW-12, SEC-03); santinela ARH-08 | ambele |
| TST-12 | Disc plin | ordinea de urgență; ultima oră păstrată (STO-03); rezerva (REL-10) | ambele |
| TST-13 | Codecuri | H.265 → rezervă H.264 (APP-05; în CI doar ramura de rezervă); Firefox × flux principal High → substream cu „profil video nesuportat” (§8.3 pasul 7); B-frames și smart codec → avertisment; 4K High@5.1 → avertisment la onboarding | ambele |
| TST-14 | Update | build stricat → rollback (HUB-14, REL-05); build bun → verificarea de sănătate trece | ambele |
| TST-15 | Fără hub | F6 pe Drive, OneDrive și S3 falși, cu fiecare cale de recuperare (FLW-20, FLW-21) | ambele |

### 14.7 Matricea CI și device farm

| Runner | Joburi |
|---|---|
| `ubuntu-24.04` (x64, KVM) | unit Go/TS, buf, lint, build încrucișat hub (Windows, Linux amd64/arm64/arm/v7; testele unit arm/v7 rulează sub QEMU user-mode, T39), laborator E2E pe Chromium + Firefox (obligatoriu) și WebKit (informativ, panel), emulator Android API 29 și 36, TST-09 nightly. **Decizie tehnică (H.264 în browserele de test):** Playwright fixat la o versiune al cărei Chromium vine din Chrome for Testing (≥ 1.57) sau `channel: 'chrome'`; un test canar pică imediat dacă `RTCRtpReceiver.getCapabilities('video').codecs` nu conține H264; Firefox rulează cu pluginul OpenH264 preinstalat în profil, altfel Firefox WebRTC devine informativ |
| `ubuntu-24.04-arm` | unit și imagine hub arm64, nativ; niciun E2E video, fiindcă Chromium open-source pe arm64 nu are H.264 |
| `windows-2025` | serviciul hub (instalare, ACL, UNC pe un share SMB local), Tauri + installer, Playwright `connectOverCDP` pe WebView2 cu hub nativ și `camsim` pentru Windows |
| `macos-15` | build iOS, smoke XCUITest pe simulator, WKWebView inspectat prin Safari (din Faza 0-bis iOS, O13) |
| self-hosted `ipcam-lab` (VM UE; din Faza 0-bis, OWN-22) | TST-07 de 6 h, stabilitate 72 h săptămânal, furnizori reali. Decizie tehnică: runner-ele găzduite de GitHub opresc joburile la 6 h |
| self-hosted `ipcam-win10` (VM Windows 10 22H2 pe gazda `ipcam-lab`) | testele de fum ale hub-ului (instalare, serviciu, 10 min de înregistrare), nightly, cât timp Windows 10 e suportat (HUB-28, OPN-15) |
| self-hosted `ipcam-h1` (Pi-ul proprietarului, oricare model suportat, O14; Pi 5 + SSD, dacă se cumpără, OWN-15; găzduit la proprietar, lângă `ipcam-home`; poate fi aceeași mașină) | benchmark-urile HUB-16, nightly; doar joburi programate din `main`, niciodată cod din PR-uri (§14.5) |

**Device farm**, la fiecare release stable și la fiecare beta care atinge `apps/`, contra hub-ului demo (§12.6). Firebase Test Lab pentru Android (un Samsung mediu din seria A, Redmi Note 13, un Pixel recent) și BrowserStack pentru iOS (iPhone cu iOS 17.x, 18.x, 26.x). Se verifică: login cu contul de test, grilă de 4 plăci 10 min (cu `framesDecoded` și, pe Android, temperatura), ecran complet, PiP, blocare biometrică (SEC-12), salt în înregistrare (APP-31).

### 14.8 Criterii de merge pentru PR-urile agenților

1. **Totul verde.** `ipcam-gate` (§15.4) așteaptă toate workflow-urile `ipcam-*` pornite pe același SHA și pică dacă unul a picat. După aprobarea OWN-35, GitHub refuză merge-ul fără `ipcam-gate` verde (ruleset pe `main`); până atunci, regula o aplică doar agenții.
2. **Artefacte atașate:** raportul JSON al aserțiunilor video (TST-01…05); capturi ale ecranelor schimbate, cu diferențele vizuale noi aprobate explicit în PR; trace și video Playwright la orice eșec, plus trace-ul ultimei rulări reușite pentru PR-urile de UI.
3. **Descriere și limite.** PR-ul listează ID-urile de cerințe atinse și testele care le acoperă. Acoperirea nu scade (§14.1); nu apar vulnerabilități noi High/Critical; licențele sunt OK; limitele de cod nativ sunt respectate (APP-01).
4. **Căi sensibile** (CODEOWNERS): `hub/internal/{crypto,session,lanapi,updater,cloudlink}`, `cloud/internal/{auth,pairing,keybackup,certs,devices}`, `contract/proto/**`, `.github/workflows/ipcam-release.yml`, `tools/licenses/`. Cer o a doua recenzie, cu checklist-ul de securitate, dată de o identitate separată: un GitHub App (sau cont bot) de recenzie, listat în CODEOWNERS pentru căile `/ipcam/...` și folosit doar de o sesiune de agent separată. Decizie tehnică: toate sesiunile de agent folosesc aceeași identitate GitHub, care nu își poate aproba propriul PR. CODEOWNERS stă în `.github/` de la rădăcină, deci rândurile `/ipcam/...` sunt o schimbare la rădăcină, aprobată de proprietar (OWN-35).
5. **Teste instabile.** Un test care trece doar la reîncercare devine issue și intră în carantină cel mult 7 zile. TST-01…15 nu pot intra în carantină.

### 14.9 Cerințe

| ID | Cerință | Test |
|---|---|---|
| TST-20 | Laboratorul pornește cu o singură comandă (`e2e/run.sh`), local și în CI, cu topologia din §14.2 | CI |
| TST-21 | Fiecare variantă și ciudățenie din §14.3 are cel puțin un test | Lista variantelor e generată și comparată cu testele |
| TST-22 | Criteriile din §14.8 sunt aplicate și impuse de GitHub (după OWN-35) | PR de probă fără artefacte → merge refuzat de GitHub; PR pe o cale sensibilă fără recenzia identității de recenzie → merge refuzat |
| TST-23 | Fiecare fixture R4 rulează suita din §14.4 | CI pe `hub/internal/cameras` |
| TST-24 | Conturile de test și rotația tokenurilor (§14.5); niciun secret disponibil PR-urilor din fork-uri | Audit al mediilor GitHub + job de rotație verde |
| TST-25 | Device farm verde înainte de fiecare release stable | Pas obligatoriu în `ipcam-release.yml` |
| TST-26 | Testul de utilizabilitate SUC-01 / FLW-05 în Faza 4 | Raport cu cronometrări |

## §15 Livrare și infrastructură

**Pe scurt (pentru proprietar).** Doar CI-ul construiește ce livrăm, iar fiecare artefact e semnat. Un update al hub-ului cere două semnături, din două locuri diferite, care trebuie să obțină exact aceiași octeți (dacă build-ul nu se poate reproduce, a doua semnătură vine după o verificare independentă, iar release-urile stable le semnați și dvs. pe YubiKey, DEL-03). Avem două canale: beta și stable. Cloud-ul stă în UE, cu backup la alt furnizor și monitorizare din exterior. Infrastructura variabilă (servere, TURN, Cloudflare, email) costă ≈ 30–70 €/lună la 100 de utilizatori și ≈ 245–1.355 €/lună la 10.000, în funcție de cât relay se folosește; la acestea se adaugă ≈ 68 €/lună costuri fixe. **Faza 0 costă 0 € (O13).** Estimarea pentru perioada de până la beta publică, ≈ 75–185 €/lună, se aplică doar după ce aprobați un buget (OPN-09, OPN-16). Detaliile, inclusiv scenariul de vizionare intensă, sunt în §15.6.

### 15.1 Artefacte

| Artefact | Conține | Platforme | Semnare | Distribuție |
|---|---|---|---|---|
| `ipcam-setup-<v>-x64.exe` (NSIS) | aplicația Tauri + serviciul `ipcam-hub` + MediaMTX + FFmpeg LGPL; componentele „aplicație” și „Acest PC e hub-ul meu” (implicit ambele); `/S /HUB_ONLY=1` pentru PC-uri fără ecran | Windows 11 x64; Windows 10 22H2 cu avertisment (T33) | Authenticode (Microsoft Artifact Signing sau, ca rezervă, certificat OV cu semnare în cloud; OWN-19) pe toate `.exe`/`.dll`; în TUF, digestul manifestului fișierelor interioare (§15.2) | site + updater |
| `ghcr.io/andreibesliu/ipcam-hub:<v>`, `:beta`, `:stable` + `ipcam-hub_<v>_linux_<arch>.tar.gz` (sloturile A/B, §4.7) | `ipcam-hub`, MediaMTX, FFmpeg static LGPL; distroless static, nonroot | linux/amd64, linux/arm64, linux/arm/v7 (plus o intrare linux/arm/v6 care doar explică lipsa suportului, T39) | imaginea: cosign (cheie KMS) + atestări SBOM și SLSA; binarele: TUF | GHCR; arhivele pe CDN |
| Android `.aab` | Capacitor | Android 10+ (minSdk 29), target 36 | cheia de upload (mediul `release`) → Play App Signing | Play: intern → testare închisă (beta) → producție etapizată |
| iOS `.ipa` | Capacitor | iOS 17.1+ | distribuție Apple (cheie API App Store Connect în `release`) | TestFlight extern (beta) → App Store, lansare etapizată în 7 zile |
| Web | SPA static + `release.json` (hash-uri SRI) | browserele din §6.7 | TUF `targets` + Rekor (doar transparență) | `beta.<domeniu>` și `<domeniu>` pe Cloudflare Pages (Decizie tehnică) |
| `ipcam-cloud` | api, gateway, worker | linux/amd64 | cosign | registry privat → VPS |
| `ipcam-detector` | binarul pentru Faza 0 | Windows x64, Linux, macOS | în Faza 0: SHA-256 și SBOM, fără Authenticode (§16.2.4); după obținerea certificatului (OWN-19): Authenticode pe Windows | GitHub Release `detector-v0.x`, link direct pentru proprietar |
| Feed de vulnerabilități | `feeds/vuln/v1.pb` (marcă/model/firmware → CVE) | — | rol TUF delegat `feeds` | CDN, zilnic |
| SBOM | CycloneDX 1.6 JSON per artefact | — | semnat împreună cu artefactul | atașat release-ului, păstrat ≥ 10 ani |

### 15.2 Lanțul de release

```
tag ipcam-vX.Y.Z (doar din main protejat)
 ├─▶ ipcam-release.yml (GitHub Actions): build reproductibil + teste + SBOM → semnătura 1 (GCP KMS, OIDC, doar tag-uri)
 └─▶ rebuilder (AWS CodeBuild, cont separat, eu-central-1): același tag, build independent
        octeți identici? → semnătura 2 (AWS KMS); altfel stop + issue
 ▼ targets (2/2) → Rekor → snapshot/timestamp (cheie online) → CDN
 ▼ beta imediat · stable după ≥ 7 zile pe beta, etapizat 5/25/100 % (13.4)
 ▼ AAB/IPA încărcate de CI → testare închisă / TestFlight → producție după aprobarea magazinului
```

**Reproductibilitate.** Go: `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, `-ldflags=-buildid=`, toolchain fixat. Web: Vite cu `pnpm install --frozen-lockfile` și `SOURCE_DATE_EPOCH`. Imagini: `ko` (Apache-2.0), cu baza fixată prin digest. Windows: se compară digestul Authenticode al PE-ului. Installer-ul NSIS nu poate fi identic octet cu octet, fiindcă builder-ul semnează Authenticode, cu marcaj de timp, fișierele `.exe`/`.dll` din interior: rebuilder-ul compară un manifest al fișierelor interioare, fiecare cu digestul Authenticode (fără semnătură), plus hash-ul scriptului și al configurației NSIS; TUF `targets` listează digestul manifestului, iar installer-ul exterior e semnat Authenticode doar de builder. Excepție documentată: FFmpeg LGPL se construiește o singură dată în `ipcam-ffmpeg.yml`, iar hash-ul lui se comite (`hub/third_party/ffmpeg/SHA256SUMS`); ambele build-uri îl verifică, nu îl reconstruiesc. AAB-ul și IPA-ul, semnate de magazine, au doar atestare de proveniență SLSA (`actions/attest-build-provenance`).

### 15.3 Canale de release

| Canal | Hub, Windows, Docker | Android | iOS | Web |
|---|---|---|---|---|
| beta | la fiecare release din `main` cu schimbări, cel mult zilnic | testare închisă | TestFlight extern | `beta.<domeniu>` |
| stable | la 2–4 săptămâni, după ≥ 7 zile pe beta, fără SEV1/SEV2 deschise, rollback < 1 % | producție 10 → 50 → 100 % | lansare etapizată Apple | `<domeniu>` |
| urgent (SEC-21) | ≤ 72 h, fără perioada pe beta, dar cu CI complet și smoke pe device farm | revizuire expeditată | revizuire expeditată | imediat |

**Decizie tehnică (T27): pe Windows, aplicația Tauri e actualizată de updater-ul hub-ului, nu de plugin-ul updater al Tauri.** Plugin-ul Tauri verifică o singură semnătură minisign, deci nu respectă pragul din §11.4. Când utilizatorul debifează „Acest PC e hub-ul meu”, serviciul `ipcam-hub` rulează doar ca updater: fără camere și fără porturi deschise (§6.7).

**Fără drept de scriere în Program Files (Decizie tehnică).** Serviciul `NT SERVICE\ipcam-hub` nu e administrator. Binarele aplicației Tauri stau în sloturile verificate TUF `<data>\app\{a,b}\ui\`; în `C:\Program Files\ipcam\` stau doar lansatorul hub-ului și un stub semnat, care verifică hash-ul țintei TUF înainte de `CreateProcess`. Orice update care cere drepturi de administrator (configurația serviciului, firewall-ul, bootstrapper-ul WebView2, lansatorul sau stub-ul) vine ca installer semnat, pe care utilizatorul îl aprobă prin UAC din aplicația Windows. Justificare: un serviciu cu drept de scriere în Program Files ar deschide, dacă e compromis, un drum spre sesiunea utilizatorului (APP-17, DEL-12).

### 15.4 Workflow-uri CI/CD (`.github/workflows/ipcam-*.yml`)

Fiecare workflow declanșat de push sau PR are `paths:` limitat la `ipcam/…` plus propriul fișier; cele programate sau pe tag-uri lucrează doar în `ipcam/`. Toate au `concurrency`, care anulează rulările depășite. `ipcam-gate.yml` adună rezultatele unui PR. **Propunere care cere aprobarea proprietarului (OWN-35):** ca să fie o verificare obligatorie fără să blocheze celelalte proiecte, `ipcam-gate.yml` rulează pe fiecare PR, fără `paths:` (singura abatere de la regula repo-ului, care cere `paths:`), și se termină cu succes în câteva secunde dacă PR-ul nu atinge `ipcam/**`; un ruleset pe `main` îl face obligatoriu. Ambele sunt schimbări la rădăcină (`CLAUDE.md` de la rădăcină). Până la aprobare, `ipcam-gate.yml` are `paths: ipcam/**` și e regula de merge din `ipcam/CLAUDE.md`, aplicată de agenți, nu de GitHub.

| Fișier | `paths:` / declanșare | Ce face |
|---|---|---|
| `ipcam-contract.yml` | `ipcam/contract/**` | buf lint/breaking/generare (CTR-02, CTR-03) |
| `ipcam-hub.yml` | `ipcam/hub/**`, `ipcam/contract/**`, `ipcam/go.work` | golangci-lint, unit + race, fuzz 60 s, govulncheck, build încrucișat (inclusiv arm/v7, cu teste unit sub QEMU), arm64 nativ |
| `ipcam-cloud.yml` | `ipcam/cloud/**`, `ipcam/contract/**` | unit, integrare (PostgreSQL, Pebble, coturn), migrații |
| `ipcam-apps.yml` | `ipcam/apps/**`, `ipcam/contract/**` | typecheck, lint, Vitest, build web, i18n, contor de linii native (APP-01) |
| `ipcam-e2e.yml` | `ipcam/{hub,cloud,apps,contract,e2e}/**` | laboratorul + TST-01…15 (varianta PR) |
| `ipcam-windows.yml` | `ipcam/hub/**`, `ipcam/apps/{web,desktop,packages}/**` | serviciul, Tauri + installer, E2E pe WebView2 |
| `ipcam-mobile.yml` | `ipcam/apps/{web,mobile,packages}/**` | emulator Android; build iOS + simulator (din Faza 0-bis, §14.7) |
| `ipcam-supply-chain.yml` | lockfile-uri, `ipcam/tools/licenses/**`; zilnic | SBOM, licențe, osv-scanner, cargo-deny, semnături npm |
| `ipcam-gate.yml` | `ipcam/**`; după OWN-35, toate PR-urile, cu ieșire rapidă în afara `ipcam/**` | agregarea pentru merge (§14.8) |
| `ipcam-nightly.yml` | `schedule` | fuzz lung, TST-07 (6 h), TST-09, furnizori reali, benchmark-uri, compatibilitate CTR-06 |
| `ipcam-release.yml` | tag `ipcam-v*` | §15.2, checklist-urile LEG-12 și TST-25, publicare beta, încărcare în magazine |
| `ipcam-ffmpeg.yml` | `ipcam/hub/third_party/ffmpeg/**` | build-ul fixat al FFmpeg LGPL |
| `ipcam-deploy-cloud.yml` | după release sau manual | deploy blue/green, migrații, rollback automat |
| `ipcam-monitor.yml` | `schedule`, orar | monitor Rekor și CT, expirarea metadatelor TUF, rotația tokenurilor de test |

### 15.5 Infrastructura cloud și TURN

- **Resurse** (§5.9): Hetzner (primar în DE, replică în FI), Scaleway Object Storage (backup), Cloudflare (DNS, CDN, WAF, TURN, Pages), Brevo (email). Decizie tehnică: resursele sunt descrise cu OpenTofu (MPL-2.0) în `cloud/deploy/`, iar secretele sunt criptate cu SOPS (MPL-2.0) + `age`. Deploy-ul folosește Docker Compose blue/green pe cele două VM-uri; un health check eșuat în 5 min readuce versiunea anterioară. Migrațiile sunt expand/contract. **Backup:** pgBackRest: WAL continuu + complet zilnic, criptat (AES-256), în Scaleway Paris, cu **retenție de 35 de zile**, aliniată la CLD-17. Restaurarea se testează săptămânal într-o VM temporară, cu numărători de rânduri și checksum-uri (CLD-19).
- **Monitorizare (Decizie tehnică, totul Apache-2.0):** VictoriaMetrics, VictoriaLogs, vmalert și Alertmanager pe o VM separată; loguri 14 zile, metrici agregate 13 luni. Probe blackbox la 60 s, de la un al doilea furnizor (Scaleway), pe `/healthz`, handshake-ul `link/v1`, o alocare TURN și CDN. Alertele ajung ca email + issue (§13.9).
- **TURN.** Cloudflare Realtime TURN acum (§5.5). Peste 2 TB/lună relay două luni la rând (DEL-11) trecem pe coturn 4.14+ (BSD-3), pe 2 VPS la 2 furnizori UE (Hetzner DE, Scaleway FR): UDP/TCP 3478 și TLS 443 (certificat Let's Encrypt), `use-auth-secret`, `no-cli`, `denied-peer-ip` pentru RFC 1918, ULA, link-local și 100.64/10, exporter Prometheus. Adăugăm un VPS când media pe 7 zile trece de 60 % din 1 Gbps pe nod; Cloudflare rămâne rezerva.

### 15.6 Modelul de cost

**Faza 0 costă 0 € (O13).** Toate liniile de mai jos, inclusiv coloana „Până la beta”, se aplică doar după ce proprietarul aprobă un buget (OPN-09, OPN-16). Până atunci, CI rulează gratuit pe repo-ul public (O12), testele folosesc dispozitivele proprietarului (O14), iar tot ce costă intră în Faza 0-bis (§16.9).

Ipotezele vin din land §2.0: 4 camere per cont, ≈ 10 GB/lună vizionare remote, 20 % prin relay, deci ≈ 2 GB relay per utilizator-lună. **Scenariul intens** (crit #12) e de 10 ori mai mare: ≈ 20 GB relay per utilizator-lună, sub limita anti-abuz `relay_gb_month` = 100 (§5.7). Prețuri: Cloudflare TURN $0,05/GB după 1 TB gratuit; VPS cu 20 TB incluși, ≈ 1 €/TB peste.

| Linie (€/lună) | Până la beta | 100, normal | 100, intens | 10.000, normal | 10.000, intens |
|---|---|---|---|---|---|
| Backend + PostgreSQL + backup (§5.9) | 6–20 (cloud-ul de test, din Faza 0-bis; un VPS de spike doar dacă testul remote nu are variantă gratuită, T41) | 10–20 | 10–20 | 150–400 | 150–400 |
| TURN | 0 (tier-ul gratuit) | 0 (0,2 TB, în tier-ul gratuit) | ≈ 45 (2 TB: $50) | ≈ 880 la Cloudflare (20 TB: $950) sau 50–150 self-hosted | self-hosted obligatoriu: 200–600 (Cloudflare: ≈ $10.000) |
| Cloudflare (DNS, CDN, WAF, Pages) + email | 0–20 (planurile gratuite ajung la teste) | 20–50 | 20–50 | 45–75 | 45–75 |
| **Infrastructură variabilă** | **≈ 6–40** | **≈ 30–70** | **≈ 75–115** | **≈ 245–1.355** | **≈ 395–1.075** |
| Fixe: Apple ≈ 8, Authenticode ≈ 10, domeniu ≈ 2, KMS ≈ 5, monitorizare și probe ≈ 10, runner `ipcam-lab` ≈ 15, tenant Microsoft de test cu Entra P1 ≈ 12, VPS pentru hub-ul demo permanent (§12.6) ≈ 6 | ≈ 68 | ≈ 68 | ≈ 68 | ≈ 68 | ≈ 68 |
| CI peste cota planului GitHub + device farm (estimare proprie) | 0–75 (repo public: CI gratuit, OWN-18; Firebase la utilizare ≈ 0–30; BrowserStack opțional ≈ 0–45) | 50–350 | 50–350 | 50–350 | 50–350 |
| **Total până la beta** | **≈ 75–185** | — | — | — | — |
| **Per utilizator, fără CI și device farm** | — | ≈ 1,0–1,4 | ≈ 1,4–1,8 | ≈ 0,03–0,14 | ≈ 0,05–0,11 |

Liniile fixe și cele de CI/device farm sunt estimări proprii, de reconfirmat la deschiderea conturilor (§18). După aprobarea bugetului, până la beta publică, totalul (≈ 75–185 €/lună) poate depăși 100 €/lună (OPN-09). Ca să rămână sub 100 €, se pot amâna: tenantul Entra P1 doar în luna testului G16 (−12 €), runner-ul `ipcam-lab` abia înainte de Faza 3 (−15 €), Authenticode și KMS abia înainte de canalul beta (−15 €, T44) și BrowserStack doar la nevoie (termenele din §16.9). Asigurarea, avocatul, organismul notificat și pentestul nu sunt incluse; apar în §18. Relay-ul e linia care decide costul, iar DEL-11 îl urmărește lunar.

### 15.7 Cerințe

| ID | Cerință | Test |
|---|---|---|
| DEL-01 | Orice artefact livrat e construit doar de CI, din `main` sau dintr-un tag `ipcam-v*` | Verificarea proveniențelor SLSA la release |
| DEL-02 | Hub-ul și updater-ul Windows instalează doar ce trece de §11.4 (prag, expirare, versiune, Rekor) | SEC-07 |
| DEL-03 | **Poartă (înainte de primul release pe canalul beta intern; infrastructura de semnare vine din Faza 0-bis, T44):** builder-ul și rebuilder-ul produc octeți identici pentru binarele hub (3 ținte), MediaMTX, bundle-ul web și layerele imaginii; la installer-ul Windows se compară manifestul fișierelor interioare, cu digestul Authenticode al fiecăruia (§15.2) | *Trece:* identic la 3 release-uri consecutive. *Pică:* a doua semnătură `targets` vine din AWS KMS după o verificare independentă făcută de o sesiune de agent separată, cu identitate GitHub App distinctă (sursă, SBOM, teste); proprietarul semnează cu YubiKey doar release-urile stable, o dată la 2–4 săptămâni (`tools/scripts/approve-release`, prin rolul TUF delegat `stable`, care cere semnătura lui); fiecare artefact nereproductibil are un issue până la remediere |
| DEL-04 | Fiecare release, inclusiv web, apare în Rekor; monitorul alertează la o intrare necunoscută în ≤ 1 h | Intrare de test semnată în afara pipeline-ului (staging) → alertă |
| DEL-05 | SBOM semnat per artefact; licențele și vulnerabilitățile se verifică înainte de publicare | SEC-19, SEC-20, LEG-10 |
| DEL-06 | Canalele și etapizarea din §15.3 și §13.4; stable doar după ≥ 7 zile pe beta | Unit pe calculul cohortei + checklist de release |
| DEL-07 | Workflow-urile se numesc `ipcam-*.yml` și au filtrele `paths:` din §15.4 | Lint pe `.github/workflows/ipcam-*.yml` |
| DEL-08 | Deploy cloud blue/green, cu rollback automat; migrații expand/contract | Deploy de probă cu health check stricat → rollback |
| DEL-09 | Datele cloud și backup-urile stau în UE; backup-ul are retenție de 35 de zile | CLD-20 + configurația pgBackRest |
| DEL-10 | Monitorizarea și probele externe din §15.5; o probă eșuată 3 min → alertă | API oprit în staging → issue + email |
| DEL-11 | Relay-ul lunar se compară cu §15.6; peste 2 TB două luni la rând → TURN self-hosted | Raport lunar automat |
| DEL-12 | Pe Windows, aplicația și hub-ul se actualizează prin același updater, inclusiv în modul „doar updater”; serviciul nu scrie în Program Files, iar update-urile care cer administrator trec prin installer-ul semnat, aprobat prin UAC (§15.3) | E2E pe runner Windows + verificarea ACL pe `C:\Program Files\ipcam\` și pe `<data>\app\` |

## §16 Faze, MVP și Faza 0

**Pe scurt (pentru proprietar).** Construim în cinci faze. Faza 0 nu costă nimic și nu cere iPhone (O13). Are 1–2 săptămâni de măsurători pe dispozitivele dvs. (PC-ul cu Windows 11, Galaxy A24, Raspberry Pi-ul), care încep după pașii gratuiți din §18 (detectorul poate porni imediat), și răspunde la întrebările care ar putea schimba planul: ce camere aveți și cum se comportă ele, dacă video-ul merge bine pe Android, în browser și pe Windows în varianta aleasă (A3) și dacă înregistrarea supraviețuiește penelor de curent și de internet. Ce ține de iOS sau costă bani stă în Faza 0-bis (§16.9): pornește după ce aprobați un buget și se face înaintea fazei care o cere; testul pe iPhone, cel târziu înainte de Faza 4. Primul lucru pe care îl primiți e **detectorul de camere**, un program pe care îl rulați o dată. Fiecare test din Faza 0 are o **poartă** cu prag de trecere și plan B. Dacă o poartă pică, aplicăm planul B scris dinainte, fără improvizații. Nu promitem date (vezi §2); fiecare fază se închide doar când trec toate criteriile din „definiția lui gata”.

### 16.1 Fazele pe scurt

| Faza | Ce livrează | Porți | Precondiții (§18) | Se închide când |
|---|---|---|---|---|
| 0 — Probe de risc, la cost zero și fără iOS (1–2 săpt. de măsurători, O13) | (a) detectorul; (b) spike video Android, web și Windows pe LAN; (c) spike înregistrare + upload pe PC-ul și pe Pi-ul proprietarului | G1–G19, partea din Faza 0 (§16.5) | (a): doar OWN-13, poate porni imediat. (b) și (c): doar rândurile „Acum (gratuit)” din §18: OWN-07 (Testing), OWN-08 (doar înregistrarea gratuită), OWN-10, OWN-12 (doar pentru testul remote gratuit), OWN-14 și OWN-15 (dispozitivele proprietarului) | partea din Faza 0 a fiecărei porți are verdict, elementele rămase sunt în §16.9 cu termenul lor, deciziile declanșate sunt scrise în §2 și raportul Fazei 0 e aprobat de proprietar (PH0-30) |
| 0-bis — Amânată: iOS sau costuri (§16.9) | verdictul iOS, testele remote și OneDrive rămase, infrastructura de semnare, conturile plătite, pregătirile juridice | părțile amânate din G2–G5, G7–G10 și G12–G16; G20, G21 | aprobarea bugetului de către proprietar, per element (OPN-09, OPN-16); un iPhone împrumutat pentru iOS | fiecare element are verdict înainte de faza care îl cere; iOS înainte de începutul Fazei 4 (PH0-41) |
| 1 — Hub + aplicația pe LAN | adăugare camere, live, PTZ, înregistrare locală, timeline, export, din shell-urile Tauri și Android (T42) | G20 (înainte de primul release pe canalul beta intern, care așteaptă infrastructura de semnare din Faza 0-bis, T44) | OWN-23, OWN-35 (gratuite); fără costuri: build-uri nesemnate pe hub-ul de test (T44) | §16.6, PH1 |
| 2 — Cloud | cont, împerechere QR/cod, acces de oriunde, TURN; web-ul pe LAN cu certificat public (T28, mutată din Faza 1) | G22; G21 rulează în Faza 0-bis (§16.9) | din Faza 0-bis: OWN-04 (domenii), OWN-11 (server UE), OWN-12 (Cloudflare), G8 complet și OWN-25 (avocatul pornește cel târziu acum) | §16.6, PH2 |
| 3 — Stocare în cloud | Drive, OneDrive, S3, NAS, WebDAV, SFTP; reguli, retenție, criptare, F6 | — | din Faza 0-bis: OWN-07 (Production, brand verificat; cere domeniul), OWN-08 (publisher verificat; cere SRL, O15), OWN-09, OWN-22, restul G14, G15, G16 și conturile reale de stocare pentru nightly | §16.6, PH3 |
| 4 — Aplicații native + magazine | Android, iOS (Capacitor sau, după T29, Flutter), Windows (Tauri + hub), publicare | G23–G25 | verdictul iOS din Faza 0-bis, înainte de începutul fazei (PH0-41); din Faza 0-bis: OWN-02, OWN-03, OWN-05 (cont de organizație), OWN-06, OWN-17, OWN-19, OWN-25, OWN-26, OWN-28…31; OWN-27 (gratuit) | §16.6, PH4 → **MVP / beta publică** |

**Decizie tehnică (cod din Faza 0).** Codul care rămâne în produs se scrie direct în modulul final: detectorul în `hub/internal/cameras` + `hub/cmd/ipcam-detector` (T09), player-ul în `apps/packages/player`, patch-urile MediaMTX în `hub/third_party/`, simulatoarele în `e2e/simulators/`. Scripturile de măsurare stau în `tools/spikes/` și se șterg după raportul Fazei 0, dacă nu sunt refolosite. Justificare: D5 cere ca detectorul să nu fie cod de aruncat, iar măsurătorile trebuie să ruleze pe codul care va fi livrat.

### 16.2 Faza 0 (a) — Detectorul de camere (primul livrabil)

**Ce face (D1–D5).** `ipcam-detector` e un singur fișier executabil, fără instalare. Proprietarul îl rulează o dată pe un PC din rețeaua camerelor. Programul găsește camerele, inclusiv pe cele cu ONVIF oprit, le identifică fără parolă unde se poate, iar cu parolă face un raport complet. Înregistrează și schimburile ONVIF/RTSP, cu datele sensibile redactate, ca fixture-uri pentru simulatoarele din laborator (R4). Detectorul nu trimite nimic pe internet: proprietarul ne trimite arhiva cu raportul.

**Experiența proprietarului (asistent în consolă, română implicit).**
1. **Pornire** (dublu-click pe Windows). Un ecran scurt spune ce face programul și ce nu face: nu salvează parole, nu salvează imagini, nu se conectează la internet, nu schimbă setările camerelor. Întrebare: „Continuăm? [D/n]”.
2. **Rețele.** Programul arată rețelele PC-ului (ex. `192.168.1.0/24`) și întreabă dacă le scanează. Se poate adăuga un interval privat (ex. VLAN-ul camerelor).
3. **Scanare** (≤ 60 s per rețea /24), cu bară de progres. Apoi un tabel: nr., IP, marcă/model (sigur / probabil / necunoscut), ONVIF da/nu, RTSP da/nu.
4. **Parole.** Pentru fiecare cameră (sau „aceeași pentru toate”): utilizator și parolă, cu tastarea ascunsă, sau Enter ca s-o sară.
5. **Probe.** La camerele PTZ: „Camera se va mișca puțin și va reveni în poziția inițială. Permiteți? [D/n]”. Pentru evenimente: „Dacă puteți, treceți prin fața camerelor în următoarele 60 de secunde.”
6. **Rezultat.** Rezumatul apare pe ecran, iar arhiva `ipcam-raport-AAAAMMZZ-HHMM.zip` se creează lângă program, cu mesajul: „Raportul nu conține parole sau imagini. Îl puteți citi în `raport.txt`. Trimiteți arhiva agentului: atașată în conversație sau pusă în folderul `ipcam-teste/` din Google Drive-ul dvs.” (O11: agentul o citește prin conectorul Drive al sesiunii; aplicația ipcam nu e implicată.) Conectorul Drive al conversației vede tot Drive-ul dvs., spre deosebire de aplicație, care vede doar fișierele ei (`drive.file`); agentul citește doar folderul `ipcam-teste/`. Dacă preferați, atașați arhiva direct în conversație.

**Linia de comandă** (pentru agenți și laborator):

```
ipcam-detector                                    # asistentul interactiv de mai sus
ipcam-detector scan [--range CIDR]... [--no-ptz] [--no-events] [--no-fixtures] [--lang ro|en] [--out DIR] [--json]
ipcam-detector scan --non-interactive --credentials-env IPCAM_DETECTOR_CREDS   # doar laborator/CI
ipcam-detector verify <arhivă.zip> [--secrets-env VAR]   # re-scanare de redactare înainte de publicare
ipcam-detector version
# coduri de ieșire: 0 ok · 1 eroare fatală · 2 nicio cameră găsită · 3 raport parțial · 4 verify a găsit date neredactate
```

#### 16.2.1 Cerințe funcționale

| ID | Cerință | Test |
|---|---|---|
| PH0-01 | Binar static pentru Windows x64, Linux x64/arm64/armv7 și macOS arm64/x64. Nu cere instalare și nici drepturi de administrator; pe Windows apare doar promptul de firewall pentru rețele private | Cross-build în CI; rulare de fum în laborator pe runner-ele Linux, Windows și macOS |
| PH0-02 | Descoperire (D1): sondă WS-Discovery (`dn:NetworkVideoTransmitter` și netipizată) pe fiecare interfață; SSDP `M-SEARCH`; sondele de descoperire ale mărcilor, doar citire (Hikvision SADP pe UDP 37020, Dahua pe UDP 37810, din memorie, de confirmat în laborator); scanare TCP pe porturile 80, 443, 554, 8000, 37777, 9000, 2020, 34567, 8899, plus 23 (doar pentru avertisment). Fiecare /24 se termină în ≤ 60 s | Laborator: camere simulate cu ONVIF pornit și oprit → toate găsite (ca FLW-06) |
| PH0-03 | Identificare fără parolă (D2), pe baza amprentelor din `hub/internal/cameras/fingerprint/rules/*.yaml`: scope-uri WS-Discovery, antetul `Server`, realm-ul `WWW-Authenticate` și titlul paginii HTTP, subiectul certificatului TLS, `Server` din RTSP `OPTIONS`, ONVIF `GetSystemDateAndTime`/`GetDeviceInformation` dacă răspund anonim, sondele mărcilor, OUI-ul MAC din tabela ARP. Fiecare identificare are încrederea (sigur/probabil) și dovezile folosite. Ce nu se potrivește rămâne „necunoscut”, fără ghicit | Replay pe fixture-uri: marca și modelul așteptate pentru fiecare; un dispozitiv necunoscut → „necunoscut” |
| PH0-04 | Credențiale (D2): stau doar în RAM și se suprascriu la final. Nu ajung pe disc, în loguri sau în mesajele de panică. Cel mult 3 încercări de autentificare per cameră și set de credențiale. Nicio încercare cu parole ghicite sau implicite (Decizie tehnică: fără blocarea conturilor camerelor și fără comportament de unealtă ofensivă; la fel ca F2) | Testul santinelă (PH0-15) + simulator care numără încercările |
| PH0-05 | Tipuri de autentificare: WS-UsernameToken (digest/text), digest HTTP și RTSP cu algoritmii oferiți (MD5, SHA-256), basic oferit, acces anonim permis la ONVIF sau RTSP | Simulator configurat pe fiecare variantă |
| PH0-06 | Decalajul de ceas: `GetSystemDateAndTime` comparat cu ceasul PC-ului, la ±1 s, plus configurația NTP a camerei. Decizie tehnică: raportul spune că referința e ceasul PC-ului, fiindcă detectorul nu iese pe internet | Simulator cu ceas decalat cu +37 s → raport 37 ± 1 s |
| PH0-07 | Fluxuri (D2), pentru fiecare profil Media1/Media2: URI-ul RTSP fără credențiale și cu gazda pseudonimizată; codec; `profile-level-id` H.264 sau profilul H.265; rezoluție; fps, bitrate și GOP configurate **și măsurate** pe 10 s (RTSP PLAY, citirea antetelor NAL, fără decodare de imagini); B-frames; suspiciune de smart codec (GOP variabil); codec audio. La camerele fără ONVIF, cu parola introdusă, încearcă căile RTSP implicite ale mărcii (Hikvision `/Streaming/Channels/101`, Dahua `/cam/realmonitor?channel=1&subtype=0`, Tapo `/stream1`, Reolink etc.), cel mult 8 încercări | Simulatoare H.264, H.265, cu și fără B-frames → valori exacte (±5 % la bitrate) |
| PH0-08 | PTZ real (D2), doar cu acordul explicit per cameră. Citește ce anunță camera (`GetNodes`, `GetConfigurations`, spații, limita de presetări). Probează fiecare axă: `ContinuousMove` 300 ms + `Stop`, verificat prin diferența de poziție din `GetStatus` sau, dacă lipsește, prin diferența a două snapshot-uri calculată **doar în memorie**. Raportează dacă `MoveStatus` se actualizează. Presetări: listă, plus `SetPreset`/`GotoPreset`/`RemovePreset` probate cu o presetare temporară `ipcam-probe`, ștearsă la final. Camera revine la poziția inițială | Simulator care anunță zoom dar nu-l execută → „zoom: anunțat, nefuncțional”; poziția revine în toleranța simulatorului |
| PH0-09 | Evenimente (D2): topicurile din `GetEventProperties`; `CreatePullPointSubscription`, `Renew`, `Unsubscribe`; `MaxPullPoints`; ascultare 60 s, cu toate camerele în paralel; dacă există topic de mișcare și dacă a sosit un eveniment. Nu lasă niciodată abonamente în urmă, nici la Ctrl-C | Simulator cu `MaxPullPoints=5`: după 10 rulări, 0 abonamente rămase |
| PH0-10 | Avertismente de securitate (D3), fiecare cu severitate, explicație RO/EN și pași de remediere: (a) parola introdusă e în lista de parole implicite a mărcii sau e `admin` cu sub 8 caractere (comparație locală); (b) porturi riscante deschise (Telnet 23, DVR-IP 34567 cu CVE-2022-45045); (c) porturile camerei apar în tabela UPnP de port mapping a routerului (citire `GetGenericPortMappingEntry`) sau camera are UPnP pornit (răspunde la SSDP); (d) firmware mai vechi decât lista de vulnerabilități inclusă în detector (instantaneu datat, cu CVE); (e) ONVIF sau RTSP accesibil anonim; (f) autentificare basic peste HTTP; (g) doar HTTP, fără HTTPS. Decizie tehnică: clientul UPnP de citire stă doar în `hub/cmd/ipcam-detector`, nu în `hubd` (ARH-02 rămâne adevărat) | Câte un caz de simulator per avertisment |
| PH0-11 | Doar citire: nicio schimbare de configurație pe camere sau router, nicio repornire. Singura excepție e presetarea temporară din PH0-08, care se șterge | Simulatorul înregistrează operațiile de scriere: doar `SetPreset`/`RemovePreset` |
| PH0-12 | Robustețe: răspunsurile camerelor sunt tratate ca ostile (P7). Parserele SOAP, RTSP și SDP sunt fuzzate nightly ≥ 1 h. O cameră care eșuează nu oprește rularea, iar panica e prinsă per cameră | Fuzzing în CI + simulator care trimite XML malformat |
| PH0-13 | Durată: ≤ 10 min pentru 10 camere, fără timpul de tastare (probele rulează în paralel, câte 4 camere). Ctrl-C lasă camerele curate (Stop, revenire PTZ, Unsubscribe) și scrie un raport parțial (cod 3) | Laborator cu 10 camere simulate; Ctrl-C injectat |
| PH0-14 | Limbă RO/EN, după sistem sau `--lang`. Fiecare întrebare are o valoare implicită. Textul nu depinde de culori. Modul neinteractiv merge în CI | Teste golden pe textele consolei |
| PH0-24 | Detectorul marchează dispozitivele al căror OUI MAC aparține Raspberry Pi drept „Raspberry Pi (modelul exact îl arată hub-ul)”, fără să le trateze drept camere, ca proprietarul să știe ce poate folosi ca hub (O14, O16); modelul exact îl citește hub-ul (HUB-34) | Replay pe fixture-uri cu MAC-uri din blocurile OUI ale Raspberry Pi → rândul „Raspberry Pi”; alt OUI → nimic în plus |

#### 16.2.2 Raportul

Arhiva conține folderul `ipcam-raport-AAAAMMZZ-HHMM/` cu:
- `report.json`: protojson al mesajului `ipcam.detector.v1.DetectorReport` (CTR-07), cu antet `format`/`format_version`;
- `raport.txt` (română) și `report.txt` (engleză): rezumatul citibil;
- `fixtures/`: schimburile redactate (§16.2.3);
- `detector.log`: log JSON redactat.

Graniță (schiță; definiția completă e în `contract/proto/ipcam/detector/v1/`):

```proto
message DetectorReport {
  string format = 1;                       // "ipcam.detector.v1"
  uint32 format_version = 2;
  string detector_version = 3;
  google.protobuf.Timestamp started_at = 4;
  repeated string scanned_ranges = 5;      // pseudonimizate (16.2.3)
  repeated FoundDevice devices = 6;
}
message FoundDevice {
  string pseudo_id = 1;                    // "cam-01"
  string address = 2;                      // pseudonim, ex. "10.255.0.11"
  Identity identity = 3;                   // brand, model, firmware, confidence, evidence[]
  repeated OpenPort ports = 4;
  bool onvif = 5; bool rtsp = 6;
  ipcam.hub.v1.CameraProbe probe = 7;      // doar cu parolă; ACELAȘI mesaj pe care îl întoarce cameras.Camera.Probe() (D5)
  repeated SecurityWarning warnings = 8;   // același tip ca CameraService.GetSecurityWarnings
  string fixture_dir = 9;
}
message Exchange {                          // o linie din fixtures/<pseudo_id>/exchanges.jsonl
  uint32 seq = 1; uint32 t_ms = 2;          // timp relativ față de începutul probei
  Proto proto = 3;                          // WSD | SSDP | VENDOR_DISCOVERY | HTTP | ONVIF | RTSP
  string action = 4;                        // ex. "tds:GetSystemDateAndTime", "DESCRIBE"
  Message request = 5; Message response = 6;   // metodă/cale/status, antete, corp (deja redactate)
}
```

Fragment din `raport.txt`:

```
RAPORT IPCAM DETECTOR — 17.10.2026 14:32 — 4 camere găsite
cam-01  Hikvision DS-2CD2143G2-I · firmware V5.7.3 (sigur)
  ✓ RTSP: principal H.265 2688×1520 20 fps GOP 40 · secundar H.264 640×360 15 fps GOP 15
  ✗ ONVIF oprit → pași: Configurare > Rețea > Avansat > Protocol de integrare > Activează ONVIF
  ! Firmware cu vulnerabilitate cunoscută (CVE-…) → actualizați de pe site-ul producătorului
cam-02  Reolink RLC-823A · PTZ: stânga/dreapta ✓, sus/jos ✓, zoom ✓, presetări ✓ (8 max) · evenimente: mișcare ✓
Ce urmează: trimiteți arhiva agentului. Nu conține parole sau imagini.
```

#### 16.2.3 Fixture-uri și redactare (D4, R4)

**Ce se înregistrează:** fiecare schimb WS-Discovery, SSDP, sondă de marcă, HTTP, ONVIF SOAP și RTSP de control (`OPTIONS`, `DESCRIBE`, `SETUP`, `PLAY`, `GET_PARAMETER`, `TEARDOWN`), cu antete, corp și timpi relativi. SDP-ul se păstrează, inclusiv `sprop-parameter-sets`, ca simulatorul să genereze video sintetic cu aceiași parametri. **Ce nu se înregistrează:** payload-ul RTP/RTCP (doar statistici: pachete, intervale între keyframe-uri, tipuri NAL) și imaginile.

**Redactarea are loc în memorie, înainte ca ceva să ajungă pe disc.** Pseudonimele sunt stabile în cadrul unei rulări (aceeași valoare → același pseudonim):

| Categorie | Exemple | Tratament |
|---|---|---|
| Credențiale | `Authorization` (basic/digest), WS-Security `Password`, `rtsp://u:p@`, Xiongmai `user=X_password=Y`, câmpurile `password`/`pwd`/`token`/`sid`/`session` din query, JSON sau XML, `Cookie`/`Set-Cookie` | `<redacted:cred>`; schema și algoritmul (Digest MD5/SHA-256, `Created` din UsernameToken) rămân, ca replay-ul să reproducă comportamentul |
| Secrete cunoscute | fiecare valoare tastată de proprietar, în formă brută, URL-encoded, base64, hex și HA1 MD5/SHA-256 | căutare finală pe toți octeții; orice potrivire se înlocuiește și se numără în log |
| Identificatori de dispozitiv | număr de serie (și în realm-ul Dahua), UUID-ul WS-Discovery, ID-uri P2P/cloud (Hik-Connect, Reolink UID), hostname | `SN-<hash8>`, `uuid-<hash8>`; MAC: OUI-ul rămâne, restul devine hash |
| Rețea | IP-uri private, IP public, gateway, DNS, SSID, chei Wi-Fi | IP privat → `10.255.a.b` (mapare stabilă); public → `<public-ip>`; Wi-Fi → `<redacted:wifi>` |
| Date personale | nume de camere date de utilizator, text OSD, emailuri, gazde și utilizatori SMTP/FTP/DDNS, locație GPS din scope-uri | `<name-N>` / `<redacted:pii>` |
| Imagini | corpuri `image/*` sau cu semnătură JPEG/PNG | `{"redacted":"image","bytes":N,"sha256":"…"}` |

| ID | Cerință | Test |
|---|---|---|
| PH0-15 | **Santinela de redactare.** În laborator, simulatorul conține secrete plantate: parolă, serie, SSID, MAC, email, ID P2P. Arhiva finală le conține de 0 ori, în oricare dintre formele din tabel | E2E în CI la fiecare PR care atinge `cameras` sau detectorul |
| PH0-16 | **Replay complet.** Detectorul rulat pe simulatorul de replay alimentat cu fixture-urile lui produce același `report.json`, în afară de timpi | Test round-trip în CI, per fixture |
| PH0-17 | `ipcam-detector verify` rescanează o arhivă cu aceleași reguli plus o listă opțională de secrete și iese cu 4 dacă găsește ceva | Unit cu arhive alterate |

#### 16.2.4 Garanții de confidențialitate (testabile)

| ID | Garanție | Test |
|---|---|---|
| PH0-18 | Detectorul nu deschide conexiuni către adrese care nu sunt private (RFC 1918, link-local, multicast local). Nu face nici interogări DNS publice | Captură de trafic în laborator: 0 pachete spre adrese publice |
| PH0-19 | Scanează doar subrețelele private ale interfețelor (cel mult /24 per interfață) și intervalele private adăugate explicit (≤ 1.024 adrese fiecare). Intervalele publice sunt refuzate. Ritm ≤ 200 SYN/s, ≤ 64 conexiuni simultane | Unit pe validarea intervalelor + măsurare în laborator |
| PH0-20 | Nicio parolă și nicio imagine pe disc: nici în raport, nici în log, nici în fișiere temporare | Santinela PH0-15 + scanarea directorului temporar după rulare |
| PH0-21 | Rezultatul rămâne pe PC până îl trimite proprietarul. Detectorul nu are cod de upload | Regulă de import în CI: `cmd/ipcam-detector` nu importă clienți HTTP pentru internet, ci doar pachetele `cameras` |
| PH0-22 | **Repo-ul e public (O12).** Fixture-urile ajung în `e2e/fixtures/<marcă>/<model>/<firmware>/` numai după `verify` (cod 0) și după aprobarea proprietarului în PR. Aceeași regulă se aplică rapoartelor și datelor publicate din Faza 0 și din Faza 0-bis (`docs/research/<data>-phase0*-results.md`, `docs/research/phase0-data/`): IP-urile publice, MAC-urile complete, seriile, SSID-urile, emailurile, numele de cont și locațiile se pseudonimizează după tabelul din §16.2.3 înainte de commit | Checklist de PR + job CI care rulează `verify` pe fixture-urile noi și același scaner de redactare pe rapoarte și pe `docs/research/phase0-data/`; orice potrivire pică PR-ul |

**Distribuție (Decizie tehnică).** În Faza 0, detectorul e un GitHub Release `detector-v0.x`, cu sume SHA-256 și SBOM, încă nesemnat Authenticode: certificatul de semnare vine prin §18. `docs/detector.md` (în română) arată pașii pentru avertismentul SmartScreen. Dacă Defender blochează binarul, se folosește varianta Linux sau macOS. Justificare: proprietarul îl rulează o singură dată, iar semnarea nu trebuie să întârzie primul livrabil.

#### 16.2.5 Cum devine detectorul modulul `cameras` (D5)

| Bucată din detector | Locul ei | Cum o folosește hub-ul |
|---|---|---|
| descoperire, amprente, sonde de mărci | `hub/internal/cameras/discovery`, `…/fingerprint` | `DiscoveryService.StartScan` (F2 pasul 1) |
| probe ONVIF/RTSP, PTZ, evenimente, ceas, autentificare | `hub/internal/cameras/{onvif,rtspprobe,ptz,events}` | `Camera.Probe()` întoarce același `CameraProbe` (§4.4); verificările din F2 (pasul 3) |
| avertismente de securitate, liste de parole implicite și de firmware vulnerabil | `hub/internal/cameras/security` | `GetSecurityWarnings`; în hub, lista de firmware vine din feed-ul semnat (§8.2), nu din instantaneu |
| înregistratorul de fixture-uri și redactarea | `hub/internal/cameras/fixture` | comanda de diagnostic opt-in `ipcam-hub diag record-fixtures --camera <id>` pe hub-ul de test al proprietarului (R4) |
| asistentul de consolă, randarea raportului, clientul UPnP de citire, arhivarea | `hub/cmd/ipcam-detector` | nu se folosesc în hub |

- **PH0-23.** `cmd/ipcam-detector` nu conține logică de cameră. Depguard permite doar importuri din `hub/internal/cameras/...`, `contract/gen/go/...` și biblioteca standard. Testele unit și de replay scrise în Faza 0 rămân în suita hub-ului. Test: regula de import în CI.

### 16.3 Faza 0 (b) — Spike-ul video Android, web și Windows (decizia de stack A3)

**Scop.** Confirmăm cu date că opțiunea B (SPA TypeScript în Capacitor și Tauri) e bună pe dispozitive reale, înainte să existe mult cod (panel). În Faza 0 se testează Android, web și Windows, pe dispozitivele proprietarului și fără costuri (O13, O14, T41); rândurile iOS din matrice rulează în Faza 0-bis (§16.9). Hub-ul de spike rulează pe Pi-ul sau pe PC-ul proprietarului și folosește fork-ul go2rtc ca gateway live, plus 9–16 camere simulate (ffmpeg cu ora și contorul de cadre arse în imagine: main 1080p la 4 Mbps, sub 640×360 la 12 fps, GOP = fps) și camerele proprietarului. Ca să nu încarce PC-ul, camerele simulate pot rula pre-codate (`-c copy`): banda arsă poartă contorul de cadre, iar emițătorul scrie ora de trimitere a fiecărui cadru. Player-ul e `@ipcam/player`, adaptat din video-rtc.js. Pe LAN, signaling-ul merge direct la hub (T42; pe web, prin unealta de spike de pe `http://localhost`, T41). Testele remote (G8) și MV12 în Chrome folosesc signaling + TURN doar dacă există o variantă gratuită, fără date de plată (T41); altfel trec în Faza 0-bis, pe VPS-ul UE și TURN Cloudflare.

**Matricea de dispozitive.**

| Cod | Dispozitiv | Motor | Rețele | Ce rulează | Sursa |
|---|---|---|---|---|---|
| iOS-1 (Faza 0-bis) | iPhone real, iOS ≥ 17.1 (de preferat ultima versiune), cel puțin clasa iPhone 12 (A14) | WKWebView în shell Capacitor (TestFlight) + Safari | Wi-Fi în LAN-ul hub-ului; 4G/5G pe cel puțin un operator românesc (Orange, Vodafone sau Digi), ideal două | MV1–MV11, MV14 | iPhone împrumutat de proprietar (O14, §16.9) |
| iOS-2 (Faza 0-bis) | Simulator iOS: ultima versiune + 17.x | WKWebView | runner macOS CI | smoke automat MV1, MV3, MV10 | GitHub Actions |
| AND-1 | Samsung Galaxy A24 al proprietarului (MediaTek Helio G99; versiunea Android și memoria se notează în raport). E sub clasa Galaxy A35/A36 vizată inițial, deci rezultatele sunt conservatoare | Android System WebView ≥ 120, în Capacitor; APK de test instalat direct (T41) | Wi-Fi în LAN-ul hub-ului (linia DIGI); 4G pe operatorul SIM-ului din telefon, doar cu varianta remote gratuită (T41) | MV1–MV11, MV14 (Kotlin), MV15 | proprietar (O14), fără cumpărare |
| AND-2 (Faza 0-bis) | Android de intrare (Android 10 = minSdk 29, 3–4 GB RAM) | WebView | Wi-Fi | MV3, MV5 cu 4 plăci (informativ: fixează plafonul implicit) | Firebase Test Lab (OWN-17, cost) |
| AND-3 | Emulator Android API 36 | WebView | CI | smoke automat | CI (KVM) |
| WIN-1 | PC-ul Windows 11 al proprietarului (O14; procesorul, GPU-ul și memoria se notează în raport) | Tauri 2 + WebView2 Evergreen, build de dezvoltare nesemnat; avertismentul SmartScreen se acceptă pentru test (T41) | LAN | MV1–MV7, 16 plăci + 1 flux principal | PC-ul proprietarului |
| WIN-2 | Runner Windows GitHub (fără GPU) | WebView2 | CI | smoke automat (decodare software) | CI |
| WEB | Faza 0: Chrome și Edge stabile pe PC-ul Windows 11 al proprietarului, plus Firefox pe același PC; Chrome pe AND-1 doar cu originea publică gratuită (T41). Faza 0-bis: Safari pe iOS-1 (și pe macOS, dacă e disponibil un Mac) | browser; în Faza 0, SPA-ul de pe `http://localhost` (unealta de spike, T41) și, pentru MV12, de pe originea publică gratuită; după domeniu, de pe `https://<domeniu>` | LAN; remote doar cu varianta gratuită (T41) | MV12, MV13, MV1–MV3 | proprietar + CI |

**Măsurători și praguri** (fiecare mostră se repetă de 20 de ori sau ține 10 min, cum scrie):

| Cod | Măsurătoare | Cum | Prag |
|---|---|---|---|
| MV1 | Primul cadru după atingere | `requestVideoFrameCallback`, 20 de porniri | LAN p90 ≤ 1,5 s (HUB-06); remote prin relay p90 ≤ 4 s |
| MV2 | Latență glass-to-glass | ora arsă în cadru vs ora dispozitivului (NTP); la camerele pre-codate, ora de trimitere a cadrului citit din bandă; 100 de mostre | LAN p95 < 1 s (SUC-02); remote p95 < 3 s (SUC-03) |
| MV3 | Fluență per placă, 10 min | `getStats`: `framesDecoded`, `framesDropped`, fps | fps ≥ 90 % din sursă; cadre pierdute ≤ 2 % |
| MV4 | Decodare hardware | `chrome://webrtc-internals` (prin `chrome://inspect`) și Safari Web Inspector; `getStats` nu expune implementarea decodorului fără o captură activă (§6.5) | ecranul complet (flux principal) mereu hardware; toate cele 9 plăci de grilă hardware pe AND-1 |
| MV5 | Temperatură și baterie, 10 min de grilă | Android `getCurrentThermalStatus`; iOS `thermalState`; % baterie | Android ≤ MODERATE; iOS ≤ `fair`; fps-ul nu scade sub MV3 din cauza încălzirii; baterie ≤ 15 %/h |
| MV6 | CPU pe Windows | Task Manager / ETW pe procesul WebView2 + proces aplicație | 16 plăci ≤ 25 % CPU total; 1 flux principal 1080p ≤ 10 % |
| MV7 | Stabilitate, 30 min cu paginare și schimbări de layout | memorie JS + proces; numărul de reporniri de player | creștere ≤ 10 %; 0 reporniri neprovocate |
| MV8 | PiP în fundal | iOS (Faza 0-bis): `<video>` PiP din WKWebView, aplicația în fundal; Android: PiP la nivel de Activity | fereastra apare și video-ul avansează 5 min fără întrerupere |
| MV9 | Fundal → prim-plan | aplicația stă 2 min în fundal, apoi revine | live reluat în ≤ 3 s, fără repornirea aplicației |
| MV10 | Înregistrări HLS (fMP4) | 10 min de înregistrare reală servită de hub; pe AND-1 și WIN-1, hls.js pe MSE; pe iOS-1 (Faza 0-bis), hls.js + ManagedMediaSource în WKWebView (APP-31) | pornire ≤ 2 s; salt ≤ 1,5 s |
| MV11 | TURN-TLS/443 pe operator mobil | `iceTransportPolicy: "relay"`, doar `turns:…:443?transport=tcp`, 30 min; în Faza 0 doar cu varianta gratuită (T41), altfel în Faza 0-bis | conectat 30 min fără cădere; MV2 remote respectat |
| MV12 | Chrome Local Network Access | SPA de pe originea publică spre hub în același LAN, în Chrome; în WebView2 (Tauri) și în Android WebView (Capacitor), de pe originile fixe ale shell-urilor (§6.7). În Faza 0, originea publică e una gratuită, dacă T41 o permite; altfel MV12 în Chrome trece în Faza 0-bis, cu domeniul | se notează promptul (da/nu) și tipul perechii ICE; trece dacă perechea nu e relay (APP-32) |
| MV13 | Candidați mDNS `.local` | Chrome, Edge și Firefox pe PC-ul proprietarului, în LAN (SPA de pe `http://localhost`, T41); Safari în Faza 0-bis | perechea aleasă are ambele capete pe adrese LAN private (`host` sau `prflx`), fără relay; un filtru de multicast între Wi-Fi și Ethernet (frecvent la routerele mesh) nu pică testul dacă perechea e `prflx` |
| MV14 | Cod nativ necesar | linii Kotlin (Faza 0) și Swift (Faza 0-bis) pentru PiP + rețea locală, măsurate în spike | ≤ 400 linii per plugin și per platformă; proiecția ≤ 10 % din codul client (APP-01). Peste 15–20 %, avantajul B dispare (panel) |
| MV15 | Argon2id în WebView | derivarea cu parametrii impliciți din §10.3, 10 rulări | ≤ 3 s pe AND-1 (ENC-10, G19) |

**Regula GO/NO-GO (decizia A3, luată pe date).**
- **Critice per platformă mobilă:** MV1, MV2, MV3 cu 4 plăci, MV9, MV10 (după planul B `hls-proxy`, APP-31), MV11; pe AND-1, și MV3, MV4 și MV5 cu 9 plăci (Faza 0 (b) aprobată: grilă de 9 plăci pe substream, decodare hardware confirmată, temperatură după 10 min). Oricare picată pe iOS-1 sau AND-1 → **NO-GO mobil**, cu excepția de mai jos.
- **Remote fără variantă gratuită (T41).** Dacă nu există signaling + TURN gratuit, criticele remote (MV1 și MV2 la distanță, MV11) se măsoară în Faza 0-bis, înainte de Faza 2; până atunci, verdictul Android se dă pe criticele LAN.
- **Grila de 9 plăci pe AND-1.** Dacă 9 plăci pică și 4 trec, verdictul e **„GO condiționat”**. Plafonul pe telefon (A6: maximum 9) se schimbă doar cu aprobarea proprietarului, în raportul Fazei 0 (PH0-30), și se scrie în §2.1 ca modificare a lui A6, nu în §2.2. Până la decizia lui, plafonul rămâne 9, iar plăcile care nu pot decoda trec pe snapshot (§6.5).
- **PiP pe iOS (MV8).** Dacă PiP-ul `<video>` din WKWebView pică, se aplică planul B, care e o **Decizie tehnică**: PiP nativ cu `AVPlayer` + `AVPictureInPictureController` pe un HLS live (fMP4) al substream-ului, servit prin pluginul `hls-proxy` (§6.6), cu latență ≤ 4 s acceptată în PiP (OPN-08). Pluginul trebuie să rămână ≤ 400 de linii (MV14). Dacă și planul B pică → **NO-GO mobil**.
- **Windows (WIN-1)** nu poate da NO-GO: dacă MV6 pică, grila desktop coboară la 9 plăci (APP-30). **Web:** dacă MV12 sau MV13 pică, web-ul folosește srflx/relay în LAN și recomandă aplicația nativă (APP-32).
- **NO-GO mobil ⇒ planul Flutter (opțiunea C, A3).** **Decizie tehnică (T29):** ambele aplicații mobile trec împreună pe Flutter, chiar dacă a picat o singură platformă. O singură bază de cod mobilă e mai ușor de întreținut decât trei. Web-ul și Windows rămân pe B. Concret:
  - codul Flutter stă în `apps/mobile-flutter/`, cu live prin flutter_webrtc (MIT), înregistrările prin `video_player` (decodoarele sistemului) și tipurile Dart generate din `contract/` cu `protoc-gen-dart`;
  - RPC-ul pe canalul de date folosește același framing (§7.6);
  - o listă de paritate a ecranelor (§6.4) e verificată în CI;
  - Flutter trebuie să treacă **aceeași** matrice (iOS-1, AND-1) înainte să fie adoptat. Dacă nici Flutter nu trece, proprietarul primește raportul cu ambele variante și decide, înainte de Faza 4.
- **Calendar (modificat de O13).** Verdictele pentru Android (AND-1), web și Windows se dau în Faza 0. Verdictul iOS (G2–G4 și părțile iOS din G7, G8, G9 și G13) se dă în Faza 0-bis (§16.9), cel târziu înainte de începutul Fazei 4 și, recomandat, imediat ce bugetul permite (RSK-27). Testele iOS-1 în WKWebView cer un iPhone împrumutat și un build prin TestFlight intern, care merge cu orice abonament plătit Apple Developer (OWN-05): ca persoană fizică (99 USD/an, fără D-U-N-S) sau ca organizație, dacă SRL-ul există atunci (O15). Contul de organizație rămâne necesar pentru publicare (Faza 4). Dacă un cont de persoană fizică se poate converti ulterior în cont de organizație trebuie verificat cu Apple înainte de înscriere; altfel spike-ul folosește un cont separat. Testerii TestFlight interni trebuie să fie utilizatori ai contului App Store Connect: dacă iPhone-ul împrumutat nu poate folosi Apple ID-ul proprietarului, spike-ul instalează un build ad hoc, cu UDID-ul telefonului înregistrat în cont, sau folosește TestFlight extern, după review-ul de beta.
- **Până la verdictul iOS.** (1) Un NO-GO pe AND-1 aplică T29 imediat, fără să aștepte iOS; Flutter se verifică pe AND-1 în Faza 0 și pe iOS-1 în Faza 0-bis. (2) Un GO pe AND-1 e „GO provizoriu (fără iOS)”: shell-ul Android rămâne subțire (spike-ul și accesul pe LAN din Faza 1, T42), iar lucrul de interfață specific telefonului (pluginurile din §6.6 marcate cu Faza 4, ecrane sau gesturi doar pentru telefon) nu începe înainte de verdictul iOS. `@ipcam/player`, `@ipcam/core` și `@ipcam/api` rămân independente de framework (APP-22), ca un NO-GO iOS târziu să schimbe doar shell-ul mobil.

### 16.4 Faza 0 (c) — Spike-ul de înregistrare și upload

**Montaj.**
- **Gazde de test (O14):** H3, PC-ul Windows 11 al proprietarului; Pi-ul proprietarului, oricare model suportat (§4.8), cu modelul citit de hub (HUB-34), pe SSD dacă există, altfel pe card SD (cu confirmarea din T40; rezultatele de pe card se raportează separat). Pi 5 + SSD (H1) și mini PC N100 (H2) doar dacă sunt disponibile (Faza 0-bis). Dacă Pi-ul proprietarului e armv6 (nesuportat), măsurătorile pe Pi trec în Faza 0-bis, cu un Pi suportat, iar G10–G13 pe Linux se bazează până atunci pe laborator.
- **Camere:** camerele reale ale proprietarului, plus `camsim` în modul pre-codat (`-c copy`, CPU neglijabil), cu contorul de cadre ars în imagine.
- **Controlorul (T43):** celălalt dispozitiv al proprietarului (Pi-ul când se testează PC-ul și invers). Trimite camerele simulate, scrie ora de trimitere a fiecărui cadru și primește de la gazda testată un semnal de viață UDP la 50 ms; momentul opririi = ultimul semnal primit (eroare ≤ 100 ms). Ceasurile se sincronizează prin NTP. Nu se folosește priză inteligentă (O13).
- **Tăierile reale:** le face proprietarul, ghidat de scriptul de spike din `tools/spikes/`, care îi spune când (la un moment aleator între 3 și 12 min de la pornire). PC desktop: scoate ștecherul din priză; dacă PC-ul e laptop, ține apăsat butonul de pornire ≥ 10 s (oprire forțată, fiindcă bateria ține curentul; se notează în raport). Pi: scoate cablul de alimentare. Apoi repornește dispozitivul, iar scriptul face restul. Rulările automate (2) din MI1 pornesc doar într-un interval ales de proprietar, fiindcă repornesc dispozitivele de mai multe ori; pe PC, fiecare bugcheck afișează ecranul albastru.
- **MediaMTX:** cu patch-urile MTX-1 (fsync pe parte), MTX-2 (scriere `age`) și MTX-3 (hook), §4.2.

| Cod | Probă | Procedură | Măsurătoare / prag |
|---|---|---|---|
| MI1 | Pană de curent (F4, T43) | Cu setările implicite ale sistemului de operare, la momente aleatoare între 3 și 12 min după pornire. (1) **Automat, în laborator** (runner-ele GitHub gratuite): 20 × kill -9 pe Linux și 20 pe Windows; 20 × `system_reset` al unei VM QEMU/KVM Linux (TST-09). (2) **Automat, pe dispozitivele proprietarului** (scriptul rulează nesupravegheat): 20 × kill -9 pe PC și 20 pe Pi; încă 20 × oprire fără scrierea cache-ului pe fiecare: pe Pi, repornire imediată (`echo b > /proc/sysrq-trigger`, cu sudo, doar pe Pi-ul de test); pe PC, un bugcheck provocat cu NotMyFault din Sysinternals (`notmyfault64 /crash`, gratuit, unealtă de test nelivrată), cu repornirea automată a Windows activă. În (1) și (2), jumătate din rulări folosesc criptarea locală (MTX-2), iar o rulare din fiecare set de 20 are un flux de 256 kbps criptat local, ca să verifice limita max(1 parte, 64 KiB ÷ bitrate) (HUB-07). (3) **Real, de mână:** 5 tăieri pe PC și 5 pe Pi, făcute de proprietar (montajul de mai sus); tăierile 1, 3 și 5 cu criptare locală | pierdere = momentul opririi (semnalul de viață de la controlor) − ora de trimitere a ultimului cadru înregistrat. Fiecare set automat de 20 și fiecare set de 5 tăieri reale: mediana ≤ 1 s, maximul ≤ 2 s. Peste tot: reconciliere fără intervenție, 0 fișiere ilizibile, HUB-04 = 0 (HUB-30, FLW-15, SUC-05). Tăierile reale confirmă hardware-ul real: cu 0 eșecuri în 10, rata de eșec reală e sub ≈ 26 % (95 % încredere); pragul fin vine din rulările automate (0 eșecuri în cel puțin 40 per sistem de operare → sub ≈ 7 %) |
| MI2 | Crash `hubd` | pe fiecare gazdă de test: 20 × `kill -9` + 72 h cu numărul recomandat de camere al gazdei (8 pe PC; pe Pi, numărul modelului); pe Pi cu card SD, proba de 72 h verifică și supravegherea cardului (T40) | înregistrarea reia în ≤ 10 s; 0 crash-uri în 72 h (HUB-31) |
| MI3 | Niveluri hardware | numărul recomandat de camere al fiecărei gazde (§4.8): 8 pe H3 (PC-ul proprietarului), numărul modelului pe Pi-ul proprietarului; 8 pe H1 și 16 pe H2, doar dacă sunt disponibile; sesiunile live ale nivelului, plus 1 sesiune de redare, 1 export și upload criptat (10 Mbps; pe H1b și H1c, cât debitul camerelor) în paralel, timp de 1 h | bugetele din §4.8; CPU total ≤ 60 % (HUB-32); la eșec, numărul recomandat coboară, modelul rămâne suportat |
| MI4 | `age` cu acces aleator | 1.000 de citiri la offset-uri aleatoare; criptare continuă pe Pi-ul proprietarului (și pe H1, dacă e disponibil); salt într-un segment criptat de 5 min; `agerange` pe AND-1 (pe iOS-1 în Faza 0-bis); decriptare cu `age` CLI și `rage` | identic cu unealta `age`; ≤ 50 ms per citire pe Pi-ul de test; debitul de criptare după pragul per model din ENC-08 (≥ 50 MB/s pe Pi 5); primul cadru după salt ≤ 1,5 s; ≥ 12 Mbps în WebView (HUB-33, ENC-08) |
| MI5 | Upload în timpul unei pene de internet | Pe PC-ul proprietarului (sau pe Pi, cu numărul recomandat al modelului), 4 camere în modul continuu + evenimente. Destinații: Google Drive (Drive-ul personal al proprietarului e permis pentru spike, O11, plus contul de test; proiect în Testing, T41); OneDrive personal (cont de test), doar dacă înregistrarea aplicației Microsoft e gratuită (T41), altfel în Faza 0-bis; OneDrive business în Faza 0-bis (tenantul MI7). WAN-ul se blochează 6 h prin firewall pe gazdă, LAN-ul rămâne. În timpul recuperării, hub-ul se repornește o dată în mijlocul unui upload | 0 segmente lipsă, 0 duplicate, toate hash-urile verificate (STO-02, SUC-04). Se notează: timpul de recuperare față de uplink, unitățile de cotă Drive per segment (consola Google Cloud), eliberarea cotei pe OneDrive personal după ștergere (`quota.deleted`), citirea de către `playback` a unui segment încă deschis |
| MI6 | OAuth pe hub (T11, T34) | PKCE pe dispozitiv, cu clientul OAuth al platformei, apoi codul predat hub-ului (la web × Google, device flow pe hub); refresh zilnic de pe hub. **Faza 0 (T41):** Google Drive × Android (AND-1), Windows (WIN-1) și web (PC), cu refresh în zilele 1–6; OneDrive personal pe aceleași platforme, doar dacă înregistrarea aplicației Microsoft nu cere date de plată (pe web, cu redirect-ul `http://localhost:<port>/oauth/callback` al uneltei de spike, pe platforma „Mobile and desktop applications”; redirect-ul de pe `<domeniu>` se verifică după domeniu). **Faza 0-bis:** combinațiile iOS, OneDrive business, OneDrive personal (dacă înregistrarea nu a fost gratuită) și refresh-ul Drive din ziua 7 și de după, cu proiectul în Production. Vizibilitatea între clienți: pentru fiecare tip de client al hub-ului (iOS, Android, Desktop, TV), un fișier creat de el trebuie să poată fi listat și citit de clientul web, de cel de device flow și de fiecare client nativ al aceluiași proiect; la fel pentru `approot`-ul OneDrive între platformele aceleiași aplicații Entra | poarta din §3.3; rezultatul vizibilității se scrie în §2.2 (dacă pică, F6 folosește tipul de client al hub-ului). În Faza 0, proiectul Google e în modul Testing (O13): tokenurile lui expiră după 7 zile (crit #13), deci refresh-ul din ziua 7 nu se poate verifica fără să pice artificial; se verifică în Faza 0-bis, cu proiectul în **Production**, înainte de Faza 3 |
| MI7 | Conditional Access Microsoft | **Faza 0-bis** (cost: Entra ID P1, OWN-09): tenant de test cu Entra ID P1 și 3 politici: MFA obligatoriu, blocare device-code, dispozitiv conform; plus `Files.ReadWrite.AppFolder` pe un cont de firmă | FLW-23; dacă `AppFolder` merge pe conturile de firmă, OneDrive business trece pe el (§8.7) |
| MI8 | Evenimente ONVIF, 72 h | camerele proprietarului pe hub-ul de spike. Un detector de mișcare din harness (diferență de cadre pe substream, doar unealtă de test, nu funcție livrată) e referința | ≥ 95 % din episoadele de mișcare au eveniment ONVIF în ≤ 5 s; 0 pierderi tăcute de abonament (> 5 min fără `Renew`/`PullMessages` reușit) |
| MI9 | Redare prin canalul de date | fișier înregistrat de 4 Mbps; canal bulk pe LAN și prin TURN-TLS/443, cu RTT de 50 ms emulat; 20 de salturi pe timeline | pragurile CTR-12 (G18) |

### 16.5 Porțile

Porțile G1–G19 sunt ale Fazei 0; G20–G25 sunt porți ale fazelor următoare. Unele porți ale Fazei 0 au o parte amânată în Faza 0-bis (O13); paranteza din prima coloană spune care parte și până când (§16.9). Fiecare poartă are un responsabil de măsurare (agentul AI) și un verdict scris în raportul Fazei 0 sau al fazei respective. Rândurile trimit la cerințele din alte secțiuni, ca pragurile să existe într-un singur loc.

| Poartă | Ce verifică | Trece dacă | Dacă pică (plan B) | Leagă |
|---|---|---|---|---|
| G1 | Detectorul pe camerele proprietarului | Proprietarul confirmă că lista e completă; fiecare cameră e identificată (marcă/model) sau marcată motivat „necunoscut”; arhiva trece `verify`; PH0-01…24 sunt verzi în CI | Camerele negăsite se adaugă manual prin IP; amprentele lipsă se adaugă din fixture-uri și se rerulează | D1–D5 |
| G2 (Faza 0-bis: iOS, înainte de Faza 4) | Live iOS în WKWebView | criticele din §16.3 pe iOS-1 | NO-GO mobil → Flutter (§16.3, T29) | APP-30 |
| G3 (Faza 0-bis: iOS, înainte de Faza 4) | PiP și fundal pe iOS | MV8 (direct sau cu planul B AVPlayer) și MV9 | NO-GO mobil | APP-30, §6.6 |
| G4 (Faza 0-bis: iOS, înainte de Faza 4) | Înregistrări pe iOS | MV10 în WKWebView | `hls-proxy` + `NSAllowsLocalNetworking`; dacă și acesta pică → NO-GO mobil | APP-31 |
| G5 (Faza 0; criticele remote doar cu varianta gratuită, T41, altfel în Faza 0-bis, înainte de Faza 2) | Grila Android | criticele pe AND-1 (Galaxy A24 al proprietarului), inclusiv grila de 9 plăci (MV3, MV4, MV5) | 9 plăci pică, 4 trec: „GO condiționat”; plafonul pe telefon se schimbă doar cu aprobarea proprietarului, ca modificare a lui A6 în §2.1 (§16.3); altfel NO-GO mobil | APP-30, §6.5 |
| G6 | Windows WebView2 | MV6, MV7 cu 16 plăci pe WIN-1 (PC-ul proprietarului) | grilă desktop de 9 plăci | APP-30 |
| G7 (Faza 0; Safari și WKWebView în Faza 0-bis; MV12 în Chrome cu origine publică gratuită, T41, altfel cu domeniul, în Faza 0-bis, înainte de Faza 2) | Chrome LNA și mDNS | MV12 (Chrome, WebView2, Android WebView) dă o pereche fără relay; MV13 dă o pereche cu ambele capete pe adrese LAN private (`host` sau `prflx`); hub-ul ascultă pe 5353/udp cu `SO_REUSEADDR`/`SO_REUSEPORT` alături de avahi (pe Pi-ul proprietarului, cu Raspberry Pi OS; același daemon ca pe Synology) și de clientul DNS din Windows | relay în LAN pentru web + recomandarea aplicației; costul intră în §15.6 | APP-32, crit C5 |
| G8 (Faza 0 doar cu varianta gratuită, T41, pe rețelele proprietarului: linia DIGI și operatorul din A24; restul rețelelor și varianta cu plată: Faza 0-bis, înainte de Faza 2; iOS-1: odată cu verdictul iOS) | Acces remote în România | MV11 trece pe toate rețelele testate. Ponderea relay se măsoară pe ≥ 4 rețele (2 operatori mobili, o linie fixă, o linie CGNAT dacă există) | ponderea relay > 25 % → bugetul TURN și pragul de self-hosting (§5.5) se recalculează înainte de Faza 2 | SUC-03, land N2 |
| G9 (Faza 0; căile iOS în Faza 0-bis) | Matricea de codecuri | o cameră H.265 (reală sau simulată) și profilurile H.264 Main/High, nivelul 5.x, pe toate căile client: regula SDP + primul cadru în 4 s alege corect | onboarding-ul impune mai strict H.264 pe substream; pe ecran complet se folosește substream-ul când fluxul principal e H.265 nedecodabil | APP-05, A6, crit lipsă #4 |
| G10 (Faza 0 pe PC-ul și Pi-ul proprietarului; H1 și H2 în Faza 0-bis, dacă există) | Pană de curent | MI1 (rulări automate + 5 tăieri reale pe dispozitiv, T43) | recorder propriu pe gortsplib/mediacommon în spatele `Recorder`, retestat înainte de Faza 1 | HUB-30, FLW-15, SUC-05 |
| G11 | Independența înregistrării | MI2 | ingest inversat (MediaMTX citește camera) | HUB-31 |
| G12 (Faza 0 pe PC-ul și Pi-ul proprietarului; H1 și H2 în Faza 0-bis, dacă există) | Niveluri hardware | MI3 | numărul recomandat al nivelului sau al modelului din §4.8 coboară (minimum 1); niciun model suportat nu devine nesuportat | HUB-32 |
| G13 (Faza 0; `agerange` pe iOS-1 în Faza 0-bis) | `age` cu acces aleator | MI4 | hub: decriptare întreagă în bufferul de redare din RAM (§8.5), iar dacă saltul tot depășește 1,5 s, segmente criptate de 2 min; F6: decriptare întreagă înainte de redare; formatul nu se schimbă | HUB-33, ENC-08 |
| G14 (Faza 0 pentru Drive și, dacă T41 o permite, OneDrive personal; restul OneDrive în Faza 0-bis, înainte de Faza 3) | Upload fără goluri | MI5 | outbox-ul se corectează înainte de Faza 3; dacă citirea segmentului deschis pică, segmente de 2 min | STO-02, SUC-04 |
| G15 (Faza 0: combinațiile gratuite din MI6, Drive în Testing; iOS, OneDrive business și ziua 7 în Production: Faza 0-bis, înainte de Faza 3) | OAuth pe hub și vizibilitatea între clienți | MI6 | planurile B din §3.3, în ordine; dacă vizibilitatea între clienți pică, F6 folosește tipul de client al hub-ului | T11, T34 |
| G16 (Faza 0-bis: Entra ID P1, înainte de Faza 3) | Conditional Access și `AppFolder` pe conturi de firmă | MI7 | OneDrive business marcat „necesită excepție CA”; recomandarea pentru firme devine S3/NAS; fără `AppFolder`, rămâne `Files.ReadWrite`, cu textul din §8.7 | FLW-23, crit C19 |
| G17 | Evenimente ONVIF | MI8 | pentru marca afectată: adaptor de evenimente nativ al mărcii (Hikvision `alertStream`, Dahua `eventManager`, Reolink push), ca Decizie tehnică, fiindcă aduce aceeași funcție pe altă cale; altfel camera e marcată „evenimente nesigure” și se aplică FLW-16 | HUB-11, FLW-16 |
| G18 | Redare prin canalul de date | MI9 | hub-ul redă înregistrarea ca track WebRTC în aceeași sesiune (1×, saltul prin RPC); HLS peste canalul de date rămâne doar pe căile care trec | CTR-12, crit lipsă #11 |
| G19 | Argon2id pe Android mediu | MV15 | parametrii impliciți scad la `m=46 MiB, t=1, p=1` (pragul OWASP); conturile existente trec pe ei cu `AccountService.ChangeKdfParams` | ENC-10 |
| G20 (înainte de primul release pe canalul beta intern; cere infrastructura de semnare din Faza 0-bis: OWN-20, OWN-21; T44) | Build reproductibil | DEL-03 | a doua semnătură `targets` vine din AWS KMS după verificarea unei sesiuni de agent separate; proprietarul semnează cu YubiKey doar release-urile stable (o dată la 2–4 săptămâni); fiecare artefact nereproductibil are un issue până la remediere | DEL-03, T26 |
| G21 (Faza 0-bis iOS: după Faza 2, odată cu verdictul iOS, înainte de Faza 4; O13) | Revizuire App Store de probă | build iOS minim (login, hub demo, live, `secure-storage`, `auth-native`) trimis la App Review cu lansare manuală → aprobat | se adaugă devreme funcțiile native din §6.6 și se retrimite; după 2 respingeri pe Apple 4.2, se evaluează Flutter pentru iOS (panel, condiția 3) | RSK-04 |
| G22 (Faza 2) | Încărcarea cloud-ului | CLD-30 | 2 noduri gateway din start | CLD-30 |
| G23 (Faza 4) | Utilizabilitate | SUC-01 / FLW-05, cu câte 5 persoane non-tehnice pe Windows și pe Docker/NAS | corecții de onboarding și retest; beta publică nu pornește înainte | SUC-01 |
| G24 (înainte de beta publică) | Pregătirea pentru raportarea CRA | LEG-09 | beta publică nu pornește | LEG-09, P11 |
| G25 (înainte de beta publică) | Avizul avocatului | LEG-13 | punctele indicate se corectează înainte de beta publică | LEG-13, P12 |

| ID | Cerință pentru închiderea Fazei 0 | Test |
|---|---|---|
| PH0-30 | Raportul `docs/research/<data>-phase0-results.md` conține, pentru partea din Faza 0 a porților G1–G19: cifrele brute (CSV în `docs/research/phase0-data/`), verdictul, decizia declanșată și ce s-a schimbat în §2 sau în cerințe; la „GO condiționat” pe AND-1, și decizia proprietarului asupra plafonului pe telefon. Conține și lista elementelor rămase pentru Faza 0-bis, fiecare cu poarta, costul estimat și termenul (§16.9), plus modelul Pi-ului proprietarului și numerele recomandate rezultate (§4.8). Raportul și datele respectă PH0-22. Proprietarul îl aprobă în PR | Revizuire PR |
| PH0-31 | Fiecare decizie declanșată de o poartă (recorder, segmente, plan OAuth, vizibilitatea între clienți, plafonul desktop, numerele recomandate pe Pi, stack-ul mobil după T29), în Faza 0 sau în Faza 0-bis, e scrisă în §2.2 ca Decizie tehnică, cu trimitere la poartă. O schimbare a plafonului pe telefon din A6 se scrie în §2.1, ca modificare a lui A6, doar cu aprobarea proprietarului (PH0-30) | Revizuire PR |
| PH0-32 | Faza 0 nu cere nicio plată și nicio dată de plată (O13): fiecare cont, serviciu și dispozitiv folosit e într-un rând „Acum (gratuit)” din §18 sau e al proprietarului (O14). Un pas care se dovedește cu plată se oprește și trece în Faza 0-bis (§16.9) | Raportul Fazei 0 listează conturile și serviciile folosite, fiecare cu rândul OWN corespunzător; revizuire PR |

### 16.6 Fazele 1–4

**Faza 1 — Hub + aplicația pe LAN (fără costuri, fără domeniu).**
- **Hub:** `hubd` cu `cameras` (din detector), `live`, `session`, `recorder`, `catalog`, `playback`, `rules` (doar destinația „disc local”), `config`, `audit`, `health`, `updater`, `lanapi` (cu certificat autosemnat, T42). Se instalează prin imaginea Docker (amd64, arm64, arm/v7, T39) și prin `ipcam-hub install` ca serviciu Windows; installer-ul complet vine în Faza 4. Până la infrastructura de semnare, hub-ul de test al proprietarului primește build-uri CI nesemnate, instalate manual (T44).
- **Aplicația:** SPA-ul, rulat pe LAN în shell-ul Tauri (Windows 11) și în shell-ul Android (APK instalat de proprietar), care fixează cheia autosemnată a hub-ului prin `HIK` (T42, APP-23): F2 (asistent, ghiduri pe mărci, avertismente), F3 pe LAN, F4 (continuu / la mișcare / oprit, pre-roll și post-roll, retenție locală, curățare de urgență), F5 (timeline, miniaturi, export MP4), alerte în aplicație, RO/EN, funcțiile GDPR locale (audio oprit implicit, avertisment pentru spațiul public, presetări de retenție). Shell-urile rămân subțiri: doar `lan-http` și ce cere accesul pe LAN (§16.3, „Până la verdictul iOS”).
- **Laboratorul E2E:** simulatoare + replay din fixture-urile G1, CI pe Linux și Windows, licențe, SBOM. În laborator, SPA-ul rulează și în Chromium și Firefox (Playwright), cu `ignoreHTTPSErrors` doar în laborator; identitatea hub-ului se verifică tot prin `HIK` (modul 1 din §11.5).
- **Decizie tehnică (T28, modificată de T42):** găzduirea statică a SPA-ului, modulul `certs` (certificatul LAN per hub, §5.7) și autentificarea hub-ului pe `link/v1` se mută la începutul Fazei 2, fiindcă toate trei cer domeniul. Împerecherea în Faza 1 e doar locală (`LanService.PairDevice`, F1.X2), fără cont.
- **Ce din Faza 1 depinde de domeniu și s-a mutat în Faza 2 (O13):** (1) găzduirea SPA-ului pe `https://<domeniu>`; (2) emiterea reală a certificatelor LAN pe `<domeniu-hub>` și DNS-ul autoritar (`certs`, §5.7); (3) autentificarea hub-ului pe `link/v1` la `cloud.<domeniu>`; (4) PH1-07 (SPA-ul încărcat de pe domeniu ajunge la hub pe LAN cu API-ul cloud oprit); (5) partea „din web” a lui PH1-02; (6) originile web din APP-19 (`https://<domeniu>`, `https://beta.<domeniu>`); (7) link-ul universal din QR (T19) și rutele HTTP de pe `<domeniu>` din §7.9; în Faza 1, QR-ul poartă doar `PairingTicket` (T42). Restul Fazei 1, inclusiv toate testele de laborator, nu depinde de domeniu.

| ID | Definiția lui gata — Faza 1 | Test |
|---|---|---|
| PH1-01 | Trec în CI: HUB-01…28 (fără HUB-23, care e în Faza 3; partea nightly a lui HUB-28 pe `ipcam-win10` vine odată cu OWN-22, Faza 0-bis), HUB-34, FLW-06…19 (partea locală), FLW-28, FLW-30, APP-02…05, APP-10, APP-11, APP-19 (originile shell-urilor: `https://localhost` și `http://tauri.localhost`; originile web în Faza 2, T42), APP-21…23, ARH-01…09, CTR-*, STO-15, SUC-02, SUC-05 (kill -9), SUC-06, SUC-07; REL-01…05, REL-07, REL-09…11; TST-20…23; SEC-01, SEC-05, SEC-15…17, SEC-19, SEC-20; DEL-01, DEL-05, DEL-07; LEG-01…03, LEG-07 | CI |
| PH1-02 | Hub-ul de test al proprietarului (pe Pi-ul sau pe PC-ul lui) rulează 7 zile la rând cu toate camerele lui: 0 goluri fără motiv, 0 crash-uri `hubd`, timeline și export funcționale din aplicația Tauri sau Android, pe LAN (T42); verificarea „din web” se face în Faza 2 | Raport `health` + verificare de către proprietar |
| PH1-03 | Fiecare marcă a proprietarului are fixture-uri în `e2e/fixtures/` și teste de replay verzi | CI |
| PH1-04 | În laborator, de la pornirea hub-ului la primul cadru trec ≤ 60 s, automat (FLW-05, partea de laborator) | CI |
| PH1-05 | Update-ul semnat A/B cu rollback merge pe Windows și Docker (HUB-14) | CI |
| PH1-06 | Deciziile din partea Fazei 0 a porților G1–G19 sunt aplicate în cod și în cerințe; cele din Faza 0-bis se aplică la verdictul lor (PH0-42) | Revizuire PR |
| PH1-09 | G20 (build reproductibil, DEL-03) are verdict înainte de primul release pe canalul beta. Canalul beta pornește doar după infrastructura de semnare din Faza 0-bis (OWN-20, OWN-21); până atunci, Faza 1 se poate închide fără niciun release pe canal (T44) | Raportul celor 3 release-uri |

**Faza 2 — Cloud: cont, împerechere, acces de oriunde.**
- **Module cloud** (§5): `auth`, `accounts` (email + parolă cu derivare pe client, TOTP, passkeys, Google și Apple), `devices`, `pairing` (QR și cod, F1), `presence`, `signaling`, `turn` (Cloudflare), `entitlements` (planul `free`, O7), `audit` și `telemetry`. Exportul de date și ștergerea contului.
- **Pe hub și în web:** `cloudlink` pe hub; F3 remote în web.
- **Mutate din Faza 1 (T28, T42), la începutul fazei:** găzduirea statică a SPA-ului pe `<domeniu>`, modulul `certs` (certificatul LAN per hub, §5.7) și autentificarea hub-ului pe `link/v1`; de aici, web-ul ajunge pe LAN la hub cu certificat public (P9).
- **Precondiție (O13):** domeniile, serverul UE și Cloudflare din Faza 0-bis (OWN-04, OWN-11, OWN-12) și G8 complet (§16.9). Sign in with Apple real cere contul Apple (OWN-05, Faza 0-bis); până atunci, `auth` folosește pentru Apple doar furnizorul OIDC fals din laborator (CLD-04).
- **Operare:** găzduire UE cu backup și monitorizare (§13.9, §15.5).

| ID | Definiția lui gata — Faza 2 | Test |
|---|---|---|
| PH2-01 | Trec CLD-01…23, FLW-01…04, FLW-29 și ARH-03 (0 pachete RTP prin cloud); APP-19 (originile web); SEC-03, SEC-04, SEC-09…11, SEC-13; REL-12; DEL-08…10; LEG-05, LEG-08, LEG-14 | CI |
| PH1-07 (mutat din Faza 1, T42) | După prima emitere a certificatului, cu API-ul cloud oprit, SPA-ul deja încărcat se conectează la hub pe LAN; live-ul și înregistrarea merg (HUB-24) | E2E |
| PH2-02 | G22 (CLD-30) trece | Test de încărcare |
| PH2-03 | SUC-03 e măsurat pe 4G românesc și prin TURN-TLS/443 de pe AND-1 (web în Chrome); pe iOS-1, odată cu verdictul iOS din Faza 0-bis (§16.9) | Raport de măsurători |
| PH2-04 | Ponderea relay și traficul TURN se măsoară 7 zile pe utilizarea reală a proprietarului; modelul de cost din §15.6 e actualizat | Telemetrie + raport |
| PH2-05 | Restaurarea din backup a bazei cloud trece (CLD-19) | Job săptămânal |
| PH2-06 | G21 trece (build aprobat, nelansat). Rulează odată cu verdictul iOS din Faza 0-bis (§16.9), după ce Faza 2 a adus login-ul și accesul remote, și cel târziu înainte de Faza 4; nu blochează închiderea Fazei 2 (O13) | App Store Connect |

**Faza 3 — Stocare în cloud, reguli, criptare.**
- **Destinații și reguli:** driverele local, NAS (SMB), Google Drive, OneDrive personal și business, S3-compatibil, WebDAV/Nextcloud, SFTP (§9.1). Regulile S1 cu șabloane și editor avansat (S2), previzualizare, outbox, retenție per destinație, politicile „când e plin”, limita de bandă și ferestrele orare, copiere sau mutare, alertele de cotă.
- **Recuperare și migrare:** catalogul pe destinații și F6; migrarea hub-ului (P9).
- **Criptare:** `age` local și pe destinații (E1–E6), ierarhia de chei, cele 4 căi de recuperare (E7) cu asistentul de configurare, `keybackup` în cloud.
- **OAuth:** planul care a câștigat G15.

| ID | Definiția lui gata — Faza 3 | Test |
|---|---|---|
| PH3-01 | Trec STO-01…14, ENC-01…18, FLW-17…27, FLW-31, APP-15, APP-16, HUB-23, REL-06, TST-24, SEC-02 | CI + nightly pe conturi reale de test |
| PH3-02 | SUC-04 trece pe conturi reale (pană de 6 h, Drive și OneDrive) | Rulare nightly + raport |
| PH3-03 | Proiectul Google e în Production, cu brandul verificat; publisher-ul Microsoft e verificat (§18). Prima cere domeniul, a doua cere și SRL-ul (O13, O15) | Consolele furnizorilor |
| PH3-04 | **Exercițiu de recuperare făcut de proprietar.** Pe un PC curat, cu ghidul din `docs/formats/` și cheia de recuperare tipărită, proprietarul decriptează cu `age` CLI o înregistrare luată din Drive | Proprietarul confirmă în PR |
| PH3-05 | F6 merge cu hub-ul oprit pe hub-ul de test al proprietarului: timeline + redare din Drive | Verificare manuală + FLW-20 în CI |

**Faza 4 — Aplicații native, Windows, magazine → MVP (beta publică).**
- **Shell-uri:** Capacitor pentru Android și iOS (sau Flutter, dacă verdictul din Faza 0 sau din Faza 0-bis a aplicat T29), cu pluginurile din §6.6 (`secure-storage`, `local-network`, `pip`, `biometric-lock`, `auth-native`, `hls-proxy` dacă G4 l-a cerut). Tauri pentru Windows, cu installer NSIS semnat care instalează și hub-ul.
- **Release:** canalele de update beta și stable, SBOM per release.
- **Publicare:** fișele din magazine (App Store, Play, web, Windows), hub demo permanent pentru recenzenți, testare pe device farm.
- **Conformitate:** procesul CRA (`security.txt`, politica de divulgare, runbook-ul 24 h/72 h), politica de confidențialitate, termenii și DPA-ul revizuite de avocat, asigurarea de răspundere (§12, §18).

| ID | Definiția lui gata — Faza 4 = MVP | Test |
|---|---|---|
| PH4-01 | Trec APP-01…21 și FLW-05; SEC-06, SEC-12, SEC-18, SEC-21; TST-25, TST-26; DEL-02…04, DEL-06, DEL-11, DEL-12; LEG-04, LEG-06, LEG-10…12 | CI + device farm |
| PH4-02 | G23 trece (SUC-01: pe Windows și pe Docker/NAS, ≥ 4 din 5 participanți sub 10 min, fără ajutor, niciunul peste 15 min) | Test de utilizabilitate |
| PH4-03 | Aplicațiile sunt aprobate în App Store și Google Play (producție); installer-ul Windows e semnat Authenticode și nu e detectat ca malware; dacă apare avertismentul de reputație SmartScreen, `docs` și asistentul de instalare îl explică, iar SUC-01 se măsoară pe Windows cu avertismentul inclus | Magazine + instalare pe un PC curat + scanare antivirus |
| PH4-04 | G24 trece: elementele CRA sunt publice înainte de beta publică (contact de securitate, `security.txt`, politica de divulgare, runbook-ul testat printr-un exercițiu, LEG-09) | Revizuire + exercițiu |
| PH4-05 | G25 trece: avocatul a confirmat documentele legale și valorile de retenție (LEG-13); asigurarea e activă | Documente semnate (§18) |
| PH4-06 | Înainte de beta publică, canalul beta intern rulează ≥ 14 zile cu testeri externi invitați, fără defecte critice deschise | Tracker + telemetrie |
| PH4-07 | Dacă OPN-14 = obligatoriu: pentestul extern pe hub, cloud și aplicații e făcut, iar problemele High+ sunt închise (SEC-23) | Raportul pentestului + issue-urile închise |

### 16.7 MVP: ce intră și ce nu

| Zonă | În MVP | În afara MVP |
|---|---|---|
| Hub | Windows 11 (serviciu; Windows 10 22H2 cu avertisment, OPN-15), Docker Linux x64/arm64/armv7 (NAS, mini PC, orice Raspberry Pi din §4.8, cu numărul recomandat de camere al modelului; card SD cu avertisment, confirmare și supraveghere, T40) | hub pe Android (telefon vechi, TV box); Raspberry Pi armv6 (Pi 1, Zero, Zero W); hardware vândut de noi |
| Camere | RTSP/ONVIF (Profile S/T), adăugare manuală IP/RTSP, asistent cu ghiduri pe mărci, avertismente de securitate, substream H.264 cu acord | camere doar-cloud (Xiaomi, Ring, Arlo, Eufy…) |
| Live | grilă pe substream (telefon 4/pagină, max 9, schimbabil doar cu aprobarea proprietarului după G5; desktop până la 16, după G6), ecran complet pe fluxul principal, WebRTC pe LAN și remote, TURN Cloudflare, PiP | TURN self-hosted (vine la pragul de cost, §5.5) |
| Control | PTZ (mișcare, zoom), presetări, pornire/oprire înregistrare | setări avansate de imagine |
| Audio | ascultare, oprită implicit (P10) | audio bidirecțional |
| Înregistrare | continuu / la mișcare (evenimente ONVIF, pre-roll 5–10 s, post-roll 15–20 s) / oprit; segmente de câteva minute; retenție; curățare de urgență | detecție de mișcare pe hub (v1.1); detecție AI persoane/vehicule; recunoaștere facială (niciodată) |
| Redare | timeline cu evenimente și miniaturi, export MP4, cea mai rapidă sursă, F6 fără hub | — |
| Stocare | local, NAS (SMB), Google Drive (`drive.file`), OneDrive personal și business, S3-compatibil, WebDAV/Nextcloud, SFTP; reguli + șabloane | Dropbox, Box, pCloud, coada lungă prin rclone |
| Criptare | la alegere per destinație și per regulă, „criptarea câștigă”, `age`, 4 căi de recuperare, lacăt în aplicație | conversia înregistrărilor vechi la schimbarea setării |
| Cont și acces | email + parolă + MFA/passkeys, Google/Apple, blocare biometrică, un proprietar per hub, jurnal de audit, export și ștergere cont | partajare cu familia/angajații (modelul de roluri există, interfața nu) |
| Notificări | alerte în aplicație (cameră offline, disc aproape plin, upload blocat, cotă plină) | notificări push la mișcare |
| Monetizare | modelul de drepturi, planul `free` | abonamente, facturare, plăți |
| Clienți | web, Android, iOS, Windows (stack-ul mobil confirmat după verdictul iOS din Faza 0-bis; la NO-GO, Android și iOS trec pe Flutter, T29) | — |

### 16.8 Backlog după MVP (ordine propusă; o decide proprietarul)

| # | Element | De ce în această ordine |
|---|---|---|
| 1 | Notificări push la mișcare (fără imagini implicit, P8) | aprobat pentru „versiunea următoare” (O8.8); cea mai cerută funcție pentru supraveghere |
| 2 | Partajare cu familia/angajații (roluri) | aprobat pentru „versiunea următoare”; modelul există deja |
| 3 | Detecție de mișcare pe hub (pixel-diff) | v1.1 aprobat; acoperă camerele cu evenimente nesigure (G17) |
| 4 | TURN self-hosted în UE | se declanșează la pragul din §5.5, nu după calendar |
| 5 | Dropbox, Box, pCloud și coada lungă prin rclone | driverul are deja interfața; Dropbox cere aprobare de producție |
| 6 | Abonamente și plăți | după decizia de monetizare (OPN-02) |
| 7 | Detecție AI persoane/vehicule (fără fețe) | cere hardware mai puternic și o evaluare GDPR/AI Act |
| 8 | Conversia înregistrărilor existente la schimbarea criptării (E5) | util, dar rar folosit |
| 9 | Audio bidirecțional | presupus după MVP (OPN-04) |
| 10 | Hub pe Android | limitele de serviciu în fundal din Android (land P2) cer o evaluare separată |
| 11 | Setări avansate de imagine | valoare mică față de efort |
| 12 | Conectori pentru camere doar-cloud (API-uri oficiale) — **nediscutat cu proprietarul (propunere)** | O8.1 le scoate doar din MVP; dacă proprietarul le vrea, doar prin API-uri oficiale, niciodată SDK-uri P2P |

### 16.9 Faza 0-bis — amânată (iOS sau costuri)

**Ce e (O13).** Un bloc de măsurători și pregătiri scos din Faza 0: tot ce ține de iOS și tot ce costă bani, oricât de puțin. Nu e o fază de produs și nu are cod propriu: folosește aceleași porți, praguri și planuri B din §16.3–§16.5. Fiecare element pornește când proprietarul aprobă bugetul pentru el (OPN-09, OPN-16; până atunci, 0 €) și trebuie să aibă verdict înainte de faza care îl cere. Elementele se aprobă și rulează separat. Testul iOS rulează cel târziu înainte de începutul Fazei 4 și, recomandat, imediat ce bugetul permite, înainte de orice lucru de interfață specific telefonului (RSK-27). Costurile de mai jos sunt cele din §18 și §15.6.

| Element amânat | Ce rulează sau ce se pregătește | Ce costă sau ce cere | Declanșator | Termen |
|---|---|---|---|---|
| Verdictul iOS: G2, G3, G4 | MV1–MV11 și MV14 pe iOS-1 (§16.3); smoke-ul iOS-2 în CI; Flutter pe iOS-1, dacă T29 s-a aplicat deja pe Android | un iPhone împrumutat (iOS ≥ 17.1, clasa iPhone 12+); Apple Developer, 99 USD/an (OWN-05) | aprobarea bugetului + iPhone disponibil | înainte de Faza 4; recomandat cât mai devreme |
| Părțile iOS din G7, G8, G9 și G13 | MV12 și MV13 în Safari și WKWebView; MV11 pe iOS-1; matricea de codecuri pe căile iOS; `agerange` pe iOS-1 (MI4, punctul 3 din ENC-08) | ca la rândul de mai sus | odată cu verdictul iOS | înainte de Faza 4 |
| G21, revizuirea App Store de probă | build iOS minim trimis la App Review (§12.6) | contul Apple (OWN-05); hub-ul demo pe un VPS (≈ 6 €/lună, §12.6) | după Faza 2 și după verdictul iOS | înainte de Faza 4 |
| Domeniile, emailul pe domeniu, serverul UE și restul infrastructurii cloud (§15.6) | T28 mutată (găzduirea SPA-ului, `certs`, `link/v1`), PH1-07, originile web din APP-19, apoi toată Faza 2 | OWN-04 (≈ 20–40 €/an pentru cele două domenii), OWN-11 (≈ 6 €/lună), OWN-12; după domeniu, OWN-24 (gratuit) | aprobarea bugetului | înainte de Faza 2 |
| G8 complet și criticele remote (MV1 și MV2 la distanță, MV11 pe AND-1), plus MV12 în Chrome, dacă T41 nu a găsit o variantă gratuită | aceleași teste, pe ≥ 4 rețele | VPS UE (OWN-11), Cloudflare TURN (OWN-12), domeniul pentru originea publică (OWN-04); pentru al doilea operator mobil, un SIM preplătit (≈ 5 € [estimare]) sau telefonul cuiva din familie | aprobarea bugetului | înainte de Faza 2 |
| Restul lui G15 (MI6) | combinațiile iOS; OneDrive personal, dacă înregistrarea aplicației nu a fost gratuită; OneDrive business; refresh-ul Drive din ziua 7 și de după, cu proiectul în Production | domeniul pentru verificarea brandului (OWN-04, OWN-07); înregistrarea Microsoft cu date de plată, dacă e nevoie (OWN-08); tenantul de test (OWN-09) | aprobarea bugetului | înainte de Faza 3 |
| G16 (MI7) și părțile OneDrive din G14 (MI5) | Conditional Access și `AppFolder` pe conturi de firmă; pana de 6 h spre OneDrive | Entra ID P1 (OWN-09, ≈ 6–7 USD/lună, doar în luna testului) | aprobarea bugetului | înainte de Faza 3 |
| Conturile reale de stocare pentru testele nightly (§14.5) | STO-01 nightly pe B2, R2, Wasabi și un bucket AWS S3 cu SSE-KMS; PH3-01 | costuri lunare mici [estimare], de confirmat la deschiderea conturilor | aprobarea bugetului | înainte de Faza 3 |
| `ipcam-lab` (VM UE) și `ipcam-win10` | testele de 6 h pe conturi reale (PH3-02), stabilitatea de 72 h săptămânală, testele nightly pe Windows 10 (HUB-28) | OWN-22 (≈ 15 €/lună) | aprobarea bugetului | înainte de Faza 3; HUB-28 nightly înainte de Faza 4 |
| Infrastructura de semnare și G20 (DEL-03) | rădăcina TUF pe YubiKey, `targets` 2 din 2 pe GCP KMS și AWS KMS, build-ul reproductibil verificat pe 3 release-uri | OWN-20 (≈ 165–240 € pentru 3 chei), OWN-21 (câțiva € pe lună) | aprobarea bugetului | înainte de primul release pe canalul beta intern (T44) |
| Semnarea de cod Windows și poarta SmartScreen | Authenticode pe hub, aplicație și detector; PH4-03 | OWN-19 (varianta A cere SRL, O15) | aprobarea bugetului | înainte de Faza 4; recomandat înainte de canalul beta pe Windows (RSK-20) |
| AND-2 și device farm | AND-2 pe Firebase Test Lab (informativ, §16.3); device farm înainte de fiecare release (§14.7) | OWN-17 | aprobarea bugetului | AND-2: înainte de Faza 4; device farm: la primul release de magazin |
| H1 (Pi 5) și H2 (N100) | G10, G12 și G13 pe aceste gazde; până atunci, ≈ 8 și 16 rămân estimări, marcate ca atare | OWN-15 (opțional) | aprobarea bugetului, doar dacă proprietarul vrea | fără termen |
| Pi-ul proprietarului e armv6 | G10–G13 pe un Pi suportat; până atunci, partea Linux a acestor porți se bazează pe laborator | un Pi suportat (OWN-15) | hub-ul sau detectorul arată un Pi armv6 | înainte de Faza 4 |
| Firma și conturile care o cer (O15) | SRL, D-U-N-S, Apple și Play ca organizație, verificarea publisher-ului Microsoft | OWN-02, OWN-03, OWN-05, OWN-06, OWN-08 | decizia proprietarului despre SRL | publisher-ul: înainte de finalul Fazei 3; restul: înainte de Faza 4 |
| Juridic și lansare | avocatul (pornește cel târziu în Faza 2), asigurarea, pentestul (dacă OPN-14 = obligatoriu), marca, organismul notificat (dacă e nevoie) | OWN-25, OWN-26, OWN-28, OWN-29, OWN-31 | aprobarea bugetului | înainte de beta publică (Faza 4) |
| Opționale | priza inteligentă (OWN-16), laboratorul de camere reale (OWN-34), BrowserStack (OWN-17) | costurile din §18 | doar dacă e nevoie | fără termen |

| ID | Cerință pentru Faza 0-bis | Test |
|---|---|---|
| PH0-40 | Un element din Faza 0-bis pornește doar după aprobarea scrisă a proprietarului pentru costul lui (OPN-09, OPN-16), dată în PR-ul sau în issue-ul elementului | Revizuire PR: fiecare cont plătit deschis are trimiterea la aprobare |
| PH0-41 | Termenele din tabel se respectă: verdictul iOS (G2–G4 și părțile iOS din G7, G8, G9 și G13) există înainte de începutul Fazei 4; fiecare alt element are verdict înainte de faza din coloana „Termen” | PR-ul care deschide o fază listează elementele Faza 0-bis cu termen la acea fază, cu verdictul lor; un element fără verdict blochează începutul fazei |
| PH0-42 | Raportul Fazei 0-bis (`docs/research/<data>-phase0bis-results.md`) urmează regulile din PH0-22, PH0-30 și PH0-31; un verdict care schimbă o decizie (de exemplu NO-GO iOS → T29) se scrie în §2.2 înainte de faza următoare | Revizuire PR |

## §17 Riscuri

Probabilitate și impact: **Mică / Medie / Mare** (impact și **Critic**). Responsabil: **AI** (agenții), **Proprietar**, **Avocat**.

| ID | Risc | Prob. | Impact | Atenuare | Resp. |
|---|---|---|---|---|---|
| RSK-01 | **Bus factor go2rtc.** Niciun release din ianuarie 2026, un singur întreținător (crit #1) | Mare | Mediu | Fork vendored, din care folosim doar `pkg/` (`hub/third_party/go2rtc` + `PATCHES.md`); interfața `LiveGateway` (ARH-05); CI pe fixture-uri și laborator; un agent compară lunar diferențele cu upstream. Rezervă documentată: MediaMTX + gortsplib pentru live | AI |
| RSK-02 | **Performanță WebView pe Android mediu** (decodare hardware neconfirmată, încălzire) | Medie | Mare | G5 pe Galaxy A24 (sub clasa țintă, deci conservator); plafoane de plăci; snapshot peste plafon; coborâre termică (§6.5); planul Flutter pentru mobil (§16.3) | AI |
| RSK-03 | **PiP și fundal în WKWebView pe iOS** (rapoarte de PiP doar în prim-plan) | Mare | Mediu | G3; planul B AVPlayer + HLS prin `hls-proxy`; altfel NO-GO mobil | AI |
| RSK-04 | **Respingere Apple 4.2** („doar un site împachetat”) | Medie | Mare | Valoare nativă reală (PiP, biometrie, passkeys, rețea locală); cont + hub demo permanent (Apple 2.1); G21 odată cu verdictul iOS din Faza 0-bis, după Faza 2, înainte de Faza 4; după 2 respingeri se evaluează Flutter pe iOS | AI + Proprietar (Account Holder) |
| RSK-05 | **Camere H.265** (plăci negre, CPU) | Mare | Mediu | Substream H.264 la onboarding (A6); regula SDP + primul cadru (APP-05); înregistrările H.265 marcate; G9 | AI |
| RSK-06 | **Evenimente ONVIF nesigure** (abonamente pierdute în tăcere, `MaxPullPoints`) | Mare | Mediu | Termen 10 min / reînnoire la 8 min; indicator „evenimente OK”; FLW-16 păstrează continuul; G17; adaptoare native pe marcă; pixel-diff în v1.1 | AI |
| RSK-07 | **Banda de upload** (o cameră de 4 Mbps ≈ 43 GB/zi, land N5) | Mare | Mediu | Șabloanele trimit implicit evenimentele în cloud; continuul în cloud e opt-in, după măsurarea uplink-ului; limită de bandă și ferestre orare; alertă „timp până la disc plin” (§9.9) | AI |
| RSK-08 | **Unități de cotă Google Drive.** Facturarea anunțată pentru 2026 se aplică **proiectului nostru**, pe care îl folosesc toți utilizatorii | Medie | Mare | Fără `files.list` (STO-07); segmente de 5 min; descărcări de segment întreg; Drive ultimul la redare (§8.5); G14 măsoară unitățile per segment; alerte de buget în consolă; limite per hub prin drepturi | AI + Proprietar (cont de facturare) |
| RSK-09 | **CRA, clasa I.** Regulamentul de punere în aplicare (UE) 2025/2392 descrie categoria „smart home cu funcții de securitate, inclusiv camere”; CE de la 11.12.2027 | Mare | Mare | Clasificare confirmată de avocat până la finalul Fazei 2; cerințele esențiale aplicate din prima zi (SBOM, update-uri semnate, gestionarea vulnerabilităților, setări sigure implicite); buget pentru examinare de tip UE dacă standardele armonizate întârzie | Proprietar + Avocat |
| RSK-10 | **Pierderea cheilor de criptare** | Mică | Critic | Cele 4 căi (E7); configurarea cere confirmarea cheii de recuperare; ENC-06 în CI; exercițiul PH3-04 | AI |
| RSK-11 | **Costul TURN** (relay peste 25 %, utilizatori care privesc mult fluxul principal) | Medie | Mediu | G8; plafonul de bandă și controlul congestiei (§8.3 pașii 9–10); opțiunea „Economisire date” (implicit oprită); timeout pentru sesiunile inactive; `relay_gb_month` în drepturi; self-hosting peste 2 TB/lună (§5.5) | AI + Proprietar (buget) |
| RSK-12 | **Rețeaua locală pe Android 17** (`ACCESS_LOCAL_NETWORK` la targetSdk 37, estimat aug. 2027) | Mare | Mediu | Plugin `local-network` cu ecran explicativ; teste de acum pe Android 16, prin opt-in; rezervă srflx/relay; reminder în calendar în fiecare august | AI |
| RSK-13 | **Schimbări de firmware la camere** (un update Hikvision oprește ONVIF; digest SHA-256; căi RTSP noi) | Mare | Mediu | `health` detectează schimbarea, camera se reprobează la reconectare; fixture-uri per firmware; hub-ul de test al proprietarului rulează nightly (R4); ghidurile se actualizează; SHA-256 digest în fork | AI |
| RSK-14 | **Derivă în întreținerea de către AI** (arhitectura se erodează, API-uri inventate, teste slăbite) | Mare | Mare | Contract-first cu cod generat; fiecare ID de cerință are cel puțin un test care îl citează (verificare de trasabilitate în CI); reguli de import, limite de linii, teste santinelă; Renovate cu perioadă de așteptare; audit lunar spec-vs-cod făcut de un agent, cu rezumat pentru proprietar; orice abatere de la spec are ADR | AI + Proprietar |
| RSK-15 | **Coada lungă de compatibilitate cu camerele** | Mare | Mare | Listă de compatibilitate pe niveluri, bazată pe fixture-uri; detectorul ca unealtă de diagnostic; adăugare manuală RTSP; coduri de eroare în telemetrie | AI |
| RSK-16 | **Politici și verificări OAuth** (tokenuri de 7 zile în Testing, verificarea brandului, publisher Microsoft, CA) | Medie | Mare | Proiectul Google în Testing din Faza 0 (O13); Production, verificarea brandului și a publisher-ului în Faza 0-bis, înainte de Faza 3 (cer domeniul și SRL-ul); G15, G16; planurile B din §3.3 | Proprietar |
| RSK-17 | **Compromiterea cheii de semnare a update-urilor sau a release-ului web** (P3) | Mică | Critic | Rădăcina TUF offline, 2 din 3 pe YubiKey (OWN-20), și `targets` 2 din 2 la doi furnizori, cu jurnal Rekor (§11.4, T26); build-uri reproductibile; release-uri web semnate; afirmații de marketing limitate la ce e literal adevărat (§11.2) | AI + Proprietar |
| RSK-18 | **Compromiterea hub-ului** (istoric RCE în go2rtc, intrări ostile de la camere) | Medie | Mare | Sidecar-uri doar pe 127.0.0.1; sursele `exec` scoase; fuzzing; update-uri semnate; țintă de patch în 72 h | AI |
| RSK-19 | **Pierderi de înregistrări** (pene de curent, reporniri Windows Update, antivirus, disc plin) | Medie | Mare | G10, G11; fsync; reconciliere; SSD recomandat; pe card SD, avertisment, confirmare și supravegherea cardului (T40); excluderea din Defender; golurile apar cu motiv | AI |
| RSK-20 | **Semnare Windows.** Artifact Signing poate să nu accepte un SRL nou; reputația SmartScreen a unei identități noi de semnare nu e garantată | Medie | Mediu | Certificat OV cu semnare în cloud ca rezervă (§18 OWN-19); pornit în Faza 0-bis, după aprobarea bugetului, cel târziu înainte de Faza 4 (O13). Reputația SmartScreen se acumulează în timp, deci canalul beta pe Windows începe cât de devreme permite bugetul ca s-o construiască; până atunci, avertismentul e explicat în `docs` și în asistent (PH4-03) | Proprietar |
| RSK-21 | **Repo public** (codul, specificația și fixture-urile sunt publice) | Mare | Mediu | Decizia O12 (public deocamdată; OPN-06 închis); scanare de secrete; fixture-uri, rapoarte și date redactate + aprobare (PH0-22) | Proprietar |
| RSK-22 | **Efort subestimat** (≈ 30–45 persoane-lună, crit C18) și timpul proprietarului | Mare | Mediu | Pași mici verificați automat; „definiția lui gata” per fază; fără date promise; replanificare după fiecare fază | AI + Proprietar |
| RSK-23 | **GDPR și legea română** (monitorizarea angajaților max. 30 de zile; spațiul public) | Medie | Mare | Funcțiile din P10; valori confirmate de avocat (PH4-05) | Proprietar + Avocat |
| RSK-24 | **Upgrade-uri continue** (Capacitor anual, target API Play în fiecare august, Tauri; WebView2 pe Windows 10 până în oct. 2028) | Mare | Mediu | Renovate grupat; sarcini programate; matricea CI pe toate platformele | AI |
| RSK-25 | **Certificatele LAN per hub** (limitele Let's Encrypt la mulți hub-uri; routere care filtrează răspunsurile DNS cu IP privat) | Medie | Mediu | Domeniu separat pentru hub-uri, certificate doar după revendicare, cu cote (T35); cerere Public Suffix List (OWN-24) și limite mărite la cerere; Google Trust Services ca a doua CA (§5.7, §11.5); fără certificat sau DNS, live-ul pe LAN merge în continuare prin signaling în cloud și candidați ICE locali, se pierde doar signaling-ul local fără internet | AI + Proprietar |
| RSK-26 | **Windows 10 fără update-uri de securitate** (ESU pentru consumatori, inclusiv varianta gratuită din SEE, s-a oprit pe 13.10.2026) | Mare | Mediu | Windows 11 e platforma principală; Windows 10 22H2 „suportat cu avertisment”, cu alerta din §13.3; teste de fum nightly pe un VM Windows 10 (HUB-28); decizia de suport e a proprietarului (OPN-15) | AI + Proprietar |
| RSK-27 | **Verdict iOS târziu** (O13: testul iOS așteaptă bugetul; cel târziu înainte de Faza 4). Impact: dacă iOS pică târziu, aplicațiile mobile trec pe Flutter (T29), iar munca pe shell-ul Android Capacitor se pierde; web-ul și Windows nu sunt afectate | Medie | Mare | `@ipcam/player`, `@ipcam/core` și `@ipcam/api` rămân independente de framework (APP-22); shell-ul Android rămâne subțire, fără lucru de interfață specific telefonului până la verdictul iOS (§16.3); testul iOS rulează imediat ce bugetul permite (OPN-16), înainte de orice astfel de lucru; tabelul de stări din §6.3.1 și lista de paritate sunt gata pentru Flutter | AI + Proprietar (buget) |

## §18 Ce are de făcut proprietarul

Prețurile au fost verificate pe 2026-10-17. **[oficial]** = pagina furnizorului; **[terți]** = mai multe surse terțe care concordă, pagina oficială nu s-a putut deschide din sandbox; **[estimare]** = neverificabil, de confirmat la cumpărare. Ordinea rândurilor e cea în care sunt necesare (ID-urile rămân stabile, deci rândurile adăugate la revizie nu sunt consecutive). Coloana „În jurnalul aprobat?” spune dacă punctul era în lista aprobată în principiu în conversație („da”) sau e o obligație nouă, propusă de AI, care are nevoie de OK-ul dvs. Coloana „Acum (gratuit) / Amânat” aplică decizia O13: „Acum (gratuit)” = se poate face fără niciun cost și fără SRL; „Amânat” = cere bani sau SRL (O15) și intră în Faza 0-bis (§16.9), cu faza care îl cere.

| ID | Ce | De ce | Cost aprox. | Timp de obținere | Când | În jurnalul aprobat? | Acum (gratuit) / Amânat |
|---|---|---|---|---|---|---|---|
| OWN-01 | Răspunsurile la §19. OPN-05, 06 și 07 au primit răspuns pe 2026-10-18; rămân mai ales OPN-01, OPN-09 și OPN-16 | blochează conturile și achizițiile | — | 1 zi | înainte de Faza 0 (OPN-09 și OPN-16: înainte de Faza 0-bis) | da | **Acum (gratuit)** |
| OWN-02 | **SRL** (dacă nu există) | Apple și Play ca organizație, publisher Microsoft, DPA-uri, semnare de cod | taxe ONRC de câteva sute de lei + contabil lunar [estimare] | 1–2 săpt. | mai târziu (O15): înainte de verificarea publisher-ului Microsoft (finalul Fazei 3) și de conturile de organizație (Faza 4) | da | **Amânat** (cost; O15) — Faza 0-bis, înainte de finalul Fazei 3 |
| OWN-03 | **Număr D-U-N-S** al SRL-ului (prin instrumentul de căutare al Apple) | obligatoriu pentru Apple și Google Play ca organizație | gratuit [terți]; nu plătiți intermediari | Apple: ~5 zile lucrătoare + 2 [terți]; Google: „până la 30 de zile” [oficial] | după SRL (O15) | da | **Amânat** (cere SRL) — înainte de Faza 4 |
| OWN-04 | **Domeniu neutru de infrastructură + email pe domeniu + site public minim**, plus un al doilea domeniu pentru hub-uri (`<domeniu-hub>`) | Apple cere site funcțional pe domeniul organizației și email de serviciu pe acel domeniu; Google îl cere la verificarea brandului; îl folosesc link-urile universale. Al doilea domeniu servește doar certificatele LAN ale hub-urilor (T35) | ~10–20 €/an per domeniu, două domenii [estimare]; site-ul îl face AI-ul | 1 zi | Faza 0-bis, înainte de Faza 2 (T28, T42) | da (al doilea domeniu e nou — propus de AI, cere aprobare) | **Amânat** (cost) — înainte de Faza 2 |
| OWN-05 | **Apple Developer Program (organizație)**; pentru spike, dacă organizația nu e gata, un cont de persoană fizică (§16.3) | TestFlight intern pentru spike-ul iOS (G2–G4); App Store | **99 USD/an** [oficial] fiecare; organizația cere entitate juridică, D-U-N-S, site, email pe domeniu, Apple Account cu 2FA, drept de semnătură; persoana fizică nu cere D-U-N-S. Conversia ulterioară persoană fizică → organizație se verifică cu Apple | persoană fizică: ~1–2 zile [estimare]; organizație: după D-U-N-S, ~2 zile de verificare [terți] | Faza 0-bis iOS, cel târziu înainte de Faza 4 (persoană fizică sau organizație, dacă SRL-ul există); Faza 4 (organizație, pentru publicare) | da (contul de persoană fizică pentru spike e nou — propus de AI, cere aprobare) | **Amânat** (cost, iOS) — Faza 0-bis, înainte de Faza 4 |
| OWN-06 | **Google Play Console (organizație)** | publicarea pe Android; contul personal ar cere un test închis cu 12 testeri × 14 zile, contul de organizație nu [terți]. APK-ul de test din Fazele 0–1 nu are nevoie de el (T41) | **25 USD o singură dată** [terți]; D-U-N-S obligatoriu [oficial] | zile, după D-U-N-S | Faza 4 (înregistrare și publicare; cere SRL, O15) | da | **Amânat** (cost, SRL) — Faza 4 |
| OWN-07 | **Proiect Google Cloud + ecran de consimțământ OAuth**, doar cu `drive.file`. **Acum:** în modul Testing, pe contul Google personal al proprietarului, cu el și conturile de test ca utilizatori de test; tokenurile de 7 zile sunt acceptate la spike-uri (T41). **Mai târziu:** în **Production**, cu verificarea brandului: pagina principală pe domeniul verificat, link de confidențialitate identic, video demo cu fluxul OAuth și scope-urile exacte [oficial]. `drive.file` nu cere CASA (land S1, crit C8) | upload-ul și testele G14/G15; în Testing, tokenurile expiră după 7 zile | gratuit (verificarea brandului cere domeniul, OWN-04) | ~2–3 zile lucrătoare pentru verificarea brandului [terți] | Faza 0 (Testing); Faza 0-bis, înainte de Faza 3 (Production + brand) | da | **Acum (gratuit):** Testing. **Amânat** (cere domeniul): Production + brand, înainte de Faza 3 |
| OWN-08 | **Microsoft:** tenant Entra + înregistrarea aplicației + Microsoft AI Cloud Partner Program + **verificare de publisher**. Cere Partner ID de cont global (nu de locație) și un domeniu propriu verificat DNS în Entra, același cu cel al contactului principal; `*.onmicrosoft.com` nu e acceptat [oficial] | OneDrive personal (Faza 0), OneDrive business (Faza 3; tenant-urile firmelor blochează aplicațiile neverificate) | gratuit [estimare pentru înscrierea în program] | zile – 2 săpt. [estimare] | Faza 0 (înregistrarea, dacă e gratuită; altfel Faza 0-bis), Faza 3 (verificare) | da | **Acum (gratuit):** doar înregistrarea aplicației pe contul personal, dacă nu cere date de plată (T41). **Amânat** (SRL, domeniu): Partner Program + verificarea de publisher, înainte de finalul Fazei 3 |
| OWN-09 | **Tenant de test cu Entra ID P1** (Conditional Access) | G16 | ~6–7 USD/utilizator/lună [terți]; un trial, dacă există, e de verificat; poate fi activ doar în luna testului G16 (§15.6) | 1 zi | Faza 0-bis, în luna testului G16, înainte de Faza 3 | nou — propus de AI, cere aprobare | **Amânat** (cost) — înainte de Faza 3 |
| OWN-10 | **Conturi de test dedicate** (Google și Microsoft personal) pentru CI; Drive-ul dvs. personal rămâne pentru testele manuale (O11) | testele automate nu ating datele dvs.; refresh token-urile stau ca secrete CI | gratuit | 1 zi | Faza 0 | da | **Acum (gratuit)** |
| OWN-11 | **Server UE** (Hetzner sau echivalent) | cloud-ul (din Faza 2); server de spike pentru signaling doar dacă testul remote nu are variantă gratuită (T41) | CX23 ~5,49 €/lună + 0,50 € IPv4 (după două scumpiri în 2026) [terți]; buget total la început ~20–50 €/lună, cu Cloudflare | 1 zi | Faza 0-bis, înainte de Faza 2 | da | **Amânat** (cost) — înainte de Faza 2 |
| OWN-12 | **Cont Cloudflare** (DNS + Realtime TURN) | TURN-TLS/443 în G2/G8; DNS-01 pentru certificate | DNS gratuit; TURN **0,05 USD/GB** după **1.000 GB/lună gratuit**, comun cu SFU [oficial] | 1 zi | Faza 0 doar pentru testul remote gratuit (T41); altfel Faza 0-bis, înainte de Faza 2 | da | **Acum (gratuit)** doar dacă testul remote din Faza 0 merge pe planurile gratuite, fără date de plată (T41); altfel **Amânat**, înainte de Faza 2 |
| OWN-13 | **Rularea detectorului** pe un PC din rețeaua camerelor și trimiterea arhivei (§16.2) | G1; fixture-urile pentru laborator | gratuit | 30 min | Faza 0, ziua ~3 | da (D1) | **Acum (gratuit)** |
| OWN-34 | **Laborator de camere reale** (opțional) | nu e necesar acum, aveți camere (O10); se reevaluează după G1 dacă lipsesc mărci importante (Dahua, Uniview, Tapo) | ~400–700 € [estimare] | 1–2 săpt. | după G1, doar dacă e nevoie | da | **Amânat** (cost), doar dacă e nevoie |
| OWN-14 | **Dispozitive de test:** PC-ul Windows 11 și Samsung Galaxy A24 ale proprietarului (O14) acoperă WIN-1, WEB și AND-1, deci nu se cumpără nimic; iPhone-ul (iOS ≥ 17.1, clasa iPhone 12+) se împrumută în Faza 0-bis (iOS-1) | matricea din §16.3 | 0 € acum; iPhone împrumutat | — | Faza 0 (PC, A24); Faza 0-bis (iPhone împrumutat) | da (O14, conv. 2026-10-18) | **Acum (gratuit):** PC + A24, deja ale proprietarului. **Amânat** (iOS): iPhone împrumutat, înainte de Faza 4 |
| OWN-15 | **Hardware pentru hub-ul de test:** Raspberry Pi-ul proprietarului, oricare model suportat (§4.8; modelul îl citește hub-ul, HUB-34), cu SSD dacă există, altfel card SD (T40); opțional, Raspberry Pi 5 8 GB + SSD și mini PC N100 | G10–G12; hub-ul de test permanent (R4) | 0 € acum; Pi 5 + SSD ~150–200 € [estimare] și N100 ~150–250 € (land §3.3), doar dacă se aprobă bugetul | 2–5 zile (doar pentru cele cumpărate) | Faza 0 (Pi-ul existent); Faza 0-bis (opțional) | da (O14, O16) pentru Pi-ul existent; Pi 5 și N100: nou — propus de AI, cere aprobare | **Acum (gratuit):** Pi-ul existent. **Amânat** (cost): Pi 5, N100, opțional |
| OWN-16 | **Priză inteligentă cu API local** (ex. Shelly) | tăieri de curent automate și repetabile (MI1); în Faza 0 le înlocuiesc tăierile manuale, câte 5 pe dispozitiv (T43) | ~20–30 € [estimare] | 2 zile | opțional, după aprobarea bugetului | nou — propus de AI, cere aprobare | **Amânat** (cost), opțional |
| OWN-17 | **Device farm.** Firebase Test Lab (Android + iOS): 5 USD/oră dispozitiv fizic, 1 USD/oră virtual, gratuit 30 min/zi fizic și 60 min/zi virtual, pe planul Blaze [oficial]. BrowserStack App Live ~29–49 USD/lună; App Automate ~199–249 USD/lună per sesiune paralelă [terți] | AND-2 în Faza 0-bis; matricea înainte de fiecare release (§14.7) | Firebase cu plată la utilizare; BrowserStack doar o lună, dacă lipsesc dispozitive | 1 zi | Faza 0-bis (AND-2), Faza 4 | da (R3) | **Amânat** (cost) — AND-2 înainte de Faza 4; device farm la primul release de magazin |
| OWN-18 | **CI GitHub Actions.** Repo-ul DevWorld e **public** (verificat prin API pe 2026-10-17) și rămâne public (O12), deci runner-ele standard, inclusiv macOS, sunt gratuite. Dacă se trece pe privat: macOS 0,062 USD/min [oficial] | build-uri iOS și teste pe toate platformele | 0 acum; ~50–150 €/lună dacă devine privat [estimare] | — | acum (O12) | da (R3) | **Acum (gratuit)** |
| OWN-19 | **Semnare de cod Windows.** (A) Microsoft Artifact Signing: GA din ian. 2026 pentru organizații din SUA, Canada, UE și Marea Britanie [terți]; regula din apr. 2025 cu „≥ 3 ani de istoric verificabil” s-ar putea aplica încă unui SRL nou [oficial pentru 2025, neconfirmat pentru 2026]; Basic: 5.000 semnături/lună [oficial], ~9,99 USD/lună [estimare din memorie; pagina de preț nu afișează suma]. (B) Certificat OV cu semnare în cloud (fără token USB, merge în CI): ~116 USD/an (Certum, prin revânzător, 3 ani) până la 219–400 USD/an (Sectigo, DigiCert) [terți] | evită blocarea SmartScreen la hub și la aplicația Windows | vezi coloana „Ce” | A: verificarea identității, zile–săptămâni; B: 1–5 zile [estimare] | Faza 0-bis, cel târziu înainte de Faza 4 (recomandat înainte de canalul beta pe Windows, RSK-20); până atunci, build-urile Windows de test sunt nesemnate (T41, T44) | da | **Amânat** (cost; varianta A cere SRL) — înainte de Faza 4 |
| OWN-20 | **3 chei YubiKey 5 (PIV/FIDO2)** și o persoană de încredere (sau avocatul) care păstrează a treia; una stă la proprietar acasă, una în seif bancar | rădăcina TUF offline, 2 din 3 (§11.4, RSK-17) și ceremonia anuală; dacă G20 pică, semnătura release-urilor stable, o dată la 2–4 săptămâni (DEL-03); MFA pe conturile de administrare | ~55–80 € per bucată [estimare] | 2–5 zile | Faza 0-bis, înainte de primul release pe canalul beta (G20) | nou — propus de AI, cere aprobare | **Amânat** (cost) — înainte de canalul beta |
| OWN-21 | **Cont AWS separat + proiect Google Cloud cu Cloud KMS** pentru semnarea release-urilor (rolul `targets` 2 din 2: builder pe GCP KMS, rebuilder pe AWS KMS, §11.4) | o singură cheie compromisă nu ajunge pentru un update rău (T26) | câțiva euro pe lună pentru chei și build-uri [estimare; linia KMS din §15.6] | 1 zi | Faza 0-bis, înainte de G20 | nou — propus de AI, cere aprobare | **Amânat** (cost) — înainte de canalul beta |
| OWN-22 | **VM UE pentru runner-ul self-hosted `ipcam-lab`** | testele lungi (pană de 6 h, stabilitate 72 h) și furnizorii reali; runner-ele GitHub opresc joburile la 6 h (§14.7) | ≈ 15 €/lună [estimare, §15.6] | 1 zi | Faza 0-bis, înainte de Faza 3 (și de testele nightly pe Windows 10, HUB-28) | nou — propus de AI, cere aprobare | **Amânat** (cost) — înainte de Faza 3 |
| OWN-23 | **Aprobarea unei GitHub App** cu drept doar asupra secretelor repo-ului | rotația săptămânală a refresh token-urilor conturilor de test; cele Microsoft expiră la 90 de zile (§14.5) | gratuit | 1 zi | Faza 1 | nou — propus de AI, cere aprobare | **Acum (gratuit)**, când e nevoie (Faza 1) |
| OWN-35 | **Aprobarea a două schimbări la rădăcina repo-ului:** un ruleset pe `main` care face obligatorie verificarea `ipcam-gate` (cu `ipcam-gate.yml` fără `paths:`, ieșire rapidă în afara `ipcam/**`) și rândurile `/ipcam/...` din CODEOWNERS pentru un GitHub App (sau cont bot) de recenzie | „fiecare schimbare trebuie să treacă” (R3) e impus de GitHub, nu doar de agenți (§14.8, §15.4) | gratuit | 1 zi | Faza 1, înainte de primul PR cu cod | nou — propus de AI, cere aprobare | **Acum (gratuit)**, înainte de primul PR cu cod |
| OWN-24 | **Cererea de includere a `<domeniu-hub>` în Public Suffix List** (pregătită de AI, trimisă de titularul domeniului) | limitele Let's Encrypt se numără atunci per hub (§11.5, RSK-25) | gratuit | săptămâni–luni [estimare] | după domeniu (Faza 0-bis), înainte de Faza 2 | nou — propus de AI, cere aprobare | **Amânat** (cere domeniul) — înainte de Faza 2 |
| OWN-25 | **Avocat român:** valorile de retenție, clasificarea CRA, Legea 333/2003 + HG 301/2012, politica de confidențialitate, termenii, DPA-ul (P12) | conformitate și răspundere | 1.500–4.000 € pachetul inițial [estimare; cereți 2–3 oferte] | 2–6 săpt. | Faza 0-bis: începe cel târziu în Faza 2, gata înainte de Faza 4 | da | **Amânat** (cost) — înainte de Faza 4 |
| OWN-26 | **Buget pentru un organism notificat**, dacă hub-ul e Clasa I și standardele armonizate nu se pot aplica integral (§12.5) | evaluarea conformității CRA până la 11.12.2027 | neverificat [estimare: cereți ofertă după avizul avocatului] | luni [estimare] | decizia la finalul Fazei 2 (RSK-09) | da (P11) | **Amânat** (cost) |
| OWN-27 | **Contact de securitate CRA** (o persoană + email/telefon) și aprobarea politicii de divulgare | obligațiile Art. 14 se aplică din 11.09.2026 (crit #10) | — | 1 zi | înainte de beta publică (Faza 4) | da (P11) | **Acum (gratuit)**, înainte de beta publică |
| OWN-28 | **Asigurare de răspundere pentru produs** (Directiva 2024/2853, transpunere până la 9.12.2026) | software-ul e „produs” | neverificabil [estimare: cereți ofertă de la broker] | 2–4 săpt. | înainte de beta publică | da | **Amânat** (cost) — înainte de beta publică |
| OWN-29 | **Pentest extern** pe hub, cloud și aplicații (SEC-23), apoi anual: recomandat; obligatoriu doar dacă alegeți asta la OPN-14 | dacă e obligatoriu, problemele High+ se închid înainte de beta publică (PH4-07) | neverificat [estimare: cereți 2–3 oferte] | 2–4 săpt. | înainte de beta publică (Faza 4), dacă OPN-14 = obligatoriu | nou — propus de AI, cere aprobare | **Amânat** (cost) — dacă OPN-14 = obligatoriu |
| OWN-30 | **Statut de comerciant (DSA) în App Store și Play**: adresă, telefon și email publice pentru UE [din memorie] | distribuția în UE | — | 1 zi | Faza 4 | nou — propus de AI, cere aprobare | **Amânat** (cere conturile de magazin și SRL-ul) — Faza 4 |
| OWN-31 | **Căutare și înregistrare de marcă** pentru numele ales (TMview, EUIPO, OSIM; clasele 9, 38, 42, 45) | numele din magazine | EUIPO online: 850 € prima clasă, +50 € a doua, +150 € fiecare de la a treia → ~1.200 € pentru 4 clase [terți] | căutarea: 1 zi; înregistrarea: ~4–6 luni [estimare] | după alegerea numelui, înainte de Faza 4 | da | **Amânat** (cost) — înainte de Faza 4 |
| OWN-32 | **Verificare de dezvoltator Android** (Android Developer Console), doar pentru APK-uri distribuite în afara Play: beta APK sau hub Android viitor | aplicarea începe pe 30.09.2026 în BR, ID, SG, TH; global din 2027 [oficial + crit] | 25 USD pentru cont cu distribuție completă [oficial] | până la 28 de zile [oficial] | doar dacă e nevoie (după MVP, sau dacă regulile ajung să blocheze APK-ul de test din Fazele 0–1, §12.6) | da | **Amânat** (cost), doar dacă e nevoie |
| OWN-33 | **TVA OSS sau merchant of record** | vânzarea de abonamente în UE | — | — | după decizia de monetizare (OPN-02) | da | **Amânat** (după monetizare) |

## §19 Întrebări deschise

| ID | Întrebare | De ce contează | Implicit până la răspuns | Termen |
|---|---|---|---|---|
| OPN-01 | **Numele produsului** (32 de variante respinse) | magazine, marcă, site | Decizie tehnică: identificatorii permanenți (bundle ID iOS, `applicationId` Android, domeniul de infrastructură) folosesc un nume **neutru** legat de SRL (ex. `ro.<srl>.ipcam`), ca numele comercial să se poată schimba fără să schimbăm ID-uri care nu se mai pot modifica după publicare. Până la SRL (O15), build-urile de test (APK-ul și build-ul Tauri din Fazele 0–1) folosesc un ID temporar care nu se publică niciodată | înainte de Faza 4 (fișele din magazine și marca) |
| OPN-02 | **Monetizarea și canalele de plată în UE.** IAP 26 %/15 %, procesator alternativ 20 %/10 % sau link-out cu comision 15 %/10 % la Apple (crit C1, de reconfirmat în Attachment 14); opțiuni similare la Google; TVA OSS sau merchant of record | marje, efortul de facturare | MVP gratuit; modelul de drepturi e gata (O7) | înainte de backlog #6 |
| OPN-03 | **Mărcile și modelele camerelor dvs.** | laboratorul, ghidurile, fixture-urile | răspunsul îl dă detectorul (G1) | Faza 0, săpt. 1 |
| OPN-04 | **Audio bidirecțional** (nediscutat) și ascultarea în MVP | permisiuni de microfon, review în magazine | bidirecțional după MVP; ascultarea e în MVP, oprită implicit (P10) | înainte de Faza 4 |
| OPN-05 | **[Închis 2026-10-18]** **Aveți deja un SRL** (nume, sediu, cod CAEN)? | D-U-N-S, Apple, Play și Microsoft depind de el; cu SRL nou, Artifact Signing poate fi indisponibil (OWN-19) | **Răspuns:** „mai târziu” (O15). Începem cu munca ce nu cere firmă; conturile care o cer sunt în Faza 0-bis (§16.9) | închis |
| OPN-06 | **[Închis 2026-10-18]** **Repo-ul DevWorld e public.** Codul, specificația și fixture-urile ipcam vor fi vizibile tuturor. Rămâne public sau mutăm `ipcam/` într-un repo privat? | concurență, imagine; CI gratuit pe public (OWN-18); un produs comercial cu cod public rămâne sub CRA (crit C9) | **Răspuns:** rămâne public deocamdată (O12); niciun secret în repo; fixture-urile, rapoartele și datele de test se publică doar după PH0-22 | închis |
| OPN-07 | **[Închis 2026-10-18]** **Ce dispozitive aveți:** iPhone (model și iOS), Android, PC Windows desktop, Raspberry Pi; ce operator mobil folosiți | ce cumpărăm (OWN-14, OWN-15) și pe ce rețele testăm G8 | **Răspuns (O14):** PC cu Windows 11, Raspberry Pi (modelul îl citește hub-ul), Samsung Galaxy A24, internet acasă de la DIGI; fără iPhone (unul se împrumută în Faza 0-bis); nu se cumpără nimic. Operatorul mobil al telefonului se notează la testul remote (G8) | închis |
| OPN-08 | **Latența acceptată în PiP pe iOS**, dacă trece doar planul B (AVPlayer + HLS, ~3–4 s) | decide dacă G3 trece | acceptăm ≤ 4 s în PiP; live-ul din aplicație rămâne < 1 s pe LAN | la verdictul G3 |
| OPN-09 | **Plafonul de buget lunar** pentru infrastructură și unelte până la beta publică | alegerea device farm-ului, momentul self-hosting-ului TURN, pornirea Fazei 0-bis | **0 € până decide proprietarul (O13).** După aprobare, estimarea „Până la beta” din §15.6 e ≈ 75–185 €/lună; dacă vreți ≤ 100 €, Entra P1 rămâne doar în luna testului G16, `ipcam-lab`, Authenticode și KMS pornesc abia când le cere faza lor (§16.9), iar BrowserStack doar la nevoie | înainte de Faza 0-bis (OPN-16) |
| OPN-10 | **Cine răspunde utilizatorilor** (suport, contact de securitate CRA) | runbook-ul 24 h/72 h are nevoie de un om de contact | proprietarul, cu ciorne pregătite de AI | înainte de beta publică |
| OPN-11 | **Resetarea locală a proprietarului unui hub** (§8.1 F1.X3): o acțiune pe hub care golește lista de dispozitive și păstrează înregistrările, pentru hub-uri vândute sau conturi pierdute | o propunem ca Decizie tehnică, dar o puteți vedea ca funcție nouă | o păstrăm, cu audit și doar cu acces fizic sau administrativ la hub | înainte de Faza 2 |
| OPN-12 | **Limitele anti-abuz ale planului gratuit** (T31): 5 hub-uri, 32 de camere per hub, 4 sesiuni remote simultane, 100 GB relay pe lună | protejează costul TURN și serviciul; un utilizator mare le poate atinge | le păstrăm, configurabile fără release; peste limita de relay doar alertă internă | înainte de beta publică (Faza 4) |
| OPN-13 | **Parola de rezervă pentru acces** la conturile Google, Apple și doar cu passkey (§10.3, T37): aplicația o oferă încă de la onboarding, opțional, cu „Mai târziu” și un avertisment? E aceeași parolă cu „parola pentru înregistrări” din E7 | fără ea, un telefon nou al acestor conturi se aprobă doar de pe alt dispozitiv de încredere sau cu codul de pe hub (acces fizic); cu ea, „telefonul nou vede tot” (E7.1) merge și fără criptare | nu o oferim la onboarding; parola se setează la configurarea criptării, ca în E7 | înainte de Faza 2 |
| OPN-14 | **Pentestul extern** înainte de beta publică (cost: ofertă, OWN-29): obligatoriu sau doar recomandat? | e o poartă de lansare cu cost încă necunoscut | recomandat, nu obligatoriu; PH4-07 se aplică doar dacă alegeți „obligatoriu” | înainte de Faza 4 |
| OPN-15 | **Windows 10 ca platformă pentru hub și aplicație**, după oprirea update-urilor de securitate (13.10.2026, inclusiv ESU) | un produs de securitate pe un sistem fără patch-uri; costul testelor pe Windows 10 | „suportat cu avertisment” în MVP: alertă în aplicație, recomandarea Windows 11 sau a unui hub Docker, teste de fum pe VM (HUB-28) | înainte de Faza 4 |
| OPN-16 | **Când pornim Faza 0-bis** (buget, iPhone împrumutat) | testul iOS și conturile plătite sunt precondiții pentru Fazele 2–4; un verdict iOS târziu poate muta aplicațiile mobile pe Flutter (RSK-27) | nu pornim până decideți; elementele și termenele sunt în §16.9, iar agentul cere aprobarea pentru fiecare element cel târziu la începutul fazei dinaintea celei care îl cere (PH0-40, PH0-41) | înainte de Faza 4 pentru iOS; înainte de faza care o cere pentru restul |

## Anexa A Glosar

| Termen | Explicație pe înțelesul tuturor |
|---|---|
| Hub | Programul nostru care stă pe un calculator mereu pornit la utilizator. Vorbește cu camerele, înregistrează și urcă fișierele. |
| Cloud (al nostru) | Serverele noastre mici din UE: conturi, legarea hub-ului de cont, punerea în legătură a aplicației cu hub-ul. Nu văd video. |
| RTSP | „Limba” în care camera IP trimite imaginea prin rețea. |
| ONVIF, WS-Discovery | Standard prin care camere de mărci diferite înțeleg aceleași comenzi (mișcare, presetări, evenimente); WS-Discovery e mesajul la care camerele răspund „sunt aici”. |
| PTZ, presetare | Pan-Tilt-Zoom: rotire stânga-dreapta, sus-jos și apropiere; presetarea e o poziție salvată, la care revii dintr-o atingere. |
| Flux principal / substream | Camera trimite două imagini: una de calitate mare (ecran complet, înregistrare) și una mică (grilă, rețele lente). |
| Codec, H.264, H.265, fps, bitrate | Metoda de comprimare a imaginii (H.264 merge peste tot; H.265 ocupă mai puțin, dar nu merge oriunde); cadre pe secundă; date pe secundă (ex. 4 Mbps). |
| Keyframe, GOP, B-frames, smart codec | Keyframe = cadru complet de la care poate porni redarea; GOP = distanța dintre două keyframe-uri; B-frames și „smart codec” sunt trucuri de compresie care strică redarea în browser, deci le oprim la configurare. |
| Latență | Întârzierea dintre ce se întâmplă în fața camerei și ce apare pe ecran. |
| WebRTC, P2P | Tehnologia video în timp real din browsere și telefoane (ca la apelurile video); P2P = legătură directă hub ↔ telefon, fără server la mijloc. |
| NAT, CGNAT, port forwarding, UPnP | Felul în care routerele și operatorii ascund dispozitivele, deci nu se poate intra direct din internet într-o casă; port forwarding și UPnP = deschiderea routerului spre internet, pe care nu o folosim. |
| STUN, TURN, ICE | Metodele prin care WebRTC găsește drumul între hub și telefon; TURN e releul de rezervă, care vede doar date criptate. |
| Signaling, SDP | Schimbul scurt de „cărți de vizită” dintre telefon și hub înainte de pornirea video-ului (SDP = conținutul lor: codecuri, adrese, chei); trece prin cloud sau, acasă, direct prin rețeaua locală. |
| Canal de date, RPC | Un „fir” de date în aceeași conexiune WebRTC, prin care aplicația cere lucruri hub-ului (PTZ, setări, timeline); RPC = un apel de funcție peste rețea. |
| Placă, slot, canal bulk | Placa e o cameră afișată în grilă; slotul e locul din conexiunea video în care hub-ul pune camera cerută; canalul bulk e un fir separat din aceeași conexiune pentru date mari (redare, export, miniaturi). |
| Protobuf, contract, API | Descrierea unică a tuturor mesajelor dintre componente (API-ul); din ea se generează automat cod Go și TypeScript, ca părțile să nu se contrazică. |
| MSE, HLS | Moduri prin care browserul redă video livrat pe bucăți; HLS e folosit pentru înregistrări. |
| fMP4, segment, parte, fsync | Formatul fișierelor video: un segment are câteva minute și e scris în părți de ~1 s; fsync obligă sistemul să scrie efectiv pe disc, ca o pană de curent să piardă de obicei în jur de o secundă (cel mult 2 s în teste). |
| Pre-roll, post-roll, retenție | Secundele păstrate înainte și după o mișcare detectată; retenția = cât timp (sau câți GB) se păstrează înregistrările înainte de ștergere. |
| Inel local, gol, plafon de retenție | Inelul local = ultima oră înregistrată continuu pe hub în modul „doar la mișcare”, din care se taie clipurile; golul = o perioadă fără înregistrare, afișată cu motivul ei (cameră offline, pană, disc plin); plafonul de retenție = limita legală de zile a unei camere, care se aplică pe toate destinațiile. |
| Destinație, regulă de stocare, spool, outbox | Destinația e un loc unde se salvează (disc, NAS, Drive, OneDrive, S3…); regula spune ce se salvează, de la ce camere, când, unde și dacă se criptează; spool = discul local ca zonă tampon; outbox = coada de fișiere care așteaptă să fie urcate. |
| OAuth, PKCE, refresh token | Metoda standard prin care aplicația primește acces la Drive/OneDrive fără să afle parola contului; PKCE e o protecție în plus; refresh token-ul e „cheia” de lungă durată, ținută doar pe hub. |
| `drive.file`, CASA | Permisiunea Google prin care aplicația vede doar fișierele create de ea; folosind-o evităm CASA, evaluarea de securitate anuală și plătită cerută pentru permisiuni largi. |
| S3, NAS, SMB, WebDAV, SFTP | S3 = standard de stocare în cloud (AWS, Backblaze B2, Wasabi, Cloudflare R2 și alții); NAS = disc de rețea de acasă/birou; SMB = partajarea de fișiere din Windows; WebDAV (ex. Nextcloud) și SFTP = alte moduri standard de a salva fișiere pe un server. |
| `age`, cheie de recuperare | Format de criptare deschis și documentat: fișierele se pot decripta și fără aplicația noastră; cheia de recuperare e cheia tipărită sau salvată la configurare, care deschide înregistrările dacă se pierde tot restul. |
| Cheile ipcam: HIK, DK, RI, RK, BK, EK | `HIK` = identitatea hub-ului; `DK` = cheia fiecărui telefon sau calculator; `RI` = cheia de înregistrări a hub-ului, cu generații numerotate; `RK` = cheia de recuperare; `BK` = cheia care încuie backup-ul din cont; `EK` = cheia prin care un dispozitiv nou dovedește că a deschis backup-ul din cont (cu parola sau cu cheia de recuperare) (§10.3). Rotația = trecerea la o generație nouă de chei pentru fișierele noi. |
| End-to-end (E2E), DTLS-SRTP | Criptare în care doar capetele (hub și aplicația) pot citi datele, nu și serverele de la mijloc; DTLS-SRTP e varianta folosită de WebRTC. |
| DPAPI, TPM, sigilare | Mecanisme Windows/hardware care leagă un secret de acel calculator; „sigilat” = criptat cu o astfel de cheie. |
| Fixarea cheii (pinning) | Aplicația ține minte cheia hub-ului de la împerechere, ca nimeni, nici serverele noastre, să nu se poată da drept hub. |
| Amprentă (fingerprint) | Un șir scurt calculat din cheia hub-ului (16 caractere), pe care utilizatorul îl compară în aplicația Windows a hub-ului (sau pe pagina `/pair`, ori în `docker logs`) și pe telefon, ca să fie sigur că vorbește cu hub-ul lui. |
| Certificat LAN, ACME, DNS-01, CT | Certificatul care face ca adresa locală a hub-ului să fie „sigură” în browser (pe un domeniu separat, `<domeniu-hub>`); ACME/DNS-01 = felul automat în care îl obținem de la Let's Encrypt; CT = jurnalele publice în care apare orice certificat emis. |
| Passkey, MFA | Autentificare fără parolă, cu telefonul sau amprenta; MFA = al doilea pas de verificare la login. |
| SPA, PWA | Aplicație web într-o singură pagină; PWA = aplicație web care se poate „instala” din browser. |
| Capacitor, Tauri, WebView, PiP | „Cutiile” care transformă aplicația web în aplicație Android/iOS (Capacitor) și Windows (Tauri); WebView (WebView2, WKWebView) e motorul de browser din interiorul lor; PiP = imaginea camerei rămâne într-o fereastră mică peste alte aplicații. |
| Docker, watchdog, sidecar | Docker = mod standard de a rula programe pe NAS, Linux sau Raspberry Pi cu o singură comandă; watchdog = paznicul care repornește automat o componentă căzută; sidecar = program auxiliar pornit și supravegheat de programul principal. |
| LAN, VLAN, mDNS | Rețeaua locală din casă/birou; VLAN = o rețea separată, de exemplu doar pentru camere; mDNS = felul în care dispozitivele se găsesc singure în rețeaua locală. |
| CI | Sistemul care construiește și testează automat fiecare schimbare înainte să fie acceptată. |
| E2E, laborator, Playwright | Teste cap-coadă cu hub, camere simulate, cloud și aplicația reală; Playwright pilotează browserul și verifică faptul că video-ul chiar rulează. |
| Record-replay, fuzzing | Înregistrăm răspunsurile camerelor reale și le „redăm” în teste, ca să prindem ciudățeniile fiecărei mărci; fuzzing = testare cu date aleatoare sau stricate, ca să găsim căderi și breșe. |
| Licență permisivă vs. GPL/AGPL, SBOM | MIT/BSD/Apache permit un produs comercial fără obligația de a-i publica codul; GPL/AGPL ne-ar obliga să publicăm codul, deci sunt interzise în produs; SBOM = lista tuturor componentelor dintr-o versiune livrată, cerută de CRA. |
| TUF, Rekor, build reproductibil | TUF = regulile după care hub-ul verifică un update (semnături multiple, expirare); Rekor = un jurnal public în care apare fiecare release, ca un update ascuns să fie vizibil; build reproductibil = două calculatoare independente obțin exact același program din același cod. |
| Test santinelă | Test care pune valori-capcană (o parolă de test, un nume de cameră) și verifică automat că nu apar unde n-au voie: în cloud, loguri, telemetrie. |
| CRA, DNSC, ENISA | Regulamentul european de securitate pentru produse cu software (Cyber Resilience Act); DNSC și ENISA sunt autoritatea română și agenția europeană cărora le raportăm vulnerabilitățile. |
| GDPR, DPA | Regulamentul de protecție a datelor; DPA = contractul de prelucrare a datelor cu clienții business. |
| MVP, entitlements (drepturi) | MVP = prima versiune publică, cu funcțiile minime aprobate; drepturile = ce poate folosi un cont după plan (în MVP toți au planul gratuit). |
| Faza 0, spike, poartă | Teste scurte de risc înainte de construcția propriu-zisă; poarta e criteriul care decide dacă mergem pe planul A sau pe planul B. |
| Canal beta (intern), beta publică | Canalul beta = versiunile de test, care ies cel mult zilnic, după ce există infrastructura de semnare (Faza 0-bis, T44), spre hub-urile de test și testerii invitați („primul release pe canalul beta”); beta publică = lansarea MVP-ului (Faza 4), deschisă tuturor, după porțile G23–G25. |
| Telemetrie, jurnal de audit | Telemetria = date tehnice minime trimise automat (versiuni, coduri de eroare), fără imagini și nume de camere; jurnalul de audit = lista cu cine a făcut ce (a privit live, a redat, a exportat, a mișcat camera, a schimbat setări). |
| Telemetrie A / B | A = datele tehnice minime fără de care serviciul nu merge (versiune, prezență); B = date de fiabilitate pe care utilizatorul le poate opri (§12.2). |
| go2rtc, MediaMTX, Pion | Componente open-source (licență MIT) pe care construim hub-ul: video live, înregistrare, WebRTC. |

## Anexa B Surse

**Cercetarea proiectului** (în `docs/research/`; ordinea autorității e în §0):
- `docs/research/2026-10-17-technical-landscape.md`: sinteza (recomandarea Flutter e înlocuită de A3);
- `docs/research/2026-10-17-research-critique.md`: corecțiile adversariale (C1–C19, subiectele lipsă);
- `docs/research/2026-10-17-client-stack-judge-panel.md`: alegerea opțiunii B și măsurile obligatorii;
- `docs/research/2026-10-17-research-briefs.json`: cele 7 brief-uri de specialitate (video, camere, client, acces remote, stocare, înregistrare, securitate și legislație);
- `docs/brief.md`: cerințele confirmate.

**Surse externe cheie** (accesate pe 2026-10-17; „căutare” = verificat prin căutare web, pagina nu s-a putut deschide direct din sandbox):

| Subiect | URL |
|---|---|
| go2rtc releases / README | https://github.com/AlexxIT/go2rtc/releases · https://github.com/AlexxIT/go2rtc |
| MediaMTX: înregistrare și redare | https://github.com/bluenviron/mediamtx/blob/main/docs/2-features/09-record.md · …/10-playback.md |
| age (specificație C2SP, bibliotecă Go) | https://github.com/C2SP/C2SP/blob/main/age.md · https://github.com/FiloSottile/age |
| kerberos-io/onvif | https://github.com/kerberos-io/onvif |
| ONVIF oprit implicit pe Hikvision | https://legacy.hikvision.com/sites/default/files/tech_bulletin_-_enabling_onvif.pdf |
| Cloudflare Realtime TURN: prețuri și FAQ | https://developers.cloudflare.com/realtime/pricing/ (căutare) · https://developers.cloudflare.com/realtime/turn/faq/ |
| Android: permisiunea de rețea locală, target SDK, timeout FGS | https://developer.android.com/privacy-and-security/local-network-permission · https://developer.android.com/google/play/requirements/target-sdk · https://developer.android.com/develop/background-work/services/fgs/timeout |
| Verificarea dezvoltatorilor Android | https://developer.android.com/developer-verification · https://support.google.com/android-developer-console/answer/16561738 (căutare) |
| Apple: înscriere, App Review, DMA/UE, TN3179 | https://developer.apple.com/programs/enroll/ · https://developer.apple.com/app-store/review/guidelines/ · https://developer.apple.com/support/dma-and-apps-in-the-eu/ · https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy |
| Google Play: datele necesare pentru cont | https://support.google.com/googleplay/android-developer/answer/13628312 (căutare) |
| Google OAuth: cerințe de verificare | https://support.google.com/cloud/answer/13464321 (căutare) |
| Limitele Drive API / documentația rclone pentru Drive | https://developers.google.com/workspace/drive/api/guides/limits (căutare) · https://rclone.org/drive/ |
| Microsoft Graph: permisiuni; verificarea de publisher; Conditional Access pe fluxuri de autentificare | https://learn.microsoft.com/graph/permissions-reference · https://learn.microsoft.com/entra/identity-platform/publisher-verification-overview (căutare) · https://learn.microsoft.com/entra/identity/conditional-access/policy-block-authentication-flows (căutare) |
| Microsoft Artifact Signing: prețuri și FAQ | https://azure.microsoft.com/pricing/details/artifact-signing/ (căutare; suma nu apare) · https://learn.microsoft.com/azure/artifact-signing/faq (căutare) |
| TUF, Sigstore Rekor, Public Suffix List | https://theupdateframework.io · https://docs.sigstore.dev · https://publicsuffix.org |
| Prețurile runner-elor GitHub Actions | https://docs.github.com/billing/reference/actions-runner-pricing (căutare) |
| Firebase Test Lab: prețuri | https://firebase.google.com/docs/test-lab/usage-quotas-pricing (căutare) |
| Hetzner: modificările de preț din 2026 | https://docs.hetzner.cloud/whats-new (căutare) |
| CRA: calendar; descrierile produselor importante (Reg. 2025/2392) | https://raw.githubusercontent.com/orcwg/cra-hub/main/faq/cra-itself/timeline.md · https://eur-lex.europa.eu/eli/reg/2024/2847/oj · https://eur-lex.europa.eu/eli/reg_impl/2025/2392/oj (căutare) |
| Directiva privind răspunderea pentru produse | https://eur-lex.europa.eu/eli/dir/2024/2853/oj |
| WICG Local Network Access | https://github.com/WICG/local-network-access |
| Docker pe Raspberry Pi OS 32-bit (v28 e ultima versiune majoră; accesat pe 2026-10-18) | https://docs.docker.com/engine/install/raspberry-pi-os/ (căutare) |
| Taxe EUIPO | https://www.euipo.europa.eu/en/trade-marks/before-applying/fees-payable-direct-filing (căutare) |
