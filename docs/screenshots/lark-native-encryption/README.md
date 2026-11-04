# 原生加密文字发送回归（#689）

2026-11-03，iPhone 18 Pro / iOS 27 模拟器。全部账号、文字、密码和发送结果来自独立 mock Hub；没有操作真实会话。以下 16 张截图已逐张检查。

| 场景 | 浅色 | 深色 |
| --- | --- | --- |
| 普通消息输入区 | [查看](light/native-removedordinary.png) | [查看](dark/native-removedordinary.png) |
| 选择历史对外展示 | [查看](light/native-removedcovers.png) | [查看](dark/native-removedcovers.png) |
| 手机解锁 | [查看](light/native-removedunlock.png) | [查看](dark/native-removedunlock.png) |
| 加密发送回显 | [查看](light/native-removedsent.png) | [查看](dark/native-removedsent.png) |
| 失败重试 | [查看](light/native-removedretry.png) | [查看](dark/native-removedretry.png) |
| 收件箱保护加密草稿 | [查看](light/native-removedinbox-protected.png) | [查看](dark/native-removedinbox-protected.png) |
| 回到消息恢复草稿 | [查看](light/native-removeddraft-restored.png) | [查看](dark/native-removeddraft-restored.png) |
| 清空后普通收件箱 | [查看](light/native-removedinbox-unchanged.png) | [查看](dark/native-removedinbox-unchanged.png) |

`LarkNativeEncryptionFlowTests` 的 3 个流程在浅色、深色各通过一次；此前一轮相同流程也通过，随后视觉检查发现模式栏背景透字，修复后重新跑完并更新此处截图。错误密码留在解锁页，正确密码继续发送；明确拒绝后重试使用原密文；切到收件箱不能将加密草稿作为普通消息发送，返回消息及重启后草稿仍在，清空草稿后普通收件箱不显示新模式栏。

模式栏只占输入框上方一行，选中加密时同排显示对外展示。模式栏和聊天内容之间的背景隔离已检查，键盘展开时无文字重叠。发送气泡的圆环沿用原有已读状态，不是再次提交消息的加载指示；本机发件箱发送标记已由 UI 测试断言消失。

截图不表示真实 lark-connector / Hub 已部署，也不覆盖加密附件、话题和线程输入区。
