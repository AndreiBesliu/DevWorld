# Furniture Pro: arhitectură și plan de dezvoltare

**Stare:** APROBAT de proprietar pe 2026-11-01, inclusiv Rust pentru nucleul de calcul, ordinea etapelor din secțiunea 8 și România ca piață de start. Codul de produs începe doar după aprobarea planului de implementare al fiecărei etape.

**Data:** 2026-11-01
**Cerințe:** [`../../brief.md`](../../brief.md)

---

## 1. Ce am înțeles

Cerințele complete sunt în brief. Pe scurt:

- Aplicație Windows comercială, vândută altor ateliere și fabrici de mobilă.
- Fluxul complet: proiectare → piese → canturi → feronerie → debitare optimizată → 3D → costuri și preț → documente PDF/Excel.
- Reguli de calcul configurabile, verificabile și exacte, fără valori inventate.
- Modelul 3D și calculele folosesc aceeași sursă de date.
- Aplicația funcționează local; internetul servește pentru licențe, actualizări, sincronizare și AI.
- Dezvoltare pe etape, cu verificare după fiecare pas.

Ipotezele încă neconfirmate sunt listate în brief, la „Ipoteze”.

---

## 2. Cerințe care lipsesc sau trebuie precizate

| Subiect | De ce contează | Propunere |
|---|---|---|
| **Dimensiune finită vs. dimensiune de debitare** | O piesă cu cant de 2 mm se taie mai mică decât dimensiunea finală. Fiecare atelier aplică reguli proprii. | Regulă configurabilă pentru fiecare mașină de cantuit (grosime cant, frezare prealabilă) |
| **Etichete pe piese** (cod de bare/QR) | Piesele trebuie identificate în atelier | Din prima versiune |
| **Prelucrări** (găuri pentru balamale, găuri pentru suporturile polițelor, confirmat, nuturi) | Necesare pentru documentația de atelier și pentru CNC | Modelate de la început; exportul CNC vine mai târziu |
| **Tipul mașinii de debitat** | Fierăstrăul de panouri taie doar „de la cap la cap” (ghilotină); CNC-ul poate așeza piesele liber | Optimizarea le suportă pe amândouă; tipul se alege pentru fiecare mașină |
| **Desene tehnice 2D cu cote** | Atelierul lucrează după desene; clientul semnează pe desen | Vederi frontale cu cote, din prima versiune |
| **Încăperea** (pereți, colțuri, ferestre, țevi) | Bucătăriile se proiectează pe spațiul real | Înainte de versiunea 1.0 |
| **Mai multe comenzi debitate împreună** | Economisește mult material | Din prima versiune |
| **Stoc de plăci și resturi** | Resturile reutilizabile cer o evidență | Evidență simplă în versiunea 1.0 |
| **Fronturi comandate la terți** (ex. MDF vopsit, plătit la m²) | Nu intră la debitare, ci într-o comandă către furnizor | Listă separată de comandă |
| **Versiuni de ofertă și „înghețarea” ofertei trimise** | Prețurile se schimbă, dar o ofertă trimisă nu are voie să se modifice | Din prima versiune |
| **Import listă de piese din Excel** | Modulul de debitare poate fi folosit și fără proiectare | Din prima versiune |
| **Modificări manuale ale unei piese** | Exemplu: „lateralul acesta mai scurt cu 20 mm, din cauza țevii” | Permise, dar marcate vizibil în toate listele |
| **Numele „Furniture Pro”** | Este generic și poate fi deja înregistrat de altcineva | Verificare de marcă la OSIM/EUIPO înainte de marketing |

---

## 3. Arhitectura

### 3.1 Cele 10 principii de bază

