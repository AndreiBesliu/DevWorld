# Brief: aplicație pentru camere IP

Confirmat de proprietar pe 2026-10-17. Actualizat cu răspunsurile lui din 2026-10-18, inclusiv cele la întrebările din ghidul PDF al Fazei 0.

## Ce vrea proprietarul

- Produs **comercial, public**, distribuit în App Store, Google Play, pe web și pe Windows.
- Un utilizator își leagă **mai multe camere IP**, vede live fiecare cameră, le **înregistrează** și le **controlează**.
- Stocarea înregistrărilor e la alegerea utilizatorului: **Google Drive, OneDrive, alți furnizori și local**.
- Înregistrarea 24/7 o face un **hub software** care rulează pe un dispozitiv pe care utilizatorul îl are deja (PC Windows, NAS/Docker, Raspberry Pi). Costurile de infrastructură ale proprietarului rămân mici, iar video-ul stă la utilizator.
- Dezvoltarea și întreținerea le face **AI-ul**. AI-ul alege stack-ul cel mai fiabil.
- **Monetizarea se decide mai târziu.** MVP-ul pornește gratuit, iar arhitectura lasă loc pentru abonamente fără rescrieri.

## Ipoteze confirmate

1. **Camere:** cele standard **RTSP/ONVIF** (Hikvision, Dahua, Reolink, Uniview, Tapo cu RTSP activat etc.). Camerele „doar cloud” (Xiaomi, Ring, Arlo, Eufy etc.) nu intră în MVP.
2. **Control în MVP:** PTZ (mișcare, zoom), presetări, pornire/oprire înregistrare. Setările avansate de imagine vin mai târziu.
3. **Înregistrare:** continuă și la mișcare (din evenimentele camerei), în bucăți de câteva minute, cu retenție configurabilă. Detecția AI de persoane și mașini vine mai târziu.
4. **Stocare în MVP:** local (disc sau NAS prin SMB), Google Drive, OneDrive (personal și pentru firme), S3-compatibil (AWS, Backblaze B2, Wasabi, Cloudflare R2, servere proprii), WebDAV/Nextcloud și SFTP. Dropbox, Box, pCloud și alții vin după. Dacă internetul cade, hub-ul păstrează fișierele local și le urcă la revenire. (Lista a fost extinsă de proprietar mai târziu în aceeași conversație.)
5. **Acces de oriunde**, pe o conexiune criptată. Când clientul e în aceeași rețea cu hub-ul, conexiunea merge direct.
6. **Hub-ul rulează în MVP pe:** Windows și Docker/Linux (NAS, Raspberry Pi; pentru Raspberry Pi, vezi decizia 5 din 2026-10-18). Hub pe Android (telefon vechi, TV box) vine mai târziu.
7. **Piața:** România și UE la început, cu interfață în română și engleză și conformitate GDPR.
8. **În MVP:** un singur proprietar pe hub. Partajarea cu familia sau angajații și notificările push la mișcare vin în versiunea următoare.

## Decizii luate în design (2026-10-17)

- **Arhitectura:** hub în Go, lângă camere; cloud subțire în Go + PostgreSQL, găzduit în UE; aplicațiile client într-un singur cod TypeScript (web, plus Capacitor pentru Android și iOS și Tauri pentru Windows). Video-ul nu trece prin serverele proprietarului.
- **Stocare configurabilă până la cel mai mic detaliu, inclusiv hibridă.** Utilizatorul adaugă oricâte destinații și definește reguli: ce camere, ce conținut (24/7, evenimente, miniaturi, exporturi), ce calitate, în ce program și unde merge (una sau mai multe destinații). Fiecare destinație are retenția ei și ce se întâmplă când se umple. Utilizatorul poate limita banda și alege ferestrele orare de upload. Pentru începători există șabloane.
- **Criptarea o alege utilizatorul.** Aplicația întreabă explicit când adaugă o destinație. Alegerea se poate face și pe regulă: per cameră, tip de conținut, tip de eveniment sau program. Dacă două reguli se contrazic, criptarea câștigă. Funcționează și pe discul local, iar fiecare înregistrare criptată are un lacăt în aplicație.
- **Patru căi de recuperare a cheii:** hub-ul, dispozitivele logate, backup în cont criptat cu parola și cheia de recuperare.
- **Redare fără hub:** dacă hub-ul e furat, înregistrările se pot reda direct din cloud.
- **Detectorul de camere** e primul lucru construit, în Faza 0. Proprietarul are camere IP de diferite modele pentru teste.
- **Google Drive pentru teste:** proprietarul și-a oferit Drive-ul personal. Pentru testele automate recomandăm conturi de test separate.

