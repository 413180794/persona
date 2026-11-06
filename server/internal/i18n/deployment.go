package i18n

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// DeploymentText translates recognized deployment text at the response boundary. Deployment records and manager
// reports retain their original Chinese, including records written before i18n. Unknown node/SSH text is untouched.
// Only deployment catalogs are eligible: this is not a general reverse translation of user content or transcripts.
func DeploymentText(l Lang, text string) string { return deploymentText(l, text, 0) }

type deploymentTemplate struct {
	key     string
	pattern *regexp.Regexp
	names   []string
	fixed   int
}

var deploymentPlaceholders = regexp.MustCompile(`\{(\w+)\}`)
var deploymentTemplates = makeDeploymentTemplates()

func makeDeploymentTemplates() []deploymentTemplate {
	var out []deploymentTemplate
	for _, l := range Langs {
		for key, text := range catalogs[l] {
			if !strings.HasPrefix(key, "devices-deploy.") && !strings.HasPrefix(key, "hub-machine.deploy_") {
				continue
			}
			base := key
			if strings.HasPrefix(key, "devices-deploy.minutes_") || strings.HasPrefix(key, "devices-deploy.seconds_") {
				base = key[:strings.LastIndex(key, "_")]
			}
			var pattern strings.Builder
			pattern.WriteString("(?s)^")
			names := []string{}
			at, fixed := 0, 0
			for _, loc := range deploymentPlaceholders.FindAllStringIndex(text, -1) {
				literal := text[at:loc[0]]
				pattern.WriteString(regexp.QuoteMeta(literal))
				fixed += len(literal)
				name := text[loc[0]+1 : loc[1]-1]
				names = append(names, name)
				if name == "count" {
					pattern.WriteString("([0-9]+)")
				} else {
					pattern.WriteString("(.+?)")
				}
				at = loc[1]
			}
			pattern.WriteString(regexp.QuoteMeta(text[at:]))
			fixed += len(text[at:])
			pattern.WriteString("$")
			out = append(out, deploymentTemplate{base, regexp.MustCompile(pattern.String()), names, fixed})
		}
	}
	// A more specific template (e.g. not_back_why) must precede its shorter, open-ended counterpart.
	slices.SortFunc(out, func(a, b deploymentTemplate) int {
		if a.fixed != b.fixed {
			return b.fixed - a.fixed
		}
		if n := strings.Compare(a.key, b.key); n != 0 {
			return n
		}
		return strings.Compare(a.pattern.String(), b.pattern.String())
	})
	return out
}

func deploymentText(l Lang, text string, depth int) string {
	if text == "" || depth >= 8 {
		return text
	}
	for _, form := range deploymentTemplates {
		match := form.pattern.FindStringSubmatch(text)
		if match == nil {
			continue
		}
		args := Args{}
		for i, name := range form.names {
			value := match[i+1]
			switch name {
			case "count":
				n, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return text
				}
				args[name] = n
			case "err", "why", "wait":
				args[name] = deploymentText(l, value, depth+1)
			default:
				args[name] = value // OS, architecture and versions are data, not text to translate.
			}
		}
		return T(l, form.key, args)
	}
	return text
}
