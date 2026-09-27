package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/weavster-dev/weavster/internal/adapters"
	"github.com/weavster-dev/weavster/internal/codecs"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// mllpIdleTimeout closes an mllp source connection that sends nothing for
// this long.
const mllpIdleTimeout = 5 * time.Minute

// mllpHandler runs each HL7 v2 message an mllp source receives through flow
// id and answers it with an ACK (#107 D-60): AA once the message is stored,
// AR when it is refused (resending it unchanged would be refused again), AE
// when it could not be stored (the sender may try again). MSA-3 says why in
// fixed words, never with message content.
func mllpHandler(id string, ingest gateway.SourceIngester) adapters.MLLPHandler {
	return func(frame []byte, readErr error) []byte {
		ack := func(code, text string) []byte {
			// HL7ACK answers any input (the HL7 parser accepts every byte string).
			b, _ := codecs.HL7ACK(frame, codecs.HL7AckOptions{Code: code, Text: text, ControlID: newControlID(), Now: time.Now()})
			return b
		}
		if errors.Is(readErr, adapters.ErrMLLPFrameTooLarge) {
			return ack(codecs.AckApplicationReject, "message larger than 10 MiB")
		}
		if !bytes.HasPrefix(bytes.TrimLeft(frame, "\r\n\t "), []byte("MSH")) {
			return ack(codecs.AckApplicationReject, "not an HL7 v2 message (no MSH segment)")
		}
		res, err := ingest.IngestFrom(context.Background(), id, frame, map[string]string{"source.mllp.controlId": controlID(frame)})
		switch {
		case err == nil, res.ID != "":
			// Stored: the flow has it, and a resend would duplicate it.
			return ack(codecs.AckApplicationAccept, "")
		case errors.Is(err, gateway.ErrInvalidMessage):
			return ack(codecs.AckApplicationReject, "message refused by the flow")
		case errors.Is(err, gateway.ErrFlowNotRunning), errors.Is(err, gateway.ErrFlowNotFound):
			return ack(codecs.AckApplicationError, "flow is not accepting messages")
		default:
			return ack(codecs.AckApplicationError, "message could not be processed")
		}
	}
}

// controlID is the message's MSH-10.
func controlID(frame []byte) string {
	v, _ := codecs.HL7v2().Parse(frame) // never fails
	for _, seg := range v.(*codecs.HL7Message).Segments {
		if seg.Name == "MSH" {
			if f := seg.Field(10); len(f) > 0 {
				return f[0]
			}
			return ""
		}
	}
	return ""
}

// newControlID is a unique MSH-10 for an ACK: 20 hex characters, the
// HL7 v2 field's usual limit.
func newControlID() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
