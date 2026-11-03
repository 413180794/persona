# 飞书草稿与发件箱恢复回归（#688）

2026-11-03，iPhone 18 Pro / iOS 27。浅色、深色各完成以下九条 mock 流程，并检查关键状态截图。真实账号只读核对接口结构，没有发送消息或已读回执。连续用例共用 mock 历史，所以部分截图包含先前用例发出的文件和图片；气泡旁圆环为主线已读标记。

| 状态 | 浅色 | 深色 |
| --- | --- | --- |
| 后台保存、终止 App 后恢复文字和照片 | [草稿](light/lark-draft-restored.png) | [草稿](dark/lark-draft-restored.png) |
| 失败文件重启后仍可显式重试 | [失败](light/lark-failure-restored.png) | [失败](dark/lark-failure-restored.png) |
| 回执丢失只检查结果，不重复上传 | [菜单](light/lark-file-check-only.png) | [菜单](dark/lark-file-check-only.png) |
| 成功文件没有消息 ID，只允许移除本机记录 | [菜单](light/lark-idless-file-receipt.png) | [菜单](dark/lark-idless-file-receipt.png) |
| 本机草稿无法读取 | [入口](light/lark-draft-unreadable.png) | [入口](dark/lark-draft-unreadable.png) |
| 清除本机草稿前说明删除范围 | [确认](light/lark-draft-recovery-confirm.png) | [确认](dark/lark-draft-recovery-confirm.png) |
| 确认清除后可重新编辑 | [编辑](light/lark-draft-recovered.png) | [编辑](dark/lark-draft-recovered.png) |
| 旧版草稿先确认飞书账号归属 | [确认](light/lark-legacy-draft-confirm.png) | [确认](dark/lark-legacy-draft-confirm.png) |
| 旧版草稿恢复后重启，文字和文件仍保留 | [恢复](light/lark-legacy-draft-restored.png) | [恢复](dark/lark-legacy-draft-restored.png) |

`LarkAttachmentFlowTests` 九条流程：图片预览/下拉关闭/移除，图片回显一次，文件明确拒绝后编辑与重试，照片草稿后台保存及重启恢复，失败文件重启恢复与重试，丢失回执后的检查及重启核验，无 ID 成功文件移除本机记录，损坏草稿确认清除后重新编辑，旧版草稿确认归属后迁移并重启恢复。恢复用例重新启动时移除 DEBUG 注入参数；照片用例在离开前断言完整输入，避免把键盘未完成输入当成存储结果。

48 项存储、附件及队列测试（3 个 suite）和 5 项 mock 测试通过。覆盖：Hub 组及实际飞书账号隔离、路由/令牌更新沿用草稿、发送前核对账号、切换账号阻止 POST、草稿一秒写入合并、离开及发送时立即保存、迟到的延迟写入不恢复旧草稿、解除配对取消进行中的请求并清理本机记录、迟到的身份响应不恢复绑定、其他 Hub 数据保留、旧版媒体确认迁移、无 ID 成功文件可移除，以及原子事务、丢失媒体、损坏记录和不确定发送结果。

Xcode 26.0 的 iOS（含 Watch 和扩展）及 macOS 构建通过；隐私扫描、diff 检查通过。GitHub Actions 的隐私与工作流检查也已恢复执行，运行时代码提交 628e6d87 通过，不再以此前 runner 未分配作为本轮验证依据。

草稿按 Hub 组、实际飞书 userId 和会话保存，不依赖地址或令牌。旧版记录没有账号身份，只有当前/备用地址可定位的旧记录和旧文字草稿提供确认恢复入口；不自动推测所有历史凭据对应的账号。发送、重试和刷新期间可继续编辑，输入框只在本机提交事务期间短暂锁定。

文件上传没有上游幂等协议。不确定结果不允许重新上传；成功但缺少消息 ID 的记录不按同名文件猜测回显，可能与服务端消息暂时并存，可手动移除本机记录。损坏草稿的恢复是明确确认后的本机清除，不是备份或远端撤回。移除 Hub 会失效队列后清理本机数据；普通文件系统删除仍可能因系统 I/O 错误失败，不承诺故障环境中的绝对清理。

本阶段复用已有只读 accounts 接口，不需要新增服务端部署。加密发送、话题附件和置顶列表不在本 PR 范围。
