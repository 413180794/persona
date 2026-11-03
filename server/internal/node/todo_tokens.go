package node

import (
	"context"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/host"
)

// syncTodoTokens receives expected persona_* tokens for sessions from hub and triggers reconciliation.
func (n *Node) syncTodoTokens(ctx context.Context, expected map[string]map[string]string) error {
	n.expectedTokensMu.Lock()
	if expected == nil {
		n.expectedTokens = make(map[string]map[string]string)
	} else {
		n.expectedTokens = expected
	}
	n.expectedSynced = true
	n.expectedTokensMu.Unlock()

	return n.reconcileTodoTokens(ctx)
}

// reconcileTodoTokens compares expected persona_* tokens against live herdr pane tokens,
// updating missing/different tokens, clearing obsolete ones, and consuming persona_from.
// It only touches persona_todo, persona_todo_ref, and persona_from.
func (n *Node) reconcileTodoTokens(ctx context.Context) error {
	n.expectedTokensMu.Lock()
	if !n.expectedSynced {
		// Before receiving sync from hub, read-only: do not clear any tokens.
		n.expectedTokensMu.Unlock()
		return nil
	}
	expectedBySession := make(map[string]map[string]string, len(n.expectedTokens))
	for sid, m := range n.expectedTokens {
		cp := make(map[string]string, len(m))
		for k, v := range m {
			cp[k] = v
		}
		expectedBySession[sid] = cp
	}
	n.expectedTokensMu.Unlock()

	var reporter host.MetadataReporter
	var h host.Host
	for _, candidate := range n.hosts.All() {
		if r, ok := candidate.(host.MetadataReporter); ok {
			reporter = r
			h = candidate
			break
		}
	}
	if reporter == nil || h == nil {
		return nil
	}

	// Get live agents from host
	agents, err := h.Agents(ctx)
	if err != nil {
		return err
	}

	// livePaneIDs tracks all live panes reported by host (even if Tokens is nil, e.g. fresh pane).
	// livePanes maps pane ID to its live metadata token map.
	livePaneIDs := make(map[string]bool, len(agents))
	livePanes := make(map[string]map[string]string, len(agents))
	for _, a := range agents {
		if a.PaneID != "" {
			livePaneIDs[a.PaneID] = true
			livePanes[a.PaneID] = a.Tokens
		}
	}

	// Resolve session IDs to live pane IDs (#873, #892).
	// Seed from n.agents first because its keys are the public session IDs sent by hub.
	// Only fall back to host-reported AgentSession for panes not covered by n.agents.
	paneToLiveSession := make(map[string]string)
	sessionToPane := make(map[string]string)

	// 1. From n.agents: authoritative for the public session IDs hub sends.
	n.mu.Lock()
	for sid, a := range n.agents {
		if a.PaneID != "" && livePaneIDs[a.PaneID] {
			paneToLiveSession[a.PaneID] = sid
			sessionToPane[sid] = a.PaneID
		}
	}
	n.mu.Unlock()

	// 2. Fall back to host-reported AgentSession only for panes n.agents does not cover, and only for a session that
	// has no pane yet: the same session open in two panes (`claude --resume` it in a second one) keeps its marks on the
	// pane it already has, instead of moving them to the other (#948).
	for _, a := range agents {
		if a.PaneID != "" && livePaneIDs[a.PaneID] {
			if _, covered := paneToLiveSession[a.PaneID]; !covered {
				if a.AgentSession != nil && a.AgentSession.Kind == "id" && a.AgentSession.Value != "" {
					if _, claimed := sessionToPane[a.AgentSession.Value]; claimed {
						continue
					}
					paneToLiveSession[a.PaneID] = a.AgentSession.Value
					sessionToPane[a.AgentSession.Value] = a.PaneID
				}
			}
		}
	}

	expected := make(map[string]map[string]string)
	for sid, tokens := range expectedBySession {
		if paneID, ok := sessionToPane[sid]; ok && paneID != "" {
			if activeSid, ok := paneToLiveSession[paneID]; ok && activeSid == sid {
				expected[paneID] = tokens
			}
		}
	}

	// Collect all pane IDs to inspect: either in expected or in live panes that have persona_* tokens
	panesToInspect := make(map[string]bool)
	for p := range expected {
		panesToInspect[p] = true
	}
	for p, tokens := range livePanes {
		for k := range tokens {
			if strings.HasPrefix(k, "persona_") {
				panesToInspect[p] = true
				break
			}
		}
	}

	ourKeys := []string{"persona_todo", "persona_todo_ref"}

	for paneID := range panesToInspect {
		paneRef := n.hosts.Ref(paneID)
		if paneRef.Kind != h.Kind() {
			// Defensive: the panes come from h's own Agents, so they are h's; a pane whose id names another host (an
			// Orca pane) is never written through h's reporter (#762 F4, #948).
			continue
		}

		live := livePanes[paneID]
		exp := expected[paneID]

		patch := make(map[string]*string)

		// Check persona_todo and persona_todo_ref
		for _, k := range ourKeys {
			expVal, hasExp := exp[k]
			liveVal, hasLive := live[k]

			if hasExp && expVal != "" {
				if !hasLive || liveVal != expVal {
					v := expVal
					patch[k] = &v
				}
			} else {
				if hasLive {
					patch[k] = nil // clear
				}
			}
		}

		// Check persona_from:
		// If pane has persona_from and the session already has an active link (on any todo), consume persona_from.
		if live != nil {
			if fromVal, ok := live["persona_from"]; ok && fromVal != "" {
				// Has active todo if it has expected persona_todo_ref
				if exp != nil && exp["persona_todo_ref"] != "" {
					patch["persona_from"] = nil
				}
			}
		}

		if len(patch) > 0 {
			if err := reporter.ReportMetadata(ctx, paneRef, "persona", patch); err != nil {
				// Continue reconciling other panes even if one fails
				n.logf("todo reconcile: report metadata for %s failed: %v", paneID, err)
			}
		}
	}

	return nil
}

// startTodoTokensReconciler periodically reconciles tokens every 30s to recover after herdr restart.
func (n *Node) startTodoTokensReconciler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := n.reconcileTodoTokens(ctx); err != nil {
					n.logf("todo reconcile: periodic check failed: %v", err)
				}
			}
		}
	}()
}
