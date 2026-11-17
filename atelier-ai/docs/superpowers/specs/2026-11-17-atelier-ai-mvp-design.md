# Atelier AI — Design MVP

**Data:** 2026-11-17
**Stare:** design aprobat pe secțiuni în conversație; specificația așteaptă review.

Textul e în română. Numele de câmpuri, valorile și rolurile scrise `astfel` sunt identificatori din cod, deci sunt în engleză.

---

## 1. Context și scop

Aplicație web pentru atelierele de tâmplărie și mobilier la comandă. Utilizatorul încarcă o schiță de mână (fotografie) sau un PDF tehnic de la arhitect, iar aplicația:

1. extrage corpurile de mobilier cu ajutorul unui model AI de viziune;
2. calculează lista de piese (BOM) după regulile de construcție ale atelierului;
3. optimizează debitarea pe foi standard (ex. 2800 × 2070 mm), ținând cont de kerf și de fibră;
4. afișează un preview 3D simplu al corpurilor.

Produsul este gândit pentru **mai multe ateliere**. Fiecare atelier își configurează catalogul de materiale, regulile de construcție și setările de debitare.

## 2. Criteriu de succes

Pentru o bucătărie de ~10 corpuri, un tâmplar ajunge de la schiță la lista de piese și la planul de tăiere în **mai puțin de 20 de minute**. Piesele calculate din corpuri verificate de utilizator nu trebuie corectate de mână.

## 3. Ce intră și ce nu intră în MVP

**Intră:**
- Corpuri de bază, suspendate și coloane, cu polițe, uși și sertare.
- Reguli de construcție configurabile prin opțiuni predefinite (fără formule scrise de utilizator).
- Listă de piese editabilă, cu modificări care rezistă la recalculare.
- Optimizare de debitare ghilotină, cu setări alese de utilizator.
- Export Excel și CSV al listei de piese.
- Preview 3D cu cutii colorate.
- Extragere AI din imagini (JPG, PNG, HEIC) și PDF-uri.

**Nu intră (amânat):**
- Autentificare și conturi de utilizator. MVP-ul rulează cu un singur atelier implicit (vezi §9.3).
- Corpuri de colț, piese care nu sunt dreptunghiulare.
- Formule de construcție scrise de atelier, biblioteci de corpuri-șablon.
- Export CNC/DXF, formate specifice fiecărui furnizor de debitare.
- Oferte cu prețuri, abonamente, plăți.
- Inventar de resturi între proiecte.
- Texturi în 3D.

## 4. Arhitectură și flux de date

```
upload ──► [Ingestie AI] ──► ExtractedProject (corpuri + nesiguranțe)
                                   │
                         verificare de către utilizator
                                   │
                                   ▼
          [Motor BOM] corpuri + reguli + catalog ──► piese + feronerie + poziții 3D
                                   │                          │
                    modificări din tabel (suprascrieri)       │
                                   │                          ▼
                                   ▼                    [Preview 3D]
                    [Optimizare debitare] ──► plan de tăiere SVG + rezumat
                                   │
                                   ▼
                          export Excel / CSV
```

**Decizia principală:** AI-ul **nu** calculează piese. AI-ul descrie corpurile prin parametri (tip, dimensiuni, materiale, polițe, fronturi), iar piesele sunt calculate de un motor cu reguli fixe, testat automat. Motivele:
- modelele greșesc calculele aritmetice când au zeci de piese;
- regulile de construcție rămân în cod testabil și în configurația atelierului, nu într-un prompt;
- 3D-ul și optimizarea primesc piese care știu ce rol au (lateral, ușă etc.);
- aceeași logică servește și introducerea manuală a corpurilor.

**Separarea pe straturi (backend):**
- `domain/` — modele de date pure (Pydantic), fără I/O.
- `engines/` — motoarele de calcul (BOM, debitare, export): funcții pure, fără FastAPI și fără bază de date.
- `ingestion/` — integrarea cu modelul AI.
- `api/`, `db/`, `jobs/` — HTTP, persistență, sarcini în fundal.

## 5. Model de date

### 5.1 Unități și convenții

- Toate dimensiunile sunt în **mm**, stocate ca `Decimal` rotunjit la **0,1 mm** (nu `float`), ca să putem scădea exact cantul de 0,4.
- **W / H / D** = lățimea / înălțimea / adâncimea exterioară a carcasei. **G** = grosimea materialului corpului.
- **Lungimea** unei piese = dimensiunea pe direcția fibrei. Pentru materialele fără fibră, lungimea = latura mai lungă.
- Muchiile unei piese: două muchii lungi și două scurte. În interfață și în export se notează **L1, L2, l1, l2**; în cod, `L1`, `L2`, `W1`, `W2`. **L1 este muchia lungă care se vede din față.**
- Fibra foii este pe lungimea foii (ex. pe 2800).

