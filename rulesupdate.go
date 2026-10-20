package main

import (
	"fmt"

	"github.com/wwricu/bad-proxy-gui/internal/config"
	"github.com/wwricu/bad-proxy-gui/internal/rules"
)

// UpdateRules downloads the newest routing dataset, swaps it in and restarts the
// engine so the new lists take effect. It is a bound method called by the
// settings window's Update button; the returned string is the success line shown
// to the user. The fetching, the checks and the version record are internal/
// rules' own business - what is business here is where the app keeps its data
// and the fact that a new dataset only takes effect through a restart.
//
// A copy that is already the newest costs nothing: the dataset is rebuilt once
// a day, so most clicks are the same file arriving again, and the restart below
// drops every live connection. The upstream's own validators decide that, so an
// afternoon of repeated clicks is one HEAD request each and no restart.
func (a *App) UpdateRules() (string, error) {
	// Always write into the data dir: ensureRulesDat reads that path first, so a
	// file installed here takes precedence over the bundled copy for as long as
	// it is there.
	dir, err := config.UserDir()
	if err != nil {
		return "", fmt.Errorf("no writable data directory: %w", err)
	}

	etag, modified, err := rules.Upstream(rules.URL)
	// A date the server did not send is not a version to state: fall back to the
	// sentence that says nothing about which build it is.
	day := rules.Day(modified)
	if err == nil && rules.SameBuild(dir, etag, modified) {
		if day == "" {
			return "Up to date.", nil
		}
		return "Up to date: " + day, nil
	}
	// A failed check is not a reason to refuse the update: the GET below is the
	// same endpoint and answers for itself.
	n, err := rules.Install(rules.URL, rules.Path(dir))
	if err != nil {
		return "", err
	}
	// The record is what the next click compares against. A failure to write it
	// only costs that click its shortcut: this dataset is installed and routing.
	_ = rules.Remember(dir, etag, modified)
	// The core reads the named lists when the engine starts, so the new rules
	// only bite after a restart. AffectsEngine cannot see this one: the dataset
	// is not a field of the state, so the change is on the engine by definition.
	if err := a.restartEngine(a.snapshot()); err != nil {
		return "", err
	}
	if day == "" {
		return fmt.Sprintf("Updated (%d KB).", n/1024), nil
	}
	return "Updated to: " + day, nil
}

// RulesVersion is the day the installed dataset was built, for the settings
// window to show before anything is clicked. Empty means the app has never
// replaced its bundled copy, which has no build it can name.
func (a *App) RulesVersion() string {
	dir, err := config.UserDir()
	if err != nil {
		return ""
	}
	return rules.StoredDate(dir)
}
