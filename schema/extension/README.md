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
| Swift | `apple/Shared/Generated/ExtensionWire.swift` | 类型名加 `Extension` 前缀（`ExtensionView`），`nonisolated`、`Codable`、`Sendable`；缺字段、字段类型不对都不失败（取零值或 nil）；联合类型是 enum，不认识的或读不出来的组件是 `.unknown(type:)`；任意 JSON 用 `JSONValue` |
| Kotlin | `android/core/contracts/src/main/kotlin/persona/core/contracts/generated/ExtensionWire.kt` | kotlinx.serialization 的 data class，同样加 `Extension` 前缀，字段都有默认值；联合类型是 sealed interface + 读 `type` 的 serializer，不认识或读不出来的是 `Unknown(type, raw)`，写回去时原样写。和 Swift 不同，组件以外的对象有字段类型不对时整个读不出来（hub 先按 schema 查过 View，App 拿到的都合规） |

生成的文件头写着「do not edit」，别手改：改 schema，再生成。Go 的输出过一遍 `gofmt`（生成器要求 PATH 上有它）。

## 子集

生成器只认下面这些写法，别的一律报错退出，不去猜：

- 每个 schema 文件只有 `$defs`（清单的根另有一个 `$ref` 指向 `Manifest`，给校验用）；**定义名全局唯一**，跨文件引用写 `<文件>.json#/$defs/<名字>`。
- 定义是下面之一：
  - **结构体**：`"type": "object"` + `properties` + `required`。`additionalProperties: false` 只影响校验（清单是严格读的）；生成的类型只有声明了的字段。
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

## 加一个组件

1. 在 `view.schema.json` 写一个定义 `XxxComponent`：`"type": "object"`、`"x-since"`（新的 UI 版本）、`properties` 里 `"type": {"const": "xxx"}` 和它的字段、`required`。
   字段名和别的组件的同名字段类型要一样（Go 是平铺的）。
2. 把它加进 `Component` 的 `oneOf`。
3. 在 `examples/` 的某个 `View.*.json` 里放一个它的例子（测试查每种组件都有例子），能写错的规则加一个 `examples/invalid/` 的反例。
4. `python3 scripts/gen/extension_schema.py`，四端的类型就有了；再在各端的渲染器里画它（不认识的组件各端本来就跳过）。

动作（`Action`）是一个对象，以后的页面跳转（push / pop）、新的上下文（`UIContext`）、新的插槽（`Placement` 的值）都是往已有的定义里加字段或值，老的 App 不认识就忽略。
3b（#1850）的新组件（markdown、列表、表格、空状态、加载中、行图标 / 头像 / 缩略图）、`persona-card` 并进组件、`ui.invalidate` 照上面的步骤加。
