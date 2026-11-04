package umpire

// FieldAvailability is the per-field status a Check produces.
type FieldAvailability struct {
	Enabled   bool
	Required  bool
	Fair      bool
	Satisfied bool
	// Reason is the first failure reason, or "" when the field passed.
	Reason string
	// Reasons collects every failure reason in evaluation order.
	Reasons []string
	// Valid is the validator outcome; nil when no validator applied.
	Valid *bool
	// Error is the validator's error message, set only when Valid is false.
	Error string
}

// AvailabilityMap maps field names to their status.
type AvailabilityMap = map[string]FieldAvailability

// Foul is a recommended value correction when form state transitions.
type Foul struct {
	Field          string
	Reason         string
	SuggestedValue any
}
