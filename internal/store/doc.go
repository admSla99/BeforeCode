// Package store reads and writes plans in the plans ref
// (refs/beforecode/plans) through git plumbing run as subprocesses. It never
// touches the working tree, the index or the current branch.
package store
