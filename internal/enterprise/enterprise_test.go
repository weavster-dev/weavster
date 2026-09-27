package enterprise

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrNotImplementedWraps(t *testing.T) {
	err := fmt.Errorf("%w: DICOM", ErrNotImplemented)
	if !errors.Is(err, ErrNotImplemented) || err.Error() != "not implemented in this edition: DICOM" {
		t.Errorf("err = %v", err)
	}
}
