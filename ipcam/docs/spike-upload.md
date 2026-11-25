# Proba de upload (Faza 0 c2): ghid pas cu pas

Acest ghid vă arată cum rulați proba de upload pe PC-ul cu Windows 11, pe Raspberry Pi și pe telefonul Samsung Galaxy A54. Proba verifică trei lucruri:

- **MI5:** hub-ul înregistrează camere simulate cât internetul lui e blocat 6 ore, apoi urcă totul în cloud, fără să piardă nimic.
- **MI6:** autentificarea la Google Drive (și la OneDrive, dacă se poate) pe telefon, pe PC și în browser, apoi reîmprospătarea zilnică a accesului, timp de 7 zile.
- **MI8:** evenimentele de mișcare ale camerelor dvs. reale, timp de 72 de ore.

Textele între «» apar exact așa în programele probei. Textele între „” sunt ale Windows, Android, Google sau Microsoft și pot diferi puțin, în funcție de versiune și de limbă. Totul este gratuit. Nu vi se cere niciun card.

În comenzile de mai jos, `<adresa PC-ului>` și `<adresa Pi-ului>` înseamnă adresa dispozitivului în rețeaua de acasă. Scrieți adresa fără `<` și `>`.

## 0. Ce aveți nevoie și cât durează

Aveți nevoie de:

- PC-ul cu Windows 11, cu internet;
- Raspberry Pi-ul, dacă v-am recomandat să țină hub-ul (decizia D2);
- telefonul Galaxy A54, pe Wi-Fi-ul de acasă;
- contul Google de test (pasul 1) și contul dvs. Google personal;
- camerele alese pentru proba MI8 (decizia D3), cu parola lor de administrator.

| Partea | Unde | Cât lucrați dvs. |
|---|---|---|
| Conturile și proiectul Google (pașii 1–5) | PC | circa 40 min, o singură dată |
| Instalarea (pașii 6–8) | PC, Pi, telefon | circa 30 min |
| Programul și camerele (pașii 9–10) | dispozitivul hub-ului | circa 15 min pe cameră |
| Ziua 0: autentificările (pasul 10) | telefon, PC, browser | circa 20 min |
| În fiecare zi: starea probei (pasul 10) | dispozitivul hub-ului | 2 min pe zi |
| MI5, proba fără internet (pasul 11) | PC sau Pi | 10 min de lucru, apoi circa 8 ore de așteptare |
| Rezultatele și oprirea (pașii 12–14) | toate | circa 20 min |

În total: circa 2 ore de lucru, plus un dispozitiv pornit 7 zile.

**Regula pentru parole.** Nu ne trimiteți niciodată parole sau secretele clienților (în chat, pe e-mail sau în altă pagină). Le scrieți doar în programul probei, pe dispozitivul hub: secretele în pagina de configurare, parolele camerelor la cererea programului.

Capturile de ecran pe care ni le trimiteți rămân în conversație. Ele nu se publică și nu intră în raport.

## 1. Conturile de test

1. Creați un cont Google nou, doar pentru teste. Google poate cere un număr de telefon. Nu cere card.
2. Nu ne trimiteți parola contului.
3. Contul Microsoft de test îl creați doar dacă verificarea de la pasul 4 trece și vă cerem asta în chat.

## 2. Limba consolei, apoi proiectul Google Cloud

Faceți acest pas pe PC, cu contul dvs. Google **personal**, nu cu cel de test.

### Limba

1. Deschideți <https://console.cloud.google.com>.
2. Spuneți-ne în chat în ce limbă apare consola. Dacă nu e engleza, avem două variante: vă trimitem etichetele în limba dvs., sau treceți consola pe engleză pentru această configurare. Mai jos etichetele sunt în engleză.
3. Deschideți și <https://entra.microsoft.com> (consola Microsoft de la pasul 4) și spuneți-ne și limba ei. Variantele sunt aceleași.

### Proiectul

1. Dacă Google vă cere să activați verificarea în doi pași, activați-o. E gratuită și obligatorie pentru consolă.
2. Dacă apare o ofertă „free trial” sau o cerere de card, închideți-o. Proiectul nu are nevoie de facturare.
3. Sus, lângă sigla Google Cloud, apăsați pe lista de proiecte, apoi „New project”. Numele: `ipcam-proba`. Apăsați „Create”.
4. Verificați că sus apare proiectul `ipcam-proba`.
5. În meniul din stânga: „APIs & Services” → „Library”.
6. Căutați „Google Drive API”, deschideți-l și apăsați „Enable”.
7. În meniul din stânga: „Google Auth Platform”. Apăsați „Get started”.
8. La „App name” scrieți `ipcam proba`. La „User support email” alegeți adresa dvs. Apăsați „Next”.
9. La „Audience” alegeți „External”. Apăsați „Next”.
10. La „Contact information” scrieți adresa dvs. Apăsați „Next”, bifați acordul și apăsați „Create”.
11. În stânga: „Audience”. Verificați că starea este „Testing”. Nu apăsați „Publish app”.
12. Tot acolo, la „Test users”, apăsați „Add users”. Adăugați adresa dvs. și adresa contului de test. Apăsați „Save”.
13. În stânga: „Data access”. Apăsați „Add or remove scopes”.
14. Bifați doar `https://www.googleapis.com/auth/drive.file`. Dacă nu apare în listă, scrieți-l în câmpul „Manually add scopes” și apăsați „Add to table”. Apăsați „Update”, apoi „Save”.

