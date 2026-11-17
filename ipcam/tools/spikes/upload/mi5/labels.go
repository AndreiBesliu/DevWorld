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
	// The lab's settings.
	MsgPartSizeNeedsFakes = "--part-size se folosește doar cu --fakes (laboratorul)."
	MsgOneDriveDeferred   = "amânat în Faza 0-bis: înregistrarea Microsoft cere un director plătit (A5)"
	// G14's floor of the block (P22).
	ReasonBlockLeaked = "blocarea nu a oprit urcarea hub-ului"
	// G14's plans B.
	PlanBOutbox     = "outbox-ul se corectează înainte de Faza 3"
	PlanBTwoMinSegs = "segmente de 2 min"
)
