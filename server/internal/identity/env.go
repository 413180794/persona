package identity

import (
	"os"
	"strings"
)

func names(name string) (string, string) {
	_, suffix, _ := strings.Cut(name, "_")
	return "PERSONA_" + suffix, "JARVIS_" + suffix
}

func LookupEnv(name string) (string, bool) {
	if !strings.HasPrefix(name, "JARVIS_") && !strings.HasPrefix(name, "PERSONA_") {
		return os.LookupEnv(name)
	}
	current, legacy := names(name)
	if v, ok := os.LookupEnv(current); ok {
		return v, true
	}
	return os.LookupEnv(legacy)
}

func Getenv(name string) string {
	v, _ := LookupEnv(name)
	return v
}

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
	currentUpdates := make(map[string]bool)
	for _, entry := range updates {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "JARVIS_") || strings.HasPrefix(key, "PERSONA_") {
			current, legacy := names(key)
			if key == current {
				currentUpdates[current] = true
			} else if currentUpdates[current] {
				continue
			}
			put(current + "=" + value)
			put(legacy + "=" + value)
		} else {
			put(entry)
		}
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, key+"="+values[key])
	}
	return out
}
