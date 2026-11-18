// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

// ServiceTrigger is one trigger of a Windows service, as the service
// manager holds it (SERVICE_TRIGGER): what starts or stops the service, such
// as W32Time's start when the PC joins a domain and its stop when it is not
// in one. `recspike install` saves W32Time's triggers and removes them, and
// uninstall puts them back exactly (C1-R16b).
type ServiceTrigger struct {
	// Type is dwTriggerType, Action dwAction (1 start, 2 stop), Subtype the
	// pTriggerSubtype GUID, "" for none.
	Type    uint32 `json:"type"`
	Action  uint32 `json:"action"`
	Subtype string `json:"subtype,omitempty"`
	// Data is the trigger's data items, in order.
	Data []ServiceTriggerData `json:"data,omitempty"`
}

// ServiceTriggerData is one data item of a trigger
// (SERVICE_TRIGGER_SPECIFIC_DATA_ITEM): its type and its bytes.
type ServiceTriggerData struct {
	Type  uint32 `json:"type"`
	Bytes []byte `json:"bytes"`
}
