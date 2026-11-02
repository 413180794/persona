# 输入栏接线

当前 App 已在 `ChatWiringModule.createChatWiring` 通过 `data.messages.connectComposer` 接入 `HttpChatComposerService`，页面和 outbox 复用同一传输及就绪门禁，原 ID 重试结果写回原队列，凭据失效汇入统一撤销。

`ShellChat` 和默认 `ChatDestination` 已传入 composer、终端与清空上下文回调，并以 `ChatDraftEditor` 读写当前配对 DraftStore；已上传附件可作为消息路径文本恢复，未上传 URI／结构化附件草稿仍缺。助手首页的附件选择和光标插入尚未接入。

文件先上传到会话所在电脑，成功后把带引号的路径插入输入框，不自动发送。照片和拍照结果先压缩为 JPEG，再上传到同一电脑；发送时使用现有 `<attached_images>` 路径标记，让文字和附件一起进入现有 outbox。图片正文渲染和真实 hub 往返仍待验收。

相机使用模块自己的 FileProvider，manifest 随库合并，无须编辑 App manifest。照片走系统 Photo Picker，文件走 SAF；选择器或上传失败会保留输入并提供重试。

拒发详情只保留在当前配对实例内。重启后若无法恢复 `clearable`，页面要求先到终端处理，不猜测电脑草稿，不展示清空按钮。中断请求结果不明时停用重复中断，直到会话离开工作状态。

静态接线已完成；深浅色、相机授权、拍照恢复和真实 hub 往返仍待验收，性能未实测。
