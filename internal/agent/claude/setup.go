package claude

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// The project files that bfc setup claude writes (ADR 0010), relative to
// the repository root, with slashes.
const (
	SettingsPath = ".claude/settings.json"
	SkillPath    = ".claude/skills/beforecode/SKILL.md"
)

// Skill is the beforecode skill, which points Claude Code to bfc prime
// (ADR 0008). It ships in the binary and bfc setup claude rewrites the
// repository's copy on every run, so it matches the installed bfc.
//
//go:embed SKILL.md
var Skill []byte

// SkillMatches reports whether data is this binary's Skill. Line endings
// do not count, so a checkout with CRLF matches.
func SkillMatches(data []byte) bool {
	return bytes.Equal(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), Skill)
}

// HookArgs are the arguments of bfc that Claude Code runs as the
// PreToolUse hook.
const HookArgs = "hook pre-tool-use"

// HookBinary returns the bfc binary that command runs as the PreToolUse
// hook, such as "bfc" or a quoted path to bfc.exe, and false when command
// is not bfc's hook.
func HookBinary(command string) (string, bool) {
	bin, ok := strings.CutSuffix(strings.TrimSpace(command), " "+HookArgs)
	bin = strings.Trim(strings.TrimSpace(bin), `"'`)
	if !ok || strings.TrimSuffix(filepath.Base(filepath.FromSlash(bin)), ".exe") != "bfc" {
		return "", false
	}
	return bin, true
}

// Rules are what bfc setup claude puts into the settings: the PreToolUse
// hook for the tools in Matcher, and the permissions.
type Rules struct {
	Matcher string   // such as "Edit|Write"
	Command string   // the hook, such as "bfc hook pre-tool-use"
	Allow   []string // permissions.allow, such as "Bash(bfc prime:*)"
	Deny    []string // permissions.deny
}

// preToolUse is the part of a PreToolUse entry of the settings that
// MergeSettings reads.
type preToolUse struct {
	Matcher string `json:"matcher"`
	Hooks   []struct {
		Command string `json:"command"`
	} `json:"hooks"`
}

// MergeSettings adds the rules to the Claude Code settings in data (empty
// for a new file) and returns the new settings and what it added, one line
// each. Everything else in data stays as it was, in its order. A
// PreToolUse entry for Matcher that already runs bfc's hook counts as the
// hook. When nothing is missing, it returns data unchanged and no lines.
func MergeSettings(data []byte, r Rules) ([]byte, []string, error) {
	settings := object{}
	if len(bytes.TrimSpace(data)) > 0 {
		var err error
		if settings, err = parseObject(data); err != nil {
			return nil, nil, err
		}
	}
	var added []string

	hooks, err := settings.object("hooks")
	if err != nil {
		return nil, nil, err
	}
	entries, err := hooks.array("hooks", "PreToolUse")
	if err != nil {
		return nil, nil, err
	}
	if !slices.ContainsFunc(entries, func(raw json.RawMessage) bool { return runsHook(raw, r.Matcher) }) {
		type command struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		}
		entry, err := marshal(struct {
			Matcher string    `json:"matcher"`
			Hooks   []command `json:"hooks"`
		}{r.Matcher, []command{{"command", r.Command}}})
		if err != nil {
			return nil, nil, err
		}
		hooks.setArray("PreToolUse", append(entries, entry))
		settings.set("hooks", hooks.marshal())
		added = append(added, fmt.Sprintf("hooks.PreToolUse %s for %s", r.Command, r.Matcher))
	}

	perms, err := settings.object("permissions")
	if err != nil {
		return nil, nil, err
	}
	changed := false
	for _, list := range []struct {
		key   string
		rules []string
	}{{"allow", r.Allow}, {"deny", r.Deny}} {
		items, err := perms.array("permissions", list.key)
		if err != nil {
			return nil, nil, err
		}
		have := make([]string, 0, len(items))
		for _, raw := range items {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				have = append(have, s)
			}
		}
		n := len(items)
		for _, rule := range list.rules {
			if slices.Contains(have, rule) {
				continue
			}
			raw, err := marshal(rule)
			if err != nil {
				return nil, nil, err
			}
			items = append(items, raw)
			have = append(have, rule)
			added = append(added, fmt.Sprintf("permissions.%s %s", list.key, rule))
		}
		if len(items) > n {
			perms.setArray(list.key, items)
			changed = true
		}
	}
	if changed {
		settings.set("permissions", perms.marshal())
	}

	if len(added) == 0 {
		return data, nil, nil
	}
	var out bytes.Buffer
	if err := json.Indent(&out, settings.marshal(), "", "  "); err != nil {
		return nil, nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), added, nil
}

// runsHook reports whether the PreToolUse entry raw is for matcher and
// runs bfc's hook.
func runsHook(raw json.RawMessage, matcher string) bool {
	var e preToolUse
	if json.Unmarshal(raw, &e) != nil || e.Matcher != matcher {
		return false
	}
	return slices.ContainsFunc(e.Hooks, func(h struct {
		Command string `json:"command"`
	}) bool {
		_, ok := HookBinary(h.Command)
		return ok
	})
}

// object is a JSON object that keeps the order of its members and the
// text of their values, so a merge changes only what it adds.
type object []member

type member struct {
	key string
	val json.RawMessage
}

// parseObject reads a JSON object. It fails with what is wrong: data that
// is not valid JSON, or valid JSON that is not an object.
func parseObject(data []byte) (object, error) {
	if !json.Valid(data) {
		var v any
		err := json.Unmarshal(data, &v)
		return nil, fmt.Errorf("not valid JSON: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, _ := dec.Token(); tok != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	var o object
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		o = append(o, member{tok.(string), val})
	}
	return o, nil
}

func (o object) get(key string) (json.RawMessage, bool) {
	for _, m := range o {
		if m.key == key {
			return m.val, true
		}
	}
	return nil, false
}

func (o *object) set(key string, val json.RawMessage) {
	for i, m := range *o {
		if m.key == key {
			(*o)[i].val = val
			return
		}
	}
	*o = append(*o, member{key, val})
}

// object returns the member key as an object, empty when it is missing.
func (o object) object(key string) (object, error) {
	raw, ok := o.get(key)
	if !ok {
		return object{}, nil
	}
	sub, err := parseObject(raw)
	if err != nil {
		return nil, fmt.Errorf("%q is not a JSON object", key)
	}
	return sub, nil
}

// array returns the member key as an array, empty when it is missing; path
// names it in an error.
func (o object) array(path ...string) ([]json.RawMessage, error) {
	raw, ok := o.get(path[len(path)-1])
	if !ok {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("%q is not a JSON array", strings.Join(path, "."))
	}
	return items, nil
}

func (o *object) setArray(key string, items []json.RawMessage) {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(it)
	}
	b.WriteByte(']')
	o.set(key, b.Bytes())
}

func (o object) marshal() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := marshal(m.key) // a string always marshals
		b.Write(key)
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// marshal is json.Marshal without escaping <, > and &, which Claude Code
// settings do not need.
func marshal(v any) (json.RawMessage, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}