### 5.2 Nivelul atelier

| Entitate | Câmpuri principale |
|---|---|
| `Workshop` | `id`, `name` |
| `SheetMaterial` | `id`, `code`, `name`, `kind` (`PAL`/`MDF`/`PFL`/`HDF`), `thickness`, `finish` (`melamine`/`painted`/`foil`/`raw` = melaminat/vopsit/infoliat/brut), `has_grain`, `sheet_length`, `sheet_width`, `color_hex` (pentru 3D) |
| `EdgeBand` | `id`, `code`, `name`, `thickness` (ex. 0,4 / 2), `width` (ex. 22 / 42) |
| `Hardware` | `id`, `kind` (`hinge` / `slide` / `leg` / `hanger` = balama / glisieră / picior / agățătoare), `name`; la glisieră și `length` și `width_deduction` (cât se scade din lățimea sertarului) |
| `ConstructionProfile` | regulile de construcție (vezi §6.2) |
| `CuttingSettings` | setările implicite de debitare (vezi §7.2) |

### 5.3 Nivelul proiect

**`Project`**: `id`, `workshop_id`, `name`, `client`, `status`, fișierele încărcate.

**`Cabinet`** (corp parametric):
- `label` (ex. „B1”), `type`: `base` | `wall` | `tall` (bază / suspendat / coloană), `position` (ordinea pe perete, pentru 3D);
- `width`, `height`, `depth`: dimensiunile exterioare ale carcasei, fără picioare și fără fronturi;
- `carcass_material_id`, `back_material_id`, `front_material_id`: referințe în catalog (implicit, cele din profilul atelierului);
- `shelf_count`;
- `front_zones`: listă ordonată **de sus în jos**. Fiecare zonă are:
  - `kind`: `door` | `drawer` | `open` (ușă / sertar / deschis);
  - `door_count`: 1 sau 2 (doar pentru `door`);
  - `hinge_side`: `left` | `right` (doar pentru o singură ușă);
  - `height`: număr, sau `null` pentru „restul”. Cel mult o zonă poate avea `null`. Înălțimile trebuie să se adune la H.
- `source`: `manual` | `ai`, plus presupunerile și problemele semnalate de AI.

### 5.4 Rezultatul calculat

**`Part`** (piesă):
- `key` stabilă, ex. `B1.side`, `B1.shelf`, `B2.drawer_side`. Piesele identice cu același rol în același corp formează o singură `Part` cu `quantity` > 1.
- `role`, cu denumirea afișată în română:

  | `role` | Denumire | `role` | Denumire |
  |---|---|---|---|
  | `side` | lateral | `door` | ușă |
  | `bottom` | fund | `drawer_front` | front sertar |
  | `top` | capac | `drawer_side` | laterală sertar |
  | `front_rail` | traversă față | `drawer_front_back` | față/spate cutie sertar |
  | `back_rail` | traversă spate | `drawer_bottom` | fund sertar |
  | `shelf` | poliță | `manual` | piesă adăugată manual |
  | `back` | spate | | |

- `name` (afișat în română), `material_id`, `thickness`.
- `finished_length`, `finished_width`, `cut_length`, `cut_width`.
- `edges`: pentru fiecare muchie (`L1`, `L2`, `W1`, `W2`), referința de cant sau nimic.
- `quantity` și `placements`: câte o poziție în corp pentru fiecare bucată (folosită în 3D).

**`HardwareLine`**: corpul, articolul din catalog, cantitatea.

**`PartOverride`** (modificare făcută de utilizator în tabel): `part_key`, `field`, `value`, `base_value` (valoarea calculată în momentul modificării). Câmpurile care se pot modifica: `name`, `finished_length`, `finished_width`, `edges`, `material_id`, `quantity` și `deleted`. Dimensiunile de debitare se recalculează mereu din dimensiunile finite și canturi.

**Piese manuale**: utilizatorul poate adăuga piese care nu aparțin niciunui corp (cheie `M.1`, `M.2`, …).

## 6. Motorul BOM

### 6.1 Interfață și pași

```
generate_bom(cabinets, profile, catalog, overrides) -> BomResult(parts, hardware, warnings, errors)
```