1. **O singură sursă de adevăr.** Proiectul salvează doar ce a decis utilizatorul, adică „rețeta”: corpurile, dimensiunile, materialele și opțiunile. Piesele, canturile, feroneria, 3D-ul, debitarea și prețurile se calculează din rețetă, ca într-un tabel Excel.
2. **Ce vezi în 3D este exact ce se debitează.** 3D-ul afișează piesele calculate, cu dimensiunile lor reale. Imaginea și lista nu pot diferi.
3. **Calculele se fac într-un singur loc, în nucleul de calcul.** Interfața doar afișează rezultate și transmite comenzi, de exemplu „lățimea corpului 3 devine 650 mm”.
4. **Regulile sunt date, nu cod.** Sistemele constructive, rosturile, formulele pieselor și regulile de feronerie se păstrează în baza de date. Se pot edita din aplicație și au versiuni.
5. **Fiecare cotă are o explicație.** Exemplu: *Lățime fund = 600 − 2 × 18 = 564 mm (regula „Fund între laterale”, versiunea 3)*.
6. **Precizie exactă.** Cotele se țin intern ca numere întregi de sutimi de milimetru (564 mm = 56.400). Banii se calculează cu zecimale exacte. Rotunjirile sunt reguli explicite și configurabile.
7. **O dată lipsă produce o eroare vizibilă, niciodată zero.** Dacă un material nu are preț, aplicația spune exact ce lipsește. Aplicația nu vine cu prețuri sau date inventate.
8. **Documentele emise se „îngheață”.** O ofertă trimisă sau o comandă intrată în producție își păstrează piesele, planurile și prețurile din acel moment.
9. **Modulele au „contracte” clare.** Fiecare modul primește și returnează date într-un format fix, documentat și cu versiune.
10. **Aplicația e pregătită pentru mai mulți utilizatori.** Folosim de la început identificatori unici universali, istoric al modificărilor și un strat separat pentru accesul la date.

### 3.2 Schema generală

```
 INTERFAȚA  (React + TypeScript + Three.js)
   Editor 3D · Liste · Planuri de debitare · Deviz/ofertă · Cataloage
        │  doar afișează rezultate și trimite comenzi
        ▼
 NUCLEUL DE CALCUL  (Rust) — singurul loc unde se calculează
   Motor parametric → Piese → Canturi → Feronerie → Debitare → Costuri → Documente
        │
        ▼
 DATE LOCALE  (SQLite — un singur fișier pe calculator)
   Cataloage · Reguli · Clienți · Proiecte · Oferte · Stoc

        ⇅  internet (opțional)
 SERVER FURNITURE PRO: licențe · actualizări · (mai târziu) sincronizare, AI
```

### 3.3 Tehnologii

| Componentă | Propunerea proprietarului | Recomandarea AI |
|---|---|---|
| Cadrul aplicației Windows | Tauri 2 | **Păstrăm.** Aplicație de câteva ori mai mică decât alternativele, folosește motorul de afișare inclus în Windows (WebView2), actualizări semnate digital. |
| Interfața | React + TypeScript | **Păstrăm.** Cel mai răspândit ecosistem. |
| 3D | Three.js | **Păstrăm**, prin React Three Fiber. |
| Nucleul de calcul | Python | **Rust** (secțiunea 3.4). |
| Baza de date | — | **SQLite**: un singur fișier, fără server, foarte fiabil. |
| PDF și Excel | — | Generate de nucleu din șabloane personalizabile (logo, antet). Exemple de unelte: Typst pentru PDF, rust_xlsxwriter pentru Excel. |

**Alternative respinse:**
- **Totul în TypeScript:** calculele ar fi ușor de copiat, iar optimizarea mai lentă.
- **C#/.NET:** un 3D modern se obține mai greu, iar o versiune web ar cere rescrierea interfeței.
- **Electron în loc de Tauri:** aplicație de câteva ori mai mare, care consumă mai multă memorie.

**Cerințe de sistem:** Windows 10 sau 11. Pentru 3D ajunge o placă video integrată modernă.

### 3.4 Rust sau Python pentru nucleul de calcul

Amândouă pot face corect orice calcul de producție. Diferențele sunt trei.

**1. Cum ajunge programul la client.**
- Python se traduce rând cu rând de fiecare dată când rulează. Odată cu aplicația trebuie livrat și „traducătorul”: un al doilea program, ascuns, de zeci de MB.
- Rust se traduce o singură dată, la noi, în limba procesorului. Clientul primește un singur program.

Consecințe:
- **Antivirus:** pachetele cu Python sunt des semnalate fals ca viruși.
- **Copiere:** codul Python livrat se recuperează ușor, în timp ce codul mașină produs de Rust se copiază greu.
- **Viteză:** Rust este de zeci de ori mai rapid la optimizarea debitării, deci produce planuri mai bune în același timp.

