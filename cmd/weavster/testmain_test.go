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

// sqliteShared makes the tests' own handle on a SQLite file they share
// with the server wait for the server's lock instead of failing (the
// server's connections wait by themselves, sqliteWaits, #389).
const sqliteShared = "?_pragma=busy_timeout(5000)"
