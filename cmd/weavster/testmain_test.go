package main

import (
	"os"
	"testing"
)

// testAdminPassword is the bootstrap admin password every composed server in
// this package's tests starts with (D-22).
const testAdminPassword = "Test-Admin-Pass-1"

func TestMain(m *testing.M) {
	_ = os.Setenv(envBootstrapPassword, testAdminPassword)
	os.Exit(m.Run())
}
