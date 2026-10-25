package compiler

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Plain scalars resolve as PyYAML's safe_load does (YAML 1.1, lab/labs.py read the packs with it until Academy
// compiled them): yes/no/on/off are booleans, 0755 is octal, 1:30 is 90, a float needs a dot ("1e5" is a string).
// The values must stay the same as Python's, or content hashes would change and recorded passes turn stale.
var (
	yamlBool      = regexp.MustCompile(`^(?:yes|Yes|YES|no|No|NO|true|True|TRUE|false|False|FALSE|on|On|ON|off|Off|OFF)$`)
	yamlFloat     = regexp.MustCompile(`^(?:[-+]?(?:[0-9][0-9_]*)\.[0-9_]*(?:[eE][-+][0-9]+)?|\.[0-9][0-9_]*(?:[eE][-+][0-9]+)?|[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)
	yamlInt       = regexp.MustCompile(`^(?:[-+]?0b[0-1_]+|[-+]?0[0-7_]+|[-+]?(?:0|[1-9][0-9_]*)|[-+]?0x[0-9a-fA-F_]+|[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+)$`)
	yamlNull      = regexp.MustCompile(`^(?:~|null|Null|NULL|)$`)
	yamlTimestamp = regexp.MustCompile(`^(?:[0-9][0-9][0-9][0-9]-[0-9][0-9]?-[0-9][0-9]?|[0-9][0-9][0-9][0-9]-[0-9][0-9]?-[0-9][0-9]?(?:[Tt]|[ \t]+)[0-9][0-9]?:[0-9][0-9]:[0-9][0-9](?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9][0-9]?(?::[0-9][0-9])?))?)$`)
)

// decodeYAML parses one YAML document into JSON values: nil, bool, int64, float64, string, []any and
// map[string]any. An empty document is nil.
func decodeYAML(data []byte) (any, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if root.Kind == 0 {
		return nil, nil
	}
	return fromNode(&root)
}

func fromNode(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return fromNode(n.Content[0])
	case yaml.AliasNode:
		return fromNode(n.Alias)
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := fromNode(c)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.MappingNode:
		return fromMapping(n)
	case yaml.ScalarNode:
		return fromScalar(n)
	}
	return nil, fmt.Errorf("line %d: unsupported YAML node", n.Line)
}

// fromMapping builds a mapping; merge keys (<<) give way to the mapping's own keys, and among several merged
// mappings the first wins, as in PyYAML. A repeated key keeps its last value.
func fromMapping(n *yaml.Node) (map[string]any, error) {
	out := map[string]any{}
	var merged []map[string]any
	type pair struct {
		key   string
		value any
	}
	var own []pair
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		value, err := fromNode(v)
		if err != nil {
			return nil, err
		}
		if k.Kind == yaml.ScalarNode && k.Style&^yaml.TaggedStyle == 0 && k.Value == "<<" {
			switch m := value.(type) {
			case map[string]any:
				merged = append(merged, m)
			case []any:
				for _, item := range m {
					mm, ok := item.(map[string]any)
					if !ok {
						return nil, fmt.Errorf("line %d: << merges mappings only", k.Line)
					}
					merged = append(merged, mm)
				}
			default:
				return nil, fmt.Errorf("line %d: << merges mappings only", k.Line)
			}
			continue
		}
		key, err := fromNode(k)
		if err != nil {
			return nil, err
		}
		name, err := keyString(key)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", k.Line, err)
		}
		own = append(own, pair{name, value})
	}
	for i := len(merged) - 1; i >= 0; i-- {
		for k, v := range merged[i] {
			out[k] = v
		}
	}
	for _, p := range own {
		out[p.key] = p.value
	}
	return out, nil
}

// keyString is a mapping key as JSON writes it (Python's json.dumps turns True into "true", 3 into "3").
func keyString(key any) (string, error) {
	switch k := key.(type) {
	case string:
		return k, nil
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(k), nil
	case int64:
		return strconv.FormatInt(k, 10), nil
	case float64:
		return pyFloat(k), nil
	}
	return "", errors.New("a mapping key must be a scalar")
}

func fromScalar(n *yaml.Node) (any, error) {
	tag := ""
	if n.Style&yaml.TaggedStyle != 0 {
		tag = n.ShortTag()
	} else if n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return n.Value, nil
	}
	s := n.Value
	switch {
	case tag == "!!str" || tag == "!!binary":
		return s, nil
	case tag == "!!bool" || tag == "" && yamlBool.MatchString(s):
		switch strings.ToLower(s) {
		case "yes", "true", "on":
			return true, nil
		case "no", "false", "off":
			return false, nil
		}
	case tag == "!!float" || tag == "" && yamlFloat.MatchString(s):
		return yamlFloatValue(s, n.Line)
	case tag == "!!int" || tag == "" && yamlInt.MatchString(s):
		return yamlIntValue(s, n.Line)
	case tag == "!!null" || tag == "" && yamlNull.MatchString(s):
		return nil, nil
	case tag == "!!timestamp" || tag == "" && yamlTimestamp.MatchString(s):
		return nil, fmt.Errorf("line %d: %s is a date, which a pack cannot hold (quote it)", n.Line, s)
	case tag == "":
		return s, nil
	}
	return nil, fmt.Errorf("line %d: %q is not a valid %s", n.Line, s, tag)
}

func yamlIntValue(s string, line int) (int64, error) {
	v := strings.ReplaceAll(s, "_", "")
	sign := int64(1)
	if v != "" && (v[0] == '-' || v[0] == '+') {
		if v[0] == '-' {
			sign = -1
		}
		v = v[1:]
	}
	var n int64
	var err error
	switch {
	case v == "0":
		return 0, nil
	case strings.HasPrefix(v, "0b"):
		n, err = strconv.ParseInt(v[2:], 2, 64)
	case strings.HasPrefix(v, "0x"):
		n, err = strconv.ParseInt(v[2:], 16, 64)
	case strings.HasPrefix(v, "0"):
		n, err = strconv.ParseInt(v[1:], 8, 64)
	case strings.Contains(v, ":"):
		for _, part := range strings.Split(v, ":") {
			d, perr := strconv.ParseInt(part, 10, 64)
			if perr != nil {
				err = perr
				break
			}
			n = n*60 + d
		}
	default:
		n, err = strconv.ParseInt(v, 10, 64)
	}
	if err != nil {
		return 0, fmt.Errorf("line %d: %q is not an integer this compiler can hold", line, s)
	}
	return sign * n, nil
}

func yamlFloatValue(s string, line int) (float64, error) {
	v := strings.ToLower(strings.ReplaceAll(s, "_", ""))
	sign := 1.0
	if v != "" && (v[0] == '-' || v[0] == '+') {
		if v[0] == '-' {
			sign = -1
		}
		v = v[1:]
	}
	if v == ".inf" || v == ".nan" {
		return 0, fmt.Errorf("line %d: %s cannot be written as JSON", line, s)
	}
	if strings.Contains(v, ":") {
		f := 0.0
		for _, part := range strings.Split(v, ":") {
			d, err := strconv.ParseFloat(part, 64)
			if err != nil {
				return 0, fmt.Errorf("line %d: %q is not a number", line, s)
			}
			f = f*60 + d
		}
		return sign * f, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsInf(f, 0) {
		return 0, fmt.Errorf("line %d: %q is not a number", line, s)
	}
	return sign * f, nil
}
