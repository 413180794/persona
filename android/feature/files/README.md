# 会话文件与 Git

`:feature:files` 提供 `SessionFilesScreen`，接收已有的 `ChatRoute`、会话目录、`SessionFiles`、返回对话和插入路径回调。文件与 Git 是对话页内的两种视图，不增加返回栈路由。

已实现会话目录浏览、面包屑、隐藏文件开关、大小和修改时间、Git 标记、拷贝与插入路径；代码轻量高亮、固定行号、长文本横竖滚动、图片缩放和平移；Git 分支及上游状态、改动列表、暂存与未暂存 diff、新旧行号、提交历史分页及提交 diff。空目录、非仓库、二进制文件、截断内容、加载失败都有说明或重试入口。颜色取自现有设计系统，按钮标签单行。

App 已依赖 `:feature:files`，`AppServices` 用同一活动连接创建 `HubSessionFiles`；`ShellChat` 已接会话内文件／Git 视图，按目录及仓库状态显示入口，助手隐藏这些入口。插入路径使用 `insertFilePath` 更新原草稿后回到对话，不自动发送；真实路径插入、返回及预览仍待验收。

本车道只新增 contracts/hub 文件和 feature 模块，在 settings 中增加自己的 include 行；未修改 App、其他 feature、iOS 或服务端。接口复用 `/fs`、`/file`、`/git/status`、`/git/diff`、`/git/log`、`/git/show`，路径和查询值分别编码。列表中的空值按现有 `JsonCodec` 处理。会话目录可以是仓库子目录，Git diff 使用响应里的仓库根目录，避免相对路径偏移。切换目录或预览时取消旧任务，并用请求代次防止迟到响应覆盖当前状态。

验证命令（在 `android/` 执行）：

```sh
./gradlew :feature:files:testDebugUnitTest --tests '*FilesCriticalPathTest' :core:hub:test --tests '*HubSessionFilesTest' -Pkotlin.compiler.execution.strategy=in-process --console=plain
```

上述命令已通过，同时编译了新模块、contracts 和 hub。两项测试覆盖特殊字符路径编码、空列表响应、HTTP 失败、目录及 diff 切换、仓库根目录、diff 行号和路径插入。

本次按快速模式未做模拟器截图和真实 hub 联调；性能未实测。Markdown 当前按源文件预览，PDF 显示不支持预览的说明；Markdown 渲染、PDF 阅读与分享不在本次指定的代码、图片和长文本预览范围内。提交历史变化期间的分页采用现有服务端偏移协议，不保证跨页快照一致。
