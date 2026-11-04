package lark

import (
	"context"
	"errors"
	"net/http"
)

// MessageDisplaySettings exposes no scripts or automatic actions from local settings.
type MessageDisplaySettings struct {
	AccountID       string `json:"account_id"`
	AntiRecall      bool   `json:"anti_recall"`
	ShowEditHistory bool   `json:"show_edit_history"`
}

type MessageDisplayChange struct {
	AccountID string `json:"account_id"`
	Key       string `json:"key"`
	Value     bool   `json:"value"`
	Expected  bool   `json:"expected"`
}

var ErrDisplayAccountChanged = errors.New("飞书账号已切换，请重新打开设置")

// SelectedUserID follows the explicitly selected account, never another primary account.
func (c *Client) SelectedUserID(ctx context.Context) (string, error) {
	var out struct {
		ActiveID string `json:"activeId"`
		Accounts []struct {
			ID       string `json:"id"`
			UserID   string `json:"userId"`
			SignedIn bool   `json:"signedIn"`
			Active   bool   `json:"active"`
		} `json:"accounts"`
	}
	if err := c.do(ctx, "GET", "/accounts", nil, nil, &out); err != nil {
		return "", err
	}
	selected := ""
	count := 0
	for _, a := range out.Accounts {
		if (out.ActiveID != "" && a.ID == out.ActiveID) || (out.ActiveID == "" && a.Active) {
			count++
			if a.SignedIn {
				selected = a.UserID
			}
		}
	}
	if count != 1 || selected == "" {
		return "", ErrNotLoggedIn
	}
	return selected, nil
}

func (c *Client) MessageDisplay(ctx context.Context, change *MessageDisplayChange) (MessageDisplaySettings, error) {
	out := MessageDisplaySettings{}
	account, err := c.SelectedUserID(ctx)
	if err != nil {
		return out, err
	}
	method, path := "GET", "/settings/local"
	var body any
	if change != nil {
		if change.AccountID != account {
			return out, ErrDisplayAccountChanged
		}
		field := map[string]string{"anti_recall": "antiRecall", "show_edit_history": "showEditHistory"}[change.Key]
		if field == "" {
			return out, errors.New("unsupported message display setting")
		}
		method, path = "PATCH", "/settings/local/message-display"
		body = map[string]any{"patch": map[string]bool{field: change.Value}, "expected": map[string]bool{field: change.Expected}}
	}
	var raw struct {
		AntiRecall      bool `json:"antiRecall"`
		ShowEditHistory bool `json:"showEditHistory"`
	}
	if err := c.do(ctx, method, path, nil, body, &raw); err != nil {
		return out, err
	}
	after, err := c.SelectedUserID(ctx)
	if err != nil {
		return out, err
	}
	if after != account {
		return out, ErrDisplayAccountChanged
	}
	return MessageDisplaySettings{AccountID: account, AntiRecall: raw.AntiRecall, ShowEditHistory: raw.ShowEditHistory}, nil
}

func MessageDisplayErrorStatus(err error) int {
	if errors.Is(err, ErrDisplayAccountChanged) {
		return http.StatusConflict
	}
	var upstream *doError
	if errors.As(err, &upstream) {
		switch upstream.Status {
		case http.StatusConflict, http.StatusForbidden, http.StatusNotFound:
			return upstream.Status
		}
	}
	return http.StatusBadGateway
}