Faceți capturi ale paginilor „Audience” și „Data access” și trimiteți-ni-le în chat.

## 3. Clienții Desktop, TV și Web

Tot în „Google Auth Platform”, în stânga: „Clients”. Creați trei clienți, pe rând.

1. Apăsați „Create client”. La „Application type” alegeți „Desktop app”. Numele: `ipcam desktop`. Apăsați „Create”.
2. În fereastra care apare, apăsați „Download JSON”. Păstrați fișierul pe PC, de exemplu în `Documente\ipcam-clienti`. Google arată secretul o singură dată.
3. Apăsați „Create client” din nou. Alegeți „TVs and Limited Input devices”. Numele: `ipcam tv`. Apăsați „Create”, apoi „Download JSON”.
4. Apăsați „Create client” din nou. Alegeți „Web application”. Numele: `ipcam web`.
5. La „Authorized JavaScript origins” apăsați „Add URI” și scrieți `http://localhost:7091`. Apăsați „Create”, apoi „Download JSON”.

Dacă ați pierdut un secret: deschideți clientul, apăsați „Add secret” și descărcați din nou fișierul. Nu creați clientul din nou.

Trimiteți-ne în chat cele trei ID-uri de client (se termină cu `.apps.googleusercontent.com`) și o captură a listei „Clients”. Nu ne trimiteți secretele și nici fișierele JSON.

## 4. Verificarea Entra de 2 minute (OneDrive)

Etichetele de mai jos sunt în engleză, ca la pasul 2. Dacă ați ales etichetele în limba dvs., folosiți-le pe cele trimise în chat.

1. Deschideți <https://entra.microsoft.com> și autentificați-vă cu contul Microsoft personal.
2. Opriți-vă și scrieți-ne în chat dacă vedeți oricare dintre acestea:
   - sub numele contului apare doar un șir lung de litere și cifre;
   - mesajul „You can't sign in here with a personal account”;
   - o cerere de înscriere Azure, un cont gratuit Azure, un abonament sau un card.

   În aceste cazuri OneDrive trece în Faza 0-bis, cum ne așteptam. Nu e o greșeală a dvs.
3. Nu folosiți contul de la serviciu sau de la școală. Nu vă înscrieți în Microsoft 365 Developer Program. Nu porniți un cont gratuit Azure.
4. Continuați doar dacă sub numele contului apare un director al dvs. și „Entra ID” → „App registrations” → „New registration” se deschide.

Dacă verificarea trece:

1. La „New registration”, numele: `ipcam proba`. La „Supported account types” alegeți „Personal accounts only” (în versiunile mai vechi: „Personal Microsoft accounts only”). Apăsați „Register”.
2. Deschideți „Authentication” → „Redirect URI configuration” → „Add Redirect URI” → „Mobile and desktop applications”.
3. La „Custom redirect URIs” adăugați `http://localhost/oauth/callback` (fără port) și `ro.ipcam.spike://oauth/callback`. Nu adăugați o adresă `http://127.0.0.1`. Salvați.
4. Tot la „Add Redirect URI”, alegeți „Single-page application” și adăugați `http://localhost:7091/oauth/f6`. Salvați.
5. La „Allow public client flows” (sau „Enable the following mobile and desktop flows”) alegeți „Yes”. Salvați.
6. Deschideți „API permissions” → „Add a permission” → „Microsoft Graph” → „Delegated permissions”. Bifați `Files.ReadWrite.AppFolder` și `offline_access`. Apăsați „Add permissions”.
7. Trimiteți-ne capturi ale paginilor „Overview”, „Authentication” și „API permissions”.

## 5. Ecranul „aplicație neverificată”

La autentificări, Google arată „Google hasn't verified this app” (în română: „Google nu a verificat această aplicație”). E normal: proiectul e în modul de test.

1. Verificați că numele aplicației este `ipcam proba`.
2. Apăsați „Continue” (în română: „Continuați”).
3. Pe pagina următoare, acceptați accesul cerut.

## 6. Instalarea

Începeți acest pas doar după ce ați terminat pasul 14 din ghidul probei de înregistrare.

### Descărcarea

