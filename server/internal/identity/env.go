package identity

import (
	"os"
	"strings"
)

func LookupEnv(name string) (string, bool) {
	return os.LookupEnv(name)
}

func Getenv(name string) string {
	v, _ := LookupEnv(name)
	return v
}

// Environment applies updates once per key, preserving first appearance order.
func Environment(base []string, updates ...string) []string {
	values := make(map[string]string)
	order := make([]string, 0, len(base)+len(updates))
	put := func(entry string) {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			return
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	for _, entry := range base {
		put(entry)
	}
	for _, entry := range updates {
		put(entry)
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, key+"="+values[key])
	}
	return out
}
