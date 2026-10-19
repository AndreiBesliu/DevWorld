# Proba video (Faza 0 b): ghid pas cu pas

Acest ghid vă arată cum rulați proba video pe PC-ul cu Windows 11 și pe telefonul Samsung Galaxy A24. Proba măsoară cât de repede și cât de fluent afișează aplicația noastră mai multe camere deodată. Camerele sunt simulate de un program care rulează pe PC: `spikehub`. Toate rezultatele ajung singure într-un folder de pe PC. La final ni-l trimiteți.

## Ce aveți nevoie

- PC-ul cu Windows 11, legat la routerul de acasă (prin Wi-Fi sau cablu), cu Chrome, Edge și Firefox.
- Telefonul Galaxy A24 și încărcătorul lui, pe Wi-Fi-ul aceluiași router.
- Aproximativ 4 ore și 40 de minute în total, fără pasul opțional (camerele dvs. reale, încă 10 minute). Le puteți împărți în mai multe sesiuni, chiar în zile diferite.
- Nimic de plătit și niciun cont nou. Nu vă cerem nicio parolă.

Textele între «» apar exact așa în programele de test. Textele între „” sunt ale Windows, Android sau ale browserului și pot diferi puțin, în funcție de versiune.

## Cât durează

| Unde | Scenarii, în această ordine | Durată aproximativă |
|---|---|---|
| PC: pregătire (pașii 1 și 2) | | 15 min |
| PC: Chrome | S10, S1, S2, S3-9, S3-16, S4, S7, S9 | 1 oră și 5 min |
| PC: Edge | S10, S1, S2, S3-9, S9 | 20 min |
| PC: Firefox | S10, S1, S2, S3-9, S9 | 25 min, cu verificarea pluginului |
| PC: aplicația ipcam-desktop | S10, S1, S2, S3-16, S4, S7, S9 | 55 min, cu instalarea |
| Telefon: Galaxy A24 | S10, S1, S2, S3-9, pauză de 10 min, S3-4, S4, S5, S6, S7, S8, S9 | 1 oră și 35 min, cu instalarea și pauza |
| Trimiterea rezultatelor | | 5 min |
| **Total, fără pasul opțional** | | **4 ore și 40 min** |
| Opțional: camerele dvs. reale | | 10 min |

Folosiți tabelul și ca listă: bifați pe o foaie fiecare scenariu terminat. Aplicația nu ține minte ce ați rulat într-o sesiune anterioară.

## Ce face fiecare scenariu

| Scenariu | Ce face | Durată |
|---|---|---|
| S10 | Verifică tipul conexiunii și vă întreabă dacă ați văzut o cerere de permisiune. Rulează primul pe fiecare dispozitiv. | 1 min |
| S1 | Pornește aceeași cameră de 20 de ori și măsoară în cât timp apare prima imagine. | 1–2 min |
| S2 | Măsoară întârzierea imaginii față de cameră, de 100 + 100 de ori. | 1–2 min |
| S3-4, S3-9, S3-16 | Arată o grilă de 4, 9 sau 16 camere timp de 10 minute, apoi o cameră pe tot ecranul 2 minute. | 13 min |
| S4 | Schimbă paginile de camere și ecranul complet timp de 30 de minute; verifică stabilitatea. | 30 min |
| S5 | Aplicația stă cel puțin 2 minute în fundal; măsoară cât de repede revine imaginea. | 3–4 min |
| S6 | Video într-o fereastră mică (picture-in-picture) timp de 5 minute, doar pe telefon. | 6–7 min |
| S7 | Pornește o înregistrare de 20 de ori și sare în ea de 20 de ori. | 1 min |
| S8 | Măsoară calculul care va proteja parola din aplicație (Argon2id), de 10 ori. | 1 min |
| S9 | Încearcă două camere cu formate video mai rare (H.265 și H.264 High 5.1). | 1 min |

## Reguli pentru toate scenariile

