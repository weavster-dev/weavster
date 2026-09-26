package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"

	"github.com/weavster-dev/weavster/internal/auth"
)

// Environment variables for the first-run admin password (D-22).
const (
	envBootstrapPassword     = "WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD"
	envBootstrapPasswordFile = "WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE"
	bootstrapAdmin           = "admin"
)

// bootstrapAdminUser creates the first admin account when no users exist
// (D-22). The password comes from WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD or the
// file named by WEAVSTER_BOOTSTRAP_ADMIN_PASSWORD_FILE; otherwise a random
// one-time password is printed once to stderr and must be changed at first
// login.
func bootstrapAdminUser(ctx context.Context, p *auth.LocalProvider, policy auth.PasswordPolicy, stderr io.Writer) error {
	users, err := p.ListUsers(ctx)
	if err != nil || len(users) > 0 {
		return err
	}
	password, generated := os.Getenv(envBootstrapPassword), false
	if file := os.Getenv(envBootstrapPasswordFile); password == "" && file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}
		password = strings.TrimRight(string(data), "\r\n")
	}
	if password == "" {
		if password, err = generatePassword(policy); err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}
		generated = true
	}
	err = p.CreateUser(ctx, auth.User{
		Username: bootstrapAdmin, PasswordHash: password,
		Permissions: []string{auth.PermAdmin}, MustChangePassword: generated,
	})
	if err != nil {
		return fmt.Errorf("bootstrap: admin password rejected by auth.passwordPolicy: %w", err)
	}
	if generated {
		_, _ = fmt.Fprintf(stderr, "First-run admin account created.\n  username: %s\n  password: %s\n"+
			"This password is shown once and must be changed at first login (POST /api/v1/auth/password).\n",
			bootstrapAdmin, password)
	}
	return nil
}

// generatePassword returns a random password that satisfies policy: 24
// characters drawn from every class the policy does not forbid, with at
// least the required count of each class.
func generatePassword(policy auth.PasswordPolicy) (string, error) {
	classes := []struct {
		chars string
		min   int
	}{
		{"ABCDEFGHJKLMNPQRSTUVWXYZ", policy.MinUpper},
		{"abcdefghijkmnopqrstuvwxyz", policy.MinLower},
		{"23456789", policy.MinNumeric},
		{"!#%+-=?@^_", policy.MinSpecial},
	}
	var pool string
	var out []byte
	for _, c := range classes {
		if c.min < 0 {
			continue
		}
		pool += c.chars
		for i := 0; i < c.min; i++ {
			ch, err := pick(c.chars)
			if err != nil {
				return "", err
			}
			out = append(out, ch)
		}
	}
	for len(out) < max(24, policy.MinLength) {
		ch, err := pick(pool)
		if err != nil {
			return "", err
		}
		out = append(out, ch)
	}
	// Shuffle so required characters are not always first.
	for i := len(out) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		out[i], out[j.Int64()] = out[j.Int64()], out[i]
	}
	return string(out), nil
}

func pick(chars string) (byte, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
	if err != nil {
		return 0, err
	}
	return chars[n.Int64()], nil
}
