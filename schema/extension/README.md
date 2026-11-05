# 扩展的 schema

扩展的清单、进程协议和声明式 UI（View 和它的组件）以这里的 JSON Schema 为准（#1849，总 issue #1843 的阶段 3）。
四端的类型都从这里生成，不再各写一份：

| 文件 | 内容 |
| --- | --- |
| `manifest.schema.json` | 清单 `persona-extension.json`（改名前叫 `persona-plugin.json`，字段一样）：根、`engines`、`runtime`、`secrets`、`permissions`、`contributes` 和每个扩展点的贡献 |
| `protocol.schema.json` | 进程协议：一行一帧（`Frame`、`RPCError`）、错误码、`initialize` 的参数和回答、`ui.render` / `ui.action` 的参数 `UIParams`、角标事件，以及 App 画一个插槽时发给 hub 的 `UIContext` / `UIRequest` 和 hub 列出的 `UIPlace` |
| `view.schema.json` | View、节、组件（`Component`，按 `type` 区分）、动作、`ui.action` 的回答，和助手回答里的 `persona-card`（`PersonaCard`，#1850 并进组件） |
| `examples/` | 例子：`<定义名>.<名字>.json` 必须合 schema；`examples/invalid/` 下的必须不合 |

## 生成

```sh
python3 scripts/gen/extension_schema.py           # 改了 schema 后重新生成
python3 scripts/gen/extension_schema.py --check   # 只比对，不一致退出 1（测试和 CI 用）
python3 scripts/test/test_extension_schema.py     # --check + 例子合不合 schema
(cd sdk/go && go test ./...)                      # Go 类型把每个例子读进来再写回去，一字不差
```

| 语言 | 文件 | 样子 |
| --- | --- | --- |
| Go | `sdk/go/wire/wire_gen.go`（独立 module `github.com/nwverse/persona/sdk/go`，不依赖任何东西） | 结构体；联合类型是**一个平铺的结构体**（各变体的字段合在一起，都可选，`Type` 说是哪种），和 hub 一直用的 `UIElem` 一样；枚举是字符串常量和一个列表（`Tones`）；`ComponentSince` 是每种组件从哪个 UI 版本开始有 |
| TypeScript | `sdk/ts/src/wire.ts`，网页一份同样的 `web/src/lib/extension-wire.ts` | interface；联合类型是各变体的联合（`Component = TextComponent \| …`）；枚举是字面量联合再加 `string & {}`（新 hub 发来的值照样收） |
| Swift | `apple/Shared/Generated/ExtensionWire.swift` | 类型名加 `Extension` 前缀（`ExtensionView`），`nonisolated`、`Codable`、`Hashable`、`Sendable`；缺字段、字段类型不对都不失败（取零值或 nil）；联合类型是 enum，不认识的或读不出来的组件是 `.unknown(type:)`；任意 JSON 用 `JSONValue` |
| Kotlin | `android/core/contracts/src/main/kotlin/persona/core/contracts/generated/ExtensionWire.kt` | kotlinx.serialization 的 data class，同样加 `Extension` 前缀，字段都有默认值；联合类型是 sealed interface + 读 `type` 的 serializer，不认识或读不出来的是 `Unknown(type, raw)`，写回去时原样写。和 Swift 不同，组件以外的对象有字段类型不对时整个读不出来（hub 先按 schema 查过 View，App 拿到的都合规） |

生成器还把三份 schema 原样拷到 `sdk/go/wire/schema/`（`go:embed` 读不到 module 外面的文件）：`wire.Validate(<定义名>, 值)` 按它们查，
hub 查扩展画的 View（`extension.CheckView`）、一致性测试查 `initialize` 的回答都用它，和 `scripts/test/test_extension_schema.py` 的校验器认同一个子集。
共同的 View 语义检查在 `sdk/go/wire/viewcheck.go`，扩展和助手均调用，避免复制图表、表格和字段检查。

## 助手卡片（#2018）

