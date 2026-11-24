# Proba video (Faza 0 b): ghid pas cu pas

Acest ghid vă arată cum rulați proba video pe PC-ul cu Windows 11 și pe telefonul Samsung Galaxy A24. Proba măsoară cât de repede și cât de fluent afișează aplicația noastră mai multe camere deodată. Camerele sunt simulate de un program care rulează pe PC: `spikehub`. Toate rezultatele ajung singure într-un folder de pe PC. La final ni-l trimiteți.

Din versiunea `spike-video-v0.1.1`, pe fiecare dispozitiv apăsați un singur buton: «Rulează tot pentru acest dispozitiv»<!--app-->. Aplicația recunoaște singură dispozitivul, rulează scenariile lui în ordine și le sare pe cele care au deja rezultate complete în spikehub.

## Ce aveți nevoie

- PC-ul cu Windows 11, legat la routerul de acasă (prin Wi-Fi sau cablu), cu Chrome, Edge și Firefox.
- Telefonul Galaxy A24 și încărcătorul lui, pe Wi-Fi-ul aceluiași router.
- Aproximativ 3 ore și 30 de minute pentru ce a rămas după Chrome, în una sau mai multe sesiuni. Cât rulează, nu trebuie să stați lângă dispozitiv.
- Nimic de plătit și niciun cont nou. Nu vă cerem nicio parolă.

Textele între «» apar exact așa în programele de test. Textele între „” sunt ale Windows, Android sau ale browserului și pot diferi puțin, în funcție de versiune.

## Cât durează

| Unde | Scenarii, în această ordine | Durată aproximativă |
|---|---|---|
| PC: pregătire (pașii 1 și 2) | | 10 min |
| PC: Chrome | S10, S1, S2, S3-9, S3-16, S4, S7, S9 | gata pe 17.11.2026, deci 0 min |
| PC: Edge | S10, S1, S2, S3-9, S9 | 20 min |
| PC: Firefox | S10, S1, S2, S3-9, S9 | 25 min, cu verificarea pluginului |
| PC: ipcam-desktop | S10, S1, S2, S3-16, S4, S7, S9 | 55 min, cu instalarea |
| Telefon: Galaxy A24 | S10, S1, S2, S3-9, pauză de 10 min, S3-4, S4, S5, S6, S7, S8, S9 | 1 oră și 35 min, cu instalarea și pauza |
| Trimiteți rezultatele | | 5 min |

Aplicația ține minte ce s-a rulat: pe pagina ei, rândul «Rămase pe acest PC: …»<!--app--> arată dispozitivele PC-ului care mai au scenarii de rulat, iar rândul de dedesubt arată telefonul. Un scenariu care are deja rezultat complet arată «gata (rezultatul e în spikehub)»<!--app-->.

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

## Cum merge rularea automată

- La început aplicația pune singurele întrebări: cea a lui S10, «A apărut o cerere de permisiune (de exemplu pentru rețeaua locală)?»<!--app-->, și, dacă urmează S3, o cerere de acces la cameră. Răspundeți la S10 «Da»<!--app--> dacă browserul sau telefonul v-a cerut o permisiune pentru această pagină sau aplicație, altfel «Nu»<!--app-->. La cameră, permiteți: aplicația o pornește la 1 pixel, câteva secunde, ca să afle dacă video-ul se decodează hardware. Nimic nu se înregistrează și nimic nu se trimite.
- Apoi rularea merge singură. Pe PC: lăsați fereastra vizibilă (jumătate de ecran e de ajuns: Windows + ←) și nu blocați PC-ul. Lăsați PC-ul liber: fără jocuri, fără programe grele, fără o scanare antivirus pornită de dvs.
- Dacă fereastra e micșorată sau acoperită, scenariul în curs se întrerupe și rularea așteaptă. Când fereastra e din nou vizibilă, scenariul pornește din nou de la început, iar sus scrie de ce s-a întrerupt și «A pornit din nou de la început, iar rularea continuă.»<!--app--> La fel după o conexiune pierdută cu hub-ul. După 3 întreruperi ale aceluiași scenariu, rularea se oprește cu «Rularea s-a oprit: … s-a întrerupt de 3 ori, ultima dată pentru că …»<!--app--> și vă spune ce să faceți înainte să apăsați din nou butonul. Ce e gata nu se mai rulează.
- Un scenariu care eșuează se mai rulează o dată; dacă eșuează din nou, rularea trece mai departe și la final îl numește.
- La final apare «Gata: toate scenariile acestui dispozitiv au rezultate.»<!--app--> Atunci treceți la dispozitivul următor.
- «Rulează din nou tot»<!--app--> rulează toate scenariile dispozitivului, și pe cele care sunt deja gata. Folosiți-l doar dacă vă cerem noi.

