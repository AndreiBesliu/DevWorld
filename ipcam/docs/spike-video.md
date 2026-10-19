# Proba video (Faza 0 b): ghid pas cu pas

Acest ghid vă arată cum rulați proba video pe PC-ul cu Windows 11 și pe telefonul Samsung Galaxy A24. Proba măsoară cât de repede și cât de fluent afișează aplicația noastră mai multe camere deodată. Camerele sunt simulate de un program care rulează pe PC: `spikehub`. Toate rezultatele ajung singure într-un folder de pe PC. La final ni-l trimiteți.

## Ce aveți nevoie

- PC-ul cu Windows 11, legat la routerul de acasă (prin Wi-Fi sau cablu), cu Chrome, Edge și Firefox.
- Telefonul Galaxy A24 și încărcătorul lui, pe Wi-Fi-ul aceluiași router.
- Aproximativ 4 ore și 20 de minute în total. Le puteți împărți în mai multe sesiuni, chiar în zile diferite.
- Nimic de plătit și niciun cont nou. Nu vă cerem nicio parolă.

Textele între «» apar exact așa în programele de test. Textele între „” sunt ale Windows, Android sau ale browserului și pot diferi puțin, în funcție de versiune.

## Cât durează

| Unde | Scenarii | Durată aproximativă |
|---|---|---|
| PC: pregătire (pașii 1 și 2) | | 15 min |
| PC: Chrome | S1, S2, S3-9, S3-16, S4, S7, S9, S10 | 1 oră |
| PC: Edge | S1, S2, S3-9, S9, S10 | 20 min |
| PC: Firefox | S1, S2, S3-9, S9, S10 | 20 min |
| PC: aplicația ipcam-desktop | S3-16, S4, S10 | 50 min, cu instalarea |
| Telefon: Galaxy A24 | S1, S2, S3-4, S3-9, S4, S5, S6, S7, S8, S9, S10 | 1 oră și 30 min, cu instalarea |
| Opțional: camerele dvs. reale | | 10 min |
| Trimiterea rezultatelor | | 5 min |

Folosiți tabelul și ca listă: bifați pe o foaie fiecare scenariu terminat. Aplicația nu ține minte ce ați rulat într-o sesiune anterioară.

## Ce face fiecare scenariu

| Scenariu | Ce face | Durată |
|---|---|---|
| S1 | Pornește aceeași cameră de 20 de ori și măsoară în cât timp apare prima imagine. | 1–2 min |
| S2 | Măsoară întârzierea imaginii față de cameră, de 100 + 100 de ori. | 1–2 min |
| S3-4, S3-9, S3-16 | Arată o grilă de 4, 9 sau 16 camere timp de 10 minute, apoi o cameră pe tot ecranul 2 minute. | 12 min |
| S4 | Schimbă paginile de camere și ecranul complet timp de 30 de minute; verifică stabilitatea. | 30 min |
| S5 | Aplicația stă 2 minute în fundal; măsoară cât de repede revine imaginea. | 3 min |
| S6 | Video într-o fereastră mică (picture-in-picture) timp de 5 minute, doar pe telefon. | 5 min |
| S7 | Pornește o înregistrare de 20 de ori și sare în ea de 20 de ori. | 1–2 min |
| S8 | Măsoară calculul care va proteja parola din aplicație (Argon2id), de 10 ori. | 1 min |
| S9 | Încearcă două camere cu formate video mai rare (H.265 și H.264 High 5.1). | 1 min |
| S10 | Verifică tipul conexiunii și vă întreabă dacă ați văzut o cerere de permisiune. | 1 min |

## Reguli pentru toate scenariile

- Rulați un singur scenariu odată, pe un singur dispozitiv.
- Cât rulează un scenariu, starea lui este «rulează… nu închideți ecranul». Lăsați ecranul aprins și aplicația în față. Nu micșorați fereastra și nu o acoperiți cu alte ferestre. Excepție: S5 și S6, unde aplicația vă spune ce să faceți.
- Lăsați PC-ul liber: fără jocuri, fără programe grele, fără o scanare antivirus pornită de dvs.
- Un scenariu terminat arată «gata în … s».
- Dacă apare «întrerupt: …», ecranul s-a stins sau aplicația a trecut în fundal. Apăsați «Repetă».

