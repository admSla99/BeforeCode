// Package core owns the plan lifecycle: plan state derived from events,
// questions, threads and the approval policy. It does no I/O and must not
// import store, agent, tui or mcp. It does not know which harness is running.
package core
