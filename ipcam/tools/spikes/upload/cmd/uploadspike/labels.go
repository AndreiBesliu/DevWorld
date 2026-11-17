// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

// Every line that uploadspike shows the owner, on the screen and in its
// pages, is one of these constants, or one of its packages' (mi6's status
// lines, sdconfirm, hostinfo's reasons, hubproc's start errors): Task 12
// checks the guide against them. Romanian, addressing the owner as «dvs.».

// The usage.
const usage = `Folosire:
  uploadspike setup [--terminal]
      dă hub-ului clienții OAuth (ID-urile și secretele), sigilați; pe PC se deschide pagina de configurare,
      pe Pi (sudo ./uploadspike setup) se întreabă în terminal, apoi confirmarea cardului SD, dacă e cazul.
  uploadspike host
      spune ce dispozitiv ține hub-ul (recomandarea D2).
  uploadspike mi6 [--drive test|test+personal|personal] [--onedrive] [--platforms android,windows,web]
      proba MI6: autentificările din ziua 0, matricea de vizibilitate, apoi reîmprospătarea zilnică 7 zile.
  uploadspike mi6 --b1 android|windows|web
      reconectează o platformă cu B1 (codul afișat de aplicație), în aceeași zi.
  uploadspike mi6 reader --hub ADRESA
      pe PC, când hub-ul rulează pe Pi: pagina cititorului web.
  uploadspike resume
      ține hub-ul probei pornit (îl pornește pornirea automată).
  uploadspike status
      starea probelor deschise.
  uploadspike results
      scrie rezultatele și le pune în results-<gazdă>.zip, în folderul curent.
`

