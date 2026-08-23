package codecs

import (
	"fmt"
	"math/big"
	"strings"
)

// NCPDP returns the NCPDP pharmacy-claims codec. The NCPDP Telecommunication
// Standard uses ASCII control delimiters: field separator 0x1C (FS),
// component separator 0x1D (GS), segment terminator 0x1E (RS).
func NCPDP() *EDICodec {
	return newEDI("ncpdp", 0x1E, 0x1C, 0x1D, nil)
}

// FormatAmount encodes a decimal amount (e.g. "12.34") as an NCPDP fixed-width,
// zero-padded integer field with an implied two-decimal point, e.g. "001234".
func FormatAmount(amount string, width int) (string, error) {
	clean := strings.TrimSpace(amount)
	if clean == "" {
		return "", fmt.Errorf("codec: ncpdp: empty amount")
	}
	v, ok := new(big.Rat).SetString(clean)
	if !ok {
		return "", fmt.Errorf("codec: ncpdp: invalid amount %q", amount)
	}
	// Scale to implied two decimals (cents).
	cents := new(big.Rat).Mul(v, big.NewRat(100, 1))
	if !cents.IsInt() {
		return "", fmt.Errorf("codec: ncpdp: amount %q exceeds two decimals", amount)
	}
	s := cents.Num().String()
	if strings.HasPrefix(s, "-") {
		return "", fmt.Errorf("codec: ncpdp: negative amount %q not supported", amount)
	}
	if len(s) > width {
		return "", fmt.Errorf("codec: ncpdp: amount %q overflows width %d", amount, width)
	}
	return strings.Repeat("0", width-len(s)) + s, nil
}

// ParseAmount decodes an NCPDP fixed-width field (implied two decimals) back
// into a decimal string, e.g. "001234" -> "12.34".
func ParseAmount(field string) (string, error) {
	s := strings.TrimSpace(field)
	if s == "" {
		return "0.00", nil
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return "", fmt.Errorf("codec: ncpdp: invalid numeric field %q", field)
		}
	}
	cents := new(big.Int)
	cents.SetString(s, 10)
	whole := new(big.Int).Div(cents, big.NewInt(100))
	frac := new(big.Int).Mod(cents, big.NewInt(100))
	return fmt.Sprintf("%s.%02d", whole.String(), frac.Int64()), nil
}