**2. Câte greșeli sunt prinse înainte de livrare.**

```python
# Python
def latime_fund(latime_corp, grosime_lateral):
    return latime_corp - 2 * grosime_lateral
```

```rust
// Rust
fn latime_fund(latime_corp: Mm, grosime_lateral: Mm) -> Mm {
    latime_corp - grosime_lateral * 2
}
```

- **Rust** refuză să construiască programul dacă, din greșeală, primește lei sau bucăți în loc de milimetri.
- **Python** rulează și calculează o valoare fără sens. Greșeala se vede abia când acea parte rulează, poate chiar la client.

Proprietarul nu verifică direct codul, așa că un limbaj care respinge automat categorii întregi de greșeli este un controlor permanent, alături de testele automate.

**3. Câte „poduri” are aplicația.**

```
Cu Python:  Interfață (TypeScript) ⇄ Tauri (Rust) ⇄ program Python separat → 3 limbaje, 2 poduri
Cu Rust:    Interfață (TypeScript) ⇄ Tauri + nucleul de calcul (Rust)     → 2 limbaje, 1 pod
```

**Unde e Python mai bun:**
- e mai ușor de învățat;
- se găsesc mai mulți programatori, mai ieftini;
- este cel mai potrivit limbaj pentru AI.

Rust este mai dificil, iar dezvoltarea începe puțin mai încet.

| Criteriu | Python | Rust |
|---|---|---|
| Calcule exacte | ✅ posibil | ✅ posibil |
| Viteza la optimizarea debitării | lentă | de zeci de ori mai rapidă |
| Instalarea la client | program + „traducător” ascuns | un singur program |
| Alerte false de virus | frecvente | rare, mai ales cu semnătură digitală |
| Protecție împotriva copierii | slabă | bună |
| Greșeli prinse înainte de livrare | puține (verificări opționale) | multe (verificări obligatorii) |
| Ușor de învățat / de angajat oameni | ✅ / ✅ | ❌ / mai greu și mai scump |
| Inteligență artificială | ✅ cel mai bun | limitat |

**Recomandare:** Rust pentru tot ce se instalează la client. Funcțiile AI de mai târziu rulează pe un server online, în Python.

### 3.5 Ce merge fără internet și ce nu

- **Fără internet:** proiectarea, calculele, debitarea, ofertele și documentele.
- **Cu internet:**
  - activarea licenței, o singură dată, urmată de verificări periodice; între verificări aplicația merge offline o perioadă configurabilă;
  - actualizările;
  - mai târziu, sincronizarea și AI-ul.
- **AI-ul doar propune.** Dimensiunile de producție le calculează întotdeauna nucleul, iar utilizatorul le validează.

### 3.6 Drumul spre versiunea cu mai mulți utilizatori

- **Acum:** o bază de date locală pe fiecare calculator, plus export și import de proiecte ca fișiere (de exemplu `.fpro`).
- **Mai târziu:** un server central la care se sincronizează calculatoarele. Un proiect este „blocat” cât timp îl editează cineva.

---

## 4. Modulele aplicației