// The commands' lines.
const (
	msgUnknownCommand    = "comandă necunoscută: %s"
	msgNoCommand         = "lipsește comanda"
	msgStoppedByYou      = "Proba a fost oprită de dvs."
	msgError             = "uploadspike: eroare:"
	msgTestSeams         = "Atenție: sunt setate variabilele de test %s; rezultatele scrise acum nu se judecă."
	msgMI5Running        = "Proba MI5 rulează; rulați comanda după ce se termină."
	msgHubBusy           = "Alt program al probei ține hub-ul. Închideți-l și încercați din nou."
	msgLoginInterrupted  = "Autentificarea s-a întrerupt: alt program al probei a preluat hub-ul. Rulați din nou: %s."
	msgHandedOver        = "Alt program al probei a preluat hub-ul."
	msgHandoverMI5       = "Proba MI5 rulează: hub-ul nu se predă."
	msgSignalHandBack    = "Opresc hub-ul; pornirea automată îl preia."
	msgBackToAutostart   = "Gata. Hub-ul trece la pornirea automată, care îl ține pornit până în ziua 7; puteți închide fereastra."
	msgKeepWindowOpen    = "Lăsați fereastra deschisă până în ziua 7." //nolint:gosec // G101: an owner's line
	msgHubCrashed        = "Hub-ul s-a oprit singur; îl repornesc."
	msgHubStarted        = "Hub-ul a pornit."
	msgScanFailed        = "Scanarea rezultatelor a găsit un secret și a mutat fișierele în %s; proba se repetă. Spuneți-ne."
	msgScanError         = "Scanarea rezultatelor nu s-a putut face (%v); proba se repetă. Spuneți-ne."
	msgScanUnmoved       = "Scanarea rezultatelor a găsit un secret pe care nu l-a putut muta din folderul rezultatelor; proba se repetă. Spuneți-ne."
	msgResultsNotZipped  = "Rezultatele nu au fost puse în arhivă: scanarea nu a confirmat că nu conțin secrete. Spuneți-ne."
	msgNeedsElevation    = "Rulați comanda dintr-o fereastră de administrator (clic dreapta pe Windows Terminal, «Executare ca administrator»): scanarea rezultatelor are nevoie de drepturi de administrator."
	msgResultsZip        = "Rezultatele sunt în %s. Trimiteți-ne acest fișier."
	msgResultsWritten    = "Rezultatele probei MI6 au fost scrise."
	msgNoOpenRun         = "Nicio probă deschisă pe acest dispozitiv."
	msgOpenPage          = "Deschideți în browser: %s"
	msgPageOpened        = "S-a deschis în browser: %s"
	msgAddress           = "Adresa acestui dispozitiv pentru celălalt dispozitiv: %s"
	msgNoLANAddress      = "Acest dispozitiv nu are o adresă în rețeaua locală (192.168.x.x, 10.x.x.x sau 172.16–31.x.x): celălalt dispozitiv nu îl poate găsi. Conectați-l la rețeaua de acasă."
	msgResumeWaiting     = "Pornirea automată așteaptă o probă deschisă."
	msgMI6Closed         = "Proba MI6 s-a încheiat (ziua 7): rezultatele au fost scrise. Rulați uploadspike results."
	msgUnknownPlatform   = "platformă necunoscută: %s (android, windows sau web)"
	msgUnknownDrive      = "valoare greșită pentru --drive: %s (test, test+personal sau personal)"
	msgOneDriveNeedsA5   = "--onedrive cere clientul Microsoft (A5): rulați întâi uploadspike setup cu ID-ul lui."
	msgNoMI6Run          = "Nicio probă MI6 deschisă: rulați întâi uploadspike mi6."
	msgMI6KeepsSettings  = "Proba MI6 a început pe %s și are deja autentificări: rămâne cu setările de atunci (--drive %s, --platforms %s%s)."
	msgReaderHubNeeded   = "lipsește --hub ADRESA (adresa Pi-ului, pe care uploadspike mi6 o afișează)"
	msgReaderOpen        = "Lăsați pagina cititorului deschisă până scrie «Gata», apoi închideți această fereastră (Ctrl+C)."
	msgReaderInterrupted = "Pagina cititorului a trecut în fundal în timpul citirii: întreruperea a fost notată, iar citirea reîncepe când pagina revine."
	msgSdNeedsSetup      = "Hub-ul nu pornește: cardul SD nu este confirmat. Rulați sudo ./uploadspike setup și scrieți DA."
	msgB1NotNeeded       = "%s × %s nu are nevoie de B1 acum."
	msgSetupSaved        = "Clienții au fost dați hub-ului și sigilați în catalog; nimic nu a rămas pe disc."
	msgSetupWaitPage     = "Completați pagina de configurare din browser; aștept." //nolint:gosec // G101: an owner's line
	msgSetupClientsWrong = "Clienții nu sunt completi: %s"
)

// The MI6 login prompts, per platform and plan.
const (
	promptAndroidA  = "Pe telefon (Android): deschideți aplicația video, scenariul «OAUTH», și apăsați «Conectare %s». Aplicația găsește singură hub-ul."
	promptWindowsA  = "Pe PC (Windows): deschideți aplicația video, scenariul «OAUTH», și apăsați «Conectare %s»."
	promptWebA      = "În browserul PC-ului: deschideți aplicația video web, scenariul «OAUTH», și apăsați «Conectare %s»."
	promptB1        = "%s cu B1: apăsați «Conectare %s» în scenariul «OAUTH»; aplicația arată un cod. Deschideți adresa afișată, scrieți codul și acceptați."
	promptWaiting   = "Aștept autentificarea %s × %s…"
	promptPlanAFail = "Planul A pentru %s × %s a fost refuzat (%s). Rulăm B1 acum, în aceeași zi."
	promptB1Fail    = "B1 pentru %s × %s a fost refuzat (%s): perechea trece pe B2."
	promptTransient = "Autentificarea nu a ajuns la furnizor (rețea); încercați din nou."
	promptExpired   = "Autentificarea a expirat (10 min); încercați din nou."
	promptDone      = "%s × %s: autentificare reușită (plan %s)."
	promptReader    = "Vizibilitate: pagina cititorului web se deschide în browser; acceptați contul de test Google când vi se cere."
	promptReaderPi  = "Vizibilitate: pe PC rulați uploadspike mi6 reader --hub %s și lăsați pagina deschisă până scrie «Gata»."
	promptVisDone   = "Vizibilitate: %d din 9 celule măsurate."
	promptLoginsEnd = "Autentificările din ziua 0 s-au încheiat."
)

