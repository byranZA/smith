package main

// PROTOTYPE — throwaway. Answers issue #60; not production code.

import (
	"fmt"
	"strconv"
	"strings"
)

// A tiny path language for pulling fields out of decoded provider JSON.
// The research ticket (#54) said "JSONPath extractors" without pinning the
// grammar; this is the smallest grammar that reaches every field the two
// real CLIs actually emit.
//
// Segments split on '.' (dots inside brackets are ignored):
//
//	key        object field
//	key[0]     index
//	key[*]     every element
//	key[f=v]   elements whose field f equals v
//	[0] [*] [f=v]  the same, applied to the current value
//
// An empty path means "the value itself".
func eval(v any, path string) []any {
	cur := []any{v}
	for _, seg := range splitSegments(path) {
		key, ops := parseSegment(seg)
		var next []any
		for _, c := range cur {
			vals := []any{c}
			if key != "" {
				m, ok := c.(map[string]any)
				if !ok {
					continue
				}
				f, ok := m[key]
				if !ok {
					continue
				}
				vals = []any{f}
			}
			for _, op := range ops {
				vals = apply(vals, op)
			}
			next = append(next, vals...)
		}
		cur = next
	}
	return cur
}

// evalString returns the first match rendered as a string, or "" if the path
// matched nothing. A missing field and an explicit JSON null are both "".
func evalString(v any, path string) string {
	for _, m := range eval(v, path) {
		if s := render(m); s != "" {
			return s
		}
	}
	return ""
}

// evalStrings returns every match rendered as a string — used for tag paths,
// which may yield a whole array (doctl) or a single map value (hcloud).
func evalStrings(v any, path string) []string {
	var out []string
	for _, m := range eval(v, path) {
		if s := render(m); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func apply(vals []any, op string) []any {
	var out []any
	for _, v := range vals {
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		switch {
		case op == "*":
			out = append(out, arr...)
		case strings.Contains(op, "="):
			field, want, _ := strings.Cut(op, "=")
			for _, e := range arr {
				m, ok := e.(map[string]any)
				if !ok {
					continue
				}
				if render(m[field]) == want {
					out = append(out, e)
				}
			}
		default:
			i, err := strconv.Atoi(op)
			if err != nil || i < 0 || i >= len(arr) {
				continue
			}
			out = append(out, arr[i])
		}
	}
	return out
}

func parseSegment(seg string) (key string, ops []string) {
	for {
		open := strings.IndexByte(seg, '[')
		if open < 0 {
			return key + seg, ops
		}
		key += seg[:open]
		close := strings.IndexByte(seg[open:], ']')
		if close < 0 {
			return key + seg[open:], ops
		}
		ops = append(ops, seg[open+1:open+close])
		seg = seg[open+close+1:]
	}
}

func splitSegments(path string) []string {
	if path == "" {
		return nil
	}
	var segs []string
	depth, start := 0, 0
	for i, r := range path {
		switch r {
		case '[':
			depth++
		case ']':
			depth--
		case '.':
			if depth == 0 {
				segs = append(segs, path[start:i])
				start = i + 1
			}
		}
	}
	return append(segs, path[start:])
}

// render turns a decoded-JSON scalar into the string smith would store.
// JSON numbers decode to float64, so integer ids must not come back as "1.23e+08".
func render(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprint(t)
	}
}