## Dacă ceva nu merge

1. Faceți o captură de ecran. Pe PC: tastele Windows + Shift + S. Pe telefon: butonul de pornire și butonul de volum în jos, apăsate împreună.
2. Notați numele scenariului (de exemplu S3-9) și unde l-ați rulat (Chrome, Edge, Firefox, ipcam-desktop sau telefon).
3. Trimiteți-ne captura și numele în chat.
4. Treceți la dispozitivul următor. Nu trebuie să reparați nimic.

Cazuri pe care le puteți rezolva singur:

- **«Nu mă pot conecta la hub.»<!--app-->** Verificați, în ordine:
  - fereastra spikehub e deschisă pe PC, sau, în «ipcam-desktop»<!--desktop-->, chenarul hub-ului arată «Hub-ul rulează. Ce scrie el apare mai jos.»<!--app-->;
  - adresa din aplicație e scrisă exact ca în fereastra spikehub;
  - telefonul e pe același Wi-Fi ca PC-ul, nu pe rețeaua pentru oaspeți („Guest”);
  - dacă folosiți un VPN pe telefon sau pe PC, opriți-l pe durata probei.
- **«Hub-ul nu a răspuns la timp (timeout).»<!--app-->** De obicei e firewall-ul Windows. Deschideți „Setări” → „Rețea și internet” → rețeaua dvs. (Wi-Fi sau Ethernet) și alegeți la tipul profilului de rețea „Rețea privată”. Dacă ați apăsat „Anulare” în fereastra firewall-ului: în Start căutați „Permiteți o aplicație prin Paravanul de protecție Windows”, apăsați „Modificare setări”, găsiți spikehub și bifați coloana „Privat”.
- **«Conexiunea a fost refuzată (refused).»<!--app-->** spikehub nu rulează sau adresa are alt port. Porniți spikehub și verificați că adresa se termină cu `:7080`.
- **«Hub-ul a refuzat cererea (rejected): …»<!--app-->** spikehub e mai vechi decât aplicația: reluați pasul 1, ca să aveți spikehub-ul ultimei versiuni.
- **«întrerupt: conexiunea cu hub-ul s-a pierdut.»<!--app-->** Verificați că fereastra spikehub e deschisă și că PC-ul e pornit, apoi apăsați din nou «Rulează tot pentru acest dispozitiv»<!--app-->.
- **«Scoateți telefonul din încărcător înainte de S3.»<!--app-->** sau un rând cu **«telefonul era la încărcător»<!--app-->**. S3 se măsoară doar pe baterie. Scoateți încărcătorul și apăsați din nou «Rulează tot pentru acest dispozitiv»<!--app-->.
- **«Rezultate netrimise: …»<!--app-->** Rezultatele sunt păstrate în aplicație. Cu spikehub pornit, apăsați «Trimite acum»<!--app-->.
- **«Atenție: folderul de rezultate a atins limita»<!--hub-->** (în fereastra spikehub). spikehub nu mai salvează rezultate noi. Scrieți-ne în chat.

## Pauze

Puteți face pauză oricând, și în mijlocul unui dispozitiv. Închideți fereastra spikehub; rezultatele deja salvate rămân în folderul `results`. Data viitoare porniți din nou `spikehub.exe` și apăsați din nou butonul: aplicația continuă de unde ați rămas.