// The schedule (Programul probei), with its dates.
const (
	scheduleTitle = "Programul probei"
	scheduleDay0  = "Ziua 0 (%s): autentificările MI6, acum."
	scheduleMI8   = "Zilele 0–1 (%s–%s): porniți proba MI8 pentru 72 h (uploadspike mi8)."
	scheduleMI5   = "Zilele 1–5 (%s–%s): proba MI5 (uploadspike mi5)."
	scheduleDaily = "În fiecare zi: uploadspike status."
	scheduleHub   = "Hub-ul rămâne pornit 7 zile, până pe %s."
	scheduleEnd   = "Ziua 7 (%s): uploadspike results."
	dateLayout    = "02.01.2006"
)

// The setup's terminal prompts (the Pi).
const (
	setupIntro       = "Configurarea clienților OAuth. Pentru fiecare client lipiți conținutul fișierului JSON descărcat la creare, sau scrieți ID-ul clientului."
	setupClientID    = "Clientul Google %s (JSON sau ID): "
	setupSecret      = "Secretul clientului %s (nu se afișează): "
	setupMicrosoft   = "ID-ul clientului Microsoft (Enter dacă verificarea A5 nu a trecut): "
	setupSdWarnFirst = "Rec/ al hub-ului este pe un card SD."
)

// The host command.
const (
	hostRecommend = "Recomandarea D2: %s."
	hostStorage   = "Stocarea înregistrărilor: %s."
)

// The pages' texts.
const (
	pageTitle           = "Proba de upload (Faza 0)"
	pageSetupTitle      = "Configurarea clienților OAuth"
	pageSetupHelp       = "Trageți aici fișierele JSON ale clienților Google (descărcate la creare), toate deodată; pagina recunoaște singură fiecare client. Sau completați câmpurile."
	pageSetupFiles      = "Fișierele JSON ale clienților"
	pageSetupSave       = "Salvează în hub"
	pageSetupSaved      = "Clienții au fost dați hub-ului. Puteți închide pagina."
	pageSetupFailed     = "Hub-ul nu a primit clienții: %s"
	pageSetupMicrosoft  = "Clientul Microsoft (doar dacă verificarea A5 a trecut)"
	pageSetupID         = "ID-ul clientului"
	pageSetupSecret     = "Secretul clientului"
	pageDeviceTitle     = "Codul de conectare"
	pageDeviceHelp      = "Deschideți adresa de mai jos pe orice dispozitiv, scrieți codul și acceptați contul de test."
	pageReaderTitle     = "Cititorul web (vizibilitate)"
	pageReaderHelp      = "Pagina citește fișierele de probă ale fiecărui client al hub-ului cu clientul web și trimite doar da/nu. Lăsați-o vizibilă până scrie «Gata»."
	pageReaderStart     = "Pornește citirea"
	pageReaderDone      = "Gata. Puteți închide pagina."
	pageReaderResumed   = "Pagina a fost ascunsă: citirea a fost întreruptă și reîncepe acum."
	pageReaderSignIn    = "Conectați-vă cu contul de test Google când vi se cere."
	pageStatusTitle     = "Starea probelor"
	pageCallbackOK      = "Autentificare reușită. Puteți închide pagina."
	pageCallbackFailed  = "Autentificarea a eșuat. Încercați din nou din aplicație."
	pageNoClient        = "Lipsește clientul web: rulați uploadspike setup."
	pageVisibleWarning  = "Țineți pagina vizibilă (fereastră alăturată, fără blocarea ecranului) până la «Gata»."
	pageClientAndroid   = "Android"
	pageClientDesktop   = "Desktop (Windows)"
	pageClientTV        = "TV (dispozitive cu intrare limitată)"
	pageClientWeb       = "Web (cititorul)"
	pageClientUnknown   = "nerecunoscut: alegeți tipul"
	pageClientDetected  = "recunoscut: %s"
	pageSetupAutoButton = "Configurare automată"
)
