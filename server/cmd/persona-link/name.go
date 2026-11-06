package main

// commandName is fixed regardless of the executable's location or environment.
func commandName(_ string, _ func(string) string) string {
	return "persona-link"
}