- Rulați un singur scenariu odată, pe un singur dispozitiv.
- Pe fiecare dispozitiv, rulați întâi S10, înainte de orice scenariu care poate cere camera (S3). După ce o pagină sau o aplicație primește acces la cameră, browserul se conectează altfel, iar S10 nu mai măsoară ce trebuie.
- Cât rulează un scenariu, imaginile acoperă toată fereastra, iar sus scrie numele scenariului și «rulează… nu închideți ecranul»<!--app-->. Lăsați ecranul aprins și aplicația în față. Nu micșorați fereastra și nu o acoperiți cu alte ferestre. Excepție: la S5 și S6 scrie doar «rulează…»<!--app-->, iar aplicația vă spune ce să faceți.
- Lăsați PC-ul liber: fără jocuri, fără programe grele, fără o scanare antivirus pornită de dvs.
- Un scenariu terminat arată «gata în … s»<!--app-->.
- Dacă apare «întrerupt: …»<!--app-->, rezultatul nu intră în verdict. Textul spune de ce: de exemplu, ecranul s-a stins sau s-a pierdut conexiunea cu hub-ul. Apăsați «Repetă»<!--app-->.
- Dacă apare «eșuat: …»<!--app-->, faceți o captură de ecran și apăsați «Repetă»<!--app--> o dată. Dacă eșuează din nou, treceți la scenariul următor.

## Dacă ceva nu merge

1. Faceți o captură de ecran. Pe PC: tastele Windows + Shift + S. Pe telefon: butonul de pornire și butonul de volum în jos, apăsate împreună.
2. Notați numele scenariului (de exemplu S3-9) și unde l-ați rulat (Chrome, Edge, Firefox, ipcam-desktop sau telefon).
3. Trimiteți-ne captura și numele în chat.
4. Treceți la scenariul următor. Nu trebuie să reparați nimic.

Cazuri pe care le puteți rezolva singur:

- **«Nu mă pot conecta la hub.»<!--app-->** Verificați, în ordine:
  - fereastra spikehub e deschisă pe PC;
  - adresa din aplicație e scrisă exact ca în fereastra spikehub;
  - telefonul e pe același Wi-Fi ca PC-ul, nu pe rețeaua pentru oaspeți („Guest”);
  - dacă folosiți un VPN pe telefon sau pe PC, opriți-l pe durata probei.
- **«Hub-ul nu a răspuns la timp (timeout).»<!--app-->** De obicei e firewall-ul Windows. Deschideți „Setări” → „Rețea și internet” → rețeaua dvs. (Wi-Fi sau Ethernet) și alegeți la tipul profilului de rețea „Rețea privată”. Dacă la pasul 2 ați apăsat „Anulare” în fereastra firewall-ului: în Start căutați „Permiteți o aplicație prin Paravanul de protecție Windows”, apăsați „Modificare setări”, găsiți spikehub și bifați coloana „Privat”.
- **«Conexiunea a fost refuzată (refused).»<!--app-->** spikehub nu rulează sau adresa are alt port. Porniți spikehub și verificați că adresa se termină cu `:7080`.
- **«întrerupt: conexiunea cu hub-ul s-a pierdut.»<!--app-->** Verificați că fereastra spikehub e deschisă și că PC-ul e pornit, apoi apăsați «Repetă»<!--app-->.
- **«Scoateți telefonul din încărcător înainte de S3.»<!--app-->** sau **«întrerupt: telefonul e la încărcător.»<!--app-->** S3 se măsoară doar pe baterie. Scoateți încărcătorul și porniți din nou.
- **«Rezultate netrimise: …»<!--app-->** Rezultatele sunt păstrate în aplicație. Cu spikehub pornit, apăsați «Trimite acum»<!--app-->.
- **«Atenție: folderul de rezultate a atins limita»<!--hub-->** (în fereastra spikehub). spikehub nu mai salvează rezultate noi. Scrieți-ne în chat.

## Pauze

Puteți face pauză oricând între scenarii. Închideți fereastra spikehub; rezultatele deja salvate rămân în folderul `results`. Data viitoare porniți din nou `spikehub.exe` și continuați de unde ați rămas.

## Dacă primiți o versiune nouă

Dacă vă trimitem în chat o versiune mai nouă a aplicației de pe telefon (`ipcam-spike.apk`):

1. Întâi trimiteți rezultatele: cu spikehub pornit, verificați că în aplicație nu mai apare «Rezultate netrimise: …»<!--app-->. Dacă apare, apăsați «Trimite acum»<!--app-->.
2. Dezinstalați aplicația veche: țineți apăsat pe pictograma «Proba ipcam»<!--android--> → „Dezinstalare”.
3. Instalați noul APK, ca la pasul 5.

