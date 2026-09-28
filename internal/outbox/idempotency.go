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

// No sink is exactly-once: a delivery whose outcome was lost is repeated
// (exactly-once delivery is deferred from the MVP, D-55). The difference is
// whether the receiver gets a key to drop the repeat with.
const (
	// SemanticsKeySent sinks send the idempotency key to the receiver (the
	// HTTP Idempotency-Key header; a flow destination stores it with the
	// target's message and finds it on retry; a database destination
	// inserts it into its keyColumn, when set): effectively once when the
	// receiver honors the key.
	SemanticsKeySent DeliverySemantics = "idempotency-key-sent"
	// SemanticsAtLeastOnce sinks send no key (raw TCP/MLLP, file, SMTP,
	// …); the receiver may see a message twice (gap #5).
	SemanticsAtLeastOnce DeliverySemantics = "at-least-once"
)

// SemanticsForAdapter returns the delivery semantics for an adapter type;
// an unknown type is at-least-once (#107 §8).
func SemanticsForAdapter(adapterType string) DeliverySemantics {
	switch adapterType {
	case "http", "interflow", "database":
		return SemanticsKeySent
	}
	return SemanticsAtLeastOnce
}