Pași, în ordine:
1. **Verificarea datelor.** Exemple de erori, care opresc calculul corpului respectiv: W ≤ 2G; înălțimile zonelor nu se adună la H; mai mult de o zonă cu `null`; nicio glisieră din catalog nu încape în adâncime; material lipsă din catalog. Exemple de avertismente, care nu opresc calculul: piesă mai lungă decât foaia pe direcția fibrei; ușă mai înaltă de 2400 mm.
2. **Carcasa:** laterale, fund, capac sau traverse, polițe, spate.
3. **Fronturile:** uși și sertare (front + cutie).
4. **Canturi și dimensiuni de debitare** (§6.3).
5. **Feronerie** (§6.6).
6. **Aplicarea modificărilor utilizatorului.**
   - O modificare a cărei piesă nu mai există se marchează **orfană**.
   - O modificare a cărei valoare calculată s-a schimbat de când a fost făcută se marchează **de verificat** („era X, acum Y”).
   - Niciuna nu se șterge automat.

La pașii 2–3, fiecare piesă primește și amplasarea ei în corp: colțul minim și dimensiunile pe axe. Originea este colțul stânga-jos-spate al carcasei, cu x spre dreapta, y în sus și z spre față. Polițele se distribuie la distanțe egale pe înălțimea interioară a zonelor cu ușă sau deschise. Un corp fără zone de front se tratează ca un corp cu o singură zonă `open` pe toată înălțimea.

### 6.2 Reguli de construcție (`ConstructionProfile`)

| Regulă | Câmp și opțiuni | Implicit |
|---|---|---|
| Îmbinarea orizontalelor | `horizontal_joint`: `between_sides` (lateralele pe toată înălțimea, fundul și capacul între ele) · `over_sides` (fundul și capacul pe toată lățimea, lateralele între ele) | `between_sides` |
| Partea de sus a corpului de bază | `base_top`: `rails` (2 traverse culcate, față și spate) · `full_top` (capac); `rail_width` | `rails`, 100 |
| Partea de sus a corpului suspendat și a coloanei | `full_top` (singura opțiune în MVP) | `full_top` |
| Montarea spatelui | `back_mount`: `applied` (capsat pe spatele carcasei) · `groove` (în nut); `back_inset` (retragerea pe latură la `applied`) | `applied`, 2 |
| Parametrii nutului | `groove_depth` (n), `groove_clearance` (j), `groove_offset` (d, distanța față de muchia din spate) | n = 8, j = 1, d = 10 |
| Rosturi fronturi | `front_gap_edge` (r_m, la marginea corpului), `front_gap_between` (r_i, între fronturi) | 1,5 · 3 |
| Polițe | `shelf_setback` (retragere față de muchia din față), `shelf_side_clearance` (joc pe fiecare parte) | 20 · 1 |
| Fibra frontului de sertar | `drawer_front_grain`: `horizontal` · `vertical` | `horizontal` |
| Cutia de sertar | `drawer_box_material_id`, `drawer_box_height_reduction` (cu cât e mai joasă decât frontul), `drawer_bottom_inset` (retragerea fundului HDF pe latură) | materialul corpului · 40 · 1 |
| Spațiu liber în spatele glisierei | `slide_rear_clearance` | 10 |
| Canturi pe roluri | `edge_rules`: pentru fiecare `role` și fiecare muchie: `none` · `carcass` (cant corp) · `front` (cant fronturi) | vezi tabelul de mai jos |
| Cantul corpului / al fronturilor | `carcass_edge_band_id`, `front_edge_band_id` | ABS 0,4 / ABS 2 |
| Scăderea cantului la debitare | `deduct_edge_band` | `true` |
| Adaos de prefrezare pe muchia căntuită | `premill_per_edge` | 0 |
| Înălțimea corpurilor suspendate de la podea (3D) | `wall_cabinet_elevation` | 1450 |
| Înălțimea picioarelor (3D) | `leg_height` | 100 |

**Canturi implicite pe roluri (`edge_rules`):**

| `role` | L1 | L2 | l1 | l2 |
|---|---|---|---|---|
| `side`, `bottom`, `top`, `shelf` | `carcass` | — | — | — |
| `front_rail`, `back_rail` | `carcass` | — | — | — |
| `door`, `drawer_front` | `front` | `front` | `front` | `front` |
| `drawer_side`, `drawer_front_back` | `carcass` (muchia de sus) | — | — | — |
| `back`, `drawer_bottom` | — | — | — | — |

