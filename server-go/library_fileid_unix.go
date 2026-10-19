//go:build !windows

package main

import (
	"io/fs"
	"strconv"
	"syscall"
)

// fsFileID devolve o inode (no drvfs/9p é o File ID do NTFS, estável entre leituras).
func fsFileID(info fs.FileInfo) string {
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Ino != 0 {
		// Só o inode: o número do device de montagens 9p muda a cada boot.
		// O casamento usa (inode, tamanho), então colisão entre volumes é improvável.
		return strconv.FormatUint(st.Ino, 10)
	}
	return ""
}
