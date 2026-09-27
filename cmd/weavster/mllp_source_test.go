package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/adapters"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// mllpIngest answers with res and err.
type mllpIngest struct {
	res gateway.IngestResult
	err error
}

func (m mllpIngest) IngestFrom(context.Context, string, []byte, map[string]string) (gateway.IngestResult, error) {
	return m.res, m.err
}

// TestMLLPHandler: each outcome gets the ACK code and text D-60 gives it.
func TestMLLPHandler(t *testing.T) {
	msg := []byte("MSH|^~\\&|LAB|HOSP|WEAVSTER|HOSP|20260927120000||ORU^R01|C1|P|2.5\rOBX|1\r")
	for _, tt := range []struct {
		name    string
		frame   []byte
		readErr error
		ingest  mllpIngest
		want    string
	}{
		{"stored", msg, nil, mllpIngest{res: gateway.IngestResult{ID: "m1"}}, "MSA|AA|C1"},
		{"stored, then failed", msg, nil, mllpIngest{res: gateway.IngestResult{ID: "m1"}, err: errors.New("disk full")}, "MSA|AA|C1"},
		{"refused", msg, nil, mllpIngest{err: fmt.Errorf("%w: body must be a JSON object", gateway.ErrInvalidMessage)}, "MSA|AR|C1|message refused by the flow"},
		{"not running", msg, nil, mllpIngest{err: gateway.ErrFlowNotRunning}, "MSA|AE|C1|flow is not accepting messages"},
		{"removed", msg, nil, mllpIngest{err: gateway.ErrFlowNotFound}, "MSA|AE|C1|flow is not accepting messages"},
		{"failed", msg, nil, mllpIngest{err: errors.New("disk full")}, "MSA|AE|C1|message could not be processed"},
		{"too large", nil, adapters.ErrMLLPFrameTooLarge, mllpIngest{}, "MSA|AR||message larger than 10 MiB"},
		{"not HL7", []byte("hello"), nil, mllpIngest{}, "MSA|AR||not an HL7 v2 message (no MSH segment)"},
	} {
		ack := string(mllpHandler("f", tt.ingest)(tt.frame, tt.readErr))
		segs := strings.Split(strings.TrimSuffix(ack, "\r"), "\r")
		if len(segs) != 2 || segs[1] != tt.want {
			t.Errorf("%s: %q, want MSA %q", tt.name, ack, tt.want)
		}
		if f := strings.Split(segs[0], "|"); len(f) < 10 || len(f[9]) != 20 {
			t.Errorf("%s: ACK control id %q", tt.name, segs[0])
		}
	}
	if got := controlID([]byte("PID|1\r")); got != "" {
		t.Errorf("control id without MSH = %q", got)
	}
	if got := controlID([]byte("MSH|^~\\&|A\r")); got != "" {
		t.Errorf("control id of a short MSH = %q", got)
	}
}