**Formule pentru carcasă:**
- `between_sides`: lateral = H × D; fund = (W − 2G) × D; capac = (W − 2G) × D.
- `over_sides`: fund = W × D; capac = W × D; lateral = (H − G − G_sus) × D, unde G_sus = G dacă există capac și 0 dacă sunt traverse.
- Traverse (în ambele cazuri, între laterale): (W − 2G) × `rail_width`.
- Spate `applied`: (W − 2r) × (H − 2r), unde r = `back_inset`.
- Spate `groove`:
  - lățime = W − 2G + 2(n − j);
  - înălțime = H − 2G + 2(n − j) la corpurile cu capac;
  - înălțime = H − G + (n − j) la corpurile de bază cu traverse: muchia de sus a spatelui ajunge până sus, iar traversa din spate se mută în fața spatelui.
- Adâncime utilă: D la `applied`; D − d − grosimea spatelui la `groove`.
- Poliță: (W − 2G − 2 × `shelf_side_clearance`) × (adâncime utilă − `shelf_setback`).

**Formule pentru fronturi (uși aplicate, cu acoperire completă):**
- Înălțimea frontului = înălțimea zonei − rostul de sus − rostul de jos. Rostul este r_m la marginea corpului și r_i / 2 între două zone.
- Lățimea frontului: W − 2·r_m pentru o ușă sau un sertar; (W − 2·r_m − r_i) / 2 pentru fiecare dintre cele două uși.
- Ușile au fibra pe verticală.

**Formule pentru sertare** (un singur tip în MVP: cutie din PAL cu fund din HDF capsat dedesubt):
- Glisiera aleasă = cea mai lungă din catalog cu `length` ≤ adâncime utilă − `slide_rear_clearance`.
- Lățimea exterioară a cutiei = W − 2G − `width_deduction` al glisierei.
- Înălțimea cutiei = înălțimea frontului − `drawer_box_height_reduction`.
- Adâncimea cutiei = lungimea glisierei.
- Laterale cutie (2 buc. pe sertar): adâncime cutie × înălțime cutie.
- Față și spate cutie (2 buc. pe sertar): (lățime cutie − 2·G_cutie) × înălțime cutie.
- Fund HDF: (lățime cutie − 2·`drawer_bottom_inset`) × (adâncime cutie − 2·`drawer_bottom_inset`).

### 6.3 Dimensiunile de debitare

```
cut_length = finished_length − band(W1) − band(W2) + premill_per_edge × (nr. de muchii căntuite dintre W1, W2)
cut_width  = finished_width  − band(L1) − band(L2) + premill_per_edge × (nr. de muchii căntuite dintre L1, L2)
```

`band(x)` = grosimea cantului de pe muchia x (0 dacă nu are cant). Când `deduct_edge_band` = `false`, dimensiunile de debitare sunt egale cu cele finite.

### 6.4 Exemplul de referință B1 (aprobat)

Corp de bază 600 × 720 × 560, PAL 18, reguli implicite, 1 poliță, o zonă cu 2 uși.

| Piesă | Buc. | Finit (L × l) | Cant | Debitare |
|---|---|---|---|---|
| Lateral | 2 | 720 × 560 | 0,4 pe L1 | 720 × 559,6 |
| Fund | 1 | 564 × 560 | 0,4 pe L1 | 564 × 559,6 |
| Traversă față | 1 | 564 × 100 | 0,4 pe L1 | 564 × 99,6 |
| Traversă spate | 1 | 564 × 100 | 0,4 pe L1 | 564 × 99,6 |
| Poliță | 1 | 562 × 540 | 0,4 pe L1 | 562 × 539,6 |
| Spate PFL 3 | 1 | 716 × 596 | — | 716 × 596 |
| Ușă | 2 | 717 × 297 | 2 pe 4 laturi | 713 × 293 |

Feronerie: 4 balamale, 4 picioare.

### 6.5 Exemplul de referință B2 (sertare, propus, de verificat cu atelierul)

Corp de bază 500 × 720 × 560, PAL 18, reguli implicite, fără polițe, 3 zone de sertar de câte 240. Catalogul are glisiere cu bile de 300–500 mm, din 50 în 50, cu `width_deduction` = 26.

Glisiera aleasă: 500 (adâncimea utilă 560 − 10 = 550). Lățimea cutiei = 500 − 36 − 26 = 438. Înălțimea cutiei = 237 − 40 = 197.