## Decizii din 2026-10-18

1. **Repo-ul rămâne public** deocamdată. Ce publicăm din teste (fixture-uri, rapoarte, date) e redactat, adică fără parole, IP-uri, serii sau nume, și aprobat de proprietar.
2. **Dispozitivele proprietarului:** un PC desktop cu Windows 11 (la testul de înregistrare, oprirea de curent se face scoțând ștecherul din priză), un Raspberry Pi (modelul îl află hub-ul), un Samsung Galaxy A24 și internet acasă de la DIGI. Nu are iPhone; unul se poate împrumuta mai târziu. În Faza 0, testele folosesc doar aceste dispozitive; nu cumpărăm nimic.
3. **„Lasă tot ce ține de iOS sau ce presupune costuri pentru mai târziu.”** Faza 0 se face fără costuri și fără iOS. Tot ce costă bani, chiar și sume mici (domeniu, server, taxe Apple și Google Play, testare pe telefoane închiriate, semnarea codului, chei YubiKey, servicii cloud plătite, o priză inteligentă, telefoane de test, un mini PC, avocat, asigurare, pentest, marcă), și tot ce ține de iOS trec într-un bloc separat, „Faza 0-bis”. Acesta pornește doar după ce proprietarul aprobă un buget; până atunci bugetul e 0 €. Fiecare element se face înaintea fazei care are nevoie de el, iar testul pe iPhone cel târziu înainte de Faza 4.
4. **SRL-ul se face mai târziu.** Începem cu munca ce nu cere firmă.
5. **Raspberry Pi:** hub-ul merge pe orice Raspberry Pi care rulează Linux pe 64 sau 32 de biți (Pi 2 v1.2, Pi 3, 3B+, 4, 5, Zero 2 W, Pi 400/500, Compute Module 3/4/5). Aplicația arată câte camere recomandăm pentru fiecare model (Pi 5 ≈ 8, Pi 4 ≈ 4, Pi 3, 3B+, Zero 2 W și Pi 2 v1.2 ≈ 1–2, de confirmat pe Pi-ul proprietarului); peste acest număr apare doar un avertisment, niciodată un refuz. Pi 1, Zero și Zero W nu sunt suportate, fiindcă un singur nucleu și 512 MB nu pot înregistra și transmite fiabil, iar aplicația explică asta. Înregistrarea pe card SD e permisă, cu un avertisment clar despre uzură, o confirmare, recomandarea unui SSD și supravegherea cardului, cu alerte la erori.
6. **Planurile testelor din Faza 0 (c) sunt aprobate:** testul de înregistrare (c1) și testul de urcare în cloud (c2), în forma din `docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md` și `docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md`. Proprietarul a scris „da aprob” în firul de proiect „Răspunsuri la întrebările din PDF”. Se execută după testul video (Faza 0 b).

## Criterii de succes

- Un utilizator non-tehnic instalează hub-ul și își vede camerele live în **mai puțin de 10 minute**.
- Live view cu întârziere **sub 1 secundă** în aceeași rețea și **sub 3 secunde** de la distanță.
- Înregistrările ajung sigur în Drive sau OneDrive, **fără goluri** la căderi de internet.

## Încă deschise

- **Numele aplicației:** îl alege proprietarul. Am verificat și respins 32 de variante.
- **Monetizarea:** după MVP.
- **Mărcile camerelor proprietarului:** le va afla detectorul.
- **Modelul Raspberry Pi-ului proprietarului:** îl va citi hub-ul.
- **Bugetul și momentul Fazei 0-bis** (iOS și costuri): 0 € până decide proprietarul; testul pe iPhone cel târziu înainte de Faza 4.
