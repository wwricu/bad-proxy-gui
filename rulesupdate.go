package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// rulesDataURL is the newest dataset the upstream repo publishes. Its release
// workflow moves the releases/latest alias to each build, so this URL always
// resolves to the freshest rules.dat while every dated release stays
// downloadable for rollback.
const rulesDataURL = "https://github.com/wwricu/domain-list-community/releases/latest/download/rules.dat"

const (
	// The compiled dataset is ~2.4 MB. Anything under this floor is a truncated
	// body or an error page, never a real rules file, so it must not be trusted.
	minRulesBytes = 256 * 1024
	// A generous ceiling: the real file is far below it, and it bounds what a
	// wrong endpoint can make the app buffer into memory.
	maxRulesBytes        = 64 * 1024 * 1024
	rulesDownloadTimeout = 60 * time.Second
)

// UpdateRules downloads the newest routing dataset, swaps it in and restarts the
// engine so the new lists take effect. It is a bound method called by the
// settings window's Update button; the returned string is the success line shown
// to the user.
//
// A copy that is already the newest costs nothing: the dataset is rebuilt once
// a day, so most clicks are the same file arriving again, and the restart below
// drops every live connection. The upstream's own validators decide that, so an
// afternoon of repeated clicks is one HEAD request each and no restart.
func (a *App) UpdateRules() (string, error) {
	// Always write into the data dir: ensureRulesDat reads that path first, so a
	// file installed here takes precedence over the bundled copy for as long as
	// it is there.
	dir, err := appConfigDir()
	if err != nil {
		return "", fmt.Errorf("no writable data directory: %w", err)
	}
	target := filepath.Join(dir, "rules.dat")

	etag, modified, err := rulesUpstream(rulesDataURL)
	// A date the server did not send is not a version to state: fall back to the
	// sentence that says nothing about which build it is.
	day := rulesDay(modified)
	if err == nil && fileExists(target) && sameRulesBuild(dir, etag, modified) {
		if day == "" {
			return "Already up to date.", nil
		}
		return fmt.Sprintf("Already up to date (%s).", day), nil
	}
	// A failed check is not a reason to refuse the update: the GET below is the
	// same endpoint and answers for itself.
	n, err := installRulesDat(rulesDataURL, target)
	if err != nil {
		return "", err
	}
	// The record is what the next click compares against. A failure to write it
	// only costs that click its shortcut: this dataset is installed and routing.
	_ = rememberRules(dir, etag, modified)
	// The core reads the named lists when the engine starts, so the new rules
	// only bite after a restart. This is the same cheap restart every other
	// setting change already costs.
	if err := a.restartEngine(); err != nil {
		return "", err
	}
	if day == "" {
		return fmt.Sprintf("Rules updated (%d KB).", n/1024), nil
	}
	return fmt.Sprintf("Rules updated to %s (%d KB).", day, n/1024), nil
}

// sameRulesBuild reports whether the upstream build the server just described
// is the one already installed. The ETag is the identity when the server sends
// one; a build that only answers with Last-Modified is compared on that date,
// which lets a same-day rebuild through as "already up to date" - and that
// costs nothing, since the dataset is rebuilt once a day anyway.
func sameRulesBuild(dir, etag, modified string) bool {
	if etag != "" {
		return etag == storedRulesETag(dir)
	}
	return modified != "" && modified == storedRulesDate(dir)
}

// RulesVersion is the day the installed dataset was built, for the settings
// window to show before anything is clicked. Empty means the app has never
// replaced its bundled copy, which has no date worth claiming.
func (a *App) RulesVersion() string {
	dir, err := appConfigDir()
	if err != nil {
		return ""
	}
	return rulesDay(storedRulesDate(dir))
}

// rulesMetaFile names the small record beside rules.dat that says which upstream
// build that dataset is. Only the update path writes it.
func rulesMetaFile(dir string) string { return filepath.Join(dir, "rules.meta") }

