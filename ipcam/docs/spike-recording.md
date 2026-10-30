# Proba de înregistrare (Faza 0 c1): ghid pas cu pas

Acest ghid vă arată cum rulați proba de înregistrare pe PC-ul cu Windows 11, pe Raspberry Pi și pe telefonul Samsung Galaxy A24. Proba verifică dacă înregistrarea camerelor rezistă la opriri bruște și la pene de curent, cât timp rulează fără oprire, câte camere poate înregistra fiecare dispozitiv și cât de repede se citesc și se redau înregistrările criptate. Camerele sunt simulate de un program al probei; nu folosim camerele dvs. reale. Toate rezultatele ajung la final într-un singur folder de pe PC, pe care ni-l trimiteți.

Textele între «» apar exact așa în programele probei. Textele între „” sunt ale Windows, Android sau Raspberry Pi OS și pot diferi puțin, în funcție de versiune. Nimic de plătit și niciun cont nou.

În comenzile de mai jos, `<adresa PC-ului>` și `<adresa Pi-ului>` înseamnă adresa dispozitivului respectiv în rețeaua de acasă. Pasul 5 vă arată cum o aflați. Scrieți adresa fără `<` și `>`.

## 1. Ce aveți nevoie și cât durează

Aveți nevoie de:

- PC-ul cu Windows 11 și Raspberry Pi-ul, legate la același router (cu cablu, dacă se poate), cu internet, ca să-și potrivească ceasul;
- telefonul Galaxy A24, pe Wi-Fi-ul aceluiași router;
- spațiu liber pe disc, cât scrie mai jos la MI2 și la MI3;
- o copie a cardului SD sau a SSD-ului Pi-ului (pasul 4).

Proba are mai multe părți. Fiecare dispozitiv le face pe rând; un dispozitiv e „gazda” probei (cel măsurat), iar celălalt e „controlorul” (pasul 5).

| Partea | Ce face | Cât durează |
|---|---|---|
| Pregătirea (pașii 2–5) | descărcarea, pornirea automată, copia cardului | 45 min, o singură dată |
| Fereastra automată (MI1, pasul 6) | 42 de opriri bruște ale înregistrării, pe fiecare dispozitiv: două seturi de câte 20 de rulări, plus o rulare în plus pentru fiecare set. Pe PC, jumătate dintre opriri sunt ecrane albastre (o eroare de sistem provocată intenționat), după care PC-ul repornește singur. | ≈ 6 h și 30 min pe dispozitiv, nesupravegheat, în orele alese de dvs. |
| Tăierile reale (pasul 7) | 5 tăieri ale alimentării pe fiecare dispozitiv, făcute de dvs. când programul vă cere | ≈ 2 h și 30 min în total |
| MI2 (pasul 8) | înregistrare fără oprire, pe ambele dispozitive deodată, fiecare pornit tot timpul | 72 h |
| MI3 (pasul 9) | numărul de camere pe care îl duce fiecare dispozitiv | 1 h pe dispozitiv, plus pașii (dacă ora nu trece), plus încă 1 h la 16 camere pe un PC cu 6 nuclee sau mai multe |
| MI4 (pasul 10) | citiri din înregistrări criptate, doar pe Pi | ≈ 15 min |
| MI9 (pasul 11) | redarea pe celălalt dispozitiv a unui segment de 5 minute înregistrat pe gazdă (pe Pi se folosește cel de la MI4, dacă a rămas) | ≈ 15 min pe dispozitiv |
| Telefonul (pasul 12) | viteza de decriptare în aplicație | ≈ 10 min |
| Rezultatele (pasul 13) | colectarea pe PC și trimiterea | ≈ 15 min |

Spațiul liber pentru MI2: de cel puțin 2 × N × camere × 4 Mbps, unde N e numărul de ore pe care proba le păstrează (între 1 și 6). Cu N = 1 h, minimul, înseamnă:

- PC-ul și un Pi 5 (8 camere): cel puțin 29 GB;
- un Pi 4 sau 400 (4 camere): cel puțin 15 GB;
- un Pi 3, Pi 2 sau Zero 2 W (2 camere): cel puțin 8 GB.

Pentru fereastra întreagă de 6 h: ≈ 173, 87 și 44 GB. recspike scrie cifra exactă pentru dispozitivul dvs. când pornește MI2 (pasul 8).

Ordinea recomandată: pregătirea, ferestrele automate (de exemplu noaptea), tăierile reale, MI3, MI4, MI9, telefonul, apoi MI2 (72 h), rezultatele și, la final, ștergerea pornirii automate. Le puteți împărți în mai multe zile.

Dacă Pi-ul dvs. este armv6 (pasul 3 vă spune), el nu poate fi măsurat: e doar controlorul probelor PC-ului, iar toate părțile cu Pi-ul ca gazdă se sar.

## 2. Descărcarea, SmartScreen și firewall-ul

