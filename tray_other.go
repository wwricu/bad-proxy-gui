//go:build !windows && !darwin

package main

// startTray is never called on platforms where hasTray is false.
func startTray(onOpen, onQuit func()) {}

func addTrayQuit(onQuit func()) {}