| # | Modul | Ce face | Depinde de |
|---|---|---|---|
| M0 | Fundația | Unități, precizie, bani, limbajul de formule, erori, traduceri | — |
| M1 | Date și stocare | Baza de date, actualizarea structurii între versiuni, backup automat, import/export | M0 |
| M2 | Catalog materiale | Plăci, decoruri, formate, canturi, blaturi, furnizori, prețuri cu istoric | M1 |
| M3 | Catalog feronerie | Articole, seturi, producători, furnizori, prețuri, bucăți per ambalaj | M1 |
| M4 | Reguli de construcție | Sisteme constructive, rosturi, șabloane de corpuri, formulele pieselor | M0, M2 |
| M5 | **Motorul parametric** | Transformă rețeta proiectului în piese exacte | M2, M4 |
| M6 | Canturi și dimensiuni de debitare | Cantul de pe fiecare muchie, dimensiunea de debitare, consumul de cant | M5 |
| M7 | Reguli de feronerie | Feroneria calculată automat și efectul ei asupra pieselor | M3, M5 |
| M8 | Stoc plăci și resturi | Evidența plăcilor întregi și a resturilor | M2 |
| M9 | **Optimizarea debitării** | Planuri de debitare, ordinea tăieturilor, resturi, verificare | M6, M8 |
| M10 | Costuri și prețuri | Materiale, canturi, feronerie, manoperă, adaos, discount, TVA | M6, M7, M9 |
| M11 | Documente și exporturi | PDF, Excel, etichete, desene 2D; mai târziu CNC | M5–M10 |
| M12 | Clienți, proiecte, oferte | Clienți, proiecte, versiuni, oferte, comenzi de producție | M1 |
| M13 | Interfața și editorul 3D | Tot ce vede și folosește utilizatorul | doar afișează și trimite comenzi |
| M14 | Licențe și actualizări | Activare, verificare, actualizare automată | independent |
| Viitor | CNC, mai mulți utilizatori, AI | — | — |

### 4.1 Motorul parametric (M5)

- Un corp este un **șablon** (ex. „Corp bază cu 2 uși”) plus **parametri**: lățime, înălțime, adâncime, materiale, sistem constructiv.
- Fiecare piesă din șablon are:
  - rolul ei (lateral stânga, fund, capac…);
  - materialul;
  - formulele pentru lungime și lățime;
  - cantitatea;
  - o condiție (ex. „doar dacă are polițe”);
  - cantul pe fiecare dintre cele 4 muchii;
  - orientarea fibrei.
- Formulele folosesc un limbaj simplu și sigur, nu cod de programare. Exemplu: `Lungime = H − grosime_capac − grosime_fund`.
- Variantele constructive sunt configurabile:
  - capacul și fundul se montează între laterale sau peste ele;
  - spatele poate fi din HDF în nut, HDF aplicat sau PAL;
  - ușile pot fi aplicate, semiaplicate sau încastrate;
  - rosturile și retragerile se setează.
- Fiecare piesă are un identificator stabil, de exemplu „Corp 3 / Lateral stânga”. Astfel, modificările manuale rămân pe piesa corectă după recalculare.
- **Șabloanele au versiuni.** Proiectele vechi nu se schimbă pe ascuns. Le actualizezi doar printr-o acțiune explicită.
- **Proiectarea liberă** folosește panouri și grupuri plasate liber, cu aceleași reguli de canturi, feronerie și debitare.

### 4.2 Canturi (M6)

- Dimensiunea de debitare = dimensiunea finită − grosimile canturilor (+ adaosul pentru frezarea prealabilă, dacă mașina de cantuit o face). Regula exactă se stabilește cu proprietarul și se configurează pentru fiecare mașină.
- Consumul de cant = lungimea muchiilor + adaosul de la capete, grupat pe tip, grosime și decor.

### 4.3 Feronerie (M7)

- O regulă are trei părți: **când** se aplică (condiția), **ce** se folosește (articolul sau criteriile de alegere) și **cât** (formula de cantitate).
- Feroneria poate modifica piese. De exemplu, un sistem de sertare impune dimensiunile fundului și ale spatelui sertarului, iar o balama impune gaura pentru cupă.
- Formulele se introduc din **cataloagele oficiale** ale producătorilor, nu din memorie.

### 4.4 Optimizarea debitării (M9)

- **Intrări:**
  - piesele: dimensiunea de debitare, materialul, cantitatea, sensul fibrei, dacă se poate roti;
  - plăcile disponibile: întregi și resturi din stoc;
  - grosimea pânzei;
  - cât se tunde din marginile plăcii;
  - dimensiunea minimă a unui rest care merită păstrat.
- **Modul de tăiere se alege pentru fiecare mașină:** ghilotină (fierăstrău de panouri) sau așezare liberă (CNC).
- Aplicația rulează mai multe strategii într-un timp limitat și păstrează cel mai bun rezultat. Aceeași listă produce întotdeauna același plan.
- **Verificator independent:** un program separat verifică fiecare plan înainte de afișare. Controlează că nu există suprapuneri, că nu lipsesc piese și nu sunt în plus, că pânza și fibra sunt respectate și că tăieturile se pot executa pe mașină.
- **Rezultate:**
  - planuri cu cote și ordinea tăieturilor;
  - grad de utilizare, număr de plăci, metri de tăiere;
  - resturile generate, care intră automat în stoc;
  - etichete.