## Dacă ceva nu merge

1. Faceți o captură de ecran. Pe PC: tastele Windows + Shift + S. Pe telefon: butonul de pornire și butonul de volum în jos, apăsate împreună.
2. Notați numele scenariului (de exemplu S3-9) și unde l-ați rulat (Chrome, Edge, Firefox, ipcam-desktop sau telefon).
3. Trimiteți-ne captura și numele în chat.
4. Treceți la scenariul următor. Nu trebuie să reparați nimic.

Cazuri pe care le puteți rezolva singur:

- **«Nu mă pot conecta la hub.»** Verificați, în ordine:
  - fereastra spikehub e deschisă pe PC;
  - adresa din aplicație e scrisă exact ca în fereastra spikehub;
  - telefonul e pe același Wi-Fi ca PC-ul, nu pe rețeaua pentru oaspeți („Guest”);
  - dacă folosiți un VPN pe telefon sau pe PC, opriți-l pe durata probei.
- **«Hub-ul nu a răspuns la timp (timeout).»** De obicei e firewall-ul Windows. Deschideți „Setări” → „Rețea și internet” → rețeaua dvs. (Wi-Fi sau Ethernet) și alegeți la tipul profilului de rețea „Rețea privată”. Dacă la pasul 2 ați apăsat „Anulare” în fereastra firewall-ului: în Start căutați „Permiteți o aplicație prin Paravanul de protecție Windows”, apăsați „Modificare setări”, găsiți spikehub și bifați coloana „Privat”.
- **«Conexiunea a fost refuzată (refused).»** spikehub nu rulează sau adresa are alt port. Porniți spikehub și verificați că adresa se termină cu `:7080`.
- **«Rezultate netrimise: …»** Rezultatele sunt păstrate în aplicație. Cu spikehub pornit, apăsați «Trimite acum».
- **«eșuat: …»** Faceți captura și treceți mai departe.

## Pauze

Puteți face pauză oricând între scenarii. Închideți fereastra spikehub; rezultatele deja salvate rămân în folderul `results`. Data viitoare porniți din nou `spikehub.exe` și continuați de unde ați rămas.

## Pasul 1. Descărcați fișierele

1. Pe PC, deschideți pagina de release pe care v-o trimitem în chat. Ea se numește „spike-video-v0.1.0” (sau o versiune mai nouă) și se găsește și la <https://github.com/AndreiBesliu/DevWorld/releases>.
2. Descărcați pe PC:
   - `spikehub-windows.zip` (aproximativ 350 MB);
   - `ipcam-desktop-setup.exe`.
3. Browserul poate spune că fișierul „nu este descărcat frecvent”. Alegeți „Păstrați” (eventual „Păstrați oricum”). Programele de test nu sunt semnate digital, pentru că semnătura costă bani; de aceea apar avertismentele.
4. Fișierul `ipcam-spike.apk` îl descărcați direct pe telefon, la pasul 5. Restul fișierelor de pe pagină nu vă trebuie.

## Pasul 2. Porniți spikehub pe PC

1. Clic dreapta pe `spikehub-windows.zip` → „Extragere totală” → „Extragere”. Se deschide un folder cu `spikehub.exe`, folderul `media` și `spikehub-camere-reale.cmd`.
2. Porniți spikehub doar din acest folder, nu direct din arhivă: are nevoie de folderul `media` de lângă el.
3. Dublu clic pe `spikehub.exe`.
4. Windows poate afișa „Windows a protejat PC-ul”. Apăsați „Mai multe informații”, apoi „Rulați oricum”. Dacă nu apare „Rulați oricum” (de exemplu, mesajul pomenește „Smart App Control”), opriți-vă aici și scrieți-ne în chat.
5. La prima pornire, firewall-ul Windows întreabă dacă permite accesul. Bifați „Rețele private”, lăsați „Rețele publice” nebifat și apăsați „Permiteți accesul”. Fără acest pas, telefonul nu poate ajunge la PC.
6. În fereastra spikehub apare «Adresele acestui calculator în rețeaua locală:», urmat de una sau mai multe adrese de forma `http://…:7080`. Notați adresa de pe rândul cu Wi-Fi sau Ethernet: o veți scrie în telefon. Dacă sunt mai multe astfel de rânduri, încercați-le pe rând.
7. Lăsați fereastra deschisă cât timp rulați scenarii. Rândul «Rezultatele se salvează în:» arată folderul `results`, de lângă `spikehub.exe`.
8. Pentru oprire, închideți fereastra (sau apăsați Ctrl+C, ca în «Opriți cu Ctrl+C.»).

