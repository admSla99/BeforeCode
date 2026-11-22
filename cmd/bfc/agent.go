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

// gateCommands are the commands that are for people and refuse to run
// inside an agent: the Gates, and acknowledge (ADR 0011, ADR 0013 section
// 7).
var gateCommands = []string{"submit", "approve", "request-changes", "withdraw", "close", "done", "acknowledge"}

// insideAgent returns the harness bfc runs in, or "" when a person runs it.
func insideAgent(cmd *cobra.Command) string {
	for _, h := range harnesses {
		if v, _ := lookupEnv(cmd, h.env); v != "" {
			return h.name
		}
	}
	return ""
}

// checkPerson refuses a Gate inside an agent.
func checkPerson(cmd *cobra.Command) error {
	return core.CheckPerson(insideAgent(cmd) != "")
}

// guardGates makes each Gate among the subcommands of root refuse to run
// inside an agent, before it does anything. The check is the Gate's own
// PreRunE, so a PersistentPreRunE added later cannot replace it.
func guardGates(root *cobra.Command) {
	for _, c := range root.Commands() {
		if !slices.Contains(gateCommands, c.Name()) {
			continue
		}
		next := c.PreRunE
		c.PreRunE = func(cmd *cobra.Command, args []string) error {
			if err := checkPerson(cmd); err != nil {
				return fmt.Errorf("bfc %s: %w", cmd.Name(), err)
			}
			if next != nil {
				return next(cmd, args)
			}
			return nil
		}
	}
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