| Piesă | Buc. | Finit (L × l) | Cant | Debitare |
|---|---|---|---|---|
| Lateral | 2 | 720 × 560 | 0,4 pe L1 | 720 × 559,6 |
| Fund | 1 | 464 × 560 | 0,4 pe L1 | 464 × 559,6 |
| Traversă față | 1 | 464 × 100 | 0,4 pe L1 | 464 × 99,6 |
| Traversă spate | 1 | 464 × 100 | 0,4 pe L1 | 464 × 99,6 |
| Spate PFL 3 | 1 | 716 × 496 | — | 716 × 496 |
| Front sertar | 3 | 497 × 237 | 2 pe 4 laturi | 493 × 233 |
| Laterală sertar | 6 | 500 × 197 | 0,4 pe L1 | 500 × 196,6 |
| Față/spate sertar | 6 | 402 × 197 | 0,4 pe L1 | 402 × 196,6 |
| Fund sertar HDF 3 | 3 | 498 × 436 | — | 498 × 436 |

Feronerie: 3 perechi de glisiere de 500, 4 picioare.

### 6.6 Reguli de feronerie (implicite, configurabile)

- Balamale pe ușă, după înălțimea ușii: ≤ 900 → 2; ≤ 1600 → 3; ≤ 2000 → 4; peste → 5.
- Picioare: corp `base` și `tall` → 4; dacă W > 1000 → 6.
- Agățători: 2 pe fiecare corp `wall`.
- Glisiere: o pereche pe fiecare sertar, lungimea aleasă ca în §6.2.

## 7. Optimizarea debitării

### 7.1 Algoritm

Algoritm propriu, cu tăieturi ghilotină pe benzi, în trei trepte:
1. Tăieturi pe toată foaia, care o împart în benzi.
2. Tăieturi transversale prin fiecare bandă, care o împart în secțiuni.
3. Opțional, tăieturi de ajustare a pieselor mai înguste decât banda.

Pașii:
- Piesele se grupează pe material (implicit, și pe grosime). Fiecare grup se așază pe foile materialului său.
- Din foaie se scade întâi marginea de curățare. Între piese se lasă kerf-ul.
- La materialele cu fibră, lungimea piesei stă paralel cu lungimea foii, deci piesa nu se rotește. Piesele din materiale fără fibră se pot roti, dacă setarea o permite.
- Euristica este *first-fit decreasing* pe benzi, rulată de mai multe ori: cu mai multe criterii de sortare (lungime, lățime, arie) și, la direcția `auto`, cu ambele direcții de tăiere. Se păstrează rezultatul cu cele mai puține foi; la egalitate, cel cu cel mai mare rest refolosibil.
- O piesă care nu încape în zona utilă a foii apare ca eroare în listă și nu se așază.
- Ținta de performanță: sub 2 secunde pentru ~300 de piese.
- Intern, calculele se fac pe numere întregi în zecimi de mm.

### 7.2 Setări (alese de utilizator)

Atelierul are valori implicite (`CuttingSettings`), iar la fiecare rulare utilizatorul le poate schimba doar pentru acel proiect.

| Setare | Câmp | Implicit |
|---|---|---|
| Kerf | `kerf` | 4 mm |
| Margine de curățare pe lungime / pe lățime | `trim_length`, `trim_width` | 10 / 10 mm |
| Dimensiunea minimă a unui rest refolosibil | `min_offcut_length`, `min_offcut_width` | 300 × 100 mm |
| Direcția primei tăieturi | `first_cut`: `auto` · `along_length` · `along_width` | `auto` |
| Tăieturi de ajustare în bandă (treapta a 3-a) | `allow_trim_cuts` | `true` |
| Rotirea pieselor din materiale fără fibră | `rotate_non_grain` | `true` |
| Adaos de cant pe fiecare muchie căntuită | `edge_band_allowance` | 30 mm |

### 7.3 Ce produce

- **Rezumat pe material:** număr de foi, procentul de foaie folosit, suprafața de rest, metri de cant pe fiecare tip (cu adaos).
- **Pentru fiecare foaie:** piesele așezate (cheie, x, y, lungime, lățime, rotită), resturile refolosibile și ordinea tăieturilor (arbore: benzi → secțiuni → ajustări).
- Foile cu aceeași așezare se grupează („Foaia 1 × 3”).
- Setările folosite și o amprentă (hash) a datelor de intrare. Dacă piesele se schimbă, rezultatul se marchează **depășit**.
- Desenul se face în frontend, ca SVG, din datele de mai sus.

### 7.4 Export

- **Excel** (`openpyxl`) și **CSV**, cu coloanele: Nr., Corp, Denumire, Material, L debitare, l debitare, Buc., Fibră, Cant L1, Cant L2, Cant l1, Cant l2, L finit, l finit.
- CSV-ul folosește separatorul `;` și virgula zecimală, ca să se deschidă corect în Excel cu setări românești.

## 8. Ingestia AI

### 8.1 Fișiere acceptate

