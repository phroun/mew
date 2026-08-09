//go:build !windows || !wgpuembed

// Package wgpuembed is a no-op unless built for Windows with -tags wgpuembed,
// where it embeds and unpacks wgpu-native at startup (see embed_windows.go).
// Blank-imported by the SDL host so the init runs; harmless everywhere else,
// and it embeds nothing (no DLL is required to build without the tag).
package wgpuembed
