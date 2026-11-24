# Rezultatele Fazei 0 (c2): proba de încărcare și conturi

Raport din 2026-11-24, generat de `uploadreport` din rezultatele probei (PH0-30). Fiecare poartă e judecată o singură dată, de regula ei din probă. Raportul arată verdictele, motivele lor și cifrele din spatele lor. Gazdele apar doar cu pseudonime: `pc` (PC-ul dvs. cu Windows), `pi` (Raspberry Pi-ul dvs.), `lab-linux` și `lab-windows` (runner-ele CI). Telefonul dvs. (Galaxy A54, AND-1) apare doar în perechile MI6. Camerele apar ca `real-1`, `real-2`…, conturile ca `acct-1`, `acct-2`. Raportul se scrie doar dacă nu conține nimic de ascuns (PH0-22). Acesta e raportul probei c2; tabelul Fazei 0 îl scrie ultimul plan al fazei (G34).

## Rezumat pentru proprietar

- **G14: Încărcarea în timpul unei pene de internet (MI5):** trece.
- **G15: Conectarea hub-ului la cloud, partea din Faza 0 (MI6):** trece.
- **G17: Evenimentele ONVIF ale camerelor (MI8):** trece.
- **OneDrive personal:** nu avem încă răspunsul verificării A5. OneDrive personal trece în Faza 0-bis (C2-R4).
- **Camerele pentru MI8 (D3):** toate camerele cu evenimente ONVIF (D3-A).

Motivele fiecărui verdict sunt la „Verdicte și decizii declanșate”. Cifrele sunt la secțiunile MI5, MI6 și MI8.

## Ce a citit raportul

- Rezultate citite: 5 plicuri, din 2 foldere și 1 rulare CI.
- Rezultatele aplicației: 1 (android-sm-a546b-20261118-101500-oauth.json). Ele nu sunt plicuri și nu intră în verdicte.

## Verdicte și decizii declanșate (PH0-30)

| Poartă | Verdict |
| --- | --- |
| G14: Încărcarea în timpul unei pene de internet (MI5) | trece |
| G15: Conectarea hub-ului la cloud, partea din Faza 0 (MI6) | trece |
| G17: Evenimentele ONVIF ale camerelor (MI8) | trece |

### G14: Încărcarea în timpul unei pene de internet (MI5)

- **Verdict:** trece
- **Decizie declanșată:** Nicio decizie: outbox-ul și încărcarea rămân cele din §9.7–§9.9.
- **Ce se schimbă în §2 sau în cerințe:** Nimic în §2 sau în cerințe.

### G15: Conectarea hub-ului la cloud, partea din Faza 0 (MI6)

- **Verdict:** trece
- **Decizie declanșată:** Nicio decizie: autentificarea rămâne cea din §3.3 și §8.7.
- **Ce se schimbă în §2 sau în cerințe:** Nimic în §2 sau în cerințe.

### G17: Evenimentele ONVIF ale camerelor (MI8)

- **Verdict:** trece
- **Decizie declanșată:** Nicio decizie: evenimentele vin prin ONVIF PullPoint (HUB-11).
- **Ce se schimbă în §2 sau în cerințe:** Nimic în §2 sau în cerințe.

## Rezultate MI5 (G14)

Hub-ul înregistrează și urcă, cu internetul tăiat doar lui timp de 6 ore. După deblocare, hub-ul e repornit în mijlocul unei încărcări.

### Pe SSD sau HDD

| Cod | Valoare | Prag | Trece | Sursă |
| --- | --- | --- | --- | --- |
| MI5 | dst-1: 288/288 verificate, 0 lipsă, 0 duplicate, 0 hash-uri greșite | 0 lipsă, 0 duplicate, fiecare hash verificat; citirea segmentului deschis reușită | ✓ | MI5/pc/mi5-20261120t080000z |
| MI5 | dst-od: amânat în Faza 0-bis: înregistrarea Microsoft cere un director plătit (A5) | 0 lipsă, 0 duplicate, fiecare hash verificat; citirea segmentului deschis reușită | – | MI5/pc/mi5-20261120t080000z |

### Pe card SD (P17)

Rezultatele de pe card SD stau separat de celelalte. Rulările: `MI5/pi/mi5-20261119t080000z`. Ați confirmat avertismentul cardului SD: «DA», la 2026-11-20 08:00 UTC.

