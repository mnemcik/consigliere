package migrate

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// frontmatterMapping parses an existing frontmatter block (delimiters
// included, as manifest.SplitFrontmatter returns it) into a mapping node. It
// works on nodes rather than a map so keys the workspace already has keep
// their order, style and comments.
func frontmatterMapping(block string) (*yaml.Node, error) {
	empty := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if block == "" {
		return empty, nil
	}
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	inner := strings.Join(lines[1:len(lines)-1], "\n")
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(inner), &doc); err != nil {
		return nil, fmt.Errorf("existing frontmatter is not valid YAML: %w", err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return empty, nil
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("existing frontmatter is not a mapping")
	}
	return doc.Content[0], nil
}

func lookup(m *yaml.Node, key string) (*yaml.Node, bool) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1], true
		}
	}
	return nil, false
}

// set inserts key at its place in keyOrder, after any existing key that ranks
// before it, so added keys read in a stable order without moving the
// workspace's own.
func set(m *yaml.Node, key string, v *yaml.Node) {
	at := len(m.Content)
	for i := 0; i+1 < len(m.Content); i += 2 {
		if rank(m.Content[i].Value) > rank(key) {
			at = i
			break
		}
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	m.Content = append(m.Content[:at], append([]*yaml.Node{k, v}, m.Content[at:]...)...)
}

// setNew adds key unless frontmatter already has it. Frontmatter wins
// (DEC-005); a differing Meta value is recorded as a conflict for review.
func setNew(m *yaml.Node, key string, v *yaml.Node, comment string, res *Result) {
	if have, ok := lookup(m, key); ok {
		if a, b := flat(have), flat(v); !strings.EqualFold(a, b) {
			res.Conflicts = append(res.Conflicts, fmt.Sprintf("%s: frontmatter %q kept, ## Meta had %q", key, a, b))
		} else if comment != "" && have.LineComment == "" {
			// Same value: keep the Meta bullet's inline comment on it.
			have.LineComment = "# " + comment
		}
		return
	}
	if comment != "" {
		v.LineComment = "# " + comment
	}
	set(m, key, v)
}

func rank(key string) int {
	for i, k := range keyOrder {
		if k == key {
			return i
		}
	}
	return len(keyOrder)
}

// flat renders a node as one comparable string.
func flat(n *yaml.Node) string {
	if n.Kind == yaml.SequenceNode {
		parts := make([]string, 0, len(n.Content))
		for _, c := range n.Content {
			parts = append(parts, strings.TrimSpace(c.Value))
		}
		return strings.Join(parts, ", ")
	}
	return strings.TrimSpace(n.Value)
}

// strNode is a string scalar. The explicit tag makes the encoder quote a value
// that would otherwise read back as another type (`yes`, `1.0`, a date).
func strNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// dateNode is a bare YYYY-MM-DD, so YAML tools read it as a date.
func dateNode(d string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: d}
}

func listNode(items []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, it := range items {
		n.Content = append(n.Content, strNode(it))
	}
	return n
}

func encode(m *yaml.Node) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
