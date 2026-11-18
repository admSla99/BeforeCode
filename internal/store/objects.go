package store

import (
	"bufio"
	"bytes"
	"context"
	"strings"
)

// Object is a git object in the tree of a plans ref commit.
type Object struct {
	Path string // "" for the root tree
	ID   string
}

// MissingObjects returns the objects of commit's tree, recursively, that
// are not in the object store, in tree order. A missing tree hides what is
// below it, so only the tree is listed. Objects of older commits are not
// checked.
func (s *Store) MissingObjects(ctx context.Context, commit string) ([]Object, error) {
	root, err := s.gitLine(ctx, nil, nil, "rev-parse", "--verify", commit+"^{tree}")
	if err != nil {
		return nil, err
	}
	var missing []Object
	trees := []Object{{ID: root}}
	for len(trees) > 0 {
		have, err := s.present(ctx, trees)
		if err != nil {
			return nil, err
		}
		var next []Object
		for _, tree := range trees {
			if !have[tree.ID] {
				missing = append(missing, tree)
				continue
			}
			out, err := s.git(ctx, nil, nil, "ls-tree", "-z", tree.ID)
			if err != nil {
				return nil, err
			}
			var blobs []Object
			for _, e := range parseLsTree(out) {
				o := Object{Path: strings.TrimPrefix(tree.Path+"/"+e.name, "/"), ID: e.id}
				switch e.typ {
				case "tree":
					next = append(next, o)
				case "blob":
					blobs = append(blobs, o)
				}
			}
			haveBlobs, err := s.present(ctx, blobs)
			if err != nil {
				return nil, err
			}
			for _, b := range blobs {
				if !haveBlobs[b.ID] {
					missing = append(missing, b)
				}
			}
		}
		trees = next
	}
	return missing, nil
}

// present reports which of the objects are in the object store, with one
// git call.
func (s *Store) present(ctx context.Context, objects []Object) (map[string]bool, error) {
	have := map[string]bool{}
	if len(objects) == 0 {
		return have, nil
	}
	var in bytes.Buffer
	for _, o := range objects {
		in.WriteString(o.ID + "\n")
	}
	// A missing object is "<id> missing"; a present one "<id> <type> <size>".
	out, err := s.git(ctx, nil, in.Bytes(), "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 3 {
			have[f[0]] = true
		}
	}
	return have, sc.Err()
}

// Refetch fetches every object of the remote's plans ref again, without
// negotiating what this clone has, so that objects missing here come back
// even when the local plans ref already points past them. Like Fetch it
// moves only RemoteRef.
func (s *Store) Refetch(ctx context.Context, remote string) error {
	if err := checkRemote(remote); err != nil {
		return err
	}
	_, stderr, err := s.run(ctx, nil, nil, "fetch", "--quiet", "--no-tags", "--prune", "--refetch", "--no-write-fetch-head", remote, FetchRefspec)
	if err != nil {
		return remoteError(remote, "fetch from", stderr, err)
	}
	return nil
}
