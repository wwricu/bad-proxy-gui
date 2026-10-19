package main

import (
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