1. Pe PC, deschideți pagina de release pe care v-o trimitem în chat. Ea se numește „spike-upload-v0.1.0” (sau o versiune mai nouă) și se găsește și la <https://github.com/AndreiBesliu/DevWorld/releases>.
2. Descărcați `spike-upload-windows.zip` și `ipcam-desktop-setup.exe`. Dacă browserul spune că fișierul „nu este descărcat frecvent”, alegeți „Păstrați” (eventual „Păstrați oricum”). Programele de test nu sunt semnate digital, pentru că semnătura costă bani.
3. Pe pagina de release, la notele versiunii, copiați amprenta SHA-1 (un șir de forma `AA:BB:…`). Vă trebuie la instalarea pe telefon, mai jos.

### Pe PC

1. Faceți pasul 7.1 (deblocarea arhivei).
2. Clic dreapta pe `spike-upload-windows.zip` → „Extragere totală”. Ca folder de destinație scrieți `C:\proba-upload` și apăsați „Extragere”. Folderul conține `uploadspike.exe`, `ipcam-hub.exe`, `mediamtx.exe`, `reccontroller.exe`, folderul `media` și `THIRD_PARTY_NOTICES.txt`. Nu mutați folderul după instalare.
3. Deschideți o fereastră de comenzi: în Start scrieți `cmd` și apăsați Enter. În fereastră scrieți `cd /d C:\proba-upload` și apăsați Enter. În acest ghid, „fereastra de comenzi” înseamnă o astfel de fereastră.
4. Scrieți `uploadspike install` și apăsați Enter. Programul scrie «Comanda continuă în fereastra de administrator care se deschide; rezultatul ei apare acolo.»<!--uploadspike--> Windows cere o dată permisiunea de administrator: răspundeți „Da”.
5. Se deschide o fereastră nouă, de administrator. În ea trebuie să apară «Pornirea automată a probei este instalată și pornită; regulile firewall-ului pentru rețeaua locală sunt adăugate.»<!--uploadspike--> Apoi apare «Apăsați Enter ca să închideți această fereastră.»<!--uploadspike--> Apăsați Enter. De acum hub-ul pornește singur după o repornire a PC-ului.

   La fel se întâmplă la `uploadspike mi5`, `uploadspike unblock` și `uploadspike uninstall`: rândurile lor apar în fereastra de administrator, care rămâne deschisă până apăsați Enter.
6. Dublu clic pe `ipcam-desktop-setup.exe`. La „Windows a protejat PC-ul” urmați pasul 7.2. Instalatorul e în engleză: apăsați „Next” până la „Install”, apoi „Finish”. Dacă aplicația `ipcam-desktop` era deja instalată de la proba video, instalați peste ea.

### Pe Pi

Faceți partea aceasta doar dacă Pi-ul ține hub-ul (D2-B sau D2-C).

1. Deschideți un terminal pe Pi: pe ecranul lui („Terminal” din meniu) sau de pe PC, dintr-o fereastră de comenzi, cu `ssh <utilizator>@<adresa Pi-ului>`. `<utilizator>` e numele contului dvs. de pe Pi.
2. Scrieți, pe rând:

   ```
   mkdir -p ~/proba-upload && cd ~/proba-upload
   wget https://github.com/AndreiBesliu/DevWorld/releases/download/spike-upload-v0.1.0/uploadspike-probe-linux-armv6
   chmod +x uploadspike-probe-linux-armv6
   ./uploadspike-probe-linux-armv6
   ```

   Dacă release-ul are alt nume decât `spike-upload-v0.1.0`, folosiți-l pe acela în adresă, aici și mai jos.
3. Programul scrie una dintre:
   - «Pe acest Pi folosiți arhiva …»<!--probe-->, cu numele arhivei. Descărcați-o și despachetați-o, de exemplu pentru arm64:

     ```
     wget https://github.com/AndreiBesliu/DevWorld/releases/download/spike-upload-v0.1.0/spike-upload-linux-arm64.tar.gz
     tar -xzf spike-upload-linux-arm64.tar.gz
     ```

   - «Acest Raspberry Pi (armv6) nu poate rula hub-ul.»<!--probe--> Atunci proba pe Pi trece în Faza 0-bis, iar PC-ul ține hub-ul. Programul scrie și «Am notat în rezultate că Pi-ul este armv6: trimiteți-ne fișierul …»<!--probe-->. Păstrați fișierul `results-pi.zip` pentru pasul 13 și săriți peste restul acestei părți.
4. Scrieți `sudo ./uploadspike install`. `sudo` poate cere parola contului dvs. de pe Pi.
5. Dacă datele stau pe un card SD, programul scrie «Cardul SD se uzează repede la înregistrare continuă»<!--uploadspike--> și cere «Scrieți DA și apăsați Enter»<!--uploadspike-->. Scrieți `DA` și apăsați Enter.
6. Trebuie să apară «Proba este instalată în /opt/ipcam-spike, datele în /var/lib/ipcam-spike; pornirea automată rulează.»<!--uploadspike-->
7. Pe Pi, porniți de acum fiecare comandă a probei cu `sudo ./uploadspike …`, din folderul `~/proba-upload`. Dacă uitați `sudo`, programul vă amintește: «Rulați comanda cu sudo: sudo ./uploadspike …»<!--uploadspike-->.

