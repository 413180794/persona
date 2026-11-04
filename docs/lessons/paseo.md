# 经验：Paseo 引擎（integrations/paseo）

改 Paseo 的桥（`integrations/paseo`）、node 里运行它的部分（`node/paseobridge.go`），或者对着真的 Paseo 测试之前读。设计见 `docs/paseo.md`。

- **只连 home 里 `paseo.pid` 写的地址，不要回退到默认端口**（2026-11-04 踩过）。Paseo 一启动就写 `paseo.pid`，这时 `listen` 还是 `null`，
  开始监听后才填上地址。当时的写法在没有地址时退回 `PASEO_LISTEN` / `127.0.0.1:6767`，一个本该连临时 daemon 的原型就连到了本机正在用的 Paseo，
  还发出了一个 `agent.create`。那次因为 provider 不存在失败了，只在 `creations/` 里留下一条 failed 记录。
  - 现在桥和 node 的检测都只认 `paseo.pid` 里的地址，和 Paseo 自己的 CLI 一样。
  - 对着临时 daemon 做测试时，先确认 `daemonTarget(home)` 的端口就是你给它的端口，再发请求。
- **Claude Code 交给 Paseo 的 `ExitPlanMode` 权限请求里没有计划正文**（2.1.294 实测）：input 是 `{}`，`metadata.planText` 也没有。
  正文在 timeline 的那次工具调用里（`detail.type: "plan"`），对话记录里的 `tool_use.input.plan` 也有。桥按 `metadata.toolUseId` 去 timeline 里找。
- **Agent 快照里的待处理请求会被裁剪**：Paseo 的 `sanitizePendingPermissions` 去掉空的 input。事件 `agent_permission_request` 里的请求更完整，
  但两者可能谁先到都有，所以桥不能指望 input 一定在。
- **Claude Code 遇到只读的 Bash 命令不问权限**（`echo`、`ls` 这类）。端到端测试要拿到一张授权卡，就得用会写东西的命令（`touch …`）。
- **Paseo 0.14 刚建出来的 Claude Agent，`runtimeInfo.sessionId` 是 `null`**，Claude 会话 id 要等第一轮开始以后才有。
  所以从 Persona 新建的会话用 `paseo-<agent id>`，并把这个 id 记在标签 `persona.session` 上。不能等 Claude 的 id，否则会话 id 会中途改变。
- **provider 的模型要等 Paseo 预热**：`get_providers_snapshot` 不等预热，先回 `loading`，预热完成后（约 1 秒）推一条 `providers_snapshot_update`。
  第一次 hello 里的启动器可能还没有模型，所以桥在有 provider 处于 loading 时会再问几次。
- **发消息的回复要等这一轮真的开始才回来**，Paseo 最多等 60 秒；而 node 等 runtime 的调用只有 30 秒。载入一个 closed 的 Agent 可能更久，
  所以桥最多等 25 秒，超时就当已经交给 Paseo。新建也是这样处理。
- **订阅的事件可能和订阅的回复在同一次读里到**：`fetch_agents` 回复之后紧跟着目录更新。登记订阅要在处理回复时同步做，不能等 `await` 返回再做，
  否则这几条事件会丢。另外，订阅的回复本身也带 `subscriptionId`，所以路由要先按 `requestId` 认回复，再按 `subscriptionId` 认事件。
- **读一个 closed 的 Agent 的 timeline 会把它载入**：Paseo 会从持久化里恢复它。所以给列表取「最后一句」时跳过 closed 的 Agent；
  只有手机打开这个会话时才去读。
