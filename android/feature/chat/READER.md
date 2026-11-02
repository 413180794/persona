# 全文阅读器接入

`FullTextReader` 只保留打开时传入的字符串，关闭后重新打开才会拿到新内容。工具截断输出仍由现有 `repository.fullText` 获取；本地长回复、代码和原始输入直接使用点击时的文本。

分页目标为 8192 个 UTF-16 码元，用 `\X` 匹配完整字素。单个字素超过分页目标时允许独占一页，保证组合字符、代理对和 emoji 序列不被拆开。复制取整个快照，不受当前页和搜索范围限制。搜索默认为本页，可切换全文，匹配支持跨分页边界。

`ReaderNavigation.kt` 提供 diff 前后跳转和工作过程吸附收起。相邻增删行算一处改动。倒置聊天列表通过可见行的 key 找到工作组，在顶部画浮层；收起后等列表更新，再回到工作行。

合并时保留 `ChatScreen.kt` 的两处挂载：聊天列表上的 `WorkCollapseOverlay`，以及替换旧全文 sheet 的 `FullTextReader`。`ChatContent.kt` 和 `StructuredContent.kt` 只负责入口与 diff 列表状态。

关键测试为 `ReaderSnapshotTest`，覆盖冻结、字素边界、空内容、本页及跨页搜索和连续改动分组。未做性能实测。