### Pe telefon

1. Pe telefon, deschideți în Chrome pagina de release și apăsați pe `ipcam-spike.apk`. Dacă Chrome avertizează că fișierul poate fi periculos, alegeți „Descărcați oricum”.
2. Deschideți fișierul descărcat. Dacă Android cere permisiunea pentru această sursă, apăsați „Setări”, activați „Permiteți din această sursă”, reveniți și apăsați „Instalați”.
3. Dacă aplicația «Proba ipcam»<!--android--> e încă instalată de la proba video, Android întreabă dacă o actualizați: apăsați „Actualizați”. Dacă Android spune că aplicația nu a fost instalată, dezinstalați întâi aplicația veche (țineți apăsat pe pictograma ei → „Dezinstalare”), apoi instalați din nou.
4. Dacă Play Protect avertizează, apăsați „Mai multe detalii”, apoi „Instalați oricum”. Dacă instalarea e oprită de „Blocare automată”: „Setări” → „Securitate și confidențialitate” → „Blocare automată” → dezactivați, instalați, apoi reactivați-o.

### Al patrulea client Google: „Android”

Pe PC, în consola Google, la „Google Auth Platform” → „Clients”:

1. Apăsați „Create client”. Alegeți „Android”. Numele: `ipcam android`.
2. La „Package name” scrieți `ro.ipcam.spike`.
3. La „SHA-1 certificate fingerprint” lipiți amprenta SHA-1 din notele versiunii.
4. Deschideți „Advanced settings” și bifați „Enable custom URI scheme”. Apăsați „Create”.
5. Trimiteți-ne ID-ul acestui client. Dacă vă trimitem mai târziu o versiune nouă a aplicației, vă spunem dacă trebuie schimbată amprenta.
6. ID-ul vă trebuie și la pasul 8. Îl găsiți oricând în „Clients” → `ipcam android`, la „Client ID”.

### Fără actualizări în timpul probei

Aplicațiile de test verifică singure dacă există o versiune nouă a probei video. În timpul probei de upload, dacă apare «Există o versiune nouă: … (aveți …).»<!--app-->, nu apăsați «Actualizează acum»<!--app-->. Versiunea probei video nu are rândul probei de upload.

## 7. SmartScreen și firewall-ul

1. **Deblocarea arhivei.** Înainte de extragere: clic dreapta pe `spike-upload-windows.zip` → „Proprietăți”. Dacă jos apare caseta „Deblocare”, bifați-o și apăsați „OK”. Astfel Windows nu întreabă de fiecare program în parte (uploadspike, ipcam-hub, mediamtx, reccontroller).
2. **SmartScreen.** Windows poate afișa „Windows a protejat PC-ul”. Apăsați „Mai multe informații”, apoi „Rulați oricum”. Dacă nu apare „Rulați oricum” (de exemplu, mesajul pomenește „Smart App Control”), opriți-vă și scrieți-ne în chat.
3. **Antivirusul.** Dacă „Securitate Windows” anunță „Amenințare găsită” și un program dispare din `C:\proba-upload`, nu opriți antivirusul. Deschideți „Securitate Windows” → „Protecție împotriva virușilor și amenințărilor” → „Istoricul protecției”. Dacă intrarea numește un program din `C:\proba-upload`, alegeți „Acțiuni” → „Restaurare”. Dacă numește alt fișier sau nu sunteți sigur, nu restaurați nimic și scrieți-ne în chat.
4. **Firewall-ul.** Când uploadspike așteaptă o autentificare de pe telefon, firewall-ul Windows poate întreba dacă permite accesul. Bifați „Rețele private”, lăsați „Rețele publice” nebifat și apăsați „Permiteți accesul”.
5. **Rețeaua privată.** Dacă telefonul nu ajunge la PC, sau dacă uploadspike scrie «Windows Firewall blochează conexiunile din rețeaua locală către uploadspike»<!--uploadspike-->: deschideți „Setări” → „Rețea și internet” → rețeaua dvs. și alegeți „Rețea privată”. Apoi rulați din nou `uploadspike install`.

## 8. Configurarea, pe fiecare dispozitiv care ține un hub

Hub-ul primește aici clienții Google (ID-urile și secretele). Îi păstrează sigilați. Nimic nu rămâne pe disc în clar.

Unde faceți pasul:

- D2-A: doar pe PC;
- D2-B: pe ambele (Pi-ul ține cele 7 zile, PC-ul face proba MI5);
- D2-C: doar pe Pi.

### Pe PC

