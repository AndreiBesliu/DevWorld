# Brief: Furniture Pro

Cerințele proprietarului, transcrise din mesajul inițial din 2026-11-01. Ipotezele și cerințele propuse de AI sunt separate mai jos și așteaptă confirmarea.

## Ce vrea proprietarul

Furniture Pro este o aplicație profesională pentru Windows, destinată atelierelor și fabricilor de mobilă. Este un **produs comercial**, care se va vinde altor ateliere. Trebuie gândită de la început ca produs serios, nu ca demonstrație sau prototip.

### 1. Obiectiv

Aplicația permite proiectarea mobilierului de orice tip și generează automat tot ce trebuie pentru fabricație:

- necesarul complet de materiale;
- lista tuturor pieselor, cu dimensiunile exacte ale fiecăreia;
- canturile necesare;
- accesoriile și feroneria;
- optimizarea debitării plăcilor și planurile de debitare;
- reprezentarea mobilierului în 3D;
- calculul costurilor și al prețului de vânzare;
- documentele necesare producției.

### 2. Materiale

Cel puțin: PAL melaminat, MDF înfoliat, MDF vopsit, MDF brut, PFL/HDF, placaj, blaturi, panouri și materiale speciale, cant ABS, PVC și furnir.

Fiecare material are caracteristici configurabile: grosime, dimensiuni standard, decor, furnizor, preț, disponibilitate și pierderi tehnologice.

### 3. Tipuri de mobilier

Orice tip: bucătării, dressinguri, dulapuri, dormitoare, living, baie, birou, mobilier comercial, mobilier pentru magazine, mobilier personalizat.

Trebuie să existe atât **module standard configurabile**, cât și **proiectare liberă**, fără restricții de forme și dimensiuni.

### 4. Calculul automat al pieselor

Pentru fiecare corp, aplicația generează automat: laterale, funduri, capace, polițe, traverse, spătare, uși, fronturi, sertare, elemente decorative și alte componente necesare.

Dimensiunile se calculează în funcție de grosimea materialelor, rosturi, suprapuneri, retrageri și tipul construcției. **Toate regulile de calcul sunt configurabile și verificabile. Formulele aproximative nu sunt acceptate.**

### 5. Feronerie și accesorii

Bază de date configurabilă pentru: balamale, glisiere, sisteme de sertare, sisteme de ridicare, picioare, mânere, suporturi de polițe, șuruburi, dibluri, conectori, sisteme de uși glisante, accesorii speciale.

Reguli configurabile prin care aplicația calculează automat accesoriile în funcție de mobilier. Produse de la mai mulți furnizori, inclusiv Blum și alte mărci.

### 6. Debitarea materialelor

Unul dintre cele mai importante module. Preia toate piesele proiectului și generează planuri optimizate de debitare pentru plăci întregi, ținând cont de:

- dimensiunile plăcilor;
- grosimea pânzei și pierderea la tăiere;
- sensul decorului și orientarea fibrei;
- canturi;
- posibilitatea rotirii pieselor;
- resturile reutilizabile;
- materiale și grosimi diferite.

Planurile trebuie să fie clare și să poată fi tipărite sau exportate.

### 7. Proiectare 3D

Editor 3D cu vizualizare în timp real. Utilizatorul poate:

- crea corpuri standard și le poate modifica dimensiunile;
- adăuga și modifica uși, sertare și polițe;
- schimba materialele și culorile;
- vedea mobilierul din orice unghi;
- selecta componente individuale;
- modifica proiectul, iar calculele se actualizează automat.

**Modelul 3D și calculele de producție folosesc aceeași sursă de date.** Modificarea unei dimensiuni actualizează automat toate informațiile dependente.

### 8. Deviz și documente

Aplicația generează: necesar de materiale, listă de debitare, listă de canturi, listă de feronerie, costuri de producție, deviz pentru client, ofertă comercială, documentație pentru atelier. Exporturi în **PDF și Excel**.