- **Imagini:** JPG, PNG, HEIC. HEIC se convertește în JPEG (`pillow-heif`). Imaginile se rotesc după EXIF și se micșorează la limitele API-ului.
- **PDF-uri:** se trimit direct modelului, ca document. API-ul Claude citește PDF-ul nativ, adică atât textul (cotele scrise ca text), cât și desenul de pe fiecare pagină. Limite: 32 MB pe cerere, 600 de pagini. În ecranul de verificare, frontend-ul afișează paginile cu pdf.js.

### 8.2 Apelul către model

- Prin SDK-ul oficial `anthropic` pentru Python. Modelul și nivelul de efort sunt configurabile din setări; implicit, cel mai capabil model Opus curent.
- Răspunsul este constrâns la o schemă JSON (*structured outputs*) generată din modelul Pydantic `ExtractedProject` și validat la primire. Dacă validarea eșuează, se reîncearcă o singură dată, cu eroarea trimisă modelului.
- Se tratează explicit refuzurile modelului (`stop_reason = "refusal"`) și erorile cu reîncercare (429, 5xx).
- Promptul primește contextul atelierului:
  - denumirile materialelor din catalog, ca să poată potrivi „PAL alb” cu codul corect;
  - dimensiunile implicite pe tipuri de corp;
  - convențiile de notare (zone de front de sus în jos).
- Integrarea stă în spatele unei interfețe `VisionExtractor`. Testele folosesc o implementare simulată.

### 8.3 Ce întoarce modelul (`ExtractedProject`)

- `cabinets`: aceleași câmpuri ca `Cabinet` (§5.3), plus `confidence` pe fiecare câmp (`high` / `medium` / `low`) și `location_note` (unde apare corpul pe desen).
- `wall_length`: opțional, dacă apare pe desen.
- `assumptions`: ex. „adâncime lipsă, am folosit 560 (implicit atelier)”.
- `issues`: ex. „cota corpului B3 nu se citește”.
- Materialele care nu se potrivesc cu nimic din catalog rămân ca text și devin probleme (`issues`).

### 8.4 Verificări după extragere (fără AI)

- Suma lățimilor corpurilor față de `wall_length`.
- Dimensiuni neplauzibile pentru tipul de corp (ex. corp `base` mai înalt de 1000).
- Zone de front care nu se adună la H.
- Materiale care nu sunt în catalog.

Toate se adaugă la `issues`.

### 8.5 Verificarea de către utilizator (obligatorie)

Ecranul arată imaginea sau PDF-ul în stânga și corpurile extrase în dreapta. Câmpurile cu încredere scăzută, presupuse sau cu probleme sunt evidențiate. Corpurile intră în proiect doar după ce utilizatorul le confirmă.

### 8.6 Rulare și costuri

- Extragerea rulează ca sarcină în fundal (§9.4). Frontend-ul verifică periodic starea ei.
- Pentru fiecare sarcină se salvează tokenii consumați. Fiecare atelier are o limită lunară configurabilă; peste limită, extragerea este refuzată cu un mesaj clar.
- Fișierele clienților se păstrează privat, în spațiul atelierului, și se folosesc doar pentru extragere.

## 9. Backend: API, persistență, date

### 9.1 Tehnologii

Python 3.12+, FastAPI, Pydantic v2, SQLAlchemy 2, Alembic, PostgreSQL, stocare compatibilă S3 (MinIO local).

### 9.2 Ce se stochează

- Tabele relaționale: atelier, catalog, profil, setări de debitare, proiecte, fișiere, corpuri (zonele de front ca JSONB), suprascrieri, piese manuale, sarcini AI, rulări de debitare.
- **Lista de piese nu se stochează.** Se calculează la cerere: motorul rulează în milisecunde, deci e mereu sincronizată cu corpurile și regulile.
- **Rulările de debitare se stochează** (rezultat JSON + setări + amprenta datelor de intrare), ca să poată fi redeschise și marcate ca depășite.

### 9.3 Fără autentificare în MVP

- Există un singur atelier implicit („Atelier demo”), creat la pornire împreună cu un catalog de bază:
  - materiale: PAL alb 18, PAL stejar 18 cu fibră, PFL alb 3, HDF 3;
  - canturi: ABS 0,4 × 22, ABS 2 × 22;
  - feronerie: balama aplicată, glisiere cu bile 300–500, picior reglabil, agățătoare;
  - regulile și setările de debitare implicite.
- Toate tabelele au totuși `workshop_id`, iar tot accesul la date trece printr-un strat care cere atelierul curent. Atelierul curent vine dintr-o singură dependență FastAPI (`get_current_workshop`), care în MVP întoarce atelierul implicit. Când adăugăm autentificarea, se schimbă doar această dependență.
- **Fără autentificare, aplicația se rulează doar local sau într-o rețea privată.**

