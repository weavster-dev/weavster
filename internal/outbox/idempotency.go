package outbox

import (
	"crypto/sha256"
	"encoding/hex"
)

// IdempotencyKey derives the idempotency key for delivering a message to a
// destination from (message_id, destination). It is the same for every retry
// of that delivery, so a sink can drop duplicates (gap #5, #107 D-10).
func IdempotencyKey(messageID, dest string) string {
	sum := sha256.Sum256([]byte(messageID + "|" + dest))
	return hex.EncodeToString(sum[:])
}

// DeliverySemantics classifies a sink's idempotency guarantees.
type DeliverySemantics string

const (
	// SemanticsExactlyOnce sinks receive and honor the idempotency key.
	SemanticsExactlyOnce DeliverySemantics = "exactly-once"
	// SemanticsAtLeastOnce sinks (raw TCP MLLP) cannot carry a key and are
	// documented as at-least-once (gap #5).
	SemanticsAtLeastOnce DeliverySemantics = "at-least-once"
)

// SemanticsForAdapter returns the delivery semantics for an adapter type.
func SemanticsForAdapter(adapterType string) DeliverySemantics {
	switch adapterType {
	case "tcp", "mllp":
		return SemanticsAtLeastOnce
	default:
		return SemanticsExactlyOnce
	}
}