| Cod | Valoare | Prag | Trece | Sursă |
| --- | --- | --- | --- | --- |
| MI5 | dst-1: 144/144 verificate, 0 lipsă, 0 duplicate, 0 hash-uri greșite | 0 lipsă, 0 duplicate, fiecare hash verificat; citirea segmentului deschis reușită | ✓ | MI5/pi/mi5-20261119t080000z |
| MI5 | dst-od: amânat în Faza 0-bis: înregistrarea Microsoft cere un director plătit (A5) | 0 lipsă, 0 duplicate, fiecare hash verificat; citirea segmentului deschis reușită | – | MI5/pi/mi5-20261119t080000z |

### `pc`, rularea `mi5-20261120t080000z` (SSD, 4 camere)

- Blocarea: 6 h. Verificări ale blocării: 1; furnizorii blocați la toate: da; LAN și loopback au răspuns: da.
- Numele serviciilor s-au rezolvat în timpul blocării (DNS): da.
- Recuperarea: 1260 s după deblocare, la 18.5 Mbps.
- Repornirea: plasarea în curs avea 32 MiB; ultimul offset confirmat 16 MiB; reluată de la 16 MiB. Segmentul rupt de repornire a fost urcat: da.
- Citirea segmentului deschis (criptat): 10/10 reușite, mediana 42 ms, maxim 120 ms.
- Citirea segmentului deschis (necriptat): 20/20 reușite, mediana 30 ms, maxim 90 ms; 1 citire repetată după 5 s.

Apelurile pe segment verificat, pe metodă. Unitățile sunt calculate doar pentru metodele cu preț în §9.2 (get 5, list 100, download 200); celelalte vin din consolă.

| Destinație | Metodă | Pe segment |
| --- | --- | --- |
| dst-1 | files.create.resumable | 1 apel, unități: din consolă |
| dst-1 | files.get | 2 apeluri, 10 unități |
| dst-1 | upload.put | 3 apeluri, unități: din consolă |

### `pi`, rularea `mi5-20261119t080000z` (card SD, 2 camere)

- Blocarea: 6 h. Verificări ale blocării: 1; furnizorii blocați la toate: da; LAN și loopback au răspuns: da.
- Numele serviciilor s-au rezolvat în timpul blocării (DNS): da.
- Recuperarea: 1260 s după deblocare, la 18.5 Mbps.
- Repornirea: plasarea în curs avea 32 MiB; ultimul offset confirmat 16 MiB; reluată de la 16 MiB. Segmentul rupt de repornire a fost urcat: da.
- Citirea segmentului deschis (criptat): 10/10 reușite, mediana 42 ms, maxim 120 ms.
- Citirea segmentului deschis (necriptat): 20/20 reușite, mediana 30 ms, maxim 90 ms; 1 citire repetată după 5 s.

Apelurile pe segment verificat, pe metodă. Unitățile sunt calculate doar pentru metodele cu preț în §9.2 (get 5, list 100, download 200); celelalte vin din consolă.

| Destinație | Metodă | Pe segment |
| --- | --- | --- |
| dst-1 | files.create.resumable | 1 apel, unități: din consolă |
| dst-1 | files.get | 2 apeluri, 10 unități |
| dst-1 | upload.put | 3 apeluri, unități: din consolă |

### Unitățile de cotă Drive din consolă (P18)

Din consola Google Cloud, citit de pe captura dvs.: 812 unități pe segment.

### Laborator: TST-07, varianta de 15 minute (P09)

Rulările din laborator, pe serviciile simulate. Ele nu intră în G14.

| Cod | Valoare | Prag | Trece | Sursă |
| --- | --- | --- | --- | --- |
| TST-07 | dst-1: 16/16 verificate, 0 lipsă, 0 duplicate, 0 hash-uri greșite; recuperare 95 s | fiecare segment verificat; 0 lipsă, 0 duplicate (15 min, servicii simulate) | ✓ | MI5/lab-linux/mi5-20261119t070000z |

## Rezultate MI6 (G15)

Hub-ul primește codul de autentificare de pe fiecare platformă și își reîmprospătează accesul zilnic. Proiectul Google e în modul Testing, deci accesul expiră în ziua 7: acea zi nu se judecă.

### Rândurile judecății

| Cod | Valoare | Prag | Trece | Sursă |
| --- | --- | --- | --- | --- |
| MI6 | Android × Google Drive: plan A, zilele 1–6 ok | plan A, sau B1 după un eșec al planului A; zilele 1–6 ok | ✓ | pc |
| MI6 | Windows × Google Drive: plan A, zilele 1–6 ok | plan A, sau B1 după un eșec al planului A; zilele 1–6 ok | ✓ | pc |
| MI6 | web × Google Drive: plan B1, zilele 1–6 ok | plan A, sau B1 după un eșec al planului A; zilele 1–6 ok | ✓ | pc |
| MI6 | Android × OneDrive personal: amânat în Faza 0-bis: înregistrarea Microsoft cere un director plătit (A5) | plan A, sau B1 după un eșec al planului A; zilele 1–6 ok | – | pc |
| MI6 | Windows × OneDrive personal: amânat în Faza 0-bis: înregistrarea Microsoft cere un director plătit (A5) | plan A, sau B1 după un eșec al planului A; zilele 1–6 ok | – | pc |
| MI6 | web × OneDrive personal: amânat în Faza 0-bis: înregistrarea Microsoft cere un director plătit (A5) | plan A, sau B1 după un eșec al planului A; zilele 1–6 ok | – | pc |
| MI6 | vizibilitate: 9 din 9 celule măsurate, 0 eșuate | 9 celule judecate, fiecare listată și citită | ✓ | pc |