Fiecare versiune de test are o semnătură nouă, așa că Android nu o instalează peste cea veche. La fel, pentru un ipcam-desktop nou: trimiteți întâi rezultatele, dezinstalați «ipcam-desktop»<!--desktop--> din „Setări” → „Aplicații” → „Aplicații instalate”, apoi instalați-l pe cel nou.

## Pasul 1. Descărcați fișierele

1. Pe PC, deschideți pagina de release pe care v-o trimitem în chat. Ea se numește „spike-video-v0.1.0” (sau o versiune mai nouă) și se găsește și la <https://github.com/AndreiBesliu/DevWorld/releases>.
2. Descărcați pe PC:
   - `spikehub-windows.zip` (aproximativ 380 MB);
   - `ipcam-desktop-setup.exe`.
3. Browserul poate spune că fișierul „nu este descărcat frecvent”. Alegeți „Păstrați” (eventual „Păstrați oricum”). Programele de test nu sunt semnate digital, pentru că semnătura costă bani; de aceea apar avertismentele.
4. Fișierul `ipcam-spike.apk` îl descărcați direct pe telefon, la pasul 5. Restul fișierelor de pe pagină nu vă trebuie.

## Pasul 2. Porniți spikehub pe PC

1. Clic dreapta pe `spikehub-windows.zip` → „Extragere totală” → „Extragere”. Se deschide un folder cu `spikehub.exe`, folderul `media`, `spikehub-camere-reale.cmd` și `THIRD_PARTY_NOTICES.txt` (licențele componentelor; nu trebuie să faceți nimic cu el).
2. Porniți spikehub doar din acest folder, nu direct din arhivă: are nevoie de folderul `media` de lângă el.
3. Dublu clic pe `spikehub.exe`.
4. Windows poate afișa „Windows a protejat PC-ul”. Apăsați „Mai multe informații”, apoi „Rulați oricum”. Dacă nu apare „Rulați oricum” (de exemplu, mesajul pomenește „Smart App Control”), opriți-vă aici și scrieți-ne în chat.
5. Dacă „Securitate Windows” anunță „Amenințare găsită” și `spikehub.exe` dispare din folder, nu opriți antivirusul. Deschideți „Securitate Windows” → „Protecție împotriva virușilor și amenințărilor” → „Istoricul protecției”. Dacă intrarea numește `spikehub.exe` din folderul extras la pasul 2.1, alegeți „Acțiuni” → „Restaurare”. Restaurați doar acest fișier. Dacă intrarea numește alt fișier sau nu sunteți sigur, nu restaurați nimic și scrieți-ne în chat.
6. La prima pornire, firewall-ul Windows întreabă dacă permite accesul. Bifați „Rețele private”, lăsați „Rețele publice” nebifat și apăsați „Permiteți accesul”. Fără acest pas, telefonul nu poate ajunge la PC.
7. În fereastra spikehub apare «Adresele acestui calculator în rețeaua locală:»<!--hub-->, urmat de una sau mai multe adrese de forma `http://…:7080`. Notați adresa de pe rândul cu Wi-Fi sau Ethernet: o veți scrie în aplicația de pe telefon. Dacă sunt mai multe astfel de rânduri, încercați-le pe rând. Rândul care începe cu «Pe acest PC:»<!--hub--> dă adresa pentru browsere și pentru ipcam-desktop: `http://127.0.0.1:7080`. Dacă în loc de adrese apare «Nu am găsit nicio rețea privată activă pe acest calculator.»<!--hub-->, scrieți-ne în chat.
8. Mai jos apare «Cât timp această fereastră e deschisă, PC-ul nu intră în repaus.»<!--hub--> Ecranul PC-ului se poate stinge, dar PC-ul nu intră în repaus cât rulează spikehub, așa că telefonul ajunge la el și când nu lucrați la PC. PC-ul dvs. e un desktop; dacă folosiți vreodată un laptop, nu închideți capacul, pentru că atunci intră în repaus oricum.
9. Dacă în locul acelui rând apare «Atenție: spikehub nu poate împiedica intrarea PC-ului în repaus.»<!--hub-->, opriți repausul pe durata probei: „Setări” → „Sistem” → „Alimentare” → „Ecran și repaus” → la repaus alegeți „Niciodată”. După probă, readuceți setarea cum era.
10. Lăsați fereastra deschisă cât timp rulați scenarii. Rândul «Rezultatele se salvează în:»<!--hub--> arată folderul `results`, de lângă `spikehub.exe`.
11. Pentru oprire, închideți fereastra (sau apăsați Ctrl+C, ca în «Opriți cu Ctrl+C.»<!--hub-->).

