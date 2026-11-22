package source

import (
	"bytes"
	"encoding/json"
	"strings"
)

// selected is the answer raw with only the selected fields kept. An entry
// is a top-level key, or a dotted path into objects and lists (profiles.uid
// keeps the uid of each profile); the tree of the entries says what to keep
// at each level. Of the top-level answer, an object is filtered, and a list
// has each element filtered; anything else, and an answer without keys to
// keep, is returned as it is, without its spaces.
func selected(raw []byte, keys []string) []byte {
	out := json.RawMessage(raw)
	if len(keys) > 0 {
		tree := selectTree(keys)
		var list []json.RawMessage
		if t := bytes.TrimSpace(raw); t[0] == '[' && json.Unmarshal(t, &list) == nil {
			for i := range list {
				list[i] = pickTop(list[i], tree)
			}
			out, _ = json.Marshal(list)
		} else {
			out = pickTop(raw, tree)
		}
	}
	var buf bytes.Buffer
	if json.Compact(&buf, out) != nil {
		return out
	}
	return buf.Bytes()
}

// selectNode is one level of the paths of a select: the keys under it, and
// for each whether the path ends there (a leaf keeps its value whole).
type selectNode map[string]selectChild

type selectChild struct {
	leaf bool
	sub  selectNode
}

// selectTree builds the tree of keys, each a dotted path. A path that
// ends where another goes on is a declaration error (callread.go refuses
// the overlap), so no entry here is both.
func selectTree(keys []string) selectNode {
	root := selectNode{}
	for _, k := range keys {
		n := root
		parts := strings.Split(k, ".")
		for i, p := range parts {
			c := n[p]
			if i == len(parts)-1 {
				c.leaf = true
				n[p] = c
				break
			}
			if c.sub == nil {
				c.sub = selectNode{}
				n[p] = c
			}
			n = c.sub
		}
	}
	return root
}

// pickTop is one element of the top-level answer, or the answer itself, under
// the tree. Only an object or a list is filtered; a scalar is kept as it is,
// as the answer of a call always was.
func pickTop(raw json.RawMessage, tree selectNode) json.RawMessage {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || (t[0] != '{' && t[0] != '[') {
		return raw
	}
	if out, ok := pick(raw, tree); ok {
		return out
	}
	return raw
}

// pick is raw under the tree: an object keeps the named keys, a list has
// each element picked under the same tree. A scalar or null where the tree
// still goes on is not kept (ok is false), so an unfiltered subtree never
// passes below the top level. An object or list that holds nothing selected
// is kept empty, so a caller can still count what matched.
func pick(raw json.RawMessage, tree selectNode) (json.RawMessage, bool) {
	t := bytes.TrimSpace(raw)
	switch {
	case len(t) > 0 && t[0] == '[':
		var list []json.RawMessage
		if json.Unmarshal(t, &list) != nil {
			return nil, false
		}
		out := make([]json.RawMessage, 0, len(list))
		for _, el := range list {
			if v, ok := pick(el, tree); ok {
				out = append(out, v)
			}
		}
		b, _ := json.Marshal(out)
		return b, true
	case len(t) > 0 && t[0] == '{':
		var m map[string]json.RawMessage
		if json.Unmarshal(t, &m) != nil {
			return nil, false
		}
		out := map[string]json.RawMessage{}
		for k, c := range tree {
			v, ok := m[k]
			if !ok {
				continue
			}
			if c.leaf {
				out[k] = v
				continue
			}
			if p, ok := pick(v, c.sub); ok {
				out[k] = p
			}
		}
		b, _ := json.Marshal(out)
		return b, true
	}
	return nil, false
}