### Perechile

| Pereche | Plan | Zilele 1–6 | Ziua 7 | Încercări fără răspuns | Sursă |
| --- | --- | --- | --- | --- | --- |
| Android × Google Drive | A | toate ok | expirare așteptată | 0 | MI6/pc/mi6-20261113t090000z |
| Windows × Google Drive | A | toate ok | expirare așteptată | 0 | MI6/pc/mi6-20261113t090000z |
| web × Google Drive | B1 | toate ok | expirare așteptată | 1 | MI6/pc/mi6-20261113t090000z |

### Vizibilitatea între clienți (3 × 4)

Un fișier creat de fiecare tip de client al hub-ului (rânduri), listat și citit de fiecare cititor (coloane). Celulele aceluiași client nu se judecă.

| Creator \ Cititor | web | tv | android | desktop |
| --- | --- | --- | --- | --- |
| android | ✓ | ✓ | același client | ✓ |
| desktop | ✓ | ✓ | ✓ | același client |
| tv | ✓ | același client | ✓ | ✓ |

Autentificări predate hub-ului, pe cont și client: acct-1 × android: 1; acct-1 × desktop: 1; acct-1 × tv: 1.

## Rezultate MI8 (G17)

Hub-ul ascultă evenimentele de mișcare ale camerelor dvs. Proba le compară cu propria ei referință de mișcare, o diferență de cadre.

### Rândurile judecății

| Cod | Valoare | Prag | Trece | Sursă |
| --- | --- | --- | --- | --- |
| MI8 | real-1 (Hikvision DS-2CD2143G2-I): 24/24 episoade (100.0 %), 0 pierderi tăcute, L = 600 ms, substream | ≥ 95 % din episoade cu eveniment în 5 s (≥ 20 episoade); 0 pierderi tăcute; 72 h | ✓ | MI8/pc/mi8-20261121-060000 |
| MI8 | real-2 (Reolink RLC-510A): 21/22 episoade (95.5 %), 0 pierderi tăcute, L = 600 ms, substream | ≥ 95 % din episoade cu eveniment în 5 s (≥ 20 episoade); 0 pierderi tăcute; 72 h | ✓ | MI8/pc/mi8-20261121-060000 |

Ore observate: 73.0 h. Un episod al referinței se încheie după 10 s fără mișcare. Un episod are eveniment dacă o mișcare a camerei vine în cel mult 5 s de la începutul lui, ținând seama de întârzierea L a referinței.

### Pe marcă

#### Hikvision

| Cameră | Model | Episoade | Cu eveniment în 5 s | Acoperire | Pierderi tăcute | Referința | L | Sursă |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| real-1 | DS-2CD2143G2-I | 24 | 24 | 100.0 % | 0 | substream (FFmpeg) | 600 ms | MI8/pc/mi8-20261121-060000 |

#### Reolink

| Cameră | Model | Episoade | Cu eveniment în 5 s | Acoperire | Pierderi tăcute | Referința | L | Sursă |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| real-2 | RLC-510A | 22 | 21 | 95.5 % | 0 | substream (FFmpeg) | 600 ms | MI8/pc/mi8-20261121-060000 |

## Rămas pentru Faza 0-bis

Fiecare element pornește când aprobați bugetul pentru el. Până atunci costă 0 €. Fiecare are verdict înainte de faza care îl cere (§16.9).