Dacă fereastra arată o eroare și «Apăsați Enter pentru a închide.»<!--hub-->, faceți o captură și scrieți-ne. Două cazuri au o cauză simplă:

- «nu pot citi fișierele media»<!--hub-->: ați pornit spikehub din arhivă sau ați mutat folderul `media`. Reluați pasul 2.1.
- «îl folosește alt program?»<!--hub-->: spikehub rulează deja, într-o altă fereastră.

## Pasul 3. Pe PC, în browsere

### Chrome

1. Deschideți Chrome și scrieți în bara de adrese exact `http://127.0.0.1:7080`, apoi apăsați Enter. Nu scrieți „localhost”: cu „localhost”, unele browsere se conectează mai greu.
2. Se deschide pagina «Proba video ipcam (Faza 0 b)»<!--app-->. Câmpul «Adresa hub-ului (o vedeți în fereastra spikehub)»<!--app--> e deja completat.
3. Lângă adresă, Chrome arată ⓘ sau „Nesigur”. E normal: proba rulează doar în rețeaua dvs.
4. Apăsați «Verifică conexiunea»<!--app-->. Trebuie să apară «Conectat: …»<!--app-->.
5. Rulați întâi S10: apăsați «Pornește»<!--app--> pe rândul lui. Aplicația întreabă «A apărut o cerere de permisiune (de exemplu pentru rețeaua locală)?»<!--app-->. Răspundeți «Da»<!--app--> dacă browserul v-a cerut o permisiune pentru această pagină. Altfel, răspundeți «Nu»<!--app-->.
6. Apoi rulați pe rând, cu «Pornește»<!--app-->: S1, S2, S3-9, S3-16, S4, S7 și S9.
7. La S3-9 și S3-16, aplicația scrie «Lumina camerei se va aprinde pentru cel mult 10 secunde.»<!--app-->, iar browserul poate cere acces la cameră. Dacă PC-ul are o cameră web, permiteți. Motivul e explicat la pasul 5, la S3. Dacă PC-ul nu are cameră, nu apare nicio cerere.
8. La S9 pot apărea «profil video nesuportat»<!--app--> sau «calitate redusă: H.265 nesuportat»<!--app-->. Nu e o greșeală: exact asta verifică S9.

### Edge

1. Închideți Chrome. Deschideți Edge la aceeași adresă, `http://127.0.0.1:7080`.
2. Apăsați «Verifică conexiunea»<!--app-->.
3. Rulați întâi S10, apoi S1, S2, S3-9 și S9, ca în Chrome.

### Firefox

1. Închideți Edge. Firefox are nevoie de pluginul OpenH264, de la Cisco, ca să afișeze camerele. Firefox îl descarcă singur, la scurt timp după instalare. Verificați că îl are: meniul ☰ → „Setări” → jos, în stânga, „Extensii și teme” → „Pluginuri”. Trebuie să apară pluginul OpenH264, furnizat de Cisco Systems. Dacă apare ca dezactivat, deschideți meniul lui („…”) și alegeți să fie activat întotdeauna.
2. Dacă pluginul lipsește, lăsați Firefox deschis 5 minute, reporniți-l și verificați din nou.
3. Deschideți `http://127.0.0.1:7080` și apăsați «Verifică conexiunea»<!--app-->.
4. Rulați întâi S10, apoi S1, S2, S3-9 și S9, ca în Chrome.
5. Dacă apare «Hub-ul a refuzat cererea (rejected): …»<!--app-->, pluginul OpenH264 lipsește încă. Reluați punctul 1.

## Pasul 4. Aplicația ipcam-desktop pe PC

1. Închideți browserele. Dublu clic pe `ipcam-desktop-setup.exe`.
2. La „Windows a protejat PC-ul”, apăsați „Mai multe informații”, apoi „Rulați oricum”.
3. Instalatorul e în engleză. Apăsați „Next” până la „Install”, apoi „Finish”. La „Finish”, aplicația pornește de obicei singură și se deschide fereastra «Proba ipcam»<!--desktop-->.
4. Dacă fereastra nu s-a deschis, porniți din Start aplicația «ipcam-desktop»<!--desktop-->. Dacă fereastra «Proba ipcam»<!--desktop--> e deja deschisă, nu o mai porniți din Start: o a doua pornire doar aduce fereastra în față.
5. În câmpul «Adresa hub-ului (o vedeți în fereastra spikehub)»<!--app--> scrieți `http://127.0.0.1:7080`. Apăsați «Verifică conexiunea»<!--app-->. Aplicația ține minte adresa.
6. Rulați întâi S10 și răspundeți ca în Chrome. Apoi rulați S1, S2, S3-16, S4, S7 și S9. La S3-16, cererea pentru cameră e ca în Chrome.

