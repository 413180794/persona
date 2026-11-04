{{if .Lark}}飞书 · {{else if .Doc}}飞书文档 · {{else if .Moment}}公司圈 · {{else if .Mail}}邮件 · {{else if .Connector}}{{if eq .Connector.Source "github"}}GitHub · {{end}}{{end}}{{attr .Title}}