Dacă fereastra arată o eroare și «Apăsați Enter pentru a închide.», faceți o captură și scrieți-ne. Două cazuri au o cauză simplă:

- «nu pot citi fișierele media»: ați pornit spikehub din arhivă sau ați mutat folderul `media`. Reluați pasul 2.1.
- «îl folosește alt program?»: spikehub rulează deja, într-o altă fereastră.

## Pasul 3. Pe PC, în browsere

### Chrome

1. Deschideți Chrome și scrieți în bara de adrese exact `http://127.0.0.1:7080`, apoi apăsați Enter. Nu scrieți „localhost”: cu „localhost”, unele browsere se conectează mai greu.
2. Se deschide pagina «Proba video ipcam (Faza 0 b)». Câmpul «Adresa hub-ului (o vedeți în fereastra spikehub)» e deja completat.
3. Lângă adresă, Chrome scrie „Nesigur”. E normal: proba rulează doar în rețeaua dvs.
4. Apăsați «Verifică conexiunea». Trebuie să apară «Conectat: …».
5. Rulați pe rând, cu butonul «Pornește»: S1, S2, S3-9, S3-16, S4, S7, S9 și S10.
6. La S3-9 și S3-16, browserul poate cere acces la cameră. Dacă PC-ul are o cameră web, permiteți. Motivul e explicat la pasul 5, la S3. Dacă PC-ul nu are cameră, nu apare nicio cerere.
7. La S9 pot apărea «profil video nesuportat» sau «calitate redusă: H.265 nesuportat». Nu e o greșeală: exact asta verifică S9.
8. La S10, aplicația întreabă «A apărut o cerere de permisiune (de exemplu pentru rețeaua locală)?». Răspundeți «Da» dacă browserul v-a cerut pentru această pagină o altă permisiune decât camera. Altfel, răspundeți «Nu».

### Edge

1. Închideți Chrome. Deschideți Edge la aceeași adresă, `http://127.0.0.1:7080`.
2. Apăsați «Verifică conexiunea».
3. Rulați S1, S2, S3-9, S9 și S10, ca în Chrome.

### Firefox

1. Închideți Edge. Firefox are nevoie de pluginul OpenH264, de la Cisco, ca să afișeze camerele. Firefox îl descarcă singur, la scurt timp după instalare. Verificați că îl are: meniul ☰ → „Setări” → jos, în stânga, „Extensii și teme” → „Pluginuri”. Trebuie să apară pluginul OpenH264, furnizat de Cisco Systems. Dacă apare ca dezactivat, deschideți meniul lui („…”) și alegeți să fie activat întotdeauna.
2. Dacă pluginul lipsește, lăsați Firefox deschis 5 minute, reporniți-l și verificați din nou.
3. Deschideți `http://127.0.0.1:7080` și apăsați «Verifică conexiunea».
4. Rulați S1, S2, S3-9, S9 și S10, ca în Chrome.
5. Dacă apare «Hub-ul a refuzat cererea (rejected): …», pluginul OpenH264 lipsește încă. Reluați punctul 1.

## Pasul 4. Aplicația ipcam-desktop pe PC