仍用 View 和 Component，新增 `View.fallback`／`pages`、`Action.reply`／`open`／`pop`、`Push.page` 和 `html` 组件。
`AssistantView` 是有归属和版本的活卡片，`ViewAction` 是持久化提交记录（pending／sent／failed）；详细规格见 [助手富 UI](../../docs/assistant-views.md)。

- `wire.ParseAssistantView` 校验原始 JSON，支持带 fallback 的单组件简写，限制 64 KB 和 JSON 深度；`wire.CheckAssistantView` 再检查共同组件语义及助手的动作／页面规则。
- 根 fallback 必填；最多 8 个命名页，详情页不能嵌套 pages。所有动作 id 在卡片内唯一，push.page 引用根 pages；助手表单禁止 secret。
- `wire.StripAssistantFields` 供扩展的 Fit 调用：拿掉助手字段、动作和 html，扩展的既有 id／push.ui／state 行为不变。
- html 的 `x-since: 4` 用于老渲染器识别，本阶段**不提升扩展的 ui_version（仍为 3）**，也不启用执行；各端后续按能力和安全验收接入。
- `examples/assistant/*.json` 是四端截图和往返测试共用的合成数据，含决策、比较、多页、进度更新、表单、图表与 HTML。schema 的合法性不代表 HTML 已经通过执行安全验收。

生成的文件头写着「do not edit」，别手改：改 schema，再生成。Go 的输出过一遍 `gofmt`（生成器要求 PATH 上有它）。

## 子集

生成器只认下面这些写法，别的一律报错退出，不去猜：

- 每个 schema 文件只有 `$defs`（清单的根另有一个 `$ref` 指向 `Manifest`，给校验用）；**定义名全局唯一**，跨文件引用写 `<文件>.json#/$defs/<名字>`。
- 定义是下面之一：
  - **结构体**：`"type": "object"` + `properties` + `required`。已知字段保持强类型；明确写了 `additionalProperties: true` 或子 schema 的对象另保留额外键（见下面「自定义贡献」），未写或写了 `false` 的保持原来的生成方式。
  - **字符串枚举**：`"type": "string"` + `enum`。各语言里字段仍是字符串（不认识的值能读进来），另给常量和列表。
  - **具名标量**：`"type": "string"`（或 integer / number / boolean）不带 `enum`，比如 `ID`、`Host`：引用它的地方直接是那个标量，`pattern` 等只用来校验。
  - **按字段区分的联合**：`oneOf` 全是 `$ref`，加 `"x-discriminator": "type"`；每个变体是结构体，必有这个字段且是 `const` 字符串，各不相同。
  - **任意 JSON**：`oneOf` 加 `"x-untagged": true`（比如 `runtime.command` 是字符串或对象）：生成成任意 JSON（Go `json.RawMessage`、TS `unknown`、Swift `JSONValue`、Kotlin `JsonElement`）。
- 字段的类型：`string`（`format: date-time` 是时间：Go `time.Time`，其余语言字符串）、`integer`（`"x-go-type": "int64"` 是 64 位：Go `int64`、Swift `Int64`、Kotlin `Long`；手表上 `Int` 是 32 位，构建号放不下）、`number`、`boolean`、
  `array` + `items`、**没有 `properties` 的** `object` + `additionalProperties`（映射，值的类型是 `additionalProperties`）、`$ref`、什么都不写（任意 JSON）。
  对象字段要有 `properties` 的，先做成一个定义再引用。
- 可选字段：不在 `required` 里。Go 加 `omitempty`（时间加 `omitzero`），对象是指针；数字的「没有」和 0 意思不同时加 `"x-go-pointer": true`（`progress` 的 `ratio`）。
- 组件加 `"x-since": <UI 版本>`：生成 `ComponentSince` 一类的表，hub 按它给老 App 拿掉新组件（`Fit`）。
- 同一个联合里，几个变体的同名字段在 Go 的平铺结构体里合成一个，所以类型必须一样（枚举都算字符串）；不一样生成器会报错，给新字段换个名字。
- 校验用的关键字（`pattern`、`minLength`、`maxLength`、`minimum`、`maximum`、`minItems`、`maxItems`、`minProperties`、`const`）不影响生成，`scripts/test/test_extension_schema.py` 里的校验器认得它们。
  schema 说不清的规则（一页最多 300 个组件、图表各系列标签一致、字段名不重复、`state` 最多 16 KB）仍是 hub 手写的检查。