1. Pe PC, deschideți pagina de release pe care v-o trimitem în chat. Ea se numește „spike-recording-v0.1.0” (sau o versiune mai nouă) și se găsește și la <https://github.com/AndreiBesliu/DevWorld/releases>.
2. Descărcați `spike-recording-windows.zip`. Dacă browserul spune că fișierul „nu este descărcat frecvent”, alegeți „Păstrați” (eventual „Păstrați oricum”). Programele probei nu sunt semnate digital, pentru că semnătura costă bani; de aceea apar avertismentele.
3. Clic dreapta pe `spike-recording-windows.zip` → „Proprietăți”. Dacă jos apare caseta „Deblocare”, bifați-o și apăsați „OK”. Astfel Windows nu mai întreabă de fiecare program în parte.
4. Clic dreapta pe arhivă → „Extragere totală”. În câmpul cu folderul de destinație scrieți `C:\proba-inregistrare` și apăsați „Extragere”. Folderul conține `recspike.exe`, `reccontroller.exe`, `ipcam-hub.exe`, `mediamtx.exe`, folderul `media` și `THIRD_PARTY_NOTICES.txt` (licențele componentelor; nu trebuie să faceți nimic cu el). Nu mutați și nu redenumiți folderul după pasul 4: pornirea automată îl caută aici.
5. Programele probei se pornesc dintr-o fereastră de comenzi deschisă ca administrator: în Start scrieți `cmd`, clic dreapta pe „Linie de comandă” → „Executare ca administrator”, apoi „Da”. În fereastră scrieți `cd /d C:\proba-inregistrare` și apăsați Enter. În acest ghid, „fereastra de administrator” înseamnă o astfel de fereastră, deschisă în folderul probei. Pentru două programe deodată, deschideți două ferestre.
6. Windows poate afișa „Windows a protejat PC-ul”. Apăsați „Mai multe informații”, apoi „Rulați oricum”. Dacă nu apare „Rulați oricum” (de exemplu, mesajul pomenește „Smart App Control”), opriți-vă aici și scrieți-ne în chat.
7. Dacă „Securitate Windows” anunță „Amenințare găsită” și un program dispare din folder, nu opriți antivirusul. Deschideți „Securitate Windows” → „Protecție împotriva virușilor și amenințărilor” → „Istoricul protecției”. Dacă intrarea numește un program din `C:\proba-inregistrare`, alegeți „Acțiuni” → „Restaurare”. Restaurați doar acel fișier. Dacă intrarea numește alt fișier sau nu sunteți sigur, nu restaurați nimic și scrieți-ne în chat.
8. Când `reccontroller.exe` pornește prima dată, firewall-ul Windows întreabă dacă permite accesul. Bifați „Rețele private”, lăsați „Rețele publice” nebifat și apăsați „Permiteți accesul”. Aceeași întrebare apare pentru `recspike.exe` la prima pornire a probei MI3 sau MI9, care deschid conexiuni video cu celălalt dispozitiv; răspundeți la fel. `ipcam-hub.exe` și `mediamtx.exe` nu au nevoie de firewall.
9. Dacă un program spune mai târziu că celălalt dispozitiv nu răspunde (de exemplu, «controlorul nu primește semnalul UDP al gazdei»<!--recspike-->), de obicei e firewall-ul Windows. Deschideți „Setări” → „Rețea și internet” → rețeaua dvs. (Wi-Fi sau Ethernet) și alegeți la tipul profilului de rețea „Rețea privată”. Dacă ați apăsat „Anulare” în fereastra firewall-ului: în Start căutați „Permiteți o aplicație prin Paravanul de protecție Windows”, apăsați „Modificare setări”, găsiți `reccontroller` (sau `recspike`) și bifați coloana „Privat”.

Fișierele pentru Raspberry Pi le descărcați direct pe Pi, la pasul 3, iar `ipcam-spike.apk` direct pe telefon, la pasul 12. Restul fișierelor de pe pagină nu vă trebuie.

## 3. Pe Pi: rulați întâi `recspike-probe`

Pi-urile au procesoare diferite, iar `recspike-probe` vă spune ce arhivă folosește al dvs.

1. Deschideți un terminal pe Pi: pe ecranul lui („Terminal” din meniu) sau de pe PC, dintr-o fereastră de comenzi, cu `ssh <utilizator>@<adresa Pi-ului>`, unde `<utilizator>` e numele contului dvs. de pe Pi.
2. Scrieți, pe rând:

   ```
   mkdir -p ~/proba-inregistrare && cd ~/proba-inregistrare
   wget https://github.com/AndreiBesliu/DevWorld/releases/download/spike-recording-v0.1.0/recspike-probe-linux-armv6
   chmod +x recspike-probe-linux-armv6
   ./recspike-probe-linux-armv6
   ```

   Dacă release-ul are alt nume decât `spike-recording-v0.1.0`, folosiți-l pe acela în adresă, aici și mai jos.
