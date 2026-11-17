package store

import (
	"context"
	"fmt"
	"strings"
)

// Author returns the name and email git uses as the commit author, from
// user.name and user.email in git config (or GIT_AUTHOR_* when set).
func (s *Store) Author(ctx context.Context) (name, email string, err error) {
	// "Name <email> 1700000000 +0100"
	ident, err := s.gitLine(ctx, nil, nil, "var", "GIT_AUTHOR_IDENT")
	if err != nil {
		return "", "", fmt.Errorf("store: cannot tell who you are; set user.name and user.email in git config: %w", err)
	}
	name, rest, ok := strings.Cut(ident, " <")
	email, _, ok2 := strings.Cut(rest, ">")
	if !ok || !ok2 || email == "" {
		return "", "", fmt.Errorf("store: unexpected author identity %q; set user.email in git config", ident)
	}
	return name, email, nil
}

// Editor returns the editor command git would use: GIT_EDITOR,
// core.editor, VISUAL, EDITOR, then git's default.
func (s *Store) Editor(ctx context.Context) (string, error) {
	return s.gitLine(ctx, nil, nil, "var", "GIT_EDITOR")
}
