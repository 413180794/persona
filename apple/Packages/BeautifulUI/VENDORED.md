# BeautifulUI（拷入的副本）

来源：BeautifulUI 仓库的 `02907da`（`main` 的 `9b05853` 之后，`jarvis-host-hooks` 分支上的 Prompt Bar 接口），`BeautifulUI/` 目录原样拷入，
作为本地 Swift Package 由 `apple/project.yml` 引用。许可：`LICENSE`、`LICENSE-beautifului`、`NOTICE`，根目录的
`THIRD_PARTY_NOTICES.md` 也有一份。

**不在这里改库的代码**：要改先改 BeautifulUI 仓库，再整个目录重新拷一次：

```sh
rm -r apple/Packages/BeautifulUI   # 先看一眼 git status，确认这里没有本地改动
git -C <BeautifulUI 仓库> archive --format=tar <提交> BeautifulUI | tar -x -C apple/Packages/
```

然后把上面的来源提交改掉。Jarvis 用到的宿主接口（`typeScale`、`entrances`、`ToolChipRow`、`ToolChipGroup`……）见库的 README
「给宿主 App 的接口」。
