//go:build windows

package platform

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// These tests drive setSystemProxy/clearSystemProxy against scratch keys under
// HKCU, never the real Internet Settings: a test that moved the machine's own
// proxy would leave it moved the moment it failed halfway.
//
// The shape of a take-over is always the same - a settings key with whatever a
// user might have configured, and the backup key this app owns:
//
//	settings = the user's Internet Settings
//	backup   = Software\wwricu\bad-proxy-gui\system-proxy
//
// so a test's whole job is to say what settings held before and what it must
// hold after.

const testRoot = `Software\wwricu\bpt`

type scratch struct {
	settings string
	backup   string
}

// newScratch makes one test its own pair of keys, so tests cannot see each
// other's leftovers and a panic cannot leave a marker key that makes the next
// test skip its backup.
func newScratch(t *testing.T) scratch {
	t.Helper()
	s := scratch{settings: testRoot + `\settings`, backup: testRoot + `\backup`}
	deleteScratch(t, s)
	if _, _, err := registry.CreateKey(registry.CURRENT_USER, s.settings, registry.SET_VALUE); err != nil {
		t.Fatalf("create scratch settings key: %v", err)
	}
	t.Cleanup(func() { deleteScratch(t, s) })
	return s
}

func deleteScratch(t *testing.T, s scratch) {
	t.Helper()
	for _, p := range []string{s.backup, s.settings, testRoot} {
		if err := registry.DeleteKey(registry.CURRENT_USER, p); err != nil &&
			!errors.Is(err, registry.ErrNotExist) {
			t.Logf("could not remove %s: %v", p, err)
		}
	}
}

func setStr(t *testing.T, path, name, value string) {
	t.Helper()
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer k.Close()
	if err := k.SetStringValue(name, value); err != nil {
		t.Fatalf("set %s: %v", name, err)
	}
}

func setNum(t *testing.T, path, name string, value uint32) {
	t.Helper()
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer k.Close()
	if err := k.SetDWordValue(name, value); err != nil {
		t.Fatalf("set %s: %v", name, err)
	}
}