## 自定义贡献（#2467）

`Contributes` 明确允许 `additionalProperties`，所以内置的 `ui`、`providers` 等保持强类型，自定义 `<扩展 id>.<扩展点 id>` 键不会在官方类型的 JSON 往返中丢掉。
生成器对其他明确开放的对象也用同一套处理，不单独判断名字是不是 `Contributes`。

- Go 用 `AdditionalProperties map[string]json.RawMessage`，Swift 用 `additionalProperties: [String: JSONValue]`，Kotlin 用 `additionalProperties: Map<String, JsonElement>`。
  这些只是代码里的存储位置，编码时额外键仍放在**原对象的同一层**，不会多包一层 `additionalProperties`。
- TypeScript 保留已知属性的具体类型，再加 `[key: string]: unknown`。动态值需要调用方按自己的扩展点 schema 读取，不能把它当内置贡献。
- 已知字段始终优先：即便手动往额外映射里写了 `ui`、`providers` 等保留键，也不能覆盖强类型字段或重新引入已省略的字段。
  `UI` 不等于 `ui`；额外键的大小写、下划线、点号及嵌套的 `config`／`state`／`plugin`／`extension` 等键和值都原样保留。
  Swift 的开放对象按字典编码，不受调用方的 snake_case 转换策略影响。
- 这不是授权或安装时的豁免。自定义扩展点仍由 hub 按 `exports`、依赖、贡献 id 和导出的 schema 校验；`permissions` 等封闭对象没有被开放。
- `examples/Manifest.custom-contributions.json` 是四语言共用的往返 fixture：内置 UI、多个自定义贡献点、嵌套 JSON、大小写近似键、字面 `additionalProperties`、`__proto__` 和 `constructor`。
  后几个键专门测试 JSON 透传，**不表示它们能作为有效扩展点安装**。Go／TS SDK 的 manifest、initialize 协议测试也读取这份 fixture。

修改生成器后跑 `python3 scripts/test/test_extension_schema.py`、`python3 scripts/test/test_sdk_bundle.py`、Go SDK 测试、TS SDK 的 `bun test`，并编译 Kotlin contracts 和 Swift 对应测试。
`extension_schema.py` 还会确定性重建 `sdk/go/bundle/sdk.tar.gz`；生成类型和 bundle 一起提交，避免新建扩展拿到旧类型。

## 加一个组件

1. 在 `view.schema.json` 写一个定义 `XxxComponent`：`"type": "object"`、`"x-since"`（新的 UI 版本）、`properties` 里 `"type": {"const": "xxx"}` 和它的字段、`required`。
   字段名和别的组件的同名字段类型要一样（Go 是平铺的）。
2. 把它加进 `Component` 的 `oneOf`。
3. 在 `examples/` 的某个 `View.*.json` 里放一个它的例子（测试查每种组件都有例子），能写错的规则加一个 `examples/invalid/` 的反例。
4. `python3 scripts/gen/extension_schema.py`，四端的类型就有了；再在各端的渲染器里画它（不认识的组件各端本来就跳过）。

动作（`Action`）是一个对象，以后的页面跳转（push / pop）、新的上下文（`UIContext`）、新的插槽（`Placement` 的值）都是往已有的定义里加字段或值，老的 App 不认识就忽略。
3b（#1850）就是这样加的：`markdown` / `table` / `empty` / `loading`、行的图标 / 头像 / 缩略图、`key`、View 的 `search` / `filter`、节的 `more`、`Action.push`、`ActionResult.push` / `pop`、`InvalidateEvent`（UI 版本 3，`docs/extensions.md`「UI 版本 3」）。`persona-card` 转成组件的规则和用例在 `sdk/go/wire/personacard.go`、`examples/persona-card/`。