### 9.4 Sarcini în fundal

Coada de sarcini stă în PostgreSQL (`SELECT … FOR UPDATE SKIP LOCKED`) și este preluată de un proces separat (`worker`), din același cod. Nu folosim Redis, iar sarcinile nu se pierd la repornire. Optimizarea debitării rulează direct, în cerere.

### 9.5 API REST

| Resursă | Operații |
|---|---|
| `/materials`, `/edge-bands`, `/hardware` | CRUD catalog |
| `/construction-profile`, `/cutting-settings` | citire, actualizare |
| `/projects` | CRUD |
| `/projects/{id}/uploads` | încărcare și listare fișiere |
| `/projects/{id}/extractions` | pornire extragere, stare, rezultat, confirmare |
| `/projects/{id}/cabinets` | CRUD corpuri |
| `/projects/{id}/bom` | GET: piese + feronerie + avertismente · PATCH: suprascrieri · piese manuale |
| `/projects/{id}/nesting` | POST: rulare cu setări · GET: ultimele rulări |
| `/projects/{id}/export` | `?format=xlsx` sau `?format=csv` |

Clientul TypeScript se generează din specificația OpenAPI.

## 10. Frontend și 3D

### 10.1 Tehnologii

Next.js (App Router), TypeScript, Tailwind, shadcn/ui, TanStack Table (tabel editabil), TanStack Query (date de la server), react-three-fiber + drei (3D).

### 10.2 Ecrane

1. **Setările atelierului:** catalog, reguli de construcție (formular cu opțiunile din §6.2), setări de debitare.
2. **Lista de proiecte.**
3. **Proiectul**, cu pașii la rând:
   - **Încărcare:** pagina merge pe telefon, cu fotografiere directă.
   - **Verificare extragere** (§8.5).
   - **Corpuri:** editare și adăugare manuală.
   - **Piese:** tabelul editabil. Celulele modificate sunt marcate și pot fi readuse la valoarea calculată; modificările orfane și cele de verificat apar distinct.
   - **Debitare:** setări, rulare, plan SVG pe fiecare foaie (cu informații la trecerea mouse-ului), rezumat, export, tipărire din browser.
   - **3D.**

Interfața este în română și gândită în primul rând pentru calculator, dar trebuie să se poată folosi și pe tabletă.

### 10.3 Preview 3D

- Fiecare amplasare a unei piese este o cutie cu dimensiunile finite, la poziția dată de motorul BOM, colorată după `color_hex` a materialului.
- Corpurile se așază de la stânga la dreapta, după `position`:
  - corpurile `base` pe picioare;
  - cele `wall` la înălțimea `wall_cabinet_elevation`;
  - coloanele (`tall`) pe podea.
- Comenzi: rotire și zoom, „ascunde fronturile”, „vedere explodată”. Un click pe o piesă o selectează în tabelul de piese.

## 11. Tratarea erorilor

- Datele greșite ale unui corp produc o eroare cu `code`, `message` (în română) și `field` (calea către câmpul vizat). API-ul răspunde cu 422 pentru intrări invalide.
- La calculul BOM, erorile unui corp nu blochează celelalte corpuri: răspunsul conține piesele corpurilor valide și erorile celor invalide.
- Avertismentele vin împreună cu rezultatul și nu blochează nimic.
- Erorile AI se salvează pe sarcină, cu un mesaj clar pentru utilizator: fișier necitibil, refuz, limită depășită, timeout.
- Mesajele au coduri, ca să putem traduce interfața mai târziu.

## 12. Testare

- **Motorul BOM:** teste unitare cu rezultate calculate de mână. Exemplele B1 și B2 sunt teste de referință, plus câte un test pentru fiecare opțiune din §6.2.
- **Optimizarea debitării:** teste generate automat (Hypothesis), care verifică de fiecare dată că:
  - piesele nu se suprapun;
  - totul stă în zona utilă a foii;
  - kerf-ul și fibra sunt respectate;
  - fiecare piesă e așezată exact o dată sau raportată ca eroare.

  Plus un set de proiecte de referință, pe care urmărim procentul de foaie folosit de la o versiune la alta.
- **API:** teste pe PostgreSQL real, care acoperă și separarea datelor pe `workshop_id`.
- **AI:**
  - testele obișnuite folosesc `VisionExtractor` simulat;
  - separat, un set de schițe reale cu răspunsurile corecte și un script care măsoară precizia (cote în toleranță de câțiva mm, numărul de uși/sertare/polițe exact). Îl rulăm manual, pentru că fiecare rulare costă.