### 4.5 Editorul 3D (M13)

- Corpuri din bibliotecă, redimensionate din mânere sau prin cote.
- Uși, sertare și polițe adăugate cu un clic; materiale și culori.
- Vizualizare din orice unghi, vedere „explodată”, selecția unei piese împreună cu explicația calculului ei.
- Anulare și refacere nelimitate, salvare automată, recuperare după o închidere neașteptată.
- **Ținta de viteză:** o bucătărie obișnuită se recalculează și se redesenează în mai puțin de o zecime de secundă.

### 4.6 Costuri și documente (M10, M11)

- Costul se calculează din materiale (cu pierderile tehnologice), canturi, feronerie și manoperă (debitare, cantuire, găurire, asamblare, montaj, transport). Peste el se aplică adaosul, apoi discountul, apoi TVA. Toate valorile vin din setările utilizatorului.
- **Documente generate:**
  - necesar de materiale;
  - liste de debitare, canturi și feronerie;
  - fișa internă de costuri;
  - devizul pentru client și oferta comercială;
  - documentația de atelier (desene, liste, etichete).
- Toate se exportă în PDF și Excel, cu logo-ul firmei.

---

## 5. Structura bazei de date

Mai jos sunt tabelele principale. Fiecare coloană în detaliu (tip, reguli, legături) se descrie în documentul tehnic al modulului M1, înainte de implementare.

**A. Firmă și sistem**

| Tabel | Ce conține |
|---|---|
| firma | Datele firmei, logo, CUI, conturi bancare, TVA, monedă |
| setari | Limbă, rotunjiri, preferințe |
| utilizatori | Pregătit pentru versiunea cu mai mulți utilizatori |
| jurnal_modificari | Cine a modificat ce și când |

**B. Furnizori și producători**

| Tabel | Ce conține |
|---|---|
| furnizori | Nume, CUI, contact, monedă |
| producatori | Producători de plăci și feronerie, introduși de utilizator |

**C. Materiale**

| Tabel | Ce conține |
|---|---|
| tipuri_material | PAL melaminat, MDF înfoliat, MDF vopsit, MDF brut, PFL/HDF, placaj, blat, panou special. Pentru fiecare: are decor? are fibră? se cantuiește? se vopsește? |
| decoruri | Cod producător, nume, structură, textură |
| materiale | Tip, decor, grosime, sensul decorului/fibrei, față dublă, pierdere tehnologică % |
| formate_placa | Lungime × lățime, cât se tunde din margini, furnizor |
| canturi | Tip (ABS/PVC/furnir/melamină), grosime, lățime, decor, adaos de lungime |
| preturi | Articol, furnizor, preț, unitate (buc/m²/ml/placă), monedă, valabil de la–până la (istoric complet) |
| stoc_placi | Material, format, cantitate, locație |
| resturi | Material, dimensiuni, proiectul de proveniență, locație, stare |

**D. Feronerie**

| Tabel | Ce conține |
|---|---|
| categorii_feronerie | Balamale, glisiere, sisteme de sertare, sisteme de ridicare, picioare, mânere, suporturi, șuruburi, dibluri, conectori, uși glisante, accesorii speciale |
| articole_feronerie | Producător, cod producător, cod furnizor, caracteristici tehnice, unitate, bucăți per ambalaj |
| seturi_feronerie | Articole compuse (ex. balama = corp + talpă + capac) |
| reguli_feronerie | Condiție, articol sau criterii, formula de cantitate, prioritate, versiune |
| efecte_feronerie | Formulele și găurile pe care feroneria le impune pieselor |

**E. Reguli de construcție**

