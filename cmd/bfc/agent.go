package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
)

// This file is the one place that knows which harnesses mark bfc as run by
// an agent and which commands are the Gates (ADR 0011). core only gets a
// plain "inside an agent" flag. The check guards against mistakes, not
// against an agent that unsets the variable.

// agentEnv is the variable Claude Code sets for the commands it runs.
const agentEnv = "CLAUDECODE"

// claudeCode is the agent field of what bfc writes inside Claude Code.
const claudeCode = "claude-code"

// harnesses are the variables that tell bfc an agent, not a person, runs
// it, each with the harness it names. A new harness adds its variable here.
var harnesses = []struct{ env, name string }{
	{agentEnv, claudeCode},
}

// gateCommands are the Gates: the commands that are for people and refuse
// to run inside an agent.
var gateCommands = []string{"submit", "approve", "request-changes", "withdraw", "close"}

// insideAgent returns the harness bfc runs in, or "" when a person runs it.
func insideAgent(cmd *cobra.Command) string {
	for _, h := range harnesses {
		if v, _ := lookupEnv(cmd, h.env); v != "" {
			return h.name
		}
	}
	return ""
}

// checkPerson refuses a Gate inside an agent; every other command may run.
func checkPerson(cmd *cobra.Command) error {
	if !slices.Contains(gateCommands, cmd.Name()) {
		return nil
	}
	if err := core.CheckPerson(insideAgent(cmd) != ""); err != nil {
		return fmt.Errorf("bfc %s: %w", cmd.Name(), err)
	}
	return nil
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
