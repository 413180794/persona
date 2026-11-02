# Android 手机工具

本模块对应 `docs/redesign/android.md` 5.12、`parity.md` 第 21 节和设置中的「手机数据」。提供通讯录搜索、日历查询与新建、当前位置、照片元数据与缩放取图、剪贴板、设备信息、本地通知及 Jarvis 当前通知列表，共 11 个工具。

Android 没有统一的系统提醒事项接口，故不注册 `phone_reminders_*`。不注册健康工具，不申请通知监听权限，不读取其他 App 的通知。`phone_notifications_list` 是 Android 新增工具，只返回本应用当前显示的通知，不包含操作确认通知。

## 当前 App 接线

App 已依赖 `:platform:phonetools`；`AppServices` 按配对及同一 device_id 创建 runtime／transport，将原始 tool_call 回调挂到唯一活动 socket，并转发应用前后台和常驻服务实际状态。`Root` 已注册手机工具页和全局确认浮层，配对结束撤下处理器并关闭 runtime／transport；旧配对缺 device_id 时不供给工具，需重新配对。

工具开关默认关闭并按配对保存，权限变化及执行条件失效会刷新 register_phone；真实授权、后台确认和与 iOS 同时在线的设备路由仍待验收。

## 行为与边界

工具执行前检查开关、权限、在线状态和前台限制；确认后再次检查。日历新建在前台展示完整参数摘要，在后台通过高优先级通知请求确认；允许按钮要求解锁，通知不可用、拒绝、超时或断开连接都不会继续等待授权。通知中的令牌只在本次待确认期间有效，进程重启后失效。

日历优先使用指定的可写日历，其次默认可写日历；完全没有可写日历时创建 Jarvis 本地日历。指定名称找不到时直接报错，防止误写到其他日历。全天日程以 UTC 日期保存，结束日期不包含在内。本地日历在国产系统日历 App 中的展示尚待真机验证。

照片只读取系统允许的集合，支持 Android 14 部分授权；视频取图返回预览。没有申请媒体位置权限，因此照片元数据不含经纬度。联系人查询和日历返回量均有输入上限，照片缩放后以 MCP image 块返回。

定位使用平台 fused、GPS 和网络提供者，不依赖 Google 服务；GPS 标明 WGS-84，厂商定位标明坐标系待核对，不自动转换 GCJ-02。地址解析不可用时只返回坐标。旧版系统不调用阻塞的 Geocoder 接口。

延迟通知交给 WorkManager，受系统调度限制，不保证准点。已经创建的延迟通知属于用户已安排的本地操作；关闭工具或解除配对不会取消它们。通知投递时重新检查通知开关和渠道。

同一 runtime 内保留最近调用结果，重复 `call_id` 不重复写入；进程退出后不自动重放调用。插入操作发生后如果连接断开，服务端可能收不到结果，应先核对日历再重新调用。

权限行为参照 [Android 部分照片访问](https://developer.android.com/about/versions/14/changes/partial-photo-video-access)，通知确认参照 [Notification.Action.Builder](https://developer.android.com/reference/android/app/Notification.Action.Builder)。本模块运行性能、系统授权弹窗和国产系统后台行为均未实测。

## 验证

关键单元测试覆盖开关、权限、前后台、schema 校验，以及日历拒绝、批准、重复调用和批准后状态变化。静态接线完成，仍需验证真实 WebSocket 往返、授权弹窗、后台通知解锁确认、深浅色页面及国产系统日历显示。

本车道已通过 `:platform:phonetools:assembleDebug`、`:platform:phonetools:testDebugUnitTest`（2 个测试）和 `:core:hub:compileKotlin`。构建使用 Android Studio 自带的 JDK 21，并指定 `-Pkotlin.compiler.execution.strategy=in-process --no-watch-fs`，避免沙箱外 Kotlin daemon 目录及文件监听限制。未改仓库工具链配置。