1. În fereastra de comenzi: `uploadspike setup`. Se deschide în browser pagina de configurare.
2. Apăsați pe câmpul «Fișierele JSON ale clienților»<!--uploadspike-->. Deschideți folderul de la pasul 3, selectați toate cele trei fișiere JSON (Ctrl+A) și apăsați „Deschidere”. Puteți și să trageți fișierele oriunde în pagină. Pagina recunoaște singură fiecare client și completează câmpurile.
3. Clientul Android nu are fișier. La «Android»<!--uploadspike-->, în câmpul «ID-ul clientului»<!--uploadspike-->, lipiți ID-ul clientului `ipcam android` de la pasul 6.
4. Dacă verificarea de la pasul 4 a trecut, scrieți și ID-ul clientului Microsoft.
5. Apăsați «Salvează în hub»<!--uploadspike-->. Pagina scrie «Clienții au fost dați hub-ului. Puteți închide pagina.»<!--uploadspike-->
6. În fereastra de comenzi apare «Clienții au fost dați hub-ului și sigilați în catalog; nimic nu a rămas pe disc.»<!--uploadspike-->

### Pe Pi

1. În terminal: `sudo ./uploadspike setup`.
2. Programul întreabă, pe rând, «Clientul Google … (JSON sau ID)»<!--uploadspike-->, întâi pentru `android`. La `android`, lipiți ID-ul clientului `ipcam android` de la pasul 6 (clic dreapta) și apăsați Enter.
   La `desktop`, `tv` și `web`: pe PC, deschideți fișierul JSON al clientului cerut cu Notepad, copiați tot textul și lipiți-l în terminal (clic dreapta). Apăsați Enter.
3. Dacă scrieți doar ID-ul, programul cere apoi «Secretul clientului … (nu se afișează)»<!--uploadspike-->. Lipiți secretul și apăsați Enter. Textul nu apare pe ecran; e normal.
4. La «ID-ul clientului Microsoft (Enter dacă verificarea A5 nu a trecut)»<!--uploadspike--> scrieți ID-ul, sau apăsați doar Enter.
5. Dacă datele stau pe un card SD și nu ați confirmat încă, scrieți `DA` la «Scrieți DA și apăsați Enter»<!--uploadspike-->.
6. La final apare «Clienții au fost dați hub-ului și sigilați în catalog; nimic nu a rămas pe disc.»<!--uploadspike-->

## 9. Programul de 7 zile

1. Pe Pi, dacă ați instalat proba pe el, scrieți `sudo ./uploadspike host`. Fără Pi, scrieți `uploadspike host` pe PC. Primul rând este recomandarea: «Recomandarea D2: …»<!--uploadspike-->. Dacă ne-ați spus deja ce alegeți la D2, urmați alegerea dvs.
2. Pe dispozitivul ales, opriți repausul pe 7 zile. Pe PC: „Setări” → „Sistem” → „Alimentare” → „Ecran și repaus” → la repaus alegeți „Niciodată”.
3. Pe PC, întrerupeți actualizările Windows: „Setări” → „Windows Update” → „Întrerupeți actualizările” (cea mai lungă perioadă).
4. La pasul 10, prima comandă afișează «Programul probei»<!--uploadspike-->, cu datele zilelor. Fotografiați-l sau copiați-l. Pasul acesta e doar programul; probele pornesc la pasul 10.

## 10. Camerele, apoi pornirea probelor

### Pregătirea camerelor

Dacă ne-ați răspuns D3-C, proba MI8 nu rulează: săriți peste pregătirea camerelor și peste partea „Zilele 0–1: proba MI8”.

Vă trimitem în chat pașii pentru fiecare marcă. Pe scurt, pentru fiecare cameră aleasă (D3):

1. Porniți ONVIF și RTSP în pagina web a camerei. La Hikvision: „Configuration” → „Network” → „Advanced Settings” → „Integration Protocol”, sau, pe firmware nou, „Maintenance” → „System Service” → „ONVIF”. De la firmware 5.5.0, ONVIF e oprit din fabrică.
2. Notați portul ONVIF, dacă nu e 80. La Reolink e de obicei 8000.
3. Creați un utilizator ONVIF de nivelul cel mai mic, „User” (la Hikvision: „Media User”), cu parola lui. Nu folosiți „Administrator”. Îl treceți pe „Operator” doar dacă vă cerem, după verificarea de mai jos.
4. Porniți detecția de mișcare a camerei pe tot cadrul, cu legătura care o trimite ca eveniment ONVIF.
5. Verificați că a doua transmisie (substream) este H.264.
6. Porniți NTP sau potriviți ceasul camerei.

Ca să verificați camerele fără să porniți proba: `uploadspike mi8 --probe-only` (pe Pi: `sudo ./uploadspike mi8 --probe-only`). Programul cere adresa, utilizatorul și parola fiecărei camere și spune ce a găsit. Dacă scrie «utilizatorul sau parola ONVIF nu sunt acceptate»<!--uploadspike-->, verificați utilizatorul. Dacă scrie «portul ONVIF nu răspunde»<!--uploadspike-->, verificați portul. Trimiteți-ne rândurile, fără adrese.

