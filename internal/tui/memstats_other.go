//go:build !linux

package tui

func collectMemStats() string { return "" }
