# 经验：产品改名与兼容身份

2026-11-03 的 Jarvis → Persona 迁移经验。入口和保留清单见 [兼容迁移](../rename.md)；本页不替代各车道的实际构建、真机升级或发布验收记录。

## 先冻结提交，再区分三种名字

- **显示名称**：Persona 自己生成的品牌标题、按钮、默认文案，可以按产品要求更新。
- **源码名称**：目录、文件、纯辅助类型、Go module、Xcode target、私有 npm 包名，可以迁移，但所有真实读取路径和构建引用都要一起更新。
- **系统 / wire / stored 身份**：Bundle ID、module、Siri / Control 类型、ActivityAttributes、序列化值、服务单元、缓存键和更新包 ID，不能因为含旧品牌就改。

多 worktree 共享远端 refs；每次 `git show origin/master:…` 都会重新解析一个可能已移动的基准。先记完整 SHA，再按该 SHA 读源码和清单；机械路径映射也要记录来源提交。别人新合进来的旧目录文件和 module imports 需要在最终 rebase 后重查，不能把首次审计当成最终集成结果。

历史 SHA、当时的源码行号、日志和验收数字保留。旧路径的历史引用不是新装默认入口；查当前实现时使用迁移表。文档更新不证明运行时迁移、回退或真机升级已经成功。

## target 改名不等于 module 改名

Apple 新 target / 工程使用 Persona 名称，但 `PRODUCT_MODULE_NAME` 仍保留原模块身份。检查实际通知扩展的 `NSExtensionPrincipalClass` 和编译后的 AppIntents metadata：源码能编译，不代表系统还能加载 `JarvisNotify.NotificationService` 或找到旧快捷指令类型。

`JarvisActivityAttributes` 也是 APNs push-start 的 `attributes-type`，不是随手可改的纯 Swift struct 名。hub 和 App 分别改名会导致真实推送寻址失败，模拟器里的手动启动不能证明这条链路仍然有效。Widget kind、App Group、钥匙串组和签名 profile 同样按真实契约保留。

旧 scheme alias 不能替代旧 `.xcodeproj` 入口；老脚本可能同时写死工程路径和 scheme。新工程为 `Persona.xcodeproj`，旧工程路径软链到它；新旧 schemes 都指向新 targets。`-only-testing:` 用新测试 target，`@testable import Jarvis` 则仍按旧 module 使用。`PersonaLink` 的 Swift target 与已有菜单栏 helper / 进程路径也分别核验。

改 Info.plist 文案要改 XcodeGen 的源头 `apple/project.yml` 或对应资源 catalog，不编辑生成工程或构建产物。改 catalog 时保留 source-language 条目；归档后再检查 App / 扩展的实际 purpose strings，不把看到源码中的 Persona 当成系统弹窗已更新。

## parser 也要识别旧品牌输入

来源标题或会话 header 的 parser 如果只认 `Jarvis`，新版生成 `Persona` 前缀后，旧特殊消息可能变成普通聊天正文。输入端同时识别 `Jarvis|Persona`，输出只改 Persona 自己控制的标题，测试覆盖新旧格式以及没有品牌前缀的普通正文。

用户自取的会话名、消息内容、联系人名和外部 bot 名字不是产品文案。不能用全文替换重写这些数据；对演示 fixture 也只改明确由 Persona 控制的默认品牌字段。

URL scheme 同样要区分 **注册 / 接收** 和 **生成**：迁移客户端需要同时收 `persona://` 与 `jarvis://`，生成继续用旧 `jarvis://` 兼容已安装客户端。检查 Apple 注册和 parser、Android intent-filter 和路由、Web / 管理输入，每个入口独立记录通过或未验收。

## 源码包名不改变持久化名字

Android 改到 `persona.android` 后，kotlinx.serialization 的默认完整类名也会改变。显式保留 `@SerialName`、`AppTab.Jarvis` 和已有 rawValue；旧 Activity / alias、receiver、HubService、通知 PendingIntent 和 WorkManager Worker 必须继续能被 OS 找到。只编新源码不足以证明老通知和排队任务能升级。

runtime 的私有 npm `name` 与更新包 ID 分开维护。DSH / pi 的 `jarvis-dsh-runtime` / `jarvis-pi-runtime` 同时出现在更新清单、磁盘目录和 loader 中；只改一端会把新版装成第二个插件或让旧 loader 不再更新。包名和可见文案可以迁移，更新身份与 `jarvis-runtime.v1` 线上子协议不能盲替换。

## 服务身份也决定运行目录

部署脚本恢复根目录要定向选 hub / node / link，不能把同机第一个有环境变量的服务当成所有服务的根。旧单元存在但没有根 Environment 时，继续原默认存储；相邻服务的自定义根不能抢走它。systemd 的 `Environment=` reset、排序后的 drop-in、XDG_CONFIG_HOME 和新旧默认别名的 SameFile 都要覆盖。

`EnvironmentFile` / `UnsetEnvironment` 不是可随意忽略的配置：解析器不读外部凭据，也不能猜最终环境，必须要求显式 root。运行根解析与 CLI 真正请求时取凭据是两个职责，不把前者「不读凭据」写成后者完全不需要凭据。#773 的实现和回归在 `scripts/persona_identity.py` / `scripts/test/test_persona_script_identity.py`，完整规则见 [按服务恢复运行目录](../rename.md#部署脚本按服务恢复运行目录)。

## 机械映射也要检查格式与边界

#786 在 `scripts/test/test_apple_rename.py` 中拒绝重新创建旧 Apple 源码目录和 `mac/Sources/JarvisLink`，同时保留旧工程软链；检查新增文件时不能只查类型名或当前 target。Android 的静态 guard 也要跟已合的手填共用链接判定同步，避免源码已改、检查仍断言旧文本，或宽泛品牌扫描把有意保留的旧 OS 身份误报为残留。

Token baseline 的 JSON 路径可能编码成 `\/`；普通字符串替换会漏掉。按真实源文件映射 key，同时保留原始格式、全部 metric 数值和阈值，不重新生成、更不抬高基线。

文档检查分两类：当前源码 / 命令引用必须指向真实新入口；历史快照、方案里尚未落地的模块和兼容身份不能为了让路径扫描归零而篡改。相对 Markdown 链接还需按所在文档目录解析，不能假定都相对仓库根目录。