## Versiuni noi, direct din aplicație

De la v0.1.2, aplicațiile instalate («Proba ipcam»<!--android--> pe telefon și «ipcam-desktop»<!--desktop--> pe PC) verifică singure, la pornire și apoi la câteva ore, dacă am publicat o versiune nouă. Când există, sus în pagină scrie «Există o versiune nouă: … (aveți …).»<!--app--> și apare butonul «Actualizează acum»<!--app-->. Nu apăsați butonul în timpul unei rulări; atunci e oricum dezactivat.

- **Pe PC:** după «Se descarcă actualizarea…»<!--app-->, aplicația se închide, se instalează versiunea nouă și pornește din nou singură, în câteva secunde. Adresa hub-ului rămâne ținută minte.
- **Pe telefon:** aplicația descarcă versiunea nouă, apoi Android vă întreabă dacă o instalați: apăsați „Actualizați” (sau „Instalați”). Prima dată, Android poate cere permisiunea „Instalare aplicații necunoscute” pentru «Proba ipcam»<!--android-->: activați „Permiteți din această sursă”, reveniți în aplicație și apăsați din nou «Actualizează acum»<!--app-->.

Aplicația verifică fiecare fișier descărcat cu suma de control pe care GitHub o afișează pentru release; dacă nu se potrivește, nu instalează nimic și scrie «Actualizarea nu a reușit: …»<!--app-->. `spikehub` (arhiva `spikehub-windows.zip`) nu se actualizează singur: când o versiune nouă îl schimbă, vă scriem în chat.

## Pasul 1. Treceți la versiunea v0.1.2

