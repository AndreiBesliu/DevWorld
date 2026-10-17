# Brief: aplicație pentru camere IP

Confirmat de proprietar pe 2026-10-17.

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
4. **Stocare în MVP:** local (disk sau NAS), Google Drive, OneDrive. Dropbox, S3 și alții vin după. Dacă internetul cade, hub-ul păstrează fișierele local și le urcă la revenire.
5. **Acces de oriunde**, pe o conexiune criptată. Când clientul e în aceeași rețea cu hub-ul, conexiunea merge direct.
6. **Hub-ul rulează în MVP pe:** Windows și Docker/Linux (NAS, Raspberry Pi). Hub pe Android (telefon vechi, TV box) vine mai târziu.
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

## Criterii de succes

- Un utilizator non-tehnic instalează hub-ul și își vede camerele live în **mai puțin de 10 minute**.
- Live view cu întârziere **sub 1 secundă** în aceeași rețea și **sub 3 secunde** de la distanță.
- Înregistrările ajung sigur în Drive sau OneDrive, **fără goluri** la căderi de internet.

## Încă deschise

- **Numele aplicației:** îl alege proprietarul. Am verificat și respins 32 de variante.
- **Monetizarea:** după MVP.
- **Mărcile camerelor proprietarului:** le va afla detectorul.