- **Frontend:** Vitest pentru logica tabelului și a suprascrierilor, plus un test Playwright pentru fluxul complet: corp manual → piese → debitare → export.

## 13. Structura de directoare

Tot proiectul stă în folderul `atelier-ai/` din repository-ul DevWorld.

```
atelier-ai/
├── CLAUDE.md
├── README.md
├── backend/
│   ├── pyproject.toml
│   ├── app/
│   │   ├── main.py
│   │   ├── settings.py
│   │   ├── domain/            # modele Pydantic, fără I/O
│   │   │   ├── units.py       # Decimal mm, rotunjire
│   │   │   ├── catalog.py     # SheetMaterial, EdgeBand, Hardware
│   │   │   ├── profile.py     # ConstructionProfile, CuttingSettings
│   │   │   ├── cabinet.py     # Cabinet, FrontZone
│   │   │   └── part.py        # Part, Placement, HardwareLine, PartOverride
│   │   ├── engines/
│   │   │   ├── bom/           # carcass.py, fronts.py, drawers.py, edges.py, hardware.py, overrides.py
│   │   │   ├── nesting/       # algoritm ghilotină, rezultate
│   │   │   └── export/        # xlsx, csv
│   │   ├── ingestion/         # vision_extractor.py, claude_extractor.py, prompts/, checks.py
│   │   ├── api/routes/
│   │   ├── db/                # modele SQLAlchemy, repository-uri, migrații Alembic
│   │   └── jobs/              # coada și procesul worker
│   └── tests/
│       ├── engines/
│       ├── api/
│       └── fixtures/          # schițe reale pentru evaluarea AI
├── frontend/
│   ├── app/
│   ├── components/            # bom-table/, cutting-plan/, viewer-3d/, cabinet-form/
│   └── lib/api/               # client generat din OpenAPI
├── docs/superpowers/
│   ├── specs/
│   └── plans/
└── docker-compose.yml         # postgres, minio, backend, worker, frontend
```

Workflow-urile GitHub Actions ale proiectului stau în `.github/workflows/atelier-ai-*.yml` la rădăcina repository-ului, cu filtre `paths:` pe `atelier-ai/**`.

## 14. Ordinea de implementare

Fiecare etapă primește propriul plan de implementare, în `docs/superpowers/plans/`.

1. **Schelet backend + modele de domeniu + motorul BOM**, cu B1 și B2 ca teste de referință.
2. **Optimizarea debitării + export Excel/CSV.**
3. **Persistență + API** (atelier implicit, catalog de bază).
4. **Frontend:** setări atelier, proiecte, corpuri introduse manual, tabel de piese, plan de debitare.
5. **Preview 3D.**
6. **Ingestia AI.**

Imediat după etapa 1 facem un **test rapid, de aruncat**, cu 3–5 schițe reale, ca să verificăm cât de bine citește modelul cotele. Rezultatul poate schimba prioritățile etapei 6.

## 15. Decizii luate și alternative respinse

| Decizie | Alternativa respinsă | Motiv |
|---|---|---|
| AI → corpuri parametrice → motor cu reguli fixe | AI → direct lista de piese | Calcule netestabile, greșeli aritmetice, 3D fără semnificația pieselor |
| Reguli de construcție ca opțiuni predefinite | Formule scrise de atelier; biblioteci de șabloane | Prea mult pentru MVP; motorul permite adăugarea lor ulterior |
| Algoritm propriu de debitare pe benzi | `rectpack`; optimizare exactă (OR-Tools) | Control asupra kerf-ului, fibrei și ordinii tăieturilor; timp de calcul previzibil |
| Lista de piese calculată la cerere | Lista de piese stocată | Mereu sincronizată; motorul este rapid |
| Coadă de sarcini în PostgreSQL | Redis + Celery | O componentă în minus în MVP |
| Fără autentificare în MVP, dar cu `workshop_id` peste tot | Autentificare din prima zi | Decizia proprietarului; adăugarea ulterioară schimbă o singură dependență |

## 16. Riscuri și întrebări deschise

- **Precizia AI pe schițe de mână:** cel mai mare risc. Îl verificăm prin testul rapid de după etapa 1.
- **Valorile implicite pentru sertare (§6.5)** sunt propuse și trebuie verificate cu un atelier real.
- **Procentul de foaie folosit** de algoritmul nostru față de programele comerciale: îl măsurăm pe proiectele de referință.
- **Fără autentificare**, aplicația nu are voie să fie publică.