3. Programul scrie «Modelul: …»<!--probe--> și «Procesorul: …»<!--probe-->, apoi una dintre:
   - «Pe acest Pi folosiți arhiva …»<!--probe-->, cu numele arhivei: `spike-recording-linux-arm64.tar.gz` sau `spike-recording-linux-armv7.tar.gz`. Descărcați-o și despachetați-o în același folder, de exemplu pentru arm64:

     ```
     wget https://github.com/AndreiBesliu/DevWorld/releases/download/spike-recording-v0.1.0/spike-recording-linux-arm64.tar.gz
     tar -xzf spike-recording-linux-arm64.tar.gz
     ```

     Folderul conține acum `recspike`, `reccontroller`, `ipcam-hub`, `mediamtx`, folderul `media` și `THIRD_PARTY_NOTICES.txt`.
   - «Acest Raspberry Pi este armv6, pe care proba nu îl poate măsura.»<!--probe--> Pi-ul dvs. poate fi doar controlorul, cât testați PC-ul, iar măsurătorile Pi-ului trec în Faza 0-bis, o etapă ulterioară. Programul scrie și «Am notat în rezultate că Pi-ul este armv6.»<!--probe-->: păstrați folderul, pentru că rezultatele pleacă la final spre PC (pasul 13). Descărcați controlorul pentru armv6 și fișierele video pe care le servește:

     ```
     wget https://github.com/AndreiBesliu/DevWorld/releases/download/spike-recording-v0.1.0/reccontroller-linux-armv6
     wget https://github.com/AndreiBesliu/DevWorld/releases/download/spike-recording-v0.1.0/spike-recording-linux-armv7.tar.gz
     chmod +x reccontroller-linux-armv6
     tar -xzf spike-recording-linux-armv7.tar.gz media
     ```

     Pe un Pi armv6, oriunde ghidul scrie `sudo ./reccontroller`, folosiți `./reccontroller-linux-armv6`, fără `sudo`, și săriți tot ce are Pi-ul ca gazdă.
4. Dacă programul spune că procesorul nu este al unui Raspberry Pi, opriți-vă și scrieți-ne în chat.

De fiecare dată când deschideți un terminal nou pe Pi, scrieți întâi `cd ~/proba-inregistrare`.

## 4. Pregătirea, o singură dată

### Pe PC

1. Descărcați NotMyFault de pe pagina Microsoft Sysinternals: <https://learn.microsoft.com/sysinternals/downloads/notmyfault>. Despachetați arhiva și copiați `notmyfault64.exe` în `C:\proba-inregistrare`, lângă `recspike.exe`. E programul Microsoft care provoacă ecranul albastru din fereastra automată; nu avem voie să-l punem noi în arhivă.
2. Întrerupeți actualizările Windows pe durata probei: „Setări” → „Windows Update” → „Întrerupeți actualizările” (alegeți cea mai lungă perioadă). O actualizare care repornește PC-ul în timpul probei ar strica rulările.
3. În fereastra de administrator, scrieți `recspike.exe install`. Dacă ați pornit-o dintr-o fereastră obișnuită, apare «Windows cere acum permisiunea de administrator»<!--recspike-->: răspundeți „Da”. Programul instalează pornirea automată: după fiecare repornire din fereastra automată și după fiecare tăiere reală, proba continuă singură. Pe desktop apare scurtătura «Proba înregistrare – tăieri reale»<!--recspike-->, folosită la pasul 7.
4. Tot la instalare, recspike schimbă o setare a Windows și o spune: «Serviciul de timp Windows pornește acum automat»<!--recspike-->. Serviciul de timp Windows pornește automat, iar declanșatoarele care îl pot opri sunt scoase, ca ceasul PC-ului să se sincronizeze după fiecare repornire. La pasul 14, `recspike.exe uninstall` pune la loc ambele setări, exact cum erau.
5. Cât timp PC-ul e controlorul (pasul 5), `reccontroller.exe` nu-l lasă să intre în repaus. Dacă PC-ul intră totuși în repaus în timpul probei, sau dacă controlorul scrie «Atenție: reccontroller nu poate împiedica intrarea PC-ului în repaus»<!--reccontroller-->, opriți repausul pe durata probei: „Setări” → „Sistem” → „Alimentare” → „Ecran și repaus” → la repaus alegeți „Niciodată”. După probă, readuceți setarea cum era (pasul 14).

### Pe Pi (nu și pe un Pi armv6)

1. Faceți o copie a cardului SD sau a SSD-ului Pi-ului, de exemplu cu Win32 Disk Imager pe PC (butonul „Read”). Fereastra automată oprește Pi-ul brusc de 21 de ori, iar tăierile reale îi taie alimentarea; copia vă protejează sistemul.
2. În terminal, în folderul probei, scrieți `sudo ./recspike install`. `sudo` poate cere parola contului dvs. de pe Pi. Trebuie să apară «Pornirea automată este instalată»<!--recspike-->.
3. Pe Pi, porniți programele probei mereu cu `sudo`, cum scrie în ghid: `sudo ./recspike …` și `sudo ./reccontroller …`. Pornirea automată rulează ca administrator (root), iar rezultatele trebuie să rămână ale lui.

### Cardul SD și modelul Pi-ului

- Dacă Pi-ul înregistrează pe un card SD, la prima măsurătoare recspike afișează «Cardul SD se uzează repede la înregistrare continuă»<!--recspike--> și cere confirmarea: «Scrieți DA și apăsați Enter»<!--recspike-->. Scrieți `DA` și apăsați Enter. Confirmarea se păstrează în rezultate. Fără ea, proba nu înregistrează pe card.
- Dacă recspike scrie «Modelul acestui Raspberry Pi nu este în tabelul probei.»<!--recspike-->, apăsați Enter (o cameră) și scrieți-ne în chat modelul.

## 5. Care dispozitiv e gazda și care e controlorul

