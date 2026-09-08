//go:build unix

package obsidian

import "syscall"

// noFollowFlag refuses to open a symbolic link, atomically, as part of the open
// itself (decision DEC-A2).
//
// The Lstat in open gives the user a message worth reading; this closes the
// window between that check and the open, where the path could become a symlink.
// Belt and braces on purpose: the Lstat cannot be atomic and this cannot
// explain itself.
const noFollowFlag = syscall.O_NOFOLLOW
