// Package rules is the routing dataset: the compiled domain lists the core
// router reads, and how a newer build of it gets onto the disk.
//
// It is deliberately free of the app around it - no state, no engine, no window
// - so the download and its checks are ordinary Go tests rather than tests that
// have to build the whole GUI to run.
package rules

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wwricu/bad-proxy-gui/internal/platform"
)

// URL is the newest dataset the upstream repo publishes. Its release workflow
// moves the releases/latest alias to each build, so this URL always resolves to
// the freshest rules.dat while every dated release stays downloadable for
// rollback.
const URL = "https://github.com/wwricu/domain-list-community/releases/latest/download/rules.dat"

// DatasetName is the file the core router is pointed at, and the one Install
// swaps. It is a name and not a path because where it lives is the app's
// business: Path below resolves it.
const DatasetName = "rules.dat"

// metaName records which upstream build the installed dataset is. Only the
// update path writes it, so a bundled or hand-dropped dataset has no record and
// reads as "version unknown".
const metaName = "rules.meta"

const (
	// The compiled dataset is ~2.4 MB. Anything under this floor is a truncated
	// body or an error page, never a real rules file, so it must not be trusted.
	minRulesBytes = 256 * 1024
	// A generous ceiling: the real file is far below it, and it bounds what a
	// wrong endpoint can make the app write down.
	maxRulesBytes = 64 * 1024 * 1024
	// 30 s carries 2.4 MB on a slow link, and a dead network should say so
	// inside that budget rather than after a minute of "Updating...".
	rulesDownloadTimeout = 30 * time.Second
	// The opening bytes kept for the shape check - enough to recognise an HTML
	// document without holding the body that follows it.
	rulesHeadBytes = 64
)

// Path is the dataset inside dir.
func Path(dir string) string { return filepath.Join(dir, DatasetName) }

// Installed reports whether dir holds a dataset at all. The record beside it can
// outlive the file, so this is the question that has to be answered before
// anything is called up to date.
func Installed(dir string) bool {
	st, err := os.Stat(Path(dir))
	return err == nil && !st.IsDir()
}