- **Gazda** e dispozitivul măsurat: pe ea rulează `recspike`, care înregistrează camerele simulate.
- **Controlorul** e celălalt dispozitiv: pe el rulează `reccontroller`, care trimite camerele simulate, măsoară ce s-a pierdut și vă arată când să tăiați alimentarea.
- Când testați PC-ul, Pi-ul e controlorul. Când testați Pi-ul, PC-ul e controlorul. Un singur dispozitiv e gazdă odată (excepție: MI2, la pasul 8).
- Ambele dispozitive trebuie să aibă ceasul sincronizat prin internet. Pe PC: „Setări” → „Oră și limbă” → „Dată și oră”, cu ora setată automat, apoi „Sincronizați acum”. Pe Pi, ceasul se sincronizează singur; după fiecare repornire, recspike așteaptă sincronizarea cel mult 10 minute.

Cum porniți controlorul:

- pe PC, într-o fereastră de administrator: `reccontroller.exe`;
- pe Pi, în terminal: `sudo ./reccontroller` (pe un Pi armv6: `./reccontroller-linux-armv6`).

Controlorul scrie «reccontroller rulează …»<!--reccontroller--> și «Rezultatele se păstrează în …»<!--reccontroller-->. Pe PC scrie și «Cât timp această fereastră este deschisă, PC-ul nu intră în repaus.»<!--reccontroller-->: ecranul PC-ului se poate stinge, dar PC-ul nu intră în repaus cât rulează controlorul (dacă intră totuși, vedeți pasul 4.5). Lăsați fereastra deschisă cât rulează proba pe celălalt dispozitiv. Îl opriți cu Ctrl+C. Pe un dispozitiv poate rula un singur controlor: altfel apare «nu pot asculta pe porturile probei (rulează deja un reccontroller?)»<!--reccontroller-->.

**Când PC-ul e gazda, nu porniți controlorul Pi-ului dintr-o fereastră `ssh` deschisă de pe PC.** La ecranele albastre și la tăierile reale, fereastra `ssh` dispare odată cu PC-ul și poate opri controlorul de pe Pi, cu tot ce măsurase. Porniți controlorul de pe ecranul și tastatura Pi-ului. Dacă Pi-ul nu are ecran, porniți-l din `ssh` desprins de fereastră:

```
sudo setsid -f ./reccontroller > reccontroller-ecran.txt 2>&1
```

Pe un Pi armv6: `setsid -f ./reccontroller-linux-armv6 > reccontroller-ecran.txt 2>&1`. Apoi puteți închide fereastra `ssh`: controlorul rulează mai departe. Ce scrie el vedeți cu `tail -f reccontroller-ecran.txt` (Ctrl+C închide doar vizualizarea, nu și controlorul). Îl opriți cu `sudo pkill -x reccontroller` (pe un Pi armv6: `pkill -f reccontroller-linux-armv6`).

Cum aflați adresele:

- PC-ul: într-o fereastră de comenzi scrieți `ipconfig`. La adaptorul Wi-Fi sau Ethernet folosit, rândul „Adresă IPv4” are patru numere separate prin puncte: aceea e `<adresa PC-ului>`.
- Pi-ul: în terminal scrieți `hostname -I`. Primul grup de patru numere separate prin puncte e `<adresa Pi-ului>`.

Adresele se pot schimba după o repornire a routerului. Dacă un program nu se mai conectează, verificați-le din nou.

## 6. Fereastra automată, pe fiecare dispozitiv

Fereastra automată oprește brusc înregistrarea de 42 de ori pe fiecare dispozitiv și măsoară ce se pierde. Rulează doar în fereastra de ore aleasă de dvs., de exemplu noaptea, când nu folosiți dispozitivele. Ora se scrie ca `HH:MM-HH:MM`, de exemplu `22:00-06:00`. Fiecare dispozitiv are două seturi, pe rând: întâi `kill9` (oprește doar programul de înregistrare), apoi `bugcheck` pe PC (ecran albastru) sau `sysrq` pe Pi (repornire bruscă).

### PC-ul ca gazdă

1. Pe Pi, porniți controlorul de pe ecranul Pi-ului sau desprins de `ssh`, ca la pasul 5, nu într-o fereastră `ssh` de pe PC: `sudo ./reccontroller` (pe un Pi armv6: `./reccontroller-linux-armv6`).
2. Pe PC, în fereastra de administrator: `recspike.exe mi1 --controller <adresa Pi-ului> --window 22:00-06:00 --kind kill9`. Până la începutul ferestrei, recspike scrie «Aștept fereastra …»<!--recspike-->. Lăsați fereastra deschisă. Setul e gata când apare «Proba … are toate rulările.»<!--recspike-->.
3. Apoi: `recspike.exe mi1 --controller <adresa Pi-ului> --window 22:00-06:00 --kind bugcheck`. La fiecare rulare, PC-ul afișează un ecran albastru și repornește. E intenționat: nu atingeți nimic. După repornire, proba continuă singură, chiar dacă nu vă conectați în Windows. Pe Pi, controlorul scrie după fiecare rulare dacă a fost «validă»<!--reccontroller--> sau nu.

### Pi-ul ca gazdă (nu și pe un Pi armv6)

1. Pe PC, porniți controlorul: `reccontroller.exe` în fereastra de administrator.
2. Pe Pi: `sudo ./recspike mi1 --controller <adresa PC-ului> --window 22:00-06:00 --kind kill9`, până la «Proba … are toate rulările.»<!--recspike-->.
3. Apoi: `sudo ./recspike mi1 --controller <adresa PC-ului> --window 22:00-06:00 --kind sysrq`. Pi-ul repornește brusc la fiecare rulare și continuă singur. Rulările le urmăriți în fereastra controlorului de pe PC.

