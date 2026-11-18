// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi5

// The lines of MI5 that the owner reads, Romanian (Task 12 checks the
// guide against them).
const (
	// The block's checks (Verification).
	MsgBlockCutLAN     = "Blocarea a tăiat și rețeaua locală; proba s-a oprit."
	MsgProviderReachV4 = "Furnizorul e încă accesibil prin IPv4; blocarea nu e completă."
	MsgProviderReachV6 = "Furnizorul e încă accesibil prin IPv6; blocarea nu e completă."
	// The host (P17, P38).
	MsgPiUnknown = "Nu știm câte camere poate înregistra acest Pi. Rulați proba MI5 pe PC: uploadspike setup, apoi uploadspike mi5."
	// The pre-checks (P38, P53).
	MsgQuotaLow       = "Destinația %s are %s liberi; proba are nevoie de cel puțin %s (de 2 ori cât se adună în %s de blocare)."
	MsgSpoolLow       = "Discul înregistrărilor are %s liberi; proba are nevoie de cel puțin %s (de 1,5 ori cât se adună în blocare)."
	MsgPersonalLow    = "Drive-ul personal are %s liberi; cu --drive personal proba are nevoie de cel puțin 11 GB."
	MsgRefreshInBlock = "Blocarea ar acoperi reîmprospătarea zilnică a %s (%s). Porniți proba MI5 după %s."
	MsgLanSilent      = "Routerul nu răspunde în rețeaua locală; proba nu poate verifica blocarea."
	MsgNoGateway      = "Nu găsesc routerul în tabela de rutare; proba nu poate verifica blocarea."
	// The run's progress, so a long run says where it is.
	MsgBlockStarted = "Blocarea a început la %s și ține %s, până în jur de %s."
	MsgUnblocked    = "Blocarea s-a încheiat; hub-ul urcă ce s-a adunat, iar în timpul urcării repornește o dată, ca după o pană."
	MsgRestarted    = "Hub-ul a repornit în mijlocul unui upload; urcarea continuă."
	MsgDrained      = "Tot ce s-a adunat în blocare a urcat în %s; urmează verificarea la furnizor."
	// The run's failures (Task 9B M9, C2-R29 (1): no English reaches the
	// owner): each is a whole line, its cause kept for the lab's detail.
	MsgNoUploadAfterUnblock = "După deblocare, hub-ul nu a ajuns la mijlocul niciunui upload, deci repornirea plănuită nu s-a putut face; proba s-a oprit. Spuneți-ne."
	MsgDrainTooLong         = "Ce s-a adunat în blocare nu a urcat nici în %s de la deblocare; proba s-a oprit. Spuneți-ne."
	MsgDestProbeFailed      = "Destinația %s nu s-a putut verifica (ipcam-hub diag dest-probe); proba s-a oprit. Spuneți-ne."
	MsgAuditFailed          = "Verificarea finală la furnizor (ipcam-hub diag uploads) nu s-a putut face; proba s-a oprit. Spuneți-ne."
	MsgAuditDestFailed      = "Verificarea finală la furnizor (ipcam-hub diag uploads) nu a putut lista destinația %s; proba s-a oprit. Spuneți-ne."
	MsgReachFailed          = "Verificarea blocării (ipcam-hub diag reach) nu s-a putut face; proba s-a oprit. Spuneți-ne."
	MsgReachNoProvider      = "Verificarea blocării (ipcam-hub diag reach) nu a arătat niciun furnizor; proba s-a oprit. Spuneți-ne."
	// The run's settings that the owner typed.
	MsgBadCameras = "valoare greșită pentru --cameras: %d (de la 1 la %d)"
	MsgBadSegment = "valoare greșită pentru --segment-secs: %d (cel puțin 1)"
	MsgBadBlock   = "valoare greșită pentru --block: %s (de exemplu 6h)"
	MsgBadDrive   = "valoare greșită pentru --drive: %s (test, test+personal sau personal)"
	// The lab's settings.
	MsgPartSizeNeedsFakes = "--part-size se folosește doar cu --fakes (laboratorul)."
	MsgOneDriveDeferred   = "amânat în Faza 0-bis: înregistrarea Microsoft cere un director plătit (A5)"
	// G14's floor of the block (P22).
	ReasonBlockLeaked = "blocarea nu a oprit urcarea hub-ului"
	// G14's plans B.
	PlanBOutbox     = "outbox-ul se corectează înainte de Faza 3"
	PlanBTwoMinSegs = "segmente de 2 min"
)
