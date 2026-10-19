//go:build windows

package main

import "io/fs"

// fsFileID: no Windows nativo (dev local) não há inode barato via FileInfo;
// a identidade cai para o caminho.
func fsFileID(info fs.FileInfo) string { return "" }