| Tabel | Ce conține |
|---|---|
| sisteme_constructive | Cum se îmbină piesele (între sau peste laterale), tipul spatelui, nutul, retragerile |
| profile_rosturi | Rosturile pentru uși, sertare și fronturi |
| sabloane_corp | Modelele de corpuri, cu categorie și versiune |
| parametri_sablon | Pentru fiecare parametru: nume, unitate, valoare implicită, minim, maxim |
| piese_sablon | Rol, material, formule, cantitate, condiție, canturi pe muchii, fibră |
| prelucrari_sablon | Găuri, nuturi, frezări (pregătire pentru CNC) |

**F. Clienți, proiecte, producție**

| Tabel | Ce conține |
|---|---|
| clienti | Persoană fizică sau juridică, date de contact (date personale, deci intră sub GDPR) |
| proiecte | Client, adresă de montaj, stare (ofertă → aprobat → producție → livrat) |
| revizii_proiect | Rețeta proiectului în fiecare versiune (v1, v2…) |
| instantanee | Rezultatele înghețate (piese, planuri, prețuri) pentru ofertele emise și comenzi |
| oferte | Număr, dată, valabilitate, totaluri, TVA, discount, stare, PDF-ul emis |
| comenzi_productie | Ce versiune a proiectului intră în producție și când |
| sesiuni_debitare | Comenzile debitate împreună, parametrii folosiți, rezultatul |

**G. Costuri**

| Tabel | Ce conține |
|---|---|
| tarife_manopera | Debitare, cantuire, găurire, asamblare, montaj, transport, fiecare cu unitatea ei |
| politici_pret | Adaos pe categorii, discount maxim, rotunjiri |

### 5.1 Rețeta proiectului

```
Proiect
 └─ Încăpere (pereți, ferestre, obstacole)
     ├─ Corp = șablon + versiune + poziție + dimensiuni + materiale + sistem constructiv
     │    ├─ Uși, sertare, polițe, despărțitoare
     │    └─ Modificări manuale (marcate vizibil în rapoarte)
     └─ Piese libere (proiectare liberă)
```

Rețeta se salvează ca un document întreg, cu versiune. Piesele, planurile și prețurile **nu se salvează**. Se recalculează din rețetă și se păstrează doar în instantaneele înghețate. Astfel nu pot exista două variante diferite ale aceleiași informații.

---

## 6. Testare și tratarea erorilor

- **Cazuri de referință:** 3–5 corpuri reale, cu listele de debitare verificate în atelierul proprietarului, devin teste automate. Rezultatele trebuie să coincidă la 0,1 mm.
- **Teste de formulă:** fiecare regulă de calcul are exemple calculate manual și aprobate de proprietar.
- **Teste de proprietăți:** pentru orice dimensiuni din intervalul permis, piesele unui corp nu se suprapun și nicio dimensiune nu e negativă sau zero.
- **Verificatorul de debitare** rulează pe fiecare plan, atât în teste, cât și în aplicație.
- **Compatibilitate:** fișierele de proiect din versiunile vechi se deschid automat în teste.
- **Toate testele rulează automat** la fiecare modificare, iar rezultatele sunt vizibile pe GitHub.
- **Date lipsă sau invalide:** calculul afectat se oprește cu un mesaj clar, care spune ce lipsește și unde. Restul proiectului rămâne utilizabil.

---

## 7. Riscuri tehnice

| # | Risc | Cum îl reducem |
|---|---|---|
| 1 | **Erori de calcul în producție** | Cazuri de referință reale, teste automate, explicația fiecărei cote, verificator independent |
| 2 | **Calitatea optimizării debitării** | Mai multe strategii, comparații cu programele existente pe aceleași liste, ținte măsurabile de randament |
| 3 | **Volumul cerut** („orice mobilier, fără restricții”) | Un nucleu general și o primă versiune clar delimitată; restul vine pe etape |
| 4 | **Datele de catalog** (inclusiv licența pentru datele și texturile producătorilor) | Import din listele de prețuri în Excel, cerere oficială de date către producători, verificare juridică |
| 5 | **Distribuția pe Windows** („aplicație nesigură”, antivirus) | Certificat de semnare a codului, instalator și actualizări semnate |
| 6 | **Pierderea datelor clienților** | Salvare automată, backup zilnic automat, recuperare după erori |
| 7 | **Compatibilitate în timp** | Formate cu versiune, conversie automată, teste cu fișiere vechi |
| 8 | **Pirateria, în timp ce atelierele au adesea internet slab** | Licență semnată criptografic, legată de calculator, cu perioadă offline configurabilă |
| 9 | **Proprietarul nu poate verifica direct codul** | Criterii de acceptare în limbaj simplu, teste automate vizibile, opțional o verificare externă la etapele cheie |
| 10 | **Performanță slabă la proiecte mari** | Se recalculează doar ce s-a schimbat; ținte de viteză testate automat |
| 11 | **Aspecte legale** (GDPR, marcă, contractul de licență cu clientul, răspunderea pentru erori) | Consultanță juridică înainte de lansare |
| 12 | **Dificultatea limbajului Rust** | Cod modular, documentat și testat; istoricul complet în GitHub |

