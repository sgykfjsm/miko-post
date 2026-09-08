//go:build !unix

package obsidian

// noFollowFlag is zero where the platform has no O_NOFOLLOW.
//
// The Lstat check in open still refuses a symlink that is already there, which
// is the reachable case — a symlink sitting in a vault that was shared, cloned
// from a template, or restored from an archive. What is lost is only atomicity
// against a path that becomes a symlink between the check and the open, which
// needs a local process racing us inside a directory it can already write.
const noFollowFlag = 0