// read returns a value's text and whether the value is there at all. It asks
// the registry what kind a value is before reading it, because a test that
// guessed the type wrong would report a missing value rather than its own
// mistake - and a REG_EXPAND_SZ ProxyServer written by an administrator is a
// real thing.
func read(t *testing.T, path, name string) (string, bool) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", false
		}
		t.Fatalf("open %s: %v", path, err)
	}
	defer k.Close()
	b := make([]byte, 1024)
	_, kind, err := k.GetValue(name, b)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	switch kind {
	case regSz, regExpandSz:
		s, _, err := k.GetStringValue(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return s, true
	case regDword:
		n, _, err := k.GetIntegerValue(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return strconv.FormatUint(n, 10), true
	}
	t.Fatalf("%s is registry type %d, which this app does not write", name, kind)
	return "", false
}

const (
	regSz       = 1
	regExpandSz = 2
	regDword    = 4
)

func mustValue(t *testing.T, path, name, want string) {
	t.Helper()
	got, ok := read(t, path, name)
	if !ok {
		t.Errorf("%s is gone, want %q", name, want)
		return
	}
	if got != want {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}

func mustAbsent(t *testing.T, path, name string) {
	t.Helper()
	if got, ok := read(t, path, name); ok {
		t.Errorf("%s = %q, want it deleted", name, got)
	}
}

// TestClearGivesBackTheProxyThatWasThereBefore is the reason the backup exists.
// A machine with a corporate manual proxy that then runs this app must come out
// of it with that proxy, not with an operating system that reaches nothing.
func TestClearGivesBackTheProxyThatWasThereBefore(t *testing.T) {
	s := newScratch(t)
	setStr(t, s.settings, "ProxyServer", "proxy.corp:3128")
	setStr(t, s.settings, "ProxyOverride", "*.corp.local")
	setNum(t, s.settings, "ProxyEnable", 1)

	if err := setSystemProxy(s.settings, s.backup, "17890"); err != nil {
		t.Fatalf("set: %v", err)
	}
	mustValue(t, s.settings, "ProxyServer", "127.0.0.1:17890")
	mustValue(t, s.settings, "ProxyEnable", "1")

	if err := clearSystemProxy(s.settings, s.backup); err != nil {
		t.Fatalf("clear: %v", err)
	}
	mustValue(t, s.settings, "ProxyServer", "proxy.corp:3128")
	mustValue(t, s.settings, "ProxyOverride", "*.corp.local")
	mustValue(t, s.settings, "ProxyEnable", "1")
}

// TestClearDeletesWhatTheUserNeverHad: restoring means giving back an absent
// value as an absent value. An empty ProxyEnable of 0 is off, but an empty
// ProxyServer string is Settings showing a configured proxy with no address,
// and that is a thing this app would be leaving behind on a clean machine.
func TestClearDeletesWhatTheUserNeverHad(t *testing.T) {
	s := newScratch(t)

	if err := setSystemProxy(s.settings, s.backup, "17891"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := clearSystemProxy(s.settings, s.backup); err != nil {
		t.Fatalf("clear: %v", err)
	}
	mustAbsent(t, s.settings, "ProxyServer")
	mustAbsent(t, s.settings, "ProxyOverride")
	mustAbsent(t, s.settings, "ProxyEnable")
}

// The bug this file used to have: the clear deleted AutoConfigURL and
// ProxyPacUrl along with its own two values. A PAC is how a domain-joined
// machine reaches anything, and deleting it is not this app's business - it
// never wrote it and never needs to read it.
func TestClearLeavesProxySettingsItDidNotWrite(t *testing.T) {
	s := newScratch(t)
	setStr(t, s.settings, "AutoConfigURL", "https://corp.example/pac/proxy.pac")
	setStr(t, s.settings, "ProxyPacUrl", "https://corp.example/pac/other.pac")
	setStr(t, s.settings, "ProxyServer", "proxy.corp:3128")

	if err := setSystemProxy(s.settings, s.backup, "17892"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := clearSystemProxy(s.settings, s.backup); err != nil {
		t.Fatalf("clear: %v", err)
	}
	mustValue(t, s.settings, "AutoConfigURL", "https://corp.example/pac/proxy.pac")
	mustValue(t, s.settings, "ProxyPacUrl", "https://corp.example/pac/other.pac")
	mustValue(t, s.settings, "ProxyServer", "proxy.corp:3128")
}

// Connect, disconnect, connect again without quitting in between: the second
// take-over finds the settings already holding this app's own proxy. Backing
// that up replaces the one backup that still means something, and quitting
// would then leave the OS pointing at an engine that stopped listening.
func TestSecondTakeOverKeepsTheFirstBackup(t *testing.T) {
	s := newScratch(t)
	setStr(t, s.settings, "ProxyServer", "proxy.corp:3128")

	if err := setSystemProxy(s.settings, s.backup, "17893"); err != nil {
		t.Fatalf("first set: %v", err)
	}
	// Whatever the app wrote may as well have been rewritten a dozen times;
	// none of it is what the user had.
	if err := setSystemProxy(s.settings, s.backup, "17894"); err != nil {
		t.Fatalf("second set: %v", err)
	}
	mustValue(t, s.backup, "ProxyServer", "proxy.corp:3128")

	if err := clearSystemProxy(s.settings, s.backup); err != nil {
		t.Fatalf("clear: %v", err)
	}
	mustValue(t, s.settings, "ProxyServer", "proxy.corp:3128")
}

// A take-over that cannot store the settings it is about to destroy must not
// destroy them. Partial application is the one outcome worse than none, since
// the user's proxy is what gets lost.
func TestSetRefusesWhenTheBackupCannotBeWritten(t *testing.T) {
	s := newScratch(t)
	setStr(t, s.settings, "ProxyServer", "proxy.corp:3128")
	// A key name longer than the registry's 255 characters: nothing can be
	// saved here, so this stands for the backup location being unusable.
	unusable := testRoot + `\` + strings.Repeat("x", 300)

	err := setSystemProxy(s.settings, unusable, "17895")
	if err == nil {
		t.Fatalf("set succeeded with no place to save the proxy")
	}
	t.Logf("refused because: %v", err)
	mustValue(t, s.settings, "ProxyServer", "proxy.corp:3128")
	mustAbsent(t, s.settings, "ProxyEnable")
}

// The cleanup half of the contract: a clear consumes its backup, so the next
// connect starts a fresh one instead of restoring a snapshot taken on a
// different machine, or before the user's own administrator moved the proxy.
func TestClearReleasesTheBackupKey(t *testing.T) {
	s := newScratch(t)
	setStr(t, s.settings, "ProxyServer", "proxy.corp:3128")

	if err := setSystemProxy(s.settings, s.backup, "17896"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, ok := read(t, s.backup, "ProxyServer"); !ok {
		t.Fatal("nothing was backed up")
	}
	if err := clearSystemProxy(s.settings, s.backup); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := registry.OpenKey(registry.CURRENT_USER, s.backup, registry.READ); !errors.Is(err, registry.ErrNotExist) {
		t.Errorf("backup key survived its restore (err %v)", err)
	}

	// And the connect after it backs up what the user has now, not what they had
	// before the first connect.
	setStr(t, s.settings, "ProxyServer", "proxy.new:8080")
	if err := setSystemProxy(s.settings, s.backup, "17897"); err != nil {
		t.Fatalf("set after clear: %v", err)
	}
	mustValue(t, s.backup, "ProxyServer", "proxy.new:8080")
}

// An older build's clear, or a crash, can leave the settings holding this app's
// proxy with no backup to read. The fallback still has to switch the proxy off
// rather than fail, and it must do that with the same restraint.
func TestClearWithoutABackupTurnsTheProxyOff(t *testing.T) {
	s := newScratch(t)
	setStr(t, s.settings, "ProxyServer", "127.0.0.1:17898")
	setStr(t, s.settings, "ProxyOverride", "<local>;127.0.0.1;localhost")
	setNum(t, s.settings, "ProxyEnable", 1)
	setStr(t, s.settings, "AutoConfigURL", "https://corp.example/pac/proxy.pac")

	if err := clearSystemProxy(s.settings, s.backup); err != nil {
		t.Fatalf("clear: %v", err)
	}
	mustValue(t, s.settings, "ProxyEnable", "0")
	mustAbsent(t, s.settings, "ProxyServer")
	mustAbsent(t, s.settings, "ProxyOverride")
	mustValue(t, s.settings, "AutoConfigURL", "https://corp.example/pac/proxy.pac")
}
