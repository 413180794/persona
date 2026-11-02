# 任务进度

`ChatTasks` 挂在 `ChatHeader` 下方，接收当前投影的 `TaskIndex` 和列表状态。没有任务时不占位置。任务页复用设计系统的卡片、按钮、字号和深浅色语义色，底部保留 UX 5.6 的原文说明。

`TaskIndex.progress` 按成功工具结果的顺序重建 TodoWrite、TaskCreate、TaskUpdate、TaskList 的任务表。原有 TaskIndex 只处理后台任务，因此本车道在 `:core:domain` 新增 `TaskProgress.kt`，并在 TaskIndex 中追加默认字段和构建调用。失败和没有结果的调用不更新任务；无法识别的任务列表保留此前状态。只有已加载的记录参与计算，未展开的服务端折叠记录不会自动补拉。

点击「查看相关记录」后冻结阅读窗口，按需展开合并轮次及工作行，再按记录键定位工具调用。返回胶囊恢复原来的列表偏移和展开状态。为避免列表自动跟随最新消息覆盖跳转，额外在 `:core:chatlist` 新增 `ChatRecordNavigation.kt`，没有改现有列表文件。

合并时保留 `ChatScreen.kt` 中的一个 ChatTasks 入口、`taskPageVisible` 状态及已读上报条件，避免任务页遮挡对话时仍上报已读。未改 ChatChrome、Agents、App 外壳或 iOS。

验证范围：任务结果重建及来源关联、合并轮次展开后的工具行定位两个单元测试，以及 `:app:assembleDebug`。按本次快速模式要求，未做模拟器截图验收。
