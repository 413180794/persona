package node

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

func TestClaudeSettingsRPCLocale(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	n := &Node{home: t.TempDir(), state: newState()}
	for _, method := range []string{"claude.settings", "claude.settings.set"} {
		for _, tc := range []struct {
			locale string
			want   i18n.Lang
		}{
			{"en-US", i18n.En}, {"zh-TW", i18n.ZhHans}, {"ja-JP", i18n.Ja},
			{"de", i18n.En}, {"", i18n.Default},
		} {
			t.Run(method+"/"+tc.locale, func(t *testing.T) {
				params := map[string]any{"key": "effortLevel", "value": "high"}
				if tc.locale != "" {
					params["locale"] = tc.locale
				}
				raw, err := json.Marshal(params)
				if err != nil {
					t.Fatal(err)
				}
				out, err := n.handle(context.Background(), method, raw)
				if err != nil {
					t.Fatal(err)
				}
				got := out.(map[string]any)["settings"].([]ClaudeSetting)
				want, err := n.getClaudeSettings(tc.want)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("settings = %+v, want response in %s: %+v", got, tc.want, want)
				}
				if method == "claude.settings.set" {
					for _, setting := range got {
						if setting.Key == "effortLevel" && setting.Value != "high" {
							t.Fatalf("save did not apply: %+v", setting)
						}
					}
				}
			})
		}
	}
}
