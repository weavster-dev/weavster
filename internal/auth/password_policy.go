package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/argon2"
)

// ErrPasswordReused is returned when a new password matches a prior one.
var ErrPasswordReused = errors.New("auth: password was recently used")

// PasswordPolicy is the configurable password policy (spec §2.13.41, §4.4).
// Character-class counts use -1 to forbid the class, 0 for no requirement.
type PasswordPolicy struct {
	MinLength   int
	MinUpper    int
	MinLower    int
	MinNumeric  int
	MinSpecial  int
	Expiration  int // days until expiry (0 = never)
	Grace       int // grace days after expiry (0 = none)
	ReusePeriod int // days a password is remembered (unused beyond ReuseLimit)
	ReuseLimit  int // number of prior passwords to remember
}

// Validate checks a password against the policy (spec §2.13.41).
func (p PasswordPolicy) Validate(password string) error {
	if len(password) < p.MinLength {
		return fmt.Errorf("auth: password shorter than %d characters", p.MinLength)
	}
	upper, lower, numeric, special := 0, 0, 0, 0
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			upper++
		case unicode.IsLower(r):
			lower++
		case unicode.IsDigit(r):
			numeric++
		default:
			special++
		}
	}
	if p.MinUpper == -1 && upper > 0 {
		return errors.New("auth: password must not contain uppercase characters")
	}
	if p.MinLower == -1 && lower > 0 {
		return errors.New("auth: password must not contain lowercase characters")
	}
	if p.MinNumeric == -1 && numeric > 0 {
		return errors.New("auth: password must not contain digits")
	}
	if p.MinSpecial == -1 && special > 0 {
		return errors.New("auth: password must not contain special characters")
	}
	if upper < p.MinUpper || lower < p.MinLower || numeric < p.MinNumeric || special < p.MinSpecial {
		return errors.New("auth: password does not meet character-class requirements")
	}
	return nil
}

// CheckExpired returns an error when the password has exceeded its lifetime.
func (p PasswordPolicy) CheckExpired(changedAt time.Time) error {
	if p.Expiration <= 0 {
		return nil
	}
	expires := changedAt.Add(time.Duration(p.Expiration) * 24 * time.Hour)
	if time.Now().After(expires) {
		return errors.New("auth: password expired")
	}
	return nil
}

func (p PasswordPolicy) reused(password string, history []string) bool {
	if p.ReuseLimit <= 0 {
		return false
	}
	for _, h := range history {
		if VerifyPassword(h, password) {
			return true
		}
	}
	return false
}

const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
)

// HashPassword hashes a password with Argon2id (arch §3.1 framework).
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}

// VerifyPassword checks a password against an Argon2id hash in constant time.
func VerifyPassword(encoded, password string) bool {
	parts := strings.SplitN(encoded, "$", 2)
	if len(parts) != 2 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