// Day renders an HTTP date as the one thing worth showing: which build this is,
// to the day. Absent or unparseable says nothing.
func Day(modified string) string {
	t, err := http.ParseTime(modified)
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// Upstream asks the endpoint what it holds without fetching the dataset. GitHub
// answers a HEAD of the release alias with the asset's own validators, so the
// ETag is the identity and Last-Modified is the date to show.
func Upstream(url string) (etag, modified string, err error) {
	// 8 s: a version check that cannot answer by then is not going to - the
	// caller falls through to the download anyway.
	client := &http.Client{Timeout: 8 * time.Second}
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

// SameBuild reports whether the dataset already installed in dir is the upstream
// build the server just described. The ETag is the identity when the server sends
// one; a build that only answers with Last-Modified is compared on that date,
// which lets a same-day rebuild through as "already up to date" - and that
// costs nothing, since the dataset is rebuilt once a day anyway.
//
// A dataset that is not there is not the same build, whatever the record beside
// it says: the record outlives a deleted file, and answering "up to date" then
// would leave the engine pointed at nothing.
func SameBuild(dir, etag, modified string) bool {
	if !Installed(dir) {
		return false
	}
	if etag != "" {
		return etag == storedRulesETag(dir)
	}
	return modified != "" && modified == storedRulesDate(dir)
}

// Remember records what the server said about the build just installed, so the
// next comparison has something to work from. An absent header is recorded as an
// empty line and only costs that comparison.
func Remember(dir, etag, modified string) error {
	body := fmt.Sprintf("%s\n%s\n", etag, modified)
	return os.WriteFile(metaFile(dir), []byte(body), 0o644)
}

// metaFile is the small record beside the dataset that says which upstream build
// that dataset is. Only the update path writes it.
func metaFile(dir string) string { return filepath.Join(dir, metaName) }

// StoredDate is the build date recorded for the dataset in dir, rendered as a
// day. Empty means the app has never replaced its bundled copy, which has no
// date worth claiming.
func StoredDate(dir string) string { return Day(storedRulesDate(dir)) }

// readRulesMeta returns the ETag and date recorded for the installed dataset. A
// missing or short file reads as "nothing known", which is what a bundled or
// hand-dropped dataset is: it has no provenance, and guessing from the file's
// own mtime would claim a copy merely reinstalled today is a week old.
func readRulesMeta(dir string) (etag, modified string) {
	b, err := os.ReadFile(metaFile(dir))
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

// Install fetches the dataset at url and swaps it into target, leaving the
// previous file in place unless the whole download checks out. It reports how
// many bytes went in.
//
// The safety comes from the order: stream the bytes into a temp file beside the
// target, validate what was measured, then rename over the target in one step. A
// failure anywhere before the rename leaves the running dataset untouched, so a
// bad download cannot break routing - the worst case is "nothing changed".
//
// It streams rather than buffering because the body is going to a file anyway:
// holding two and a half megabytes in memory to write them out again buys
// nothing, and a ceiling applied to a buffer silently *is* the truncation - the
// copy stops at the ceiling, the size check is built on the same number, and a
// wrong endpoint's oversized stream lands as a short dataset that passed.
func Install(url, target string) (int, error) {
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

	tmp, err := os.CreateTemp(dir, "rules-*.tmp")
	if err != nil {
		return 0, fmt.Errorf("could not stage the download: %w", err)
	}
	tmpName := tmp.Name()
	// Cleared on every path except the one where the rename consumes it; after a
	// successful install this file no longer exists and Remove is a no-op. The
	// handle is released first - a Windows file still open cannot be deleted.
	defer os.Remove(tmpName)
	defer tmp.Close()

	head, total, err := readRulesStream(tmp, resp.Body)
	if err != nil {
		return 0, err
	}
	if err := validateRulesDat(head, total); err != nil {
		return 0, err
	}
	if err := tmp.Sync(); err != nil {
		return 0, fmt.Errorf("flush failed: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("close failed: %w", err)
	}
	// rename(2) on Unix and MoveFileEx with REPLACE_EXISTING on Windows: either
	// the old dataset or the new one is present, never a mixture.
	if err := platform.RenameOver(tmpName, target); err != nil {
		return 0, fmt.Errorf("install failed: %w", err)
	}
	return int(total), nil
}

// readRulesStream copies the body into dst as it arrives and returns what will be
// checked once it is all in: the opening bytes of the document, and how many
// bytes there were in total.
//
// The budget is the ceiling plus one byte, and that one byte is the point: it is
// what tells a body which filled the ceiling from one which went past it. A copy
// that stops *at* the ceiling leaves a truncation whose size is exactly what the
// check expects, and a wrong endpoint's oversized stream installs as a short
// dataset that passed.
func readRulesStream(dst io.Writer, src io.Reader) (head []byte, total int64, err error) {
	budget := io.LimitReader(src, int64(maxRulesBytes)+1)
	headBuf := make([]byte, rulesHeadBytes)
	n, err := io.ReadFull(budget, headBuf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, 0, fmt.Errorf("read failed: %w", err)
	}
	headBuf = headBuf[:n]
	// The head is kept to be looked at, not to be dropped: it goes back on the
	// stream, so the file ends up holding the whole document.
	written, err := io.Copy(dst, io.MultiReader(bytes.NewReader(headBuf), budget))
	if err != nil {
		return nil, 0, fmt.Errorf("read failed: %w", err)
	}
	return headBuf, written, nil
}

// validateRulesDat rejects anything that is not plausibly a full compiled
// dataset, so a 200 response carrying an HTML error page, a truncated body, or a
// stream from some other endpoint can never be installed in place of working
// rules.
func validateRulesDat(head []byte, total int64) error {
	if total > int64(maxRulesBytes) {
		return fmt.Errorf("download was %d bytes, larger than the rules dataset can be - keeping the current one", total)
	}
	if total < minRulesBytes {
		return fmt.Errorf("download was only %d bytes, too small to be the rules dataset - keeping the current one", total)
	}
	// A real dataset is protobuf: its first byte is never the '<' of an HTML
	// document. GitHub serves some mistakes as an HTML page with status 200.
	if strings.HasPrefix(strings.TrimLeft(string(head), " \t\r\n"), "<") {
		return fmt.Errorf("download was an HTML page, not a rules dataset - keeping the current one")
	}
	return nil
}
