package admission

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const ledgerTicketPrefix = "work-extension-ledger-record."
const ledgerAuditPrefix = "ledger-committed."

var ledgerID = regexp.MustCompile(`^[0-9a-f]{32}$`)

type LedgerPending struct {
	Attempt      string   `json:"attempt"`
	Operations   []string `json:"operations"`
	Unidentified int      `json:"unidentified"`
}

type LedgerSettlement struct {
	Attempt   string `json:"attempt"`
	Operation string `json:"operation_id"`
	Audit     string `json:"audit_file"`
	Status    string `json:"status"`
}

// AdmitLedger publishes the opaque operation ID before the caller can start SQL.
// A crash before the ticket is published cannot leave an empty active ticket.
func (g *Gate) AdmitLedger() (string, func(), error) {
	if g == nil {
		return "", func() {}, nil
	}
	lock, err := g.lock()
	if err != nil {
		return "", nil, err
	}
	defer lock.close()
	s, err := g.state()
	if err != nil {
		return "", nil, err
	}
	if s.Phase != "open" {
		return "", nil, ErrClosed
	}

	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", nil, err
	}
	id := hex.EncodeToString(entropy[:])
	tmp, err := os.CreateTemp(g.dir, ".ledger.")
	if err != nil {
		return "", nil, err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.WriteString(id + "\n")
	if err := errors.Join(writeErr, tmp.Sync(), tmp.Close()); err != nil {
		return "", nil, err
	}
	path := filepath.Join(g.dir, ledgerTicketPrefix+id)
	if err := os.Link(tmp.Name(), path); err != nil {
		return "", nil, err
	}
	if err := g.sync(); err != nil {
		return "", nil, err
	}
	return id, func() {
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
	if _, err := g.ledgerAttempt(attempt); err != nil {
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
		if !ledgerID.MatchString(id) || checkLedgerTicket(filepath.Join(g.dir, file.Name()), id) != nil {
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
func (g *Gate) ReconcileLedger(ctx context.Context, attempt, id string, committed func(context.Context, string) (bool, error)) (LedgerSettlement, error) {
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
	name := ledgerAuditPrefix + attempt + "." + id
	audit := filepath.Join(g.dir, name)
	ticket := filepath.Join(g.dir, ledgerTicketPrefix+id)
	result := LedgerSettlement{Attempt: attempt, Operation: id, Audit: name, Status: "committed"}
	if err := checkLedgerTicket(audit, id); err == nil {
		if _, activeErr := os.Lstat(ticket); !errors.Is(activeErr, os.ErrNotExist) {
			return LedgerSettlement{}, errors.New("archived ledger operation still has a live ticket")
		}
		if err := g.sync(); err != nil {
			return LedgerSettlement{}, err
		}
		return result, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return LedgerSettlement{}, err
	}
	if state.Phase != "draining" {
		return LedgerSettlement{}, errors.New("ledger reconciliation requires a draining fence")
	}
	if err := checkLedgerTicket(ticket, id); err != nil {
		return LedgerSettlement{}, err
	}
	proved, err := committed(ctx, id)
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

func checkLedgerTicket(path, id string) error {
	stat, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() != int64(len(id)+1) {
		return errors.New("invalid ledger ticket")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(data) != id+"\n" {
		return errors.New("ledger ticket identity does not match its filename")
	}
	return nil
}
