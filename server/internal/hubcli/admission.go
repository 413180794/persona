package hubcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/nwverse/persona/server/internal/admission"
)

func runAdmission(args []string, out io.Writer) error {
	if !(len(args) == 1 && (args[0] == "status" || args[0] == "init")) &&
		!(len(args) == 2 && (args[0] == "close" || args[0] == "check" || args[0] == "open")) {
		return errors.New("usage: personad hub admission init | status | (close|check|open) <attempt>")
	}
	gate, err := admission.FromEnv()
	if err != nil {
		return err
	}
	if gate == nil {
		return fmt.Errorf("%s is not set", admission.Env)
	}
	attempt := ""
	if len(args) == 2 {
		attempt = args[1]
	}
	status, err := gate.Control(args[0], attempt)
	if err != nil {
		return err
	}
	readback := struct {
		admission.State
		Coverage string `json:"coverage"`
	}{State: status, Coverage: "tracked_operations"}
	if err := json.NewEncoder(out).Encode(readback); err != nil {
		return err
	}
	if args[0] == "check" && status.Phase != "drained" {
		return errors.New("tracked admission operations remain unsettled")
	}
	return nil
}