### Bine de știut

- Când fereastra se încheie înaintea setului, apare «Fereastra … s-a încheiat: proba … continuă în fereastra următoare»<!--recspike-->. În ziua următoare, porniți din nou aceeași comandă; setul continuă de unde a rămas.
- O rulare care nu contează (de exemplu, controlorul a trimis neuniform) se reface singură. Dacă trei rulări la rând nu contează, recspike se oprește cu un mesaj care conține «rulări la rând nu au contat»<!--recspike-->: verificați că fereastra controlorului e deschisă și că rețeaua merge, apoi porniți din nou aceeași comandă.
- Dacă recspike scrie «Proba nu pornește: …»<!--recspike-->, motivul e pe același rând; vedeți pasul 15.
- Un set început trebuie terminat înaintea altuia: recspike vă spune care e în curs.
- La `bugcheck` și `sysrq`, ultimele rulări se încheie după o repornire, fără nicio fereastră deschisă pe gazdă. Ca să verificați că setul e gata, porniți din nou aceeași comandă, cu controlorul pornit: dacă e gata, recspike scrie «Proba … are toate rulările.»<!--recspike--> și se oprește.

## 7. Cele 5 tăieri reale pe fiecare dispozitiv

Aici dvs. tăiați alimentarea, când programul vă cere. Cererea apare la un moment ales la întâmplare, între 3 și 12 minute după ce înregistrarea a pornit: «Scoateți acum alimentarea»<!--reccontroller-->. Apare în fereastra controlorului și, când PC-ul e gazda, și în fereastra recspike de pe PC.

Când apare cererea, în cel mult 2 minute:

- pe PC-ul desktop: scoateți cablul de alimentare al PC-ului din priză;
- pe un laptop (dacă folosiți vreodată unul): țineți apăsat butonul de pornire cel puțin 10 secunde, până se stinge; bateria l-ar ține pornit dacă scoateți doar încărcătorul;
- pe Pi: scoateți cablul de alimentare al Pi-ului.

Așteptați 10 secunde, puneți alimentarea la loc și porniți dispozitivul (PC-ul poate cere apăsarea butonului de pornire).

Nu tăiați înainte de cerere și nici mai târziu de 2 minute după ea. O astfel de tăiere nu contează, iar proba o reface în același loc, cu aceleași setări. Dacă nu tăiați deloc, după 2 minute apare «Alimentarea nu a fost tăiată în 2 minute: tăierea … se reface.»<!--recspike-->, iar proba o reface.

### PC-ul ca gazdă

1. Pe Pi, porniți controlorul de pe ecranul Pi-ului sau desprins de `ssh`, ca la pasul 5, nu într-o fereastră `ssh` de pe PC: `sudo ./reccontroller` (pe un Pi armv6: `./reccontroller-linux-armv6`).
2. Pe PC, în fereastra de administrator: `recspike.exe mi1 --controller <adresa Pi-ului> --real`.
3. Când apare cererea, tăiați alimentarea PC-ului.
4. După repornire, Windows termină singur rularea întreruptă. Apoi deschideți de pe desktop scurtătura «Proba înregistrare – tăieri reale»<!--recspike--> și răspundeți „Da”. Dacă verificarea de după repornire nu s-a terminat, fereastra scrie întâi «Proba termină verificarea de după repornire. Așteptați…»<!--recspike-->. Apoi urmează următoarea tăiere.
5. Repetați până apare «Proba … are toate cele 5 tăieri valide.»<!--recspike-->.

### Pi-ul ca gazdă (nu și pe un Pi armv6)

1. Pe PC, porniți controlorul: `reccontroller.exe`.
2. Pe Pi: `sudo ./recspike mi1 --controller <adresa PC-ului> --real`.
3. Cererea apare în fereastra controlorului de pe PC. Tăiați alimentarea Pi-ului.
4. După repornire, Pi-ul continuă singur, iar următoarea cerere apare din nou pe PC. Fiecare tăiere se încheie, în fereastra controlorului, cu un rând «validă»<!--reccontroller--> sau «nevalidă»<!--reccontroller-->. Proba e gata după 5 tăieri valide.
5. Ca să verificați, după a cincea tăiere validă, porniți din nou pe Pi aceeași comandă: dacă e gata, recspike scrie «Proba … are toate cele 5 tăieri valide.»<!--recspike--> și se oprește.

## 8. MI2 (72 h)

MI2 înregistrează fără oprire 72 de ore, pe ambele dispozitive deodată. Fiecare dispozitiv își are propriul controlor, pornit cu `-local`, așa că nu depind unul de altul. Lăsați-le pornite tot timpul.

Pe PC, în două ferestre de administrator:

```
reccontroller.exe -local
recspike.exe mi2 --controller 127.0.0.1
```

Pe Pi (nu și pe un Pi armv6), în două terminale:

```
sudo ./reccontroller -local
sudo ./recspike mi2 --controller 127.0.0.1
```