## Pasul 5. Telefonul Galaxy A24

### Pregătire

1. Încărcați telefonul la cel puțin 60 %. Scoateți-l din încărcător cu cel puțin 10 minute înainte de S3-9, ca să nu fie cald.
2. Opriți modul „Economisire energie” pe durata probei. Altfel, telefonul încetinește intenționat și rezultatele ies mai slabe.
3. Recomandat: „Setări” → „Afișaj” → „Timp expirare ecran” → 10 minute, pe durata probei. Aplicația ține ecranul aprins; aceasta e o siguranță în plus.
4. Conectați telefonul la același Wi-Fi ca PC-ul, nu la rețeaua pentru oaspeți. Dacă aveți VPN pe telefon, opriți-l.

### Instalare

1. Pe telefon, deschideți în Chrome aceeași pagină de release și apăsați pe `ipcam-spike.apk`. Dacă Chrome avertizează că fișierul poate fi periculos, alegeți „Descărcați oricum”.
2. Deschideți fișierul descărcat (din notificare sau din „Fișierele mele” → „Descărcări”).
3. Android spune că nu are permisiunea să instaleze aplicații din această sursă. Apăsați „Setări”, activați „Permiteți din această sursă”, reveniți și apăsați „Instalați”.
4. Play Protect poate avertiza că nu cunoaște aplicația. Apăsați „Mai multe detalii”, apoi „Instalați oricum”. Dacă vă întreabă dacă trimiteți aplicația la scanare, puteți alege oricare variantă.
5. Dacă instalarea e oprită de „Blocare automată” (Auto Blocker): „Setări” → „Securitate și confidențialitate” → „Blocare automată” → dezactivați. Instalați, apoi reactivați-o.

Avertismentele apar pentru că aplicația de test nu vine din Magazin Play și nu e semnată pentru publicare. Nu o vom publica.

### Conectare

1. Deschideți aplicația «Proba ipcam»<!--android-->. Rulați scenariile doar în ea, nu în browserul telefonului.
2. În câmpul «Adresa hub-ului (o vedeți în fereastra spikehub)»<!--app--> scrieți adresa notată la pasul 2.7, cea de forma `http://…:7080`. Nu scrieți `127.0.0.1`: acea adresă merge doar pe PC.
3. Apăsați «Verifică conexiunea»<!--app-->. Trebuie să apară «Conectat: …»<!--app-->. Aplicația ține minte adresa.
4. Dacă telefonul cere o permisiune (de exemplu pentru rețeaua locală sau pentru dispozitivele din apropiere), permiteți și rețineți că a cerut-o: S10 vă întreabă.

### Scenarii, în această ordine

Telefonul stă scos din încărcător la toate scenariile, în afară de S4. Așezați-l pe masă, nu în buzunar și nu pe o pernă.

