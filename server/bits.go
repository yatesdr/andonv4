// bits.go — PLC bit constants and andon definitions shared across the server.
package server

// Process state bit positions (within state DINT).
// Used by server-side logic (event logging, reports, overcycle detection).
// Visual rendering rules remain in the JS-only mappings.
const (
	BitHeartbeat        = 0
	BitFaulted          = 3
	BitInCycle          = 4
	BitEStop            = 5
	BitClearToEnter     = 6
	BitLCBroken         = 7
	BitCycleStart       = 8
	BitStarved          = 9
	BitBlocked          = 10
	BitRedRabbit        = 11
	BitProdAndon        = 14
	BitMaintAndon       = 15
	BitLogisticsAndon   = 16
	BitQualityAndon     = 17
	BitHRAndon          = 18
	BitEmergencyAndon   = 19
	BitToolingAndon     = 20
	BitEngineeringAndon = 21
	BitControlsAndon    = 22
	BitITAndon          = 23
	BitPartKicked       = 24
	BitToolChangeActive = 25
)

// Computed state bit positions (within computedState DINT).
const (
	CsBitBehind            = 0
	CsBitOvercycle         = 1
	CsBitFirstHourMet      = 2
	CsBitFirstHourComplete = 3
)

// AndonDef ties a state bit position to its human-readable label and short type key.
// Used by reports, exports, and shift summaries to iterate andon categories uniformly.
type AndonDef struct {
	Bit   int
	Label string
	Type  string
}

// AndonDefs is the canonical list of andon call categories.
// Add or reorder entries here when PLC andon bits change — all report/export
// code iterates this slice instead of maintaining its own parallel arrays.
var AndonDefs = []AndonDef{
	{BitProdAndon, "Production", "prod"},
	{BitMaintAndon, "Maintenance", "maint"},
	{BitLogisticsAndon, "Logistics", "logistics"},
	{BitQualityAndon, "Quality", "quality"},
	{BitHRAndon, "HR", "hr"},
	{BitEmergencyAndon, "Emergency", "emergency"},
	{BitToolingAndon, "Tooling", "tooling"},
	{BitEngineeringAndon, "Engineering", "engineering"},
	{BitControlsAndon, "Controls", "controls"},
	{BitITAndon, "IT", "it"},
}