La pornire, recspike scrie cât spațiu îi trebuie: «Proba de … h înregistrează …»<!--recspike-->, «Sunt necesari cel puțin … GB liberi»<!--recspike--> și «Spațiu liber aici: …»<!--recspike-->, apoi «Proba MI2 … începe: …»<!--recspike-->. Dacă spațiul nu ajunge, scrie «Spațiul liber nu ajunge pentru o fereastră de cel puțin 1 h.»<!--recspike-->: eliberați spațiu și porniți din nou. La final scrie «Proba MI2 … s-a încheiat după … h.»<!--recspike-->.

- Dacă folosiți Pi-ul prin `ssh` de pe PC, lăsați ferestrele `ssh` deschise toate cele 72 de ore: închiderea lor oprește proba de pe Pi.
- Dacă un dispozitiv repornește în timpul MI2 (o pană de curent, de exemplu), porniți din nou pe el ambele comenzi: întâi `reccontroller` cu `-local`, apoi `recspike mi2`. Rularea întreruptă nu contează și se reface de la început.

## 9. MI3 (1 h)

MI3 verifică câte camere duce fiecare dispozitiv: înregistrează numărul recomandat de camere, cu vizionări live, o redare, un export și un upload în paralel, timp de o oră. Dacă ora nu trece, continuă cu pași de 15 minute la mai puține camere și confirmă numărul găsit cu încă o oră. Pe un PC cu 6 nuclee sau mai multe, la final urmează încă o oră la 16 camere: «Fiindcă acest PC are 6 nuclee sau mai multe, la final urmează încă o oră la 16 camere, informativă.»<!--recspike-->. Lăsați dispozitivul pornit și nefolosit până la final.

Spațiu liber necesar (un pas își șterge înregistrările înaintea următorului):

- PC-ul: cel puțin 17 GB, sau 32 GB dacă are 6 nuclee sau mai multe;
- un Pi 5: cel puțin 17 GB;
- un Pi 4 sau 400: cel puțin 10 GB;
- un Pi 3, Pi 2 sau Zero 2 W: cel puțin 6 GB.

Fără spațiul unui pas, recspike nu îl pornește și scrie că pasul nu a pornit, cu «eliberați spațiu»<!--recspike-->.

- **PC-ul ca gazdă.** Pe Pi: `sudo ./reccontroller` (pe un Pi armv6: `./reccontroller-linux-armv6`). Pe PC, în fereastra de administrator: `recspike.exe mi3 --controller <adresa Pi-ului>`. Dacă firewall-ul întreabă de `recspike.exe`, răspundeți ca la pasul 2.
- **Pi-ul ca gazdă** (nu și pe un Pi armv6). Pe PC: `reccontroller.exe`. Pe Pi: `sudo ./recspike mi3 --controller <adresa PC-ului>`.

Proba începe cu «Proba MI3 începe: …»<!--recspike--> și se încheie cu «Proba MI3 s-a încheiat: rezultatul este …»<!--recspike-->.

## 10. MI4 (Pi)

Doar pe Pi (nu și pe un Pi armv6). Pe PC, porniți controlorul: `reccontroller.exe`. Pe Pi:

```
sudo ./recspike mi4 --controller <adresa PC-ului>
```

`sudo` e necesar, pentru că proba golește memoria cache a discului înaintea fiecărei serii de citiri. Fără `sudo`, recspike refuză cu «MI4 trebuie pornit cu sudo»<!--recspike-->. Proba durează în jur de 15 minute: «Proba MI4 începe: …»<!--recspike-->, apoi «Proba MI4 s-a încheiat: rezultatul este …»<!--recspike-->.

## 11. MI9 pe LAN

MI9 înregistrează un segment pe gazdă și îl redă pe celălalt dispozitiv prin rețea. Rulați-o o dată pe fiecare dispozitiv ca gazdă, întâi pe Pi, apoi pe PC. Adresa de după `--controller` e mereu a celuilalt dispozitiv.

1. **Pi-ul ca gazdă** (nu și pe un Pi armv6). Pe PC: `reccontroller.exe`. Pe Pi: `sudo ./recspike mi9 --lan --controller <adresa PC-ului>`.
2. **PC-ul ca gazdă.** Opriți controlorul de pe PC (Ctrl+C). Pe Pi: `sudo ./reccontroller` (pe un Pi armv6: `./reccontroller-linux-armv6`). Pe PC, în fereastra de administrator: `recspike.exe mi9 --lan --controller <adresa Pi-ului>`. Dacă firewall-ul întreabă de `recspike.exe`, răspundeți ca la pasul 2.

Fiecare durează în jur de 15 minute și se încheie cu «Proba MI9 s-a încheiat: rezultatul este …»<!--recspike-->. Cu un Pi armv6, rulați doar pasul 2.

## 12. Pasul de pe telefon

