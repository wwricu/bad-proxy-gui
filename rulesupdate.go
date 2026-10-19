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
func (a *App) UpdateRules() (string, error) {
	// Always write into the data dir: ensureRulesDat reads that path first, so a
	// file installed here takes precedence over the bundled copy for as long as
	// it is there.
	dir, err := appConfigDir()
	if err != nil {
		return "", fmt.Errorf("no writable data directory: %w", err)
	}
	n, err := installRulesDat(rulesDataURL, filepath.Join(dir, "rules.dat"))
	if err != nil {
		return "", err
	}
	// The core reads the named lists when the engine starts, so the new rules
	// only bite after a restart. This is the same cheap restart every other
	// setting change already costs.
	if err := a.restartEngine(); err != nil {
		return "", err
	}
	return fmt.Sprintf("Rules updated (%d KB).", n/1024), nil
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