| Nr. | Poartă, cost și termen |
| --- | --- |
| 1 | G14 – OneDrive business (pana de 6 h), cu tenantul MI7: Entra ID P1, OWN-09, înainte de Faza 3 |
| 2 | G14 și G15 – OneDrive personal: înregistrarea Microsoft cu date de plată, OWN-08, înainte de Faza 3 |
| 3 | G15 – combinațiile iOS: Apple Developer (OWN-05, 99 USD/an) și un iPhone împrumutat, înainte de Faza 3 |
| 4 | G15 – refresh-ul Drive din ziua 7 și de după, cu proiectul în Production: domeniul pentru verificarea brandului (OWN-04, OWN-07), înainte de Faza 3 |
| 5 | G15 – OneDrive business: tenantul de test (OWN-09), înainte de Faza 3 |
| 6 | G16 (MI7) – Conditional Access și `AppFolder` pe conturi de firmă: Entra ID P1 (OWN-09, ≈ 6–7 USD/lună), înainte de Faza 3 |
| 7 | ipcam-lab: TST-07 de 6 h pe conturi reale (PH3-02) și stabilitatea de 72 h, OWN-22 ≈ 15 €/lună, înainte de Faza 3 |
| 8 | redirect-ul OneDrive de pe `<domeniu>`, după domeniu (OWN-04), înainte de Faza 3 |
| 9 | STO-01 nightly pe conturi reale (§14.5): Drive după trecerea proiectului în Production (OWN-07, înainte de Faza 3); OneDrive personal e gratuit dacă înregistrarea e gratuită (OWN-08) și se programează după rotația săptămânală a secretelor (OWN-23, Faza 1); OneDrive business (OWN-09), B2, R2, Wasabi și AWS SSE-KMS: Faza 0-bis, cost, înainte de Faza 3 |

## Limitări

- Canalul probei dintre aplicație și hub e HTTP simplu, prin uploadspike: nu e criptat și nu e autentificat. Astfel, un dispozitiv din rețeaua de acasă ar putea citi adresa de autentificare și ar putea strecura un cod din alt cont, cu aceeași stare (`state`). Canalul criptat din T11 (lanapi cu HIK, T42) previne asta. Până în Faza 1, această protecție rămâne netestată (P40).
- Pe un Pi, MI5 înregistrează min(4, numărul recomandat pentru modelul lui) camere. Un Pi mai slab înregistrează deci mai puține camere decât cele 4 din specificație (P17, P38).
- O rulare din laborator e varianta de 15 minute a TST-07, pe serviciile simulate. Ea nu decide niciodată G14 (P09).
- O regulă de firewall poate lăsa deschisă o conexiune deja stabilită. De aceea blocarea se verifică și din evenimentele de încărcare ale hub-ului: nicio încărcare verificată în timpul blocării (P15).
- Clipurile pe evenimente (EVENT_CLIP) sunt simulate: camera `cam01` ține locul lor (P14).
- O copie verificată nu se înlocuiește niciodată. De aceea MI5 înregistrează necriptat (P03).
- În timpul blocării, numele serviciilor se rezolvau în continuare (DNS). Blocarea taie doar conexiunile (P15).
- Pe Windows, hub-ul sigilează secretele cu `dpapi-machine`. E mai slab decât cere specificația, până în Faza 1 (P19).
- Bucla de retenție din STO-13 se testează în Faza 1 (P26).
- Referința de mișcare e a probei, nu a camerei: o diferență de cadre (FFmpeg pe substream, sau instantanee ONVIF). Ea are propria întârziere L, scrisă pe fiecare cameră, de care ține seama potrivirea (P41).
- La o continuare MI8 (`--continue`), două camere de aceeași marcă și de același model nu se pot deosebi (Task 10).
- Unitățile de cotă Drive vin din consola Google Cloud, citite de pe captura dvs. Proba numără doar apelurile (P18).
- Outbox-ul numără în memorie cele 3 încărcări cu hash greșit permise unei plasări. Astfel, fiecare repornire a hub-ului mai permite unei plasări până la 2 încărcări greșite, și ștergerile lor (P34).

## Date brute

Câte un fișier CSV pe măsurătoare, lângă acest raport, cu o linie pe mostră (PH0-30):

- `phase0-data/phase0c-upload-mi5.csv`: o linie pe destinația fiecărei rulări MI5, și pentru laborator; coloanele: `host,os,set,runId,storage,lab,valid,blockSecs,recoverySecs,uplinkMbps,dest,kind,account,cameras,segments,segmentsDuringBlock,verified,missing,duplicates,hashMismatches,verifiedDuringBlock,failedDuringBlock,rateLimitedDuringBlock,unreachableDuringBlock,deferred,callsPerSegment`.
- `phase0-data/phase0c-upload-mi6.csv`: o linie pe ziua fiecărei perechi și una pe fiecare celulă de vizibilitate; coloanele: `host,set,runId,sample,provider,platform,dest,plan,day,attempts,outcome,creator,reader,listed,read,self`.
- `phase0-data/phase0c-upload-mi8.csv`: o linie pe fiecare episod al referinței de mișcare; coloanele: `host,os,set,runId,camera,brand,model,referenceMethod,refLagMs,startMs,endMs,matched`.
