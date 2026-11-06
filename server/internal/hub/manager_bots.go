package hub

import (
	"cmp"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/extwire"
)

// managerBotConflict identifies the managers sharing a bot, without exposing the bot's account ID to clients.
type managerBotConflict struct {
	Platform string   `json:"platform"`
	Managers []string `json:"managers"`
}

// managerBotConflicts compares the last reports of installed managers, including offline ones. Reports from
// removed managers and repeated identities within one manager cannot create a warning.
func (h *Hub) managerBotConflicts(roster []managerInfo) []managerBotConflict {
	groups := map[extwire.BotIdentity][]string{}
	for _, m := range roster {
		if m.Installing != nil {
			continue
		}
		r := h.extReportOf(m.ID)
		if r == nil {
			continue
		}
		for _, bot := range r.BotIdentities {
			bot.ID = strings.TrimSpace(bot.ID)
			if bot.ID == "" || (bot.Kind != "feishu" && bot.Kind != "wechat") {
				continue
			}
			if !slices.Contains(groups[bot], m.ID) {
				groups[bot] = append(groups[bot], m.ID)
			}
		}
	}
	var out []managerBotConflict
	for bot, ids := range groups {
		if len(ids) > 1 {
			slices.Sort(ids)
			out = append(out, managerBotConflict{Platform: bot.Kind, Managers: ids})
		}
	}
	slices.SortFunc(out, func(a, b managerBotConflict) int {
		if c := cmp.Compare(a.Platform, b.Platform); c != 0 {
			return c
		}
		return slices.Compare(a.Managers, b.Managers)
	})
	return slices.CompactFunc(out, func(a, b managerBotConflict) bool {
		return a.Platform == b.Platform && slices.Equal(a.Managers, b.Managers)
	})
}
