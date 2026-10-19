package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRulesDat(t *testing.T) {
	real := append([]byte{0x0f, 0xff, 0x87}, make([]byte, minRulesBytes)...)

	t.Run("accepts a full dataset", func(t *testing.T) {
		if err := validateRulesDat(real); err != nil {
			t.Errorf("valid dataset rejected: %v", err)
		}
	})

	t.Run("rejects a truncated body", func(t *testing.T) {
		if err := validateRulesDat(make([]byte, minRulesBytes-1)); err == nil {
			t.Error("a too-small download must be refused")
		}
	})

	t.Run("rejects an HTML error page", func(t *testing.T) {
		page := []byte("<!DOCTYPE html>\n<html>" + strings.Repeat(" ", minRulesBytes) + "</html>")
		if err := validateRulesDat(page); err == nil {
			t.Error("an HTML page must not be installed as rules")
		}
	})

	t.Run("rejects empty", func(t *testing.T) {
		if err := validateRulesDat(nil); err == nil {
			t.Error("empty body must be refused")
		}
	})
}

// The one promise the settings button is built on: whatever the endpoint
// answers, the app ends up holding either the dataset it already had or the one
// it just downloaded, never a half-written or wrong file. So every case below
// checks the file left on disk, not just the error it returned.
func TestInstallRulesDat(t *testing.T) {
	good := append([]byte{0x0f, 0xff, 0x87}, bytes.Repeat([]byte{0x2a}, minRulesBytes)...)
	page := []byte("<!DOCTYPE html>\n<html>" + strings.Repeat(" ", minRulesBytes) + "</html>")
	truncated := bytes.Repeat([]byte{0x01}, minRulesBytes/2)
	old := []byte("the dataset that was already working")

	// workingDir returns a data dir holding a rules.dat that routes fine.
	workingDir := func(t *testing.T) (dir, target string) {
		t.Helper()
		dir = t.TempDir()
		target = filepath.Join(dir, "rules.dat")
		if err := os.WriteFile(target, old, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	// endpoint answers every request the way GitHub is pretending to.
	endpoint := func(t *testing.T, status int, body []byte) string {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			w.Write(body)
		}))
		t.Cleanup(srv.Close)
		return srv.URL + "/rules.dat"
	}

	staged := func(t *testing.T, dir string) []string {
		t.Helper()
		names, err := filepath.Glob(filepath.Join(dir, "rules-*.tmp"))
		if err != nil {
			t.Fatal(err)
		}
		return names
	}

	t.Run("installs a whole dataset", func(t *testing.T) {
		dir, target := workingDir(t)
		n, err := installRulesDat(endpoint(t, http.StatusOK, good), target)
		if err != nil {
			t.Fatalf("a real dataset must install: %v", err)
		}
		if n != len(good) {
			t.Errorf("reported %d bytes installed, the body was %d", n, len(good))
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, good) {
			t.Error("the file on disk is not what was downloaded")
		}
		if left := staged(t, dir); len(left) != 0 {
			t.Errorf("the staging file was left behind: %v", left)
		}
	})

	for _, bad := range []struct {
		name   string
		status int
		body   []byte
	}{
		// Each case is one the *other* guard would wave through: an HTML page
		// and a short body arrive with a 200, so only the validator can stop
		// them, and a wrong status carries bytes that would pass it, so only
		// the status check can.
		{"an HTML page", http.StatusOK, page},
		{"a truncated body", http.StatusOK, truncated},
		{"a missing release", http.StatusNotFound, good},
		{"a server error", http.StatusInternalServerError, good},
	} {
		t.Run("keeps what works against "+bad.name, func(t *testing.T) {
			dir, target := workingDir(t)
			if _, err := installRulesDat(endpoint(t, bad.status, bad.body), target); err == nil {
				t.Fatalf("%s must not be installed as rules", bad.name)
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, old) {
				t.Errorf("%s replaced the working dataset", bad.name)
			}
			if left := staged(t, dir); len(left) != 0 {
				t.Errorf("%s left a staging file behind: %v", bad.name, left)
			}
		})
	}
}
