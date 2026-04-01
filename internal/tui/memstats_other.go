//go:build !linux

package tui

func collectMemStats(_ []string) string { return "" }