### Ziua 0: autentificările MI6

1. Pe dispozitivul D2, porniți proba: `uploadspike mi6` (pe Pi: `sudo ./uploadspike mi6`). Adăugați `--onedrive` doar dacă verificarea de la pasul 4 a trecut.
   După alegerea dvs. la D1 adăugați:
   - D1-A (sau dacă nu ne-ați răspuns): nimic;
   - D1-B: `--drive test+personal`;
   - D1-C: `--drive personal`. Atunci, la toate autentificările de mai jos, alegeți contul dvs. **personal** în locul contului de test.

   De exemplu, cu D1-B și OneDrive: `uploadspike mi6 --drive test+personal --onedrive`. Scrieți aceeași alegere `--drive` și la pasul 11.
2. Programul afișează «Programul probei»<!--uploadspike-->, apoi cere autentificările, pe rând. Urmați rândul afișat. Mai jos sunt pașii pentru fiecare.

**Pe telefon:**

1. Deschideți «Proba ipcam»<!--android-->.
2. În câmpul «Adresa hub-ului (o vedeți în fereastra spikehub)»<!--app--> scrieți adresa pe care o arată uploadspike în rândul «Adresa acestui dispozitiv pentru celălalt dispozitiv: …»<!--uploadspike-->, de forma `http://<adresa>:7091`. Eticheta câmpului pomenește spikehub, de la proba video; aici scrieți adresa de la uploadspike.
3. Nu apăsați «Verifică conexiunea»<!--app-->. Proba aceasta nu are camere video, așa că butonul ar arăta o eroare.
4. Pe rândul «Conectare la stocare (MI6)»<!--app--> apăsați «Pornește»<!--app-->.
5. Se deschide pagina Google. Alegeți **contul de test**. Treceți de ecranul de la pasul 5 și acceptați.
6. uploadspike scrie «autentificare reușită (plan …)»<!--uploadspike--> și trece la următoarea.

**Pe PC, în aplicația `ipcam-desktop`:**

1. Deschideți `ipcam-desktop` din Start.
2. În câmpul «Adresa hub-ului (o vedeți în fereastra spikehub)»<!--app--> scrieți adresa din rândul uploadspike: `http://localhost:7091` când hub-ul e pe PC, sau adresa Pi-ului afișată, când hub-ul e pe Pi.
3. Pe rândul «Conectare la stocare (MI6)»<!--app--> apăsați «Pornește»<!--app-->. Se deschide browserul. Alegeți contul de test și acceptați.

**În browserul PC-ului (web):**

1. uploadspike scrie «Aplicația video web: …»<!--uploadspike-->, cu o adresă. Deschideți adresa în browserul PC-ului (Chrome sau Edge).
2. Pe rândul «Conectare la stocare (MI6)»<!--app--> apăsați «Pornește»<!--app-->.
3. Aplicația scrie «Deschideți … și scrieți codul ….»<!--app-->. Deschideți adresa într-o filă nouă, scrieți codul, alegeți contul de test și acceptați.
4. Înapoi în aplicație, la «Ați scris codul și v-ați conectat?»<!--app--> apăsați «Da»<!--app-->.

**Dacă un plan nu merge.** Dacă Google refuză o autentificare, uploadspike scrie «Rulăm B1 acum, în aceeași zi.»<!--uploadspike--> Atunci apăsați din nou «Pornește»<!--app--> pe același rând: de data asta aplicația arată o adresă și un cod, ca la web.

**Cititorul web (vizibilitatea).** După autentificări, uploadspike scrie «Autentificările din ziua 0 s-au încheiat.»<!--uploadspike-->

- Când hub-ul e pe PC, se deschide singură pagina cititorului (`http://localhost:7091/reader`). Apăsați «Pornește citirea»<!--uploadspike-->, alegeți contul de test Google și lăsați pagina vizibilă până scrie «Gata. Puteți închide pagina.»<!--uploadspike-->
- Când hub-ul e pe Pi, uploadspike scrie «Vizibilitate: pe PC rulați uploadspike mi6 reader --hub»<!--uploadspike--> și o adresă. Pe PC, în fereastra de comenzi, rulați comanda exact cum e scrisă. Apoi faceți ca mai sus.

La final apare «Gata. Hub-ul trece la pornirea automată, care îl ține pornit până în ziua 7; puteți închide fereastra.»<!--uploadspike--> Puteți închide fereastra.

### Zilele 0–1: proba MI8 (72 h)

