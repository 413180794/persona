package admission

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const ledgerTicketPrefix = "work-extension-ledger-record."
const ledgerAuditPrefix = "ledger-committed."

var ledgerID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// LedgerAuthority identifies the primary database used by the running Manager.
type LedgerAuthority struct {
	SystemIdentifier string `json:"system_identifier"`
	DatabaseOID      uint32 `json:"database_oid"`
}

func (a LedgerAuthority) valid() bool {
	system, err := strconv.ParseInt(a.SystemIdentifier, 10, 64)
	return err == nil && system != 0 && a.SystemIdentifier == strconv.FormatInt(system, 10) && a.DatabaseOID != 0
}

type LedgerOperation struct {
	ID        string          `json:"operation_id"`
	Authority LedgerAuthority `json:"ledger_authority"`
}

// BindLedgerWriter is called by the Manager from its actual database, before it
// serves. The local operator's close command never reads a DSN to set this.
func (g *Gate) BindLedgerWriter(authority LedgerAuthority) error {
	if !authority.valid() {
		return errors.New("invalid ledger writer database identity")
	}
	lock, err := g.lock()
	if err != nil {
		return err
	}
	defer lock.close()
	state, err := g.state()
	if err != nil {
		return err
	}
	if state.Phase != "open" {
		return ErrClosed
	}
	if state.LedgerAuthority != nil && *state.LedgerAuthority == authority {
		return nil
	}
	active, err := g.active()
	if err != nil {
		return err
	}
	if active["extension-ledger-record"] != 0 {
		return errors.New("cannot bind another ledger database while ledger tickets are active")
	}
	state.LedgerAuthority = &authority
	return g.publish(state)
}

type LedgerPending struct {
	Attempt      string   `json:"attempt"`
	Operations   []string `json:"operations"`
	Unidentified int      `json:"unidentified"`
}

type LedgerSettlement struct {
	Attempt     string          `json:"attempt"`
	Operation   string          `json:"operation_id"`
	Audit       string          `json:"audit_file"`
	Status      string          `json:"status"`
	ProofSource string          `json:"proof_source"`
	Authority   LedgerAuthority `json:"ledger_authority"`
}

// AdmitLedger publishes the opaque operation ID before the caller can start SQL.
// A crash before the ticket is published cannot leave an empty active ticket.
func (g *Gate) AdmitLedger() (LedgerOperation, func(), error) {
	if g == nil {
		return LedgerOperation{}, func() {}, nil
	}
	lock, err := g.lock()
	if err != nil {
		return LedgerOperation{}, nil, err
	}
	defer lock.close()
	s, err := g.state()
	if err != nil {
		return LedgerOperation{}, nil, err
	}
	if s.Phase != "open" {
		return LedgerOperation{}, nil, ErrClosed
	}
	if s.LedgerAuthority == nil {
		return LedgerOperation{}, nil, errors.New("no ledger writer database is bound")
	}

	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return LedgerOperation{}, nil, err
	}
	operation := LedgerOperation{ID: hex.EncodeToString(entropy[:]), Authority: *s.LedgerAuthority}
	tmp, err := os.CreateTemp(g.dir, ".ledger.")
	if err != nil {
		return LedgerOperation{}, nil, err
	}
	defer os.Remove(tmp.Name())
	writeErr := json.NewEncoder(tmp).Encode(operation)
	if err := errors.Join(writeErr, tmp.Sync(), tmp.Close()); err != nil {
		return LedgerOperation{}, nil, err
	}
	path := filepath.Join(g.dir, ledgerTicketPrefix+operation.ID)
	if err := os.Link(tmp.Name(), path); err != nil {
		return LedgerOperation{}, nil, err
	}
	if err := g.sync(); err != nil {
		return LedgerOperation{}, nil, err
	}
	return operation, func() {
		if err := g.finish(path); err != nil {
			log.Printf("admission left an unsettled extension-ledger-record operation: %v", err)
		}
	}, nil
}

