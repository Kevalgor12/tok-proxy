// Package analytics renders tok's savings and AI-spend reports from the local store:
// gain (filter compression), stats (token consumption), econ (cost dashboard), session
// (activity grouping), and discover (missed-optimization hints).
package analytics

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// orderedGroups is a map that remembers first-insertion order, so aggregations reproduce
// the Node build's Map iteration order before an explicit sort is applied.
type orderedGroups[V any] struct {
	order []string
	m     map[string]*V
}

func newGroups[V any]() *orderedGroups[V] {
	return &orderedGroups[V]{m: map[string]*V{}}
}

// get returns the bucket for key, creating a zero one (and recording its order) on first use.
func (g *orderedGroups[V]) get(key string) *V {
	if v, ok := g.m[key]; ok {
		return v
	}
	v := new(V)
	g.m[key] = v
	g.order = append(g.order, key)
	return v
}

type groupEntry[V any] struct {
	key string
	val *V
}

func (g *orderedGroups[V]) entries() []groupEntry[V] {
	out := make([]groupEntry[V], len(g.order))
	for i, k := range g.order {
		out[i] = groupEntry[V]{key: k, val: g.m[k]}
	}
	return out
}

func head[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func tail[T any](s []T, n int) []T {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

func reversed[T any](s []T) []T {
	out := make([]T, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}

func maxInt(vals []int) int {
	m := 0
	for i, v := range vals {
		if i == 0 || v > m {
			m = v
		}
	}
	return m
}

// bar renders a horizontal bar of block glyphs, scaled so max maps to width columns.
func bar(value, max, width int) string {
	if max <= 0 {
		return ""
	}
	n := int(math.Round(float64(value) / float64(max) * float64(width)))
	return strings.Repeat("█", n)
}

// day10 is the YYYY-MM-DD prefix of an ISO timestamp (the grouping key for daily charts).
func day10(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

// jsNumber renders a float the way JavaScript's String(number) does for the CSV columns:
// plain decimal, no trailing zeros, no exponent for the cost magnitudes tok deals with.
func jsNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// jsonPretty marshals with a 2-space indent and no HTML escaping, matching
// JSON.stringify(value, null, 2).
func jsonPretty(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return ""
	}
	return strings.TrimRight(buf.String(), "\n")
}