---

## 8. Plan etapizat

Fiecare etapă se încheie cu un raport în limbaj simplu, verificarea proprietarului, un punct de restaurare și aprobarea proprietarului. Abia apoi începe etapa următoare.

| Etapă | Conținut | Cum verifică proprietarul |
|---|---|---|
| **0** | Fundația: structura proiectului, teste automate, construirea automată a instalatorului Windows | Instalează pe Windows o aplicație „Furniture Pro” goală, construită automat |
| **1** | Documentul cu regulile de calcul: glosar, convenții, sisteme constructive, formule cu exemple numerice, plus cazurile de referință | Aprobă fiecare regulă și fiecare exemplu |
| **2** | Nucleul de calcul: precizie, bani, limbajul de formule, explicațiile calculelor | Exemplele din Etapa 1 ies corect într-un raport de teste lizibil |
| **3** | Baza de date și cataloagele de materiale și canturi, cu import din Excel | Introduce materialele reale ale atelierului |
| **4** | Motorul parametric pentru corpuri standard și listele de piese și canturi, încă fără 3D | Rezultatele coincid cu cazurile de referință la 0,1 mm |
| **5** | Feroneria: catalog, reguli, efectul asupra pieselor | Listele de feronerie ies corecte pentru cazurile de referință |
| **6** | Optimizarea debitării, verificatorul, planurile tipăribile, resturile | Comparație cu programul folosit azi, pe aceleași liste |
| **7** | Editorul 3D, legat de nucleu | O cotă modificată în 3D actualizează listele, debitarea și costul |
| **8** | Costuri, deviz, ofertă, documentație de atelier, etichete, PDF și Excel | Documentele pentru un proiect real se potrivesc cu calculul atelierului |
| **9** | Proiecte complexe: încăperi, bucătării complete, dressinguri, uși glisante, proiectare liberă | Proiecte reale complete |
| **10** | Pregătirea comercială: licențe, instalator semnat, actualizări automate, manual de utilizare, limba engleză | Instalare pe un calculator curat |
| **11** | Pilot în 2–3 ateliere reale, apoi lansarea versiunii 1.0 | Feedback de la utilizatori reali |
| v2+ | Export CNC, versiunea cu mai mulți utilizatori, AI, stoc avansat | — |

**Observații:**
- **Debitarea (Etapa 6) are nevoie doar de lista de piese.** Poate fi mutată imediat după Etapa 3 și vândută ca prim produs, cu import din Excel.
- **Durata:** nu se dau termene inventate. O primă versiune comercială de calitate înseamnă multe luni de lucru. Ritmul real depinde de viteza verificărilor.
- **Documentația:** fiecare modul primește propriul document tehnic (scop, intrări, ieșiri, reguli, exemple, teste, limitări).

---

## 9. Metoda de lucru și puncte de restaurare

- Proiectul stă în depozitul `DevWorld`, în folderul `furniture-pro/`.
- **Fiecare pas terminat** devine un commit, adică un punct salvat cu descriere.
- **Fiecare sarcină** se face pe o ramură separată și intră în versiunea principală printr-o cerere de integrare (pull request), pe care proprietarul o poate vedea pe GitHub.
- **Fiecare etapă aprobată** primește o etichetă, de exemplu `furniture-pro/etapa-3-aprobata`, la care ne putem întoarce oricând.

---

## 10. Decizii în așteptare

Sunt listate în [`../../brief.md`](../../brief.md), la „Încă deschise”.
