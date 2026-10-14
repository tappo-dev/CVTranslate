<div align="center">

# CVTranslate

### Windows / macOS 中英翻译小工具

连按 Ctrl+C 翻译 · Microsoft Translator / 自定义大模型 · 翻译人设

[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20macOS-2563eb?style=for-the-badge)](#快速开始)
[![License](https://img.shields.io/badge/License-Noncommercial-d97706?style=for-the-badge)](LICENSE)
[![Local](https://img.shields.io/badge/Data-Local-0f766e?style=for-the-badge)](#隐私与使用边界)
[![GitHub](https://img.shields.io/badge/GitHub-tappo--dev%2FCVTranslate-24292f?style=for-the-badge&logo=github)](https://github.com/tappo-dev/CVTranslate)

[快速开始](#快速开始) · [使用指南](docs/usage.md) · [开发与构建](#从源码运行) · [许可说明](#许可证与免责声明) · [GitHub 仓库](https://github.com/tappo-dev/CVTranslate)

**源码公开，非商业用途免费；商业使用须事先取得书面授权。**

</div>

---

## 简介

CVTranslate 是一个在后台运行的中英翻译工具，界面在本机浏览器中打开。类似的翻译工具已经很多，它主要做了两件事：用连续复制快捷键触发翻译，以及让大模型按指定人设翻译。

> [!IMPORTANT]
> **本地运行不等于离线翻译，软件免费不等于接口免费。** 翻译文本会发送给所选服务，在线朗读也可能发送文本。第三方服务的费用、额度和可用性由其提供方决定。

## 目录

- [主要功能](#主要功能)
- [快速开始](#快速开始)
- [隐私与使用边界](#隐私与使用边界)
- [从源码运行](#从源码运行)
- [反馈与参与](#反馈与参与)
- [许可证与免责声明](#许可证与免责声明)

## 主要功能

### 1. 连按复制键翻译

选中文字后（macOS 用 `Command+C`）：

- **连按 `Ctrl+C` 三次**：翻译完成后模拟 `Ctrl+V` 粘贴，选区仍在时直接用译文替换原文。
- **连按 `Ctrl+C` 四次**：打开 Web UI 并立即翻译，确认后再手动复制。

> **三次复制有误粘贴风险**：粘贴发送到翻译完成时的活动窗口，不校验原窗口、光标或选区；中途切换应用可能粘贴到错误位置。手势间隔和 macOS 权限要求见 [使用指南](docs/usage.md#5-全局复制快捷键)。

### 2. Microsoft Translator 与自定义大模型

- 默认使用 Microsoft Translator，无需 API Key。
- 可接入任意 OpenAI 兼容接口，自行填写地址、模型和密钥。
- 大模型翻译可设置人设（人物风格）和提示词模板，调整译文语气；Microsoft Translator 不使用这些设置。

人设和提示词只是模型指令，不保证译文准确或代码完全不变，重要内容请人工核对。

### 3. 其他

Markdown 双栏对照、本地历史（相同请求直接复用结果）、Edge / 浏览器朗读，以及简单的 token 用量统计。

## 快速开始

### 运行应用

取得对应系统和架构的发布包后：

1. 解压后，在 Windows 运行 `cvtranslate.exe`，在 macOS 打开 `CVTranslate.app`。
2. 应用会打开默认管理页面 `http://127.0.0.1:19327`，也可以在本机浏览器直接访问。
3. 在工作台输入或粘贴文本；停止输入约 600 毫秒或输入框失去焦点后自动翻译。
4. 复制译文，或使用两侧的朗读功能核对内容。

发布包已内嵌前端，无需安装 Node.js 或 Go。没有发布包时，可[从源码运行或构建](#从源码运行)。

### 接入自己的 AI 翻译服务

1. 添加 OpenAI 兼容服务，填写 API 基础地址和实际可用的模型，按服务要求填写 API Key。
2. 在工作台选择该服务，按需选择提示词模板。
3. 输入文本开始翻译；需要检查连通性时，主动点击连接测试。

**自动翻译和连接测试都可能产生第三方费用。** 启动与普通保存不会自动测试连接；失败请求不自动重试。工作台切换服务会保存为默认服务，也会影响后续全局快捷键。

### 后台运行与退出

关闭浏览器不会退出应用。Windows 通过通知区域图标、macOS 通过菜单栏图标重新打开页面或选择「退出」；再次运行应用也会打开已有实例。退出不会删除设置和历史记录。

## 隐私与使用边界

- **本地保存，不做云同步**：没有 CVTranslate 账号服务或遥测；API Key 放在系统凭据存储中，历史记录是本地**未加密文本**。
- **按操作发送文本**：工作台会自动把输入发送给所选翻译服务；Edge 朗读会把要朗读的文本发送给微软，浏览器语音也可能联网。请勿输入不允许外发的内容。
- **检查更新需手动触发**：只有在设置中点击「检查更新」时才访问 GitHub，不发送翻译内容或设备标识。
- **本机服务不是账号隔离**：管理端口仅监听 `127.0.0.1`，但能够访问端口的其他本机用户或进程也可能读写应用数据。不要将此端口暴露或转发到其他电脑。
- **优先使用 HTTPS**：HTTP 服务地址会明文传输文本和可能携带的密钥。
- **第三方能力不承诺永久免费**：Microsoft 消费级接口可能限流、变更或停止，使用时须遵守其服务条款；自带 AI 服务按供应商规则计费。
- **仅面向 Windows / macOS 桌面**：不提供移动端、PWA、浏览器扩展或独立原生主窗口。macOS 原生构建、权限和实际交互需要在 Mac 上单独验证，Windows 检查不能替代。
- **当前构建包未签名、公证**：可能出现系统信誉或安全提示；校验和只能用于检查文件完整性，不代表发行者身份验证。不要为运行应用关闭系统安全保护。

操作细节和限制请阅读 [使用指南](docs/usage.md)。

## 从源码运行

准备 Node.js **22.12 或以上版本**、npm，以及 [go.mod](go.mod) 声明的 Go 版本。macOS 还需启用 CGO 并安装 Apple 命令行编译工具。在仓库根目录执行：

```text
npm ci
go mod download
npm run doctor
npm run dev
```

开发页面为 `http://127.0.0.1:19328`，数据独立存放在 `.local/dev`，不与正式运行的数据混用。前端支持热更新，修改 Go 代码后需重启开发进程。

检查与构建：

```text
npm run check
npm run build
```

`build` 会先执行完整检查，`check` 也可单独运行。产物位于 `release/`，包含程序、ZIP、校验和、构建信息及第三方声明。只构建当前主机的平台和架构，不自动上传、签名、公证或覆盖已有发布目录。

仓库提供 VS Code / Cursor 共用的 F5 调试配置；依赖安装、调试器准备和排错见 [开发工作流](docs/development/workflows.md)，产物规则见 [构建与发布](docs/release.md)。

## 反馈与参与

项目原始仓库：<https://github.com/tappo-dev/CVTranslate>。请在 [GitHub Issues](https://github.com/tappo-dev/CVTranslate/issues) 提交问题，并说明系统、应用版本、复现步骤及预期结果。不要附带 API Key、数据库、完整个人历史或未经检查的日志；示例文本请先脱敏。

参与开发前，请阅读 [贡献指南](CONTRIBUTING.md) 和 [架构说明](docs/architecture.md)。扩展服务、增加功能和调整原生交互时，分别参照：

- [翻译服务接入指南](docs/development/provider-guide.md)
- [功能开发检查清单](docs/development/feature-checklist.md)
- [HTTP 接口约定](docs/development/api.md)
- [原生快捷键与粘贴边界](docs/development/native.md)
- [语音服务协议](docs/development/speech.md)

## 许可证与免责声明

Copyright (c) 2026 CVTranslate contributors

本项目采用自定义的 **[CVTranslate Noncommercial License 1.0](LICENSE)**，主要约定如下：

- **非商业免费**：允许非商业用途的使用、学习、复制、修改，以及遵守许可条件的源码或二进制分发。
- **商业使用须书面授权**：未获相关权利人事先书面授权，不得用于商业产品、付费翻译或托管服务、收费分发、广告变现，以及企业内部生产和商业工作流程。软件本身不收费，不代表用途属于非商业。
- **保留来源与版权**：分发原版、Fork 或修改版时，须保留许可全文、原版权声明和原始 GitHub 仓库链接，并在 README 或随附文档中展示来源；不能只留下自己的 Fork 地址。修改版需说明重要改动及其作者，不得冒充官方版本。
- **不强制公开修改源码**：本许可不要求公开私人修改或二进制对应源码，但不能移除原项目的非商业限制。仅保留署名并不意味着获得商业许可。
- **第三方许可独立**：依赖库仍遵守各自的许可证；本项目许可不替代翻译、模型及语音服务的条款，也不免除其费用。

这属于**源码可用的非商业许可**，不是 OSI 认可的开源许可证，也不是 MIT、GPL 或 PolyForm。具体权利和义务以 [LICENSE](LICENSE) 英文全文为准；本节仅为中文摘要。

商业授权请向原始发布仓库 [tappo-dev/CVTranslate](https://github.com/tappo-dev/CVTranslate) 的维护者提出，并由有权授予相关权利的权利人书面确认；提交请求、捐赠或未收到回复均不构成授权。

**免责声明**：CVTranslate 是独立项目，与 Microsoft、OpenAI 等服务提供方无官方隶属或背书关系。软件按「现状」提供，在适用法律允许的范围内不承诺翻译准确性、服务持续可用性或适合特定用途。请自行核对重要译文、数据外发风险和第三方服务条款。

## 致谢

Read Frog 为交互与协议提供了参考；本项目未复制其源码、品牌或内置提示词，Go 适配器为独立实现。第三方依赖保留各自许可，构建时收集对应声明，补充来源见 [第三方许可说明](docs/third-party/README.md)。