// LedgerPending reports the identified ledger operations under the current
// closed attempt. Older or damaged tickets remain active and unidentified.
func (g *Gate) LedgerPending(attempt string) (LedgerPending, error) {
	lock, err := g.lock()
	if err != nil {
		return LedgerPending{}, err
	}
	defer lock.close()
	state, err := g.ledgerAttempt(attempt)
	if err != nil {
		return LedgerPending{}, err
	}
	files, err := os.ReadDir(g.dir)
	if err != nil {
		return LedgerPending{}, err
	}
	p := LedgerPending{Attempt: attempt, Operations: []string{}}
	for _, file := range files {
		id, match := strings.CutPrefix(file.Name(), ledgerTicketPrefix)
		if !match {
			continue
		}
		if state.LedgerAuthority == nil || !ledgerID.MatchString(id) ||
			checkLedgerTicket(filepath.Join(g.dir, file.Name()), LedgerOperation{ID: id, Authority: *state.LedgerAuthority}) != nil {
			p.Unidentified++
			continue
		}
		p.Operations = append(p.Operations, id)
	}
	return p, nil
}

// ReconcileLedger archives exactly one ticket after verifying that operation's
// committed receipt. A single same-directory rename publishes the audit record
// and removes the active ticket. Repeating it under the same fence is safe.
func (g *Gate) ReconcileLedger(ctx context.Context, attempt, id string, committed func(context.Context, LedgerOperation) (bool, error)) (LedgerSettlement, error) {
	if !ledgerID.MatchString(id) {
		return LedgerSettlement{}, errors.New("invalid ledger operation ID")
	}
	lock, err := g.lock()
	if err != nil {
		return LedgerSettlement{}, err
	}
	defer lock.close()
	state, err := g.ledgerAttempt(attempt)
	if err != nil {
		return LedgerSettlement{}, err
	}
	if state.LedgerAuthority == nil {
		return LedgerSettlement{}, errors.New("this closed attempt has no bound ledger database")
	}
	name := ledgerAuditPrefix + attempt + "." + id
	audit := filepath.Join(g.dir, name)
	ticket := filepath.Join(g.dir, ledgerTicketPrefix+id)
	result := LedgerSettlement{Attempt: attempt, Operation: id, Audit: name, Status: "committed",
		Authority: *state.LedgerAuthority, ProofSource: "database"}
	operation := LedgerOperation{ID: id, Authority: *state.LedgerAuthority}
	if err := checkLedgerTicket(audit, operation); err == nil {
		if _, activeErr := os.Lstat(ticket); !errors.Is(activeErr, os.ErrNotExist) {
			return LedgerSettlement{}, errors.New("archived ledger operation still has a live ticket")
		}
		if err := g.sync(); err != nil {
			return LedgerSettlement{}, err
		}
		result.ProofSource = "archive"
		return result, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return LedgerSettlement{}, err
	}
	if state.Phase != "draining" {
		return LedgerSettlement{}, errors.New("ledger reconciliation requires a draining fence")
	}
	if err := checkLedgerTicket(ticket, operation); err != nil {
		return LedgerSettlement{}, err
	}
	proved, err := committed(ctx, operation)
	if err != nil {
		return LedgerSettlement{}, err
	}
	if !proved {
		return LedgerSettlement{}, errors.New("ledger operation has no committed receipt")
	}
	if err := os.Rename(ticket, audit); err != nil {
		return LedgerSettlement{}, err
	}
	if err := g.sync(); err != nil {
		return LedgerSettlement{}, err
	}
	return result, nil
}

func (g *Gate) ledgerAttempt(attempt string) (State, error) {
	s, err := g.state()
	if err != nil {
		return State{}, err
	}
	if !idPattern.MatchString(attempt) || s.Phase == "open" || s.Attempt != attempt {
		return State{}, errors.New("admission attempt does not own the closed fence")
	}
	return s, nil
}

func checkLedgerTicket(path string, expected LedgerOperation) error {
	stat, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 256 {
		return errors.New("invalid ledger ticket")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var operation LedgerOperation
	if err := json.Unmarshal(data, &operation); err != nil || operation != expected {
		return errors.New("ledger ticket does not match the operation and bound database")
	}
	return nil
}