1. Închideți browserele. Dublu clic pe `ipcam-desktop-setup.exe`.
2. La „Windows a protejat PC-ul”, apăsați „Mai multe informații”, apoi „Rulați oricum”.
3. Instalatorul e în engleză. Apăsați „Next” până la „Install”, apoi „Finish”.
4. Porniți din Start aplicația «ipcam-desktop». Se deschide fereastra «Proba ipcam».
5. În câmpul «Adresa hub-ului (o vedeți în fereastra spikehub)» scrieți `http://127.0.0.1:7080`. Apăsați «Verifică conexiunea». Aplicația ține minte adresa.
6. Rulați S3-16, S4 și S10. La S10 răspundeți ca în Chrome.

## Pasul 5. Telefonul Galaxy A24

### Pregătire

1. Încărcați telefonul la cel puțin 60 %.
2. Opriți modul „Economisire energie” pe durata probei. Altfel, telefonul încetinește intenționat și rezultatele ies mai slabe.
3. Recomandat: „Setări” → „Afișaj” → „Timp expirare ecran” → 10 minute, pe durata probei. Aplicația ține ecranul aprins; aceasta e o siguranță în plus.
4. Conectați telefonul la același Wi-Fi ca PC-ul, nu la rețeaua pentru oaspeți. Dacă aveți VPN pe telefon, opriți-l.

### Instalare

1. Pe telefon, deschideți în Chrome aceeași pagină de release și apăsați pe `ipcam-spike.apk`. Dacă Chrome avertizează că fișierul poate fi periculos, alegeți „Descărcați oricum”.
2. Deschideți fișierul descărcat (din notificare sau din „Fișierele mele” → „Descărcări”).
3. Android spune că nu are permisiunea să instaleze aplicații din această sursă. Apăsați „Setări”, activați „Permiteți din această sursă”, reveniți și apăsați „Instalați”.
4. Play Protect poate avertiza că nu cunoaște aplicația. Apăsați „Mai multe detalii”, apoi „Instalați oricum”. Dacă vă întreabă dacă trimiteți aplicația la scanare, puteți alege oricare variantă.
5. Dacă instalarea e oprită de „Blocare automată” (Auto Blocker): „Setări” → „Securitate și confidențialitate” → „Blocare automată” → dezactivați. Instalați, apoi o puteți reactiva.

Avertismentele apar pentru că aplicația de test nu vine din Magazin Play și nu e semnată pentru publicare. Nu o vom publica.

### Conectare

1. Deschideți aplicația «Proba ipcam».
2. În câmpul «Adresa hub-ului (o vedeți în fereastra spikehub)» scrieți adresa notată la pasul 2.6, cea de forma `http://…:7080`. Nu scrieți `127.0.0.1`: acea adresă merge doar pe PC.
3. Apăsați «Verifică conexiunea». Trebuie să apară «Conectat: …». Aplicația ține minte adresa.
4. Dacă telefonul cere o permisiune (de exemplu pentru rețeaua locală sau pentru dispozitivele din apropiere), permiteți și rețineți că a cerut-o: S10 vă întreabă.

### Scenarii, în această ordine

Telefonul stă scos din încărcător la toate scenariile, în afară de S4. Așezați-l pe masă, nu în buzunar și nu pe o pernă.

