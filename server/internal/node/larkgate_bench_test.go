package node

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// benchGates is two gates: the example one and a second with only a command name.
func benchGates() []proto.CommandGate {
	second := exampleGate("https://api.example.com")
	second.Command, second.Script = "ex2", "example2/scripts/ex2.sh"
	return []proto.CommandGate{exampleGate(""), second}
}

// The command an assistant's Bash call usually is: leading assignments, a timeout, a gated call with a variable.
const benchLarkCmd = `A=1 B=2 timeout 20 ex GET /conversations/$A/messages`

func BenchmarkClassifyLark(b *testing.B) {
	for _, bc := range []struct {
		name  string
		gates []proto.CommandGate
	}{{"NoGates", nil}, {"TwoGates", benchGates()}} {
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				classifyLark(benchLarkCmd, bc.gates, "", nil)
				readsOnlyMemory("timeout 5 cat MEMORY.md", "/tmp", "/tmp")
			}
		})
	}
}

func BenchmarkGateWrite(b *testing.B) {
	gates := benchGates()
	cmd := `for i in 1 2; do ex GET /messages/$i; done`
	b.ReportAllocs()
	for b.Loop() {
		for _, g := range gates {
			gateWrite([]proto.CommandGate{g}, cmd)
		}
		gateWrite(gates, cmd)
	}
}
