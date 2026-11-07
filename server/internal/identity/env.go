package identity

import (
	"os"
	"strings"

	"github.com/nwverse/persona/server/internal/compat"
)

func LookupEnv(name string) (string, bool) {
	current, legacy := compat.EnvNames(name)
	if value, ok := os.LookupEnv(current); ok {
		return value, true
	}
	return os.LookupEnv(legacy)
}

func Setenv(name, value string) error {
	current, legacy := compat.EnvNames(name)
	if err := os.Setenv(current, value); err != nil {
		return err
	}
	if legacy != current {
		return os.Setenv(legacy, value)
	}
	return nil
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
	// Canonical updates win independently of argument order. Mirror updates for
	// older child processes, without changing unrelated inherited variables.
	canonical := make(map[string]bool)
	for _, entry := range updates {
		key, _, ok := strings.Cut(entry, "=")
		current, _ := compat.EnvNames(key)
		if ok && key == current {
			canonical[current] = true
		}
	}
	for _, entry := range updates {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		current, legacy := compat.EnvNames(key)
		if key != current && canonical[current] {
			continue
		}
		put(current + "=" + value)
		if legacy != current {
			put(legacy + "=" + value)
		}
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, key+"="+values[key])
	}
	return out
}