1. **S10.** Apăsați «Pornește»<!--app-->. Aplicația întreabă «A apărut o cerere de permisiune (de exemplu pentru rețeaua locală)?»<!--app-->. Răspundeți «Da»<!--app--> dacă telefonul v-a cerut până acum o permisiune pentru această aplicație (de exemplu pentru rețeaua locală sau pentru dispozitivele din apropiere). Altfel, răspundeți «Nu»<!--app-->.
2. **S1, S2.** Apăsați «Pornește»<!--app--> și așteptați «gata în … s»<!--app-->.
3. **S3-9, pe baterie, cu telefonul rece.** La începutul grilei și după ecranul complet, aplicația scrie «Lumina camerei se va aprinde pentru cel mult 10 secunde.»<!--app-->. Prima dată, Android cere acces la cameră: alegeți „Cât timp folosiți aplicația”. De ce: aplicația pornește camera la 1 pixel, de două ori, câte cel mult 10 secunde, pentru că doar așa sistemul spune dacă video-ul e decodat de cipul video al telefonului (hardware). Asta măsurăm. Nimic nu se înregistrează și nimic nu se trimite. Indicatorul verde al camerei poate apărea în colțul ecranului. Dacă refuzați, această măsurătoare lipsește și verdictul pentru telefon nu se poate da.
4. **Pauză de 10 minute.** Lăsați telefonul pe masă, cu ecranul stins și fără încărcător. Pauza lasă telefonul să se răcească după S3-9, ca S3-4 să înceapă tot de la rece.
5. **S3-4, pe baterie.** La fel ca S3-9.
6. **S4, la încărcător.** Conectați încărcătorul înainte să apăsați «Pornește»<!--app-->. Durează 30 de minute.
7. **S5.** Scoateți telefonul din încărcător și apăsați «Pornește»<!--app-->. Aplicația scrie «Apăsați butonul Acasă și reveniți după ora … (cel puțin 2 minute).»<!--app-->, cu o oră a ceasului telefonului, de exemplu 14:33. Apăsați Acasă (sau glisați în sus de la marginea de jos). Ecranul se poate stinge între timp. Când ceasul telefonului arată ora afișată sau mai târziu, dar nu mai mult de 5 minute după ea, redeschideți «Proba ipcam»<!--android--> (din aplicațiile recente sau de pe pictograma ei). Aplicația măsoară singură cât durează până revine imaginea. Dacă vă întoarceți prea devreme, apare «întrerupt: v-ați întors după …; e nevoie de cel puțin 2 minute.»<!--app-->: apăsați «Repetă»<!--app-->.
8. **S6.** Apăsați «Pornește»<!--app-->. Aplicația trece singură într-o fereastră mică, peste ecranul de start, iar video-ul rulează în ea. Uitați-vă la ceasul telefonului și nu atingeți nimic până când au trecut 7 minute pe ceas (de exemplu de la 14:30 la 14:37). Apoi atingeți fereastra mică și pictograma de mărire, ca să reveniți în aplicație. Starea trebuie să fie «gata în … s»<!--app-->. Dacă fereastra mică se închide mai devreme (de exemplu, ați atins-o din greșeală), apare «întrerupt: fereastra mică s-a închis după …; e nevoie de 5 minute.»<!--app-->: apăsați «Repetă»<!--app-->.
9. **S7, S8, S9.** Apăsați «Pornește»<!--app--> pe rând. La S9 pot apărea «profil video nesuportat»<!--app--> sau «calitate redusă: H.265 nesuportat»<!--app-->: e normal.

## Pasul 6 (opțional). Camerele dvs. reale

Scenariile folosesc doar camerele simulate. Acest pas verifică doar dacă spikehub se conectează la camerele dvs. și ce format video trimit ele. În acest pas spikehub nu pornește serverul de test, iar camerele dvs. nu apar în nicio aplicație.

1. Închideți fereastra spikehub.
2. În același folder, dublu clic pe `spikehub-camere-reale.cmd`. Dacă Windows întreabă dacă rulați fișierul, alegeți „Executare”.
3. Fereastra întreabă «Adresa RTSP a camerei … (nu se afișează ce tastați; Enter gol = gata):»<!--hub-->. Tastați adresa RTSP a unei camere, apoi apăsați Enter. O găsiți în aplicația sau în manualul camerei; începe de obicei cu `rtsp://`. Tastați-o direct în fereastră: nu o lipiți (Ctrl+V) și nu o scrieți întâi în alt program, de exemplu în Notepad. Nu apare nimic pe ecran cât tastați: e normal.
4. spikehub scrie «Verific camera …»<!--hub--> și, după câteva secunde (cel mult circa 20), un rând:
   - «Camera … este conectată: …»<!--hub-->, cu formatul video (H264 sau H265), profilul și rezoluția; sau
   - «Conectarea la camera … a eșuat: …»<!--hub-->, cu motivul. Dedesubt, spikehub spune ce să verificați, de exemplu «Verificați adresa (utilizator, parolă, port, cale) și …»<!--hub-->, iar rândul se încheie cu «…introduceți-o din nou sau apăsați Enter pentru camera următoare.»<!--hub--> Apoi spikehub cere din nou adresa aceleiași camere: «Adresa RTSP a camerei …, din nou (nu se afișează ce tastați; Enter gol = camera următoare):»<!--hub-->. Tastați adresa din nou, sau apăsați Enter fără să scrieți nimic: rândul cu eroarea rămâne în rezumat, iar spikehub trece la camera următoare. La ultima cameră (real-4) nu mai urmează alta, așa că rândul se încheie cu «…introduceți-o din nou sau apăsați Enter pentru a încheia.»<!--hub-->
