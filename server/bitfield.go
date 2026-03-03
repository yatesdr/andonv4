package server

// bitfield.go — Pure functions for decoding PLC DINT value encodings.
//
// Bit-level state mappings (process/press bit labels, visual rules) are
// defined exclusively in the visual-mappings UI and stored in config.json.
// The browser-side mapping engine (render.js DEFAULT_MAPPINGS / visual-mappings
// API) is the single source of truth for rendering.
//
// Server-side logic (event logging, reports, overcycle detection) uses the
// named bit-position constants defined in bits.go.

// ProcessCount represents the decoded count DINT for a process cell.
// Low 16 bits = part_id, high 16 bits = counter.
type ProcessCount struct {
	PartID  int16 `json:"part_id"`
	Counter int16 `json:"counter"`
}

// DecodeProcessCount decodes a 32-bit DINT into ProcessCount.
func DecodeProcessCount(v int32) ProcessCount {
	return ProcessCount{
		PartID:  int16(v & 0xFFFF),
		Counter: int16((v >> 16) & 0xFFFF),
	}
}