1. Închideți fereastra spikehub, dacă e deschisă.
2. Pe PC, deschideți pagina de release „spike-video-v0.1.2”, pe care v-o trimitem în chat (o găsiți și la <https://github.com/AndreiBesliu/DevWorld/releases>). Descărcați `spikehub-windows.zip` și `ipcam-desktop-setup.exe`. Dacă browserul spune că fișierul „nu este descărcat frecvent”, alegeți „Păstrați” (eventual „Păstrați oricum”): programele de test nu sunt semnate digital, pentru că semnătura costă bani.
3. Clic dreapta pe `spikehub-windows.zip` → „Extragere totală”. La „Fișierele vor fi extrase în acest folder”, apăsați „Răsfoire” și alegeți folderul în care ați extras versiunea v0.1.0 (cel cu `spikehub.exe` și folderul `results`). Apăsați „Extragere”.
4. Windows anunță că folderul are deja fișiere cu aceleași nume. Alegeți „Înlocuiți fișierele din destinație”.
5. Nu ștergeți și nu mutați folderul `results`: rezultatele de până acum, cu cele din Chrome, rămân acolo și contează.

## Pasul 2. Porniți spikehub pe PC

1. Dublu clic pe `spikehub.exe`, în folderul de la pasul 1.
2. Windows poate afișa „Windows a protejat PC-ul”. Apăsați „Mai multe informații”, apoi „Rulați oricum”. Dacă nu apare „Rulați oricum” (de exemplu, mesajul pomenește „Smart App Control”), opriți-vă aici și scrieți-ne în chat.
3. Dacă „Securitate Windows” anunță „Amenințare găsită” și `spikehub.exe` dispare din folder, nu opriți antivirusul. Deschideți „Securitate Windows” → „Protecție împotriva virușilor și amenințărilor” → „Istoricul protecției”. Dacă intrarea numește `spikehub.exe` din folderul de la pasul 1, alegeți „Acțiuni” → „Restaurare”. Restaurați doar acest fișier. Dacă intrarea numește alt fișier sau nu sunteți sigur, nu restaurați nimic și scrieți-ne în chat.
4. Dacă firewall-ul Windows întreabă dacă permite accesul, bifați „Rețele private”, lăsați „Rețele publice” nebifat și apăsați „Permiteți accesul”. Fără acest pas, telefonul nu poate ajunge la PC.
5. Browserul implicit se deschide singur la pagina de test, `http://127.0.0.1:7080`, iar în fereastra spikehub scrie «Pagina de test s-a deschis în browserul implicit.»<!--hub-->
6. În fereastra spikehub apare și «Adresele acestui calculator în rețeaua locală:»<!--hub-->, cu una sau mai multe adrese de forma `http://…:7080`. Notați adresa de pe rândul cu Wi-Fi sau Ethernet: o veți scrie în aplicația de pe telefon. Dacă în loc de adrese apare «Nu am găsit nicio rețea privată activă pe acest calculator.»<!--hub-->, scrieți-ne în chat.
7. Mai jos apare «Cât timp această fereastră e deschisă, PC-ul nu intră în repaus.»<!--hub--> Ecranul se poate stinge, dar PC-ul rămâne pornit. Dacă în locul acelui rând apare «Atenție: spikehub nu poate împiedica intrarea PC-ului în repaus.»<!--hub-->, opriți repausul pe durata probei: „Setări” → „Sistem” → „Alimentare” → „Ecran și repaus” → la repaus alegeți „Niciodată”. După probă, readuceți setarea cum era.
8. Lăsați fereastra deschisă cât timp rulați scenarii. Rândul «Rezultatele se salvează în:»<!--hub--> arată folderul `results`. Pentru oprire, închideți fereastra (sau apăsați Ctrl+C, ca în «Opriți cu Ctrl+C.»<!--hub-->).

Dacă fereastra arată o eroare și «Apăsați Enter pentru a închide.»<!--hub-->, faceți o captură și scrieți-ne. Două cazuri au o cauză simplă:

- «nu pot citi fișierele media»<!--hub-->: ați pornit spikehub din arhivă sau ați mutat folderul `media`. Reluați pasul 1.3.
- «îl folosește alt program?»<!--hub-->: spikehub rulează deja, într-o altă fereastră.

## Pasul 3. Pe PC, în browsere

Pentru fiecare browser:

1. Deschideți în el `http://127.0.0.1:7080`. Browserul implicit e deja deschis acolo. Scrieți exact `127.0.0.1`, nu „localhost”: cu „localhost”, unele browsere se conectează mai greu. Lângă adresă, browserul arată ⓘ sau „Nesigur”: e normal, proba rulează doar în rețeaua dvs.
2. Apăsați «Rulează tot pentru acest dispozitiv»<!--app-->, răspundeți la întrebarea lui S10 și permiteți camera, dacă browserul o cere.
3. Lăsați fereastra vizibilă până apare «Gata: toate scenariile acestui dispozitiv au rezultate.»<!--app--> Apoi închideți browserul și treceți la următorul.

**Chrome.** L-ați rulat pe 17.11.2026. Dacă rândul «Rămase pe acest PC: …»<!--app--> nu mai numește Chrome, Chrome e gata și nu trebuie să faceți nimic în el. Dacă îl numește, apăsați butonul: aplicația rulează doar ce lipsește.

**Edge.** Ca mai sus.

**Firefox.** Firefox are nevoie de pluginul OpenH264, de la Cisco, ca să afișeze camerele. Aplicația verifică singură, înainte să pornească. Dacă pluginul lipsește, scrie «Firefox nu afișează încă H.264, pentru că îi lipsește pluginul OpenH264.»<!--app-->, nu pornește și arată pașii de mai jos:

1. Meniul ☰ → „Setări” → jos, în stânga, „Extensii și teme” → „Pluginuri”. Trebuie să apară pluginul OpenH264, furnizat de Cisco Systems.
2. Dacă apare ca dezactivat, deschideți meniul lui („…”) și alegeți să fie activat întotdeauna.
3. Dacă pluginul lipsește, lăsați Firefox deschis 5 minute, reporniți-l și apăsați din nou «Rulează tot pentru acest dispozitiv»<!--app-->.

La S9 pot apărea «profil video nesuportat»<!--app--> sau «calitate redusă: H.265 nesuportat»<!--app-->. Nu e o greșeală: exact asta verifică S9.

## Pasul 4. Aplicația ipcam-desktop pe PC

1. Închideți browserele. Dacă aveți instalată versiunea veche, dezinstalați întâi «ipcam-desktop»<!--desktop-->: „Setări” → „Aplicații” → „Aplicații instalate”.
2. Dublu clic pe `ipcam-desktop-setup.exe`. La „Windows a protejat PC-ul”, apăsați „Mai multe informații”, apoi „Rulați oricum”.
3. Instalatorul e în engleză. Apăsați „Next” până la „Install”, apoi „Finish”. Se deschide fereastra «Proba ipcam»<!--desktop-->; dacă nu, porniți din Start aplicația «ipcam-desktop»<!--desktop-->.
4. Închideți fereastra spikehub de la pasul 2, dacă e deschisă. De la v0.1.2, aplicația pornește ea spikehub, fără fereastră de terminal: ce scria spikehub în fereastra lui apare în aplicație.
5. Sus în aplicație, în chenarul «Hub-ul pe acest PC (spikehub)»<!--app-->, câmpul «Folderul spikehub (cel cu spikehub.exe și folderele media și results)»<!--app--> arată folderul în care aplicația caută spikehub. Dacă nu e folderul de la pasul 1, scrieți-l pe cel corect: în File Explorer, deschideți folderul, clic în bara de adresă, copiați și lipiți aici. Apăsați «Pornește hub-ul»<!--app-->.
6. Sub butoane apare «Hub-ul rulează. Ce scrie el apare mai jos.»<!--app-->, iar dedesubt rândurile lui spikehub, de exemplu «Adresele acestui calculator în rețeaua locală:»<!--hub-->. Dacă firewall-ul Windows întreabă ceva, faceți ca la pasul 2.4. Când spikehub e gata, aplicația scrie singură `http://127.0.0.1:7080` în câmpul «Adresa hub-ului (o vedeți în fereastra spikehub)»<!--app-->, dacă e gol, și verifică conexiunea. Dacă acolo e altă adresă, scrieți `http://127.0.0.1:7080`.
7. Apăsați «Rulează tot pentru acest dispozitiv»<!--app-->, ca în browsere, și lăsați fereastra vizibilă până la «Gata: …»<!--app-->.
8. Aplicația ține minte folderul: la următoarea pornire, pornește singură spikehub. Când închideți aplicația, se oprește și spikehub. Cu «Oprește hub-ul»<!--app--> îl opriți fără să închideți aplicația. Pentru telefon (pasul 5), lăsați aplicația deschisă, cu hub-ul pornit, sau porniți `spikehub.exe` ca la pasul 2.

Dacă în chenar scrie:

- «În acest folder nu este spikehub.exe. Scrieți folderul în care ați extras spikehub-windows.zip.»<!--app-->: corectați folderul și apăsați din nou «Pornește hub-ul»<!--app-->.
- «Hub-ul s-a oprit singur (cod …). Motivul e în ultimele rânduri de mai jos.»<!--app-->: citiți ultimele rânduri. Dacă scrie «îl folosește alt program?»<!--hub-->, fereastra spikehub de la pasul 2 e încă deschisă: închideți-o și apăsați din nou «Pornește hub-ul»<!--app-->. Altfel, faceți o captură și scrieți-ne.

## Pasul 5. Telefonul Galaxy A24

### Pregătire

1. Încărcați telefonul la cel puțin 90 %. Apoi scoateți-l din încărcător cu cel puțin 10 minute înainte să apăsați «Rulează tot pentru acest dispozitiv»<!--app-->, de exemplu înainte de instalare, ca S3-9 să înceapă cu telefonul rece. Rularea durează circa 1 oră și 30 de minute, toată pe baterie, inclusiv S4: nu conectați încărcătorul până la sfârșit. S3 se măsoară doar fără încărcător.
2. Opriți modul „Economisire energie” pe durata probei. Altfel, telefonul încetinește intenționat și rezultatele ies mai slabe.
3. Conectați telefonul la același Wi-Fi ca PC-ul, nu la rețeaua pentru oaspeți. Dacă aveți VPN pe telefon, opriți-l.

### Instalare

1. Dacă aveți aplicația veche «Proba ipcam»<!--android-->, dezinstalați-o: țineți apăsat pe pictograma ei → „Dezinstalare”. Versiunile până la v0.1.1 au avut fiecare altă semnătură, așa că Android nu instalează v0.1.2 peste ele. De la v0.1.2 semnătura rămâne aceeași, iar versiunile următoare vin din aplicație (vedeți „Versiuni noi, direct din aplicație”).
2. Pe telefon, deschideți în Chrome pagina de release „spike-video-v0.1.2” și apăsați pe `ipcam-spike.apk`. Dacă Chrome avertizează că fișierul poate fi periculos, alegeți „Descărcați oricum”.
3. Deschideți fișierul descărcat (din notificare sau din „Fișierele mele” → „Descărcări”). Dacă Android spune că nu are permisiunea să instaleze aplicații din această sursă, apăsați „Setări”, activați „Permiteți din această sursă”, reveniți și apăsați „Instalați”.
4. Play Protect poate avertiza că nu cunoaște aplicația. Apăsați „Mai multe detalii”, apoi „Instalați oricum”. Dacă instalarea e oprită de „Blocare automată” (Auto Blocker): „Setări” → „Securitate și confidențialitate” → „Blocare automată” → dezactivați. Instalați, apoi reactivați-o.

Avertismentele apar pentru că aplicația de test nu vine din Magazin Play și nu e semnată pentru publicare. Nu o vom publica.

### Rulare

1. Deschideți aplicația «Proba ipcam»<!--android-->. Rulați proba doar în ea, nu în browserul telefonului.
2. În câmpul «Adresa hub-ului (o vedeți în fereastra spikehub)»<!--app--> scrieți adresa notată la pasul 2.6, cea de forma `http://…:7080`, și apăsați «Verifică conexiunea»<!--app-->. Trebuie să apară «Conectat: …»<!--app-->. Nu scrieți `127.0.0.1`: acea adresă merge doar pe PC.
3. Verificați că telefonul e scos din încărcător (de cel puțin 10 minute, ca la pregătire) și apăsați «Rulează tot pentru acest dispozitiv»<!--app-->.
4. Răspundeți la întrebarea lui S10. Când Android cere acces la cameră, alegeți „Cât timp folosiți aplicația”: fără el, verdictul pentru telefon nu se poate da. Indicatorul verde al camerei poate apărea câteva secunde în colțul ecranului.
5. Așezați telefonul pe masă, nu în buzunar și nu pe o pernă, cu aplicația deschisă. Ecranul rămâne aprins. Aplicația numără cele 10 minute fără încărcător de când l-a văzut scos, cel mai devreme de când ați deschis-o. Dacă nu au trecut încă, înainte de S3-9 așteaptă și numără pe ecran: «Înainte de …, telefonul stă 10 minute fără încărcător, ca să nu fie cald: mai sunt …»<!--app-->. După S3-9 urmează o pauză de 10 minute, numărată pe ecran, ca telefonul să se răcească; rularea continuă singură.
6. **S5.** Telefonul sună și vibrează. Pe ecran scrie «Apăsați butonul Acasă și reveniți după ora … (cel puțin 2 minute).»<!--app-->, cu o oră a ceasului telefonului. Apăsați Acasă. Când ceasul arată ora afișată sau mai târziu, redeschideți «Proba ipcam»<!--android--> (din aplicațiile recente sau de pe pictograma ei). Aplicația măsoară singură cât durează până revine imaginea.
7. **S6.** Telefonul sună din nou. Apăsați «Continuă»<!--app-->: aplicația trece singură într-o fereastră mică, peste ecranul de start. Nu o atingeți. După 5 minute telefonul sună: atingeți fereastra mică și pictograma de mărire, ca să reveniți în aplicație.
8. Rularea continuă singură cu S7, S8 și S9, până la «Gata: toate scenariile acestui dispozitiv au rezultate.»<!--app-->

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
