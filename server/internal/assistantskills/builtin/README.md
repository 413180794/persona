# 内置 skill

每个子目录是一个随 jarvisd 发布的 skill。只放 Jarvis 能再分发的（许可证允许、我们自己写的），并登记在 `embed.go` 的
`Redistributable` 里（`TestBuiltinsAreRedistributable` 守着）。从别处拷来、不能再分发的（比如 lark-connector）放用户自己的空间
`~/.jarvis/skills/`（`docs/hub-update.md`「skill 从二进制里拿出来」），仓库里的副本在 `skills/`。
