# 经验：飞书与邮件

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 飞书消息渲染照搬 lark-connector（`docs/ios.md`「飞书消息渲染」）：hub 原样转发 `parts`，只把媒体 URL 改写到 `/api/v1/lark/media/`；
  看 lark-connector 源码直接 `git clone https://github.com/Eric-2369/lark-connector`（hub 机器上只有部署产物）。
- lark-connector 会话列表的坑（`docs/protocol.md` 飞书 API）：`isDone` 字段不可靠（done scope 里全是 false），已完成以 scope 为准；
  置顶看 `onTopRankAt`（`settings.isShortcut` 也不准），且置顶会话分散在 inbox 和 done 两个 scope；未知 scope 会被静默当成 inbox。
- 打开飞书会话不能用 `tail=1`（那是「回到最新」）：有未读时要 `aroundPosition=readPosition+1`，已读只上报真正上屏的消息。
- 有的网络（作者的 hub 机器）连不上 Google 的多数 IPv4 地址（`imap.gmail.com` / `smtp.gmail.com` 常解析到 173.194.x.x，所有端口超时），HTTPS 能通只是走了 IPv6。
  hub 访问 Gmail 一律走 `hub.json` 的 `google.proxy`（比如 hub 机器上的本地代理），别再加直连兜底（`docs/mail.md`）。
- 客户端和 hub 的 JSON 形状对不上，Go 的解码器直接 400，模拟器上不点那条路径就看不出来：iOS 回复邮件时把 `references` 发成数组，
  hub 的 `SendRequest` 只收字符串，手动回复会被 400 拒掉（#587，读代码发现）。改请求体的字段类型时 hub 两种都收（`mail.MessageIDs`），
  并加一个按各端真实形状 POST 的 HTTP 测试（`TestHTTPSendReferences`）。
- IMAP 扩展要先看服务端声明的能力再用：QQ 邮箱不声明 LIST-EXTENDED，收到带 `RETURN` 的 `LIST` 直接不回，只能等超时（`docs/mail.md`「注意事项」）。
- 飞书文档 / 表格 / 多维表格里 @我、评论我，不进 IM 会话列表（没有「云文档助手」会话），只在 lark-connector 的文档通知中心
  `documents/notifications`，而且卡片只说「谁提及了你」，不说哪一行：要自己读表按「@名字」找（`docs/inbox.md`「云文档」）。
  表格里带下拉的格子 `sheet/edit` 一律报 `DOCUMENT_EDIT_UNSUPPORTED`，改值要用 `sheet/structure` 的 `choose`；多维表格 `base/edit` 的
  `expect` 指纹要按当前值算（`BaseCell.Fingerprint`），人员字段的 id 格式和 `accounts` 给的 userId 不一样（写不进去，读得出）。
  测试只拿自己建的 demo 文档（链接放环境变量），别动用户给的真表。

## 飞书开放平台机器人（`docs/bots.md`）

- 卡片 JSON 2.0 **多一个不认识的字段整张卡片被拒**（1.0 是忽略）：2.0 没有 `action` 模块和 `note`（按钮横排用 `column_set` + `flex_mode: flow`，
  小字用 `div` + `text_size: notation`），按钮的回传要写在 `behaviors` 里；卡片回调里返回的新卡片也必须是 2.0，否则报 200830。写之前对着 2.0 的组件文档逐个字段核对。
- 1.0 的 Markdown 不支持标题、表格、引用，模型的回答直接塞进去会显示成原始符号；要用 2.0。卡片会把 `<at>`、`<font>` 这类当成自己的标签：正文里的 `<` 先转义。
- 卡片的链接按钮文档只写了 http(s) 和 AppLink，自定义 scheme（`jarvis://`）不要放，免得整张卡片被拒。
- 长连接的事件要在 3 秒内回执，不然飞书重发同一事件：处理放后台，按 message_id 去重。
- 一条消息发给正在忙的对话会排在当前这一轮后面：那一轮的结束不是它的回答，要在 transcript 里找到这条消息再取它后面的回答（`botRun.Probe`）。
