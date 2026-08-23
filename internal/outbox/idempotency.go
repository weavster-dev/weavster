package outbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// IdempotencyKey derives the deterministic idempotency key for an external
// side effect from (message_id, destination, attempt) (gap #5).
func IdempotencyKey(messageID, dest string, attempt int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", messageID, dest, attempt)))
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
