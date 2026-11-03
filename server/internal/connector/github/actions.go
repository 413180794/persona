package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/prompts"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// The assistant's writes (prompts/mcp/github): they go through the hub's gate, by default the user allows each one
// on the phone, so Describe writes the card: where, what, who will see it.

var actionTools = []string{"github_comment", "github_review"}

func (c *Connector) Actions() []proto.Tool {
	out := make([]proto.Tool, 0, len(actionTools))
	for _, n := range actionTools {
		t := prompts.Tool(n)
		out = append(out, proto.Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return out
}

type writeArgs struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Body   string `json:"body"`
	Event  string `json:"event"`
}

func (a writeArgs) ref() string { return fmt.Sprintf("%s#%d", a.Repo, a.Number) }

func parseWrite(name string, args json.RawMessage) (writeArgs, error) {
	var a writeArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return a, fmt.Errorf("invalid arguments: %v", err)
	}
	a.Body = strings.TrimSpace(a.Body)
	switch {
	case !validRepo(a.Repo) || a.Number <= 0:
		return a, fmt.Errorf("repo must be owner/name and number a PR / issue number")
	case name == "github_comment" && a.Body == "":
		return a, fmt.Errorf("body is required")
	case name == "github_review" && a.Event != "APPROVE" && a.Event != "REQUEST_CHANGES" && a.Event != "COMMENT":
		return a, fmt.Errorf("event must be APPROVE, REQUEST_CHANGES or COMMENT")
	case name == "github_review" && a.Event != "APPROVE" && a.Body == "":
		return a, fmt.Errorf("body is required to %s", strings.ToLower(strings.ReplaceAll(a.Event, "_", " ")))
	}
	return a, nil
}

// Describe is the phone's card: the action, the PR / issue (with its title when GitHub answers quickly), the full
// text. Arguments it cannot read are listed as they are.
func (c *Connector) Describe(ctx context.Context, name string, args json.RawMessage) (connector.Action, string, error) {
	a, err := parseWrite(name, args)
	if err != nil {
		return connector.Action{Name: name}, name + " " + string(args), nil
	}
	where := a.ref()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var it struct {
		Title string `json:"title"`
	}
	if _, err := c.Get(ctx, fmt.Sprintf("/repos/%s/issues/%d", a.Repo, a.Number), &it); err == nil && it.Title != "" {
		where += "「" + it.Title + "」"
	}
	act := connector.Action{Name: name, Visible: a.Repo + " 仓库能看到的所有人（公开仓库是所有人）"}
	var head string
	switch {
	case name == "github_comment":
		head = "在 GitHub 上评论 " + where
	case a.Event == "APPROVE":
		head = "【批准】在 GitHub 上批准（approve）这个 PR：" + where
	case a.Event == "REQUEST_CHANGES":
		head = "在 GitHub 上要求修改（request changes）" + where
	default:
		head = "在 GitHub 上评审（comment）" + where
	}
	if a.Body == "" {
		return act, head, nil
	}
	return act, head + "：\n\n" + a.Body, nil
}

// Do posts it (only once the gate let it through).
func (c *Connector) Do(ctx context.Context, name string, args json.RawMessage) (string, error) {
	a, err := parseWrite(name, args)
	if err != nil {
		return "", err
	}
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	switch name {
	case "github_comment":
		body, _ := json.Marshal(map[string]string{"body": a.Body})
		if _, _, err := c.request(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", a.Repo, a.Number), nil, body, &out); err != nil {
			return "", err
		}
		return "Commented on " + a.ref() + ": " + out.HTMLURL, nil
	case "github_review":
		req := map[string]string{"event": a.Event}
		if a.Body != "" {
			req["body"] = a.Body
		}
		body, _ := json.Marshal(req)
		if _, _, err := c.request(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/pulls/%d/reviews", a.Repo, a.Number), nil, body, &out); err != nil {
			return "", err
		}
		return "Review (" + a.Event + ") submitted on " + a.ref() + ": " + out.HTMLURL, nil
	}
	return "", fmt.Errorf("unknown tool %s", name)
}