1. **S1, S2.** Apăsați «Pornește» și așteptați «gata în … s».
2. **S3-4 și S3-9, pe baterie.** La început, aplicația scrie «Lumina camerei se va aprinde.», iar Android cere acces la cameră. Alegeți „Cât timp folosiți aplicația”. De ce: aplicația ține camera pornită la 1 pixel cât durează testul, pentru că doar așa sistemul spune dacă video-ul e decodat de cipul video al telefonului (hardware). Asta măsurăm. Nimic nu se înregistrează și nimic nu se trimite. Indicatorul verde al camerei poate apărea în colțul ecranului. Dacă refuzați, scenariul merge mai departe, dar fără această măsurătoare.
3. **S4, la încărcător.** Conectați încărcătorul înainte să apăsați «Pornește». Durează 30 de minute.
4. **S5.** Scoateți telefonul din încărcător. Aplicația scrie «Apăsați butonul Acasă, așteptați 2 minute, reveniți.». Apăsați Acasă (sau glisați în sus de la marginea de jos), așteptați cel puțin 2 minute, dar nu mai mult de 5, apoi redeschideți «Proba ipcam». Folosiți un ceas, de exemplu pe cel al PC-ului.
5. **S6.** Aplicația trece singură într-o fereastră mică, peste ecranul de start. Nu atingeți nimic timp de 5 minute. Apoi atingeți fereastra mică și pictograma de mărire, ca să reveniți în aplicație. Starea trebuie să fie «gata în … s».
6. **S7, S8, S9.** Apăsați «Pornește» pe rând. La S9 pot apărea «profil video nesuportat» sau «calitate redusă: H.265 nesuportat»: e normal.
7. **S10.** Aplicația întreabă «A apărut o cerere de permisiune (de exemplu pentru rețeaua locală)?». Răspundeți «Da» dacă telefonul v-a cerut, oricând în această aplicație, o altă permisiune decât camera. Altfel, răspundeți «Nu».

## Pasul 6 (opțional). Camerele dvs. reale

Scenariile folosesc doar camerele simulate. Acest pas verifică doar dacă spikehub se conectează la camerele dvs. și ce format video trimit ele.

1. Închideți fereastra spikehub.
2. În același folder, dublu clic pe `spikehub-camere-reale.cmd`. Dacă Windows întreabă dacă rulați fișierul, alegeți „Executare”.
3. Fereastra întreabă «Adresa RTSP a camerei … (nu se afișează ce tastați; Enter gol = gata):». Scrieți sau lipiți (Ctrl+V) adresa RTSP a unei camere, apoi apăsați Enter. O găsiți în aplicația sau în manualul camerei; începe de obicei cu `rtsp://`. Nu apare nimic pe ecran cât scrieți: e normal.
4. Repetați pentru încă cel mult 3 camere. La final, apăsați Enter fără să scrieți nimic.
5. Pentru fiecare cameră, spikehub scrie «Camera … este conectată», cu formatul video (H264 sau H265), sau «Conectarea la camera … a eșuat». Poate apărea și avertismentul «nu a trimis niciun cadru video decodabil».
6. Scrieți-ne în chat ce a afișat spikehub pentru fiecare cameră, de exemplu „real-1: conectată, H264”. Aceste rânduri nu conțin adrese.
7. Închideți fereastra. Dacă mai aveți scenarii de rulat, porniți din nou `spikehub.exe`.

Adresa RTSP conține de obicei utilizatorul și parola camerei. O scrieți doar în fereastra spikehub, pe PC-ul dvs. Ea rămâne doar în memoria PC-ului cât timp fereastra e deschisă: nu se scrie în niciun fișier, nu apare în rezultate și nu se trimite nicăieri. Nu ne-o trimiteți nici nouă.

## Pasul 7. Trimiteți rezultatele

1. În fiecare aplicație și browser folosit, verificați că nu apare rândul «Rezultate netrimise: …». Dacă apare, porniți spikehub și apăsați «Trimite acum».
2. Închideți fereastra spikehub.
3. În folderul spikehub, clic dreapta pe folderul `results` → „Comprimați în fișier ZIP”. Dacă nu vedeți opțiunea: „Afișați mai multe opțiuni” → „Trimitere la” → „Folder comprimat (arhivat)”.
4. Trimiteți arhiva în chat. Sau încărcați-o în Google Drive-ul dvs., în folderul `ipcam-teste` (creați-l dacă nu există), și scrieți-ne în chat că e acolo.

Rezultatele conțin doar modelul dispozitivului, versiunile de Windows, Android și browser, măsurătorile și tipul conexiunii. Nu conțin adrese IP, numele rețelei Wi-Fi și nici adresele sau parolele camerelor dvs.

## După probă

După ce ne-ați trimis rezultatele, puteți dezinstala «ipcam-desktop» și «Proba ipcam» și puteți șterge folderul spikehub. Dacă ați schimbat „Timp expirare ecran”, „Economisire energie” sau „Blocare automată”, readuceți-le cum erau.
