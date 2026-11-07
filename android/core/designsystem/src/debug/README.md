# 组件目录

对应 `docs/archive/redesign/ux.md` 3.8 和 `docs/archive/android/android-p0-status.md` 的 UX3.8-平台行。

目录入口为 `persona.core.designsystem.catalog.ComponentCatalogScreen()`，仅存在于
`:core:designsystem` 的 debug 源集。App 车道负责从调试设置页接入一个入口，并提供返回导航和系统栏边距。
本车道没有修改 `android/app/` 或业务页面。

App 应在自己的 debug 源集中引用该函数。共享源集只判断 `BuildConfig.DEBUG` 仍会让 release
编译解析目录类型，因此需要由 App 用不同构建源集提供调试入口和 release 空实现。

页面使用现有 `PersonaTheme`、token 和组件，固定关闭动态取色，便于截图比较。
提供浅色／深色切换，字号默认跟随宿主，可切换到 1、1.3、2 倍；支持按组件筛选。
按钮示例包含可用、禁用、处理中和长文案，状态行覆盖所有种类，待定徽标与状态色遍历现有枚举。
示例点击只更新本页提示，不连接 hub，不修改业务数据。

截图可通过 `initialDarkTheme`、`initialFontScale`、`initialSection` 固定初始状态。
根节点标记为 `component-catalog`，分组标记为 `catalog-<CatalogSection 枚举名>`。
源码包含深浅色和大字号窄屏的 Compose Preview；预览声明不等于已经完成截图验收。
截图时还需固定 Compose 测试时钟，因为现有按钮和待定徽标的加载指示器会自行播放动画，
`reduceMotion = true` 并不会暂停这些 Material 指示器。