5. Apoi spikehub cere adresa camerei următoare (real-2, real-3, real-4). La aceste întrebări, «Enter gol = gata»<!--hub--> înseamnă că ați terminat: după ultima cameră, apăsați Enter fără să scrieți nimic, iar spikehub nu mai cere alte camere.
6. spikehub scrie «Rezumat (trimiteți-ne aceste rânduri; nu conțin adrese):»<!--hub-->, urmat de câte un rând pentru fiecare cameră, cu rezultatul ultimei încercări: conectată sau eșuată. Faceți o captură a ferestrei și trimiteți-ne-o în chat. Dacă nu ați introdus nicio adresă, scrie «Nu ați introdus nicio adresă de cameră.»<!--hub-->
7. La final scrie «Apăsați Enter pentru a închide.»<!--hub--> Apăsați Enter: fereastra se închide singură. Dacă mai aveți scenarii de rulat, porniți din nou `spikehub.exe`.

Adresa RTSP conține de obicei utilizatorul și parola camerei. spikehub o ține doar în memorie, cât e deschisă fereastra: nu o afișează, nu o scrie în niciun fișier, nu o pune în rezultate și nu o trimite nicăieri. Nu ne-o trimiteți nici nouă. Dacă ați lipit-o totuși, apăsați Windows + V și alegeți „Ștergeți tot”, ca să dispară din istoricul clipboard-ului. Dacă Windows + V nu arată o listă, copiați orice alt text (de exemplu un cuvânt din acest ghid), ca adresa să nu mai rămână în clipboard. Dacă ați scris-o în Notepad, închideți acea filă fără să o salvați.

## Pasul 7. Trimiteți rezultatele

1. În fiecare aplicație și browser folosit, verificați că nu apare rândul «Rezultate netrimise: …»<!--app-->. Dacă apare, porniți spikehub și apăsați «Trimite acum»<!--app-->.
2. Închideți fereastra spikehub.
3. În folderul spikehub, clic dreapta pe folderul `results` → „Comprimați în fișier ZIP”. Dacă nu vedeți opțiunea: „Afișați mai multe opțiuni” → „Trimitere la” → „Folder comprimat (arhivat)”.
4. Trimiteți arhiva în chat. Sau încărcați-o în Google Drive-ul dvs., în folderul `ipcam-teste` (creați-l dacă nu există), și scrieți-ne în chat că e acolo.

Rezultatele conțin doar modelul dispozitivului, memoria lui, procesorul și placa video ale PC-ului, versiunile de Windows, Android și browser, măsurătorile și tipul conexiunii. Nu conțin adrese IP, numele rețelei Wi-Fi și nici adresele sau parolele camerelor dvs.

## După probă

După ce ne-ați trimis rezultatele, readuceți calculatorul și telefonul cum erau:

1. Dezinstalați «ipcam-desktop»<!--desktop--> („Setări” → „Aplicații” → „Aplicații instalate”) și «Proba ipcam»<!--android--> de pe telefon. Ștergeți folderul spikehub.
2. Scoateți regula de firewall a lui spikehub: în Start căutați „Permiteți o aplicație prin Paravanul de protecție Windows”, apăsați „Modificare setări”, selectați spikehub și apăsați „Eliminare”. Dacă spikehub apare pe mai multe rânduri, eliminați-le pe toate.
3. Pe telefon, luați înapoi permisiunea Chrome de a instala aplicații: „Setări” → „Aplicații” → Chrome → „Instalați aplicații necunoscute” → dezactivați „Permiteți din această sursă”.
4. Dacă ați dezactivat „Blocare automată” și nu ați reactivat-o, reactivați-o.
5. Dacă ați schimbat „Timp expirare ecran” sau „Economisire energie” pe telefon, readuceți-le cum erau.
6. Dacă ați trecut rețeaua PC-ului pe „Rețea privată” doar pentru probă, sau ați oprit repausul PC-ului, readuceți-le cum erau.
7. Dacă ați permis camera în browsere pentru `http://127.0.0.1:7080`, ștergeți permisiunea: clic pe pictograma de lângă adresă (în Chrome și Edge, ⓘ sau „Nesigur”) → „Resetați permisiunile”.