1. Pe dispozitivul D2: `uploadspike mi8` (pe Pi: `sudo ./uploadspike mi8`). Dacă ne-ați răspuns D3-B (o cameră de fiecare marcă), scrieți `uploadspike mi8 --one-per-brand` (pe Pi: `sudo ./uploadspike mi8 --one-per-brand`).
2. La «Câte camere intră în proba MI8?»<!--uploadspike--> scrieți numărul.
3. Pentru fiecare cameră, programul cere:
   - «Camera …: adresa (IP sau IP:port ONVIF; fără port, 80)»<!--uploadspike-->: scrieți adresa camerei. Adăugați `:<port>` dacă portul ONVIF nu e 80, de exemplu `192.168.1.50:8000`;
   - «Camera …: utilizatorul ONVIF»<!--uploadspike-->: scrieți utilizatorul ONVIF;
   - «Camera …: parola ONVIF (nu se afișează)»<!--uploadspike-->: scrieți parola. Ea nu apare pe ecran.
4. Prima dată, programul poate scrie «Descarc FFmpeg pentru referința de mișcare (o singură dată)…»<!--uploadspike-->. Așteptați.
5. Pe Pi apare «MI8 rulează în fundal 72 de ore. Puteți închide fereastra; progresul îl vedeți cu uploadspike status.»<!--uploadspike--> Puteți închide fereastra `ssh`.
6. Pe PC apare «Lăsați fereastra deschisă 72 de ore.»<!--uploadspike--> Lăsați fereastra deschisă.

Dacă `uploadspike status` scrie «MI8 s-a oprit; rulați uploadspike mi8 --continue.»<!--uploadspike--> (după o repornire sau o fereastră închisă), rulați `uploadspike mi8 --continue`. Programul numește fiecare cameră cu marca și modelul și cere din nou adresa, utilizatorul și parola.

**După 24 de ore:** rulați `uploadspike status`. Pentru fiecare cameră apare numărul de «episoade de mișcare»<!--uploadspike-->. Treceți prin fața fiecărei camere care are sub 20. Între două treceri, așteptați cel puțin 10 secunde.

### În fiecare zi, din ziua 1: starea probei

1. Pe dispozitivul D2: `uploadspike status` (pe Pi: `sudo ./uploadspike status`).
2. Primul rând MI6 arată ziua: «MI6: ziua … din 7 (pornită pe …).»<!--uploadspike-->
3. Dacă apare «Reconectați … cu B1: uploadspike mi6 --b1 …»<!--uploadspike-->, rulați comanda de pe rând. Dacă rândul scrie «după proba MI5»<!--uploadspike-->, rulați-o doar după ce proba MI5 s-a terminat.
4. Apoi faceți autentificarea pe platforma numită, ca în ziua 0. Aplicația arată o adresă și un cod. Deschideți adresa, scrieți codul și acceptați. În aplicație, la «Ați scris codul și v-ați conectat?»<!--app--> apăsați «Da»<!--app-->.

## 11. MI5: proba de circa 8 ore fără internet

Rulați MI5 o singură dată, într-una din zilele 1–5, când puteți lăsa dispozitivul pornit 8 ore:

- D2-A sau D2-B: pe PC;
- D2-C: pe Pi.

1. Pe PC, în fereastra de comenzi: `uploadspike mi5`. Pe Pi: `sudo ./uploadspike mi5`. Adăugați aceeași alegere `--drive` ca la pasul 10 (de exemplu `uploadspike mi5 --drive test+personal`) și `--onedrive`, dacă l-ați folosit acolo. Windows cere o dată permisiunea de administrator: răspundeți „Da”. Pe PC, proba continuă în fereastra de administrator care se deschide; de acum urmăriți acea fereastră.
2. Cu D2-B sau D1-B, programul poate cere întâi o autentificare scurtă: «Conectați contul pentru …: deschideți … pe orice dispozitiv, scrieți codul … și acceptați.»<!--uploadspike--> Deschideți adresa în orice browser, scrieți codul și acceptați contul cerut.
3. Programul scrie «Proba MI5 a pornit: blocarea de … începe în jur de …»<!--uploadspike--> și «Lăsați fereastra deschisă până la final.»<!--uploadspike-->
4. Lăsați fereastra deschisă circa 8 ore. Pe Pi, lăsați deschisă și fereastra `ssh`, cu PC-ul pornit, sau rulați proba de la tastatura și ecranul Pi-ului.
5. În acest timp doar ipcam-hub și mediamtx nu au internet. Restul PC-ului merge normal.
6. Dacă rulați atunci, pe același dispozitiv, `uploadspike setup`, `uploadspike mi6`, `uploadspike mi6 --b1 …` sau `uploadspike mi6 reader …`, programul răspunde «Proba MI5 rulează; rulați comanda după ce se termină.»<!--uploadspike--> `uploadspike status` și `uploadspike mi8 --continue` merg normal.
7. La final apare «Proba MI5 s-a încheiat; rezultatele au fost scrise. Hub-ul trece la pornirea automată.»<!--uploadspike--> Pe PC, apăsați apoi Enter ca să închideți fereastra de administrator. Dacă în locul acestui rând apare o eroare, faceți o captură înainte să apăsați Enter.

