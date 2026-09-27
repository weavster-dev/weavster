package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/weavster-dev/weavster/internal/adapters"
	"github.com/weavster-dev/weavster/internal/codecs"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// mllpIdleTimeout closes an mllp source connection that sends nothing for
// this long.
const mllpIdleTimeout = 5 * time.Minute

// mllpFrameTimeout bounds receiving one message once it started arriving
// (10 MiB over a slow link takes minutes).
const mllpFrameTimeout = 15 * time.Minute

// maxControlIDMetadata caps the MSH-10 kept as metadata; HL7 allows at most
// 199 characters, most versions 20.
const maxControlIDMetadata = 199

// mllpHandler runs each HL7 v2 message an mllp source receives through flow
// id and answers it with an ACK (#107 D-60): AA once the message is stored,
// AR when it is refused (resending it unchanged would be refused again), AE
// when it could not be stored (the sender may try again). MSA-3 says why in
// fixed words, never with message content.
func mllpHandler(id string, ingest gateway.SourceIngester) adapters.MLLPHandler {
	tooLarge := fmt.Sprintf("message larger than %d MiB", gateway.MaxMessageBytes>>20)
	return func(frame []byte, readErr error) []byte {
		msh := firstSegment(frame) // the ACK needs only MSH
		ack := func(code, text string) []byte {
			// HL7ACK answers any input (the HL7 parser accepts every byte string).
			b, _ := codecs.HL7ACK(msh, codecs.HL7AckOptions{Code: code, Text: text, ControlID: newControlID(), Now: time.Now()})
			return b
		}
		if errors.Is(readErr, adapters.ErrMLLPFrameTooLarge) {
			return ack(codecs.AckApplicationReject, tooLarge)
		}
		if !isMSH(msh) {
			return ack(codecs.AckApplicationReject, "not an HL7 v2 message (no MSH segment)")
		}
		cid := controlID(msh)
		if len(cid) > maxControlIDMetadata {
			cid = cid[:maxControlIDMetadata]
		}
		res, err := ingest.IngestFrom(context.Background(), id, frame, map[string]string{"source.mllp.controlId": cid})
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

// firstSegment is frame up to its first segment terminator, skipping line
// breaks before it.
func firstSegment(frame []byte) []byte {
	frame = bytes.TrimLeft(frame, "\r\n")
	if i := bytes.IndexAny(frame, "\r\n"); i >= 0 {
		return frame[:i]
	}
	return frame
}

// isMSH reports whether seg is an MSH segment: "MSH" and its field
// separator, which is not a letter, digit, or space.
func isMSH(seg []byte) bool {
	if len(seg) < 4 || !bytes.HasPrefix(seg, []byte("MSH")) {
		return false
	}
	c := seg[3]
	alnum := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
	return !alnum && c != ' '
}

// controlID is MSH-10 of the MSH segment msh.
func controlID(msh []byte) string {
	v, _ := codecs.HL7v2().Parse(msh) // never fails
	for _, seg := range v.(*codecs.HL7Message).Segments {
		if f := seg.Field(10); seg.Name == "MSH" && len(f) > 0 {
			return f[0]
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
