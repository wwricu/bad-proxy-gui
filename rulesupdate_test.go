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

// The second promise the settings button is built on: a click that cannot find
// anything new must not disturb the running engine. That is decided here, from
// what the server says about its build, without downloading 2.4 MB to find out.
func TestSameRulesBuild(t *testing.T) {
	record := func(t *testing.T, etag, date string) string {
		t.Helper()
		dir := t.TempDir()
		if err := rememberRules(dir, etag, date); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("an unchanged etag is the same build", func(t *testing.T) {
		dir := record(t, `"abc"`, "Mon, 19 Oct 2026 16:13:35 GMT")
		if !sameRulesBuild(dir, `"abc"`, "Tue, 20 Oct 2026 16:13:35 GMT") {
			t.Error("the same ETag under a different date is still the file we hold")
		}
	})

	// The case that pays: the dataset is rebuilt on the same day, so a date
	// alone cannot say "nothing new" while the ETag can.
	t.Run("a same-day rebuild is fetched", func(t *testing.T) {
		dir := record(t, `"abc"`, "Mon, 19 Oct 2026 08:00:00 GMT")
		if sameRulesBuild(dir, `"def"`, "Mon, 19 Oct 2026 16:13:35 GMT") {
			t.Error("a new build on the old day must be downloaded")
		}
	})

	t.Run("without an etag the date decides", func(t *testing.T) {
		const day = "Mon, 19 Oct 2026 16:13:35 GMT"
		dir := record(t, "", day)
		if !sameRulesBuild(dir, "", day) {
			t.Error("a server that only sends Last-Modified still has a way to say nothing changed")
		}
		if sameRulesBuild(dir, "", "Tue, 20 Oct 2026 16:13:35 GMT") {
			t.Error("a later date is a different build")
		}
	})

	// Nothing recorded is the state after an install, a bundle, or a hand-dropped
	// dataset. Treating it as "up to date" would silently freeze the rules.
	t.Run("an unrecorded dataset is never already current", func(t *testing.T) {
		fresh := t.TempDir()
		if sameRulesBuild(fresh, `"abc"`, "Mon, 19 Oct 2026 16:13:35 GMT") {
			t.Error("with no record the first click must download")
		}
		noEtag := record(t, "", "")
		if sameRulesBuild(noEtag, `"abc"`, "Mon, 19 Oct 2026 16:13:35 GMT") {
			t.Error("an empty record must not match a header the server did send")
		}
		if sameRulesBuild(noEtag, "", "") {
			t.Error("a server that says nothing about its build cannot be trusted to be unchanged")
		}
	})
}

func TestReadRulesMeta(t *testing.T) {
	t.Run("round-trips both fields", func(t *testing.T) {
		dir := t.TempDir()
		const day = "Mon, 19 Oct 2026 16:13:35 GMT"
		if err := rememberRules(dir, `"abc"`, day); err != nil {
			t.Fatal(err)
		}
		if etag, mod := readRulesMeta(dir); etag != `"abc"` || mod != day {
			t.Errorf("read back %q %q, want the two values written", etag, mod)
		}
	})

	// An absent header is written as an empty line, and a truncated file reads as
	// the same thing: neither is a value, so neither can match a header.
	t.Run("tolerates a short or absent record", func(t *testing.T) {
		dir := t.TempDir()
		if etag, mod := readRulesMeta(dir); etag != "" || mod != "" {
			t.Errorf("a missing record read as %q %q", etag, mod)
		}
		if err := os.WriteFile(rulesMetaFile(dir), []byte("\"abc\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if etag, mod := readRulesMeta(dir); etag != `"abc"` || mod != "" {
			t.Errorf("a one-line record read as %q %q", etag, mod)
		}
	})
}

func TestRulesDay(t *testing.T) {
	if got := rulesDay("Mon, 19 Oct 2026 16:13:35 GMT"); got != "2026-10-19" {
		t.Errorf("rendered %q, want the day the header names", got)
	}
	for _, none := range []string{"", "not a date", "19/10/2026"} {
		if got := rulesDay(none); got != "" {
			t.Errorf("rulesDay(%q) = %q, want nothing stated", none, got)
		}
	}
}

// rulesUpstream is the whole cost of a no-op click, so it is worth checking it
// asks the way GitHub answers: a HEAD that follows the release alias's redirect
// and reads the asset's own validators off the final response.
func TestRulesUpstream(t *testing.T) {
	const day = "Mon, 19 Oct 2026 16:13:35 GMT"
	asset := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"0x8DF2"`)
		w.Header().Set("Last-Modified", day)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(asset)
	t.Cleanup(srv.Close)

	// The alias moves with each build, exactly as the releases/latest URL does.
	alias := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("the version check must not fetch the dataset, got a %s", r.Method)
		}
		http.Redirect(w, r, srv.URL+"/rules.dat", http.StatusFound)
	}))
	t.Cleanup(alias.Close)

	t.Run("reads the validators off the redirect", func(t *testing.T) {
		etag, mod, err := rulesUpstream(alias.URL + "/rules.dat")
		if err != nil {
			t.Fatalf("a reachable build must be describable: %v", err)
		}
		if etag != `"0x8DF2"` || mod != day {
			t.Errorf("got etag %q modified %q", etag, mod)
		}
	})

	// A check that cannot answer is not a verdict: the caller falls through to
	// the download, so what matters is that it fails as an error, not a match.
	t.Run("reports a refusal", func(t *testing.T) {
		gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(gone.Close)
		if _, _, err := rulesUpstream(gone.URL + "/rules.dat"); err == nil {
			t.Error("a 404 must be an error, not an empty version to compare")
		}
	})
}
