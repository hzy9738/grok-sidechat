# 操作日志

## 2026-09-16 15:55 CST

- **动作**：Native Host 改为 Rust；公司密钥封进 `host-rs/src/sealed.bin`；扩展补齐划词浮层与右键选文/选图/链接
- **证据**：`cargo test` 通过；扩展测试待跑；安装脚本改为 `cargo build --release`
- **执行者**：Codex
- **状态**：完成；cargo test 9 通过；extension test 通过；已重装 native host

## 2026-09-15 20:30 CST

- **动作**：把 grok-sidechat 从 Grok CLI spawn 改成通用 OpenAI 兼容 Chat Completions
- **证据**：host 新增 api.go / chatstore.go；RunTurn 走 HTTP SSE；侧栏设置 apiBase/apiKey/model；会话写入 ~/.sidechat/sessions
- **状态**：go test 54 通过；extension test 通过；已重装 native host

## 2026-08-06 17:05 CST

- **动作**：对比本仓库 `grok-sidechat` 与 Chrome Web Store 扩展 `hehggadaopoacecdllhhajmbjkdcmajg`（ChatGPT/Codex Chrome 侧栏）的交互能力
- **证据**：
  - 本仓库 `extension/` side panel 为 checkbox + 纯文本气泡原型
  - 本地已装 Codex 扩展 `1.2.27236.6274_0`，manifest 含 sidePanel/debugger/nativeMessaging 等；sidepanel 含 thread/composer/@mention/permissions/model 等产品级 UI
- **结论**：用户要求交互对齐 Codex 侧栏，底层保持 Native Messaging + Go host + grok headless（不换技术栈）
- **状态**：已完成差距理解与分层，待用户确认优先级后再实现

## 2026-08-06 17:22 CST

- **动作**：完成 Codex 级侧栏交互 + native host 安装 + cwd 不强制绑定 + 浏览器控制面
- **证据**：go test 全绿；NativeMessagingHosts 已写入 com.hzy9738.grok_sidechat.json；once dry-run argv 无 --cwd；extension/test 全绿
- **扩展 ID**：gjpmflfaadhcbbcckbmbccggfpbdjdel（manifest key 固定）

## 2026-08-06 17:35 CST

- **动作**：修复 skeptic 三项：browser_* 仅 background 执行+去重+尊重开关；hostPing 断线/超时 ok:false
- **证据**：extension/test 含 host-bridge 策略与 sidepanel 无 executeBrowserTool 结构断言；go test 仍绿；native host manifest 仍有效
