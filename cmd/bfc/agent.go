package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// agentEnv is the variable Claude Code sets for the commands it runs
// (ADR 0011). It tells bfc that an agent, not a person, runs it. This guards
// against mistakes, not against an agent that unsets it.
const agentEnv = "CLAUDECODE"

// claudeCode is the agent field of what bfc writes inside Claude Code.
const claudeCode = "claude-code"

// insideAgent returns the harness bfc runs in, or "" when a person runs it.
func insideAgent(cmd *cobra.Command) string {
	if v, _ := lookupEnv(cmd, agentEnv); v != "" {
		return claudeCode
	}
	return ""
}

// lookupEnv returns the variable as the command sees it: from the variables
// of its place (the last one wins), otherwise from the process.
func lookupEnv(cmd *cobra.Command, name string) (string, bool) {
	env := extraEnv(cmd)
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], name+"="); ok {
			return v, true
		}
	}
	return os.LookupEnv(name)
}
