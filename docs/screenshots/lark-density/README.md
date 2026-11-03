# 消息列表密度

iPhone 18 Pro / iOS 27.0，合成 mock 会话，标准字号。AXe 读取实际行 frame；新旧布局下三条普通会话修改后均为 64 pt，修改前为 92 pt。

只调整「消息」页的会话行：头像改用已有 48 pt token，List 行上下 inset 设为 0，保留内容上下各 8 pt。动态字体不固定行高，系统 accessibility-large 下实际为 95.67 pt，标题和预览保持原有单行截断。收件箱视图及输入框未改变。

| 场景 | 截图 |
| --- | --- |
| 修改前，92 pt | [修改前](before.png) |
| 新布局，浅色玻璃，64 pt | [新布局浅色](new-light.png) |
| 新布局，深色 BeautifulUI，64 pt | [新布局深色](new-dark.png) |
| 旧布局，浅色玻璃，64 pt | [旧布局浅色](old-light.png) |
| 旧布局，深色 BeautifulUI，64 pt | [旧布局深色](old-dark.png) |
| 辅助字号，95.67 pt | [辅助字号](large-type.png) |
