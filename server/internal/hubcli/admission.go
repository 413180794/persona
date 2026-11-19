package hubcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/nwverse/persona/server/internal/admission"
	"github.com/nwverse/persona/server/internal/hub"
)

func runAdmission(ctx context.Context, args []string, out io.Writer) error {
	if !(len(args) == 1 && (args[0] == "status" || args[0] == "init")) &&
		!(len(args) == 2 && (args[0] == "close" || args[0] == "check" || args[0] == "open" || args[0] == "ledger-pending")) &&
		!(len(args) == 3 && args[0] == "reconcile-ledger") {
		return errors.New("usage: personad hub admission init | status | (close|check|open|ledger-pending) <attempt> | reconcile-ledger <attempt> <operation-id>")
	}
	gate, err := admission.FromEnv()
	if err != nil {
		return err
	}
	if gate == nil {
		return fmt.Errorf("%s is not set", admission.Env)
	}
	switch args[0] {
	case "ledger-pending":
		pending, err := gate.LedgerPending(args[1])
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(pending)
	case "reconcile-ledger":
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		result, err := gate.ReconcileLedger(ctx, args[1], args[2], hub.AdmissionLedgerCommitted)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(result)
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
