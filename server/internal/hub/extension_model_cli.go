package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// The extension model's commands (docs/extensions.md「贡献的开关」), next to extensionUsage's.
const extensionModelUsage = `  contributions <id>                its contributions and their switches
  contribution <id> <point>/<cid> on|off
                                    switch one contribution (a connector's is the connector's switch)`

// extensionModelCommand runs the extension model's commands; done is false for a command that is not one of them.
func extensionModelCommand(ctx context.Context, c *extensionClient, cmd string, positional []string, stdout io.Writer) (done bool, err error) {
	switch cmd {
	case "contributions":
		if len(positional) < 1 {
			return true, errors.New(extensionModelUsage)
		}
		var d ExtensionDetail
		if err := c.do(ctx, "GET", "/api/v1/extensions/"+url.PathEscape(positional[0])+"/-/", nil, &d); err != nil {
			return true, err
		}
		printContributions(stdout, d.Contributions)
		return true, nil
	case "contribution":
		if len(positional) < 3 || (positional[2] != "on" && positional[2] != "off") {
			return true, errors.New(extensionModelUsage)
		}
		point, cid, ok := strings.Cut(positional[1], "/")
		if !ok {
			return true, fmt.Errorf("%q is not <point>/<id>", positional[1])
		}
		var d ExtensionDetail
		path := "/api/v1/extensions/" + url.PathEscape(positional[0]) + "/-/contributions/" + url.PathEscape(point) + "/" + url.PathEscape(cid)
		if err := c.do(ctx, "PUT", path, map[string]bool{"enabled": positional[2] == "on"}, &d); err != nil {
			return true, err
		}
		printContributions(stdout, d.Contributions)
		return true, nil
	}
	return false, nil
}

func printContributions(w io.Writer, cs []ContributionView) {
	if len(cs) == 0 {
		fmt.Fprintln(w, "没有贡献")
	}
	for _, c := range cs {
		state := map[bool]string{true: "开", false: "关"}[c.Enabled]
		note := ""
		switch {
		case c.Via == "connector":
			note = "（跟着连接器的开关）"
		case !c.CanDisable:
			note = "（这里改不了）"
		}
		fmt.Fprintf(w, "%-28s %s  %s%s\n", c.Point+"/"+c.ID, state, c.Title, note)
	}
}