1. Pe PC, porniți controlorul: `reccontroller.exe`. El primește rezultatul telefonului.
2. Dacă pe telefon aveți deja aplicația «Proba ipcam»<!--android--> de la proba video și ea arată «Rezultate netrimise: …»<!--app-->, porniți `spikehub` ca în ghidul probei video și apăsați «Trimite acum»<!--app-->. Apoi dezinstalați aplicația veche: țineți apăsat pe pictograma «Proba ipcam»<!--android--> → „Dezinstalare”. Fiecare versiune de test are o semnătură nouă, așa că Android nu o instalează peste cea veche.
3. Pe telefon, deschideți în Chrome pagina de release și apăsați pe `ipcam-spike.apk`. Dacă Chrome avertizează că fișierul poate fi periculos, alegeți „Descărcați oricum”. Deschideți fișierul descărcat. La „Setări” activați „Permiteți din această sursă”, reveniți și apăsați „Instalați”. Dacă Play Protect avertizează, apăsați „Mai multe detalii”, apoi „Instalați oricum”. Dacă instalarea e oprită de „Blocare automată”: „Setări” → „Securitate și confidențialitate” → „Blocare automată” → dezactivați, instalați, apoi reactivați-o.
4. Deschideți «Proba ipcam»<!--android-->. În câmpul «Adresa hub-ului (o vedeți în fereastra spikehub)»<!--app--> scrieți `http://<adresa PC-ului>:7080`. Aici „hub-ul” e controlorul de pe PC; adresa nu apare în fereastra lui, ci o aflați ca la pasul 5.
5. Nu apăsați «Verifică conexiunea»<!--app-->: controlorul nu are camerele pe care le verifică butonul și ar apărea o eroare. Apăsați direct «Pornește»<!--app--> pe rândul «Viteză agerange (MI4)»<!--app-->.
6. Așteptați «gata în … s»<!--app-->. Dacă apare «Rezultate netrimise: …»<!--app-->, verificați că fereastra controlorului e deschisă pe PC și apăsați «Trimite acum»<!--app-->.

## 13. Rezultatele

La final, PC-ul adună rezultatele ambelor dispozitive și vă spune ce lipsește.

1. Opriți orice controlor deschis pe PC (Ctrl+C în fereastra lui).
2. Pe PC, într-o fereastră de administrator: `reccontroller.exe pull`. Apare «Aștept rezultatele Pi-ului: …»<!--reccontroller-->.
3. Pe Pi: `sudo ./recspike push --controller <adresa PC-ului>`. `sudo` e necesar, pentru că rezultatele Pi-ului sunt ale administratorului (root); fără el, recspike refuză cu «rezultatele sunt ale lui root, fiindcă proba a rulat cu sudo»<!--recspike-->. Pe un Pi armv6: `./recspike-probe-linux-armv6 --push <adresa PC-ului>`. Pi-ul scrie «Am trimis rezultatele.»<!--recspike-->.
4. Pe PC apare «Am pus rezultatele Pi-ului în …»<!--reccontroller-->, apoi «Prezente: …»<!--reccontroller--> și fie «Nu lipsește nimic.»<!--reccontroller-->, fie «Lipsesc: …»<!--reccontroller-->. Cu un Pi armv6, PC-ul scrie întâi «Modelul Pi-ului nu este suportat»<!--reccontroller--> și nu mai așteaptă rezultate cu Pi-ul ca gazdă.
5. Dacă lipsește ceva, refaceți pasul care îl produce, apoi reluați acest pas:

   | Ce lipsește | Ce refaceți |
   |---|---|
   | `MI1 pc-kill9 (pc)`, `MI1 pc-bugcheck (pc)` | pasul 6, PC-ul ca gazdă, setul numit |
   | `MI1 pi-kill9 (pi)`, `MI1 pi-sysrq (pi)` | pasul 6, Pi-ul ca gazdă, setul numit |
   | `MI1 real-pc (pc)`, `MI1 real-pi (pi)` | pasul 7, pe dispozitivul numit |
   | `MI2 (pc)`, `MI2 (pi)` | pasul 8, pe dispozitivul numit |
   | `MI3 (pc)`, `MI3 (pi)` | pasul 9, pe dispozitivul numit |
   | `MI4 (pi)` | pasul 10 |
   | `MI9 lan (pc)`, `MI9 lan (pi)` | pasul 11, pe dispozitivul numit |
   | `AGE` | pasul 12 |

6. Când nu mai lipsește nimic, deschideți `C:\proba-inregistrare` și faceți clic dreapta pe folderul `results` → „Comprimați în fișier ZIP”. Dacă nu vedeți opțiunea: „Afișați mai multe opțiuni” → „Trimitere la” → „Folder comprimat (arhivat)”. Trimiteți doar această arhivă: e a PC-ului și cuprinde și rezultatele Pi-ului.
7. Trimiteți arhiva în chat. Sau încărcați-o în Google Drive-ul dvs., în folderul `ipcam-teste` (creați-l dacă nu există), și scrieți-ne în chat că e acolo.

Rezultatele numesc dispozitivele doar `pc` și `pi` și conțin modelul Pi-ului, sistemul de operare, tipul de stocare și măsurătorile. Nu conțin adrese IP, numele rețelei, numele dispozitivelor sau numere de serie.

## 14. Cum opriți testul și ștergeți pornirea automată

După ce ne-ați trimis rezultatele:

1. Pe PC, în fereastra de administrator: `recspike.exe uninstall`. Trebuie să apară «Pornirea automată și scurtătura de pe desktop au fost șterse.»<!--recspike--> și «Serviciul de timp Windows are din nou setările de dinainte.»<!--recspike-->: serviciul de timp are din nou pornirea și declanșatoarele de dinainte de pasul 4. Dacă în locul rândului despre serviciul de timp apare «Setările de dinainte ale serviciului de timp Windows nu sunt salvate»<!--recspike-->, scrieți în aceeași fereastră, pe rând, cele două comenzi `sc` din acel rând.
2. Pe Pi (nu și pe un Pi armv6): `sudo ./recspike uninstall`. Trebuie să apară «Pornirea automată a fost ștearsă.»<!--recspike-->. Dacă pe Pi rulează încă un controlor pornit desprins de `ssh` (pasul 5), opriți-l: `sudo pkill -x reccontroller` (pe un Pi armv6: `pkill -f reccontroller-linux-armv6`).
3. Reluați actualizările Windows: „Setări” → „Windows Update” → „Reluați actualizările”.
4. Scoateți regulile de firewall ale probei: în Start căutați „Permiteți o aplicație prin Paravanul de protecție Windows”, apăsați „Modificare setări”, selectați `reccontroller` și apăsați „Eliminare”; la fel pentru `recspike`, dacă apare. Dacă un program apare pe mai multe rânduri, eliminați-le pe toate.
5. Ștergeți folderele `C:\proba-inregistrare` și, pe Pi, `~/proba-inregistrare` (`sudo rm -r ~/proba-inregistrare`).
6. Pe telefon, dezinstalați «Proba ipcam»<!--android--> și luați înapoi permisiunea Chrome de a instala aplicații: „Setări” → „Aplicații” → Chrome → „Instalați aplicații necunoscute” → dezactivați „Permiteți din această sursă”. Dacă ați dezactivat „Blocare automată” și nu ați reactivat-o, reactivați-o.
7. Dacă ați trecut rețeaua PC-ului pe „Rețea privată” doar pentru probă, sau ați oprit repausul PC-ului (pasul 4.5), readuceți-le cum erau.

## 15. Ce faceți dacă ceva nu merge

1. Faceți o captură de ecran. Pe PC: tastele Windows + Shift + S. Pe Pi, fotografiați ecranul sau copiați textul din fereastra `ssh`.
2. Notați pasul (de exemplu 7) și dispozitivul care era gazda.
3. Trimiteți-ne captura și pasul în chat.

Cazuri pe care le puteți rezolva singur:

- **«Proba nu pornește: …»<!--recspike-->** Motivul e pe același rând:
  - «notmyfault64.exe lipsește sau nu e semnat de Microsoft»<!--recspike-->: reluați pasul 4.1;
  - «recspike nu rulează ca administrator»<!--recspike-->: porniți-l din fereastra de administrator (pasul 2.5);
  - «Windows nu repornește singur după o eroare de sistem»<!--recspike-->: „Setări” → „Sistem” → „Despre” → „Setări avansate de sistem” → la „Pornire și recuperare” apăsați „Setări” și bifați „Repornire automată”;
  - «actualizările nu sunt întrerupte»<!--recspike-->: reluați pasul 4.2;
  - «recspike nu rulează ca root»<!--recspike-->: pe Pi, porniți comanda cu `sudo`;
  - «spațiu liber insuficient»<!--recspike-->: eliberați spațiu pe dispozitivul gazdă;
  - «controlorul nu primește semnalul UDP al gazdei»<!--recspike-->: firewall-ul controlorului, ca la pasul 2.9;
  - «serviciul de timp Windows nu rulează»<!--recspike-->: reluați `recspike.exe install` (pasul 4.3);
  - «ceasul gazdei nu este sincronizat prin NTP»<!--recspike-->: pe PC, într-o fereastră de administrator, scrieți `w32tm /resync`, așteptați un minut, apoi porniți din nou aceeași comandă. Pe Pi, verificați că are internet, așteptați câteva minute și porniți din nou aceeași comandă.
- **O tăiere reală nu contează.** Dacă ați tăiat alimentarea înainte de «Scoateți acum alimentarea»<!--reccontroller--> sau la mai mult de 2 minute după, controlorul scrie rândul tăierii cu «nevalidă»<!--reccontroller-->, iar proba reface tăierea în același loc. Nu e nevoie să faceți altceva decât să urmați din nou cererea.
- **Laptopul nu se oprește când scoateți încărcătorul.** Bateria îl ține pornit: țineți apăsat butonul de pornire cel puțin 10 secunde, imediat după cerere. Tăierea contează la fel.
- **Pi-ul înregistrează pe un card SD, iar în MI2 cardul dă erori sau devine doar-citire.** ipcam-hub oprește înregistrarea și notează golul; acesta e un rezultat al probei, nu o greșeală a dvs. Lăsați proba să ruleze până la final și scrieți-ne în chat. Un SSD pe USB evită problema.
- **Un dispozitiv a repornit singur** (o pană de curent, o actualizare). În fereastra automată, proba continuă singură după repornire. În MI2, porniți din nou ambele comenzi (pasul 8). În MI3, MI4 sau MI9, porniți din nou comanda.
- **«nu pot asculta pe porturile probei (rulează deja un reccontroller?)»<!--reccontroller-->** Pe același dispozitiv rulează deja un controlor, poate într-o fereastră uitată. Închideți-l și porniți din nou.
- **«Transferul nu a reușit: …»<!--reccontroller-->** la pasul 13: porniți din nou comanda `push` pe Pi.
- **Ați oprit un program cu Ctrl+C.** recspike scrie «Proba a fost oprită de dvs.»<!--recspike-->. Ce s-a măsurat până atunci rămâne; porniți din nou aceeași comandă când puteți.