// rulesUpstream asks the endpoint what it holds without fetching the dataset.
// GitHub answers a HEAD of the release alias with the asset's own validators,
// so the ETag is the identity and Last-Modified is the date to show.
func rulesUpstream(url string) (etag, modified string, err error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("could not ask the server for the rules version: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("server returned %s", resp.Status)
	}
	return resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), nil
}

// readRulesMeta returns the ETag and date recorded for the installed dataset. A
// missing or short file reads as "nothing known", which is what a bundled or
// hand-dropped dataset is: it has no provenance, and guessing from the file's
// own mtime would claim a copy merely reinstalled today is a week old.
func readRulesMeta(dir string) (etag, modified string) {
	b, err := os.ReadFile(rulesMetaFile(dir))
	if err != nil {
		return "", ""
	}
	line := strings.SplitN(strings.TrimRight(string(b), "\r\n"), "\n", 2)
	if len(line) < 2 {
		return line[0], ""
	}
	return line[0], line[1]
}

func storedRulesETag(dir string) string { etag, _ := readRulesMeta(dir); return etag }
func storedRulesDate(dir string) string { _, mod := readRulesMeta(dir); return mod }

// rememberRules records what the server said about the build just installed, so
// the next click can compare against it. An absent header is recorded as an
// empty line and only costs that comparison.
func rememberRules(dir, etag, modified string) error {
	body := fmt.Sprintf("%s\n%s\n", etag, modified)
	return os.WriteFile(rulesMetaFile(dir), []byte(body), 0o644)
}

// rulesDay renders an HTTP date as the one thing worth showing: which build
// this is, to the day. Absent or unparseable says nothing.
func rulesDay(modified string) string {
	t, err := http.ParseTime(modified)
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// installRulesDat fetches the dataset at url and swaps it into target, leaving
// the previous file in place unless the whole download checks out. It reports
// how many bytes went in.
//
// The safety comes from the order: land the bytes in a temp file beside the
// target, validate them whole, then rename over the target in one step. A
// failure anywhere before the rename leaves the running dataset untouched, so a
// bad download cannot break routing - the worst case is "nothing changed".
func installRulesDat(url, target string) (int, error) {
	// Same directory as the target, so the rename is atomic rather than a
	// copy-and-delete that could be interrupted halfway.
	dir := filepath.Dir(target)

	client := &http.Client{Timeout: rulesDownloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("server returned %s", resp.Status)
	}

	// Buffer the whole body before touching disk: validation needs every byte,
	// and a truncated stream must never reach the rename.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRulesBytes))
	if err != nil {
		return 0, fmt.Errorf("read failed: %w", err)
	}
	if err := validateRulesDat(body); err != nil {
		return 0, err
	}

	tmp, err := os.CreateTemp(dir, "rules-*.tmp")
	if err != nil {
		return 0, fmt.Errorf("could not stage the download: %w", err)
	}
	tmpName := tmp.Name()
	// Cleared on every path except the one where the rename consumes it; after a
	// successful install this file no longer exists and Remove is a no-op.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return 0, fmt.Errorf("write failed: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return 0, fmt.Errorf("flush failed: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("close failed: %w", err)
	}
	// rename(2) on Unix and MoveFileEx with REPLACE_EXISTING on Windows: either
	// the old dataset or the new one is present, never a mixture.
	if err := os.Rename(tmpName, target); err != nil {
		return 0, fmt.Errorf("install failed: %w", err)
	}
	return len(body), nil
}

// validateRulesDat rejects anything that is not plausibly a full compiled
// dataset, so a 200 response carrying an HTML error page or a truncated body
// can never be installed in place of working rules.
func validateRulesDat(b []byte) error {
	if len(b) < minRulesBytes {
		return fmt.Errorf("download was only %d bytes, too small to be the rules dataset - keeping the current one", len(b))
	}
	// A real dataset is protobuf: its first byte is never the '<' of an HTML
	// document. GitHub serves some mistakes as an HTML page with status 200.
	if strings.HasPrefix(strings.TrimLeft(string(b[:64]), " \t\r\n"), "<") {
		return fmt.Errorf("download was an HTML page, not a rules dataset - keeping the current one")
	}
	return nil
}
