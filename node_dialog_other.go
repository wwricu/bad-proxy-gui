//go:build !windows

package main

// nodeDialogSupported keeps the frontend on the HTML modal where no native
// editor exists (macOS/Linux).
const nodeDialogSupported = false

func runNodeDialog(in Node) (Node, bool) { return in, false }