Dacă fereastra se închide mai devreme, hub-ul de 7 zile merge mai departe singur. Rulați MI5 din nou în altă zi dintre zilele 1–5.

Dacă pe Pi apare «Nu știm câte camere poate înregistra acest Pi.»<!--uploadspike-->, rulați MI5 pe PC.

**Deblocarea, oricând.** Dacă internetul hub-ului rămâne blocat după probă, rulați `uploadspike unblock` (pe Pi: `sudo ./uploadspike unblock`). Trebuie să apară «Blocarea internetului hub-ului a fost scoasă.»<!--uploadspike--> (pe PC, în fereastra de administrator; apoi apăsați Enter).

## 12. Două capturi din consola Google

După proba MI5, pe PC, în consola Google, la proiectul `ipcam-proba`:

1. Deschideți „APIs & Services” → „Enabled APIs & services” → „Google Drive API”.
2. Faceți o captură a paginii de cote („Quotas”), cu unitățile folosite în ziua probei MI5.
3. Deschideți fila „Metrics”. Faceți o captură a cererilor pe metode, pentru intervalul probei.
4. Trimiteți-ne cele două capturi.

## 13. Trimiterea rezultatelor

În ziua 7, după ce `uploadspike status` scrie «Proba MI6 s-a încheiat (ziua 7): rezultatele au fost scrise. Rulați uploadspike results.»<!--uploadspike-->:

1. Pe PC: deschideți o fereastră de comenzi ca administrator. În Start scrieți `cmd`, clic dreapta pe „Linie de comandă” → „Executare ca administrator”, apoi „Da”. Scrieți `cd /d C:\proba-upload`.
2. Scrieți `uploadspike results`. Apare «Rezultatele sunt în … Trimiteți-ne acest fișier.»<!--uploadspike--> Dacă apare «Rulați comanda dintr-o fereastră de administrator»<!--uploadspike-->, reluați punctul 1.
3. Pe Pi (dacă a ținut un hub): `sudo ./uploadspike results`. Fișierul `results-pi.zip` rămâne în folderul curent.
4. Pe un Pi armv6: folosiți `results-pi.zip` lăsat de `uploadspike-probe` în folderul lui.
5. Copiați `results-pi.zip` pe PC. În fereastra de comenzi de pe PC: `scp <utilizator>@<adresa Pi-ului>:proba-upload/results-pi.zip .` Sau folosiți un stick USB.
6. Trimiteți arhivele în chat. Sau puneți-le în Google Drive-ul dvs., în folderul `ipcam-teste`, și scrieți-ne că sunt acolo.

Rezultatele numesc dispozitivele `pc` și `pi` și camerele `real-1`, `real-2` și așa mai departe. Nu conțin adrese, parole, coduri sau adresele dvs. de e-mail.

## 14. Cum opriți testul

După ce v-am confirmat în chat că rezultatele sunt complete:

1. Pe PC, în fereastra de comenzi: `uploadspike uninstall`. Windows cere permisiunea de administrator: „Da”. În fereastra de administrator trebuie să apară «Pornirea automată, blocarea și utilizatorul probei au fost scoase; datele au rămas.»<!--uploadspike--> Apăsați Enter.
2. Pe Pi: `sudo ./uploadspike uninstall`. Trebuie să apară același rând.
3. Ștergeți folderele `C:\proba-upload` și, pe Pi, `~/proba-upload`.
4. Ștergeți datele probei. Ele țin clienții și accesul la conturi (sigilate) și înregistrările probei MI5, câțiva GB.
   - Pe PC: deschideți „Explorer”, lipiți `%LOCALAPPDATA%` în bara de adrese și apăsați Enter. Ștergeți folderul `ipcam-spike`.
   - Pe Pi: `sudo rm -rf /var/lib/ipcam-spike`.
5. Reluați actualizările Windows și repausul PC-ului, cum erau.

Dacă vreți, puteți face și:

- Retrageți accesul probei. La contul Google: „Security” → „Third-party apps & services” → `ipcam proba` → ștergeți conexiunea. La contul Microsoft: pagina aplicațiilor care au acces la datele dvs. → `ipcam proba` → eliminați accesul.
- Ștergeți folderele `ipcam` din Drive-ul contului de test și, dacă e cazul, din OneDrive.
- Ștergeți aplicația Entra creată la pasul 4, dacă ați creat-o.
- Pentru fiecare cameră: păstrați utilizatorul ONVIF pentru Faza 1, sau ștergeți-l și opriți ONVIF.

Dacă ceva nu merge: faceți o captură de ecran, notați pasul și dispozitivul și trimiteți-ni-le în chat, fără parole și fără adrese de e-mail.