### 9. Cerințe tehnice

- Produs comercial pentru Windows, cu interfață desktop modernă.
- Funcțiile principale merg local, cu bază de date locală.
- Internetul se folosește pentru licențe, actualizări, sincronizare și funcții AI opționale.
- Arhitectura permite mai târziu o versiune multiutilizator.
- Propunerea inițială a proprietarului: Tauri 2, React, TypeScript, Three.js și Python pentru motoarele de calcul. AI-ul trebuie să analizeze dacă este cea mai potrivită.

### 10. Reguli obligatorii de dezvoltare

1. Nu se construiește întreaga aplicație într-o singură etapă.
2. Proiectul se împarte în module independente.
3. Înainte de implementare se stabilesc arhitectura și structura bazei de date.
4. Fiecare modul are documentație tehnică.
5. Formulele de calcul au teste automate.
6. Nu se folosesc valori fictive în calculele de producție.
7. Logica de calcul nu se duplică între interfață și backend.
8. Modulele rămân compatibile între ele.
9. Se salvează periodic puncte de restaurare ale proiectului.
10. Comenzile și pașii tehnici se explică pe înțelesul proprietarului, fără a presupune cunoștințe de programare.
11. Nu se trece la etapa următoare fără verificarea rezultatului etapei curente.
12. Modulele funcționale nu se modifică fără explicarea motivului și a efectelor.

## Decizii luate

- **2026-11-01:** proiectul stă în depozitul `DevWorld`, în folderul `furniture-pro/`.
- **2026-11-01:** comunicarea și documentele pentru proprietar sunt în română. Codul, comentariile din cod și mesajele de commit sunt în engleză.

## Ipoteze (de confirmat)

1. **Utilizatori:** proiectantul sau vânzătorul (face oferta) și șeful de atelier (folosește listele, planurile de debitare și etichetele).
2. **Unitatea de lucru:** milimetrul.
3. **Piața de start:** România (limba română, lei, TVA), apoi alte țări.
4. **Criteriu de succes:** un atelier ajunge de la măsurătoare la ofertă și la piese debitate corect fără calcule manuale, cu rezultate cel puțin la nivelul programelor folosite azi.

## Cerințe suplimentare propuse (de confirmat)

Detaliate în specificația de arhitectură, secțiunea 2.

- Diferența dintre dimensiunea finită și dimensiunea de debitare, configurabilă pentru fiecare mașină de cantuit.
- Etichete pe piese, cu cod de bare sau QR.
- Prelucrări: găuri pentru balamale, găuri pentru suporturile polițelor, confirmat, nuturi.
- Tăiere ghilotină (fierăstrău de panouri) și așezare liberă (CNC).
- Desene tehnice 2D cu cote.
- Încăperea: pereți, colțuri, ferestre, obstacole.
- Debitarea mai multor comenzi împreună.
- Stoc de plăci și resturi.
- Fronturi comandate la terți, în listă separată.
- Versiuni de ofertă și „înghețarea” ofertei trimise.
- Import listă de piese din Excel.
- Modificări manuale ale pieselor, marcate vizibil.
- Verificarea mărcii „Furniture Pro” la OSIM/EUIPO.

## Încă deschise

1. **Rust în loc de Python** pentru nucleul de calcul. Explicația diferenței este în specificație, secțiunea 3.4.
2. **Programele și mașinile folosite azi:** fierăstrău cu masă glisantă, mașină automată de debitat sau CNC; mașina de cantuit are frezare prealabilă?
3. **Corpuri de referință:** 3–5 corpuri reale, cu listele de debitare verificate în atelier.
4. **Ordinea etapelor:** debitarea ca prim produs livrabil sau ordinea standard.
5. **Piața de start:** doar România sau și alte țări de la început.
6. **Vizibilitatea depozitului:** `DevWorld` este public, deci oricine poate vedea codul.
