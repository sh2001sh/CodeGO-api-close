# Code Go Desktop 发布通道部署手册

本文档对应当前仓库里的两段实现：

- `codeboard/cc-switch-main/.github/workflows/release.yml`
- `new-api/controller/desktop_release.go`

目标是把 `Code Go Desktop` 的构建产物、网站下载页和桌面端自动更新通道接成同一条 self-hosted release channel，而不是继续依赖 GitHub Releases 作为最终用户入口。

## 1. 产物来源

打 tag 触发 `codeboard/cc-switch-main` 的 release workflow 后，会在 `publish-release` 阶段生成并上传两份关键元数据：

- `codego-desktop-release-manifest.json`
- `latest.json`

同时会上传平台安装包与 updater 产物，当前命名约定如下：

- Windows:
  - `CodeGo_<version>_x64_en-US.msi`
  - `CodeGo_<version>_arm64_en-US.msi`
  - `CodeGo_<version>_x64_portable.zip`
  - `CodeGo_<version>_arm64_portable.zip`
- macOS:
  - `CodeGo_<version>_universal.dmg`
  - `CodeGo_<version>_universal.zip`
  - `CodeGo_<version>_universal.app.tar.gz`
- Linux:
  - `CodeGo_<version>_x64.AppImage`
  - `CodeGo_<version>_arm64.AppImage`
  - `CodeGo_<version>_x64.deb`
  - `CodeGo_<version>_arm64.deb`
  - `CodeGo_<version>_x64.rpm`
  - `CodeGo_<version>_arm64.rpm`

其中：

- 网站下载页消费 `codego-desktop-release-manifest.json`
- Tauri updater 消费 `latest.json`
- `CodeGo_<version>_universal.app.tar.gz` 和带 `.sig` 的 updater 安装包仅用于自动更新链路，不是手动下载安装入口

## 2. 部署目录约定

当前代码假设 `codegoai.com` 可直接提供以下静态路径：

- 下载页：`https://codegoai.com/download`
- 安装包目录：`https://codegoai.com/downloads/codego/`
- updater API：
  - `https://codegoai.com/api/desktop/release/latest`
  - `https://codegoai.com/api/desktop/release/latest.json`

推荐把桌面端安装包和 updater 产物统一上传到：

```text
/downloads/codego/
```

这样 workflow 生成的 manifest 不需要二次改写 URL。

## 3. new-api 配置方法

`new-api` 提供两种加载方式：

1. `CODEGO_DESKTOP_RELEASE_MANIFEST_FILE`
2. `CODEGO_DESKTOP_RELEASE_MANIFEST_JSON`

若两者同时存在，代码优先读取 `CODEGO_DESKTOP_RELEASE_MANIFEST_JSON`。

生产环境推荐使用文件方式：

```dotenv
CODEGO_DESKTOP_RELEASE_MANIFEST_FILE=/opt/codego/releases/codego-desktop-release-manifest.json
```

不建议把完整 manifest 长期内嵌到环境变量里，原因很直接：

- 版本升级时 diff 不直观
- JSON 转义更容易出错
- 回滚时不如直接替换文件稳定

## 4. 最小部署流程

1. 在 `codeboard/cc-switch-main` 打正式 tag，例如 `v3.16.4`
2. 等 GitHub Actions 完成 release workflow
3. 下载本次 release 中的：
   - `codego-desktop-release-manifest.json`
   - `latest.json`
   - 所有 `CodeGo_*` 安装包
   - 所有对应 `.sig`
4. 将安装包与 `.sig` 上传到 `https://codegoai.com/downloads/codego/` 对应的静态目录
5. 将 `codego-desktop-release-manifest.json` 放到 `new-api` 运行节点可读取的位置
6. 设置或更新：

```dotenv
CODEGO_DESKTOP_RELEASE_MANIFEST_FILE=/opt/codego/releases/codego-desktop-release-manifest.json
```

7. 如果只是替换同一路径下的 manifest 文件，当前实现会在下一次请求时重新读取文件，通常不需要重启 `new-api`
8. 如果修改的是环境变量本身，或你的部署环境会把 manifest 内容再缓存到进程外层，再执行 `new-api` reload / restart
9. 执行 smoke test

## 4.1 GitHub Actions 自动部署（可选）

当前 `codeboard/cc-switch-main/.github/workflows/release.yml` 已支持一个可选的远程部署步骤。

如果在 GitHub Actions 里配置了以下 secrets，`publish-release` 阶段会在生成 release 资产后，自动把 deploy bundle 推到远端并激活：

- `CODEGO_RELEASE_DEPLOY_HOST`
- `CODEGO_RELEASE_DEPLOY_USER`
- `CODEGO_RELEASE_DEPLOY_SSH_KEY`
- `CODEGO_RELEASE_STATIC_DIR`
- `CODEGO_RELEASE_MANIFEST_FILE`

可选 secrets：

- `CODEGO_RELEASE_DEPLOY_PORT`：默认 `22`
- `CODEGO_RELEASE_METADATA_DIR`：默认复用 `CODEGO_RELEASE_STATIC_DIR`
- `CODEGO_RELEASE_REMOTE_TMP_DIR`：默认 `/tmp/codego-release-<tag>`
- `CODEGO_RELEASE_PUBLIC_BASE_URL`：用于部署后 smoke test，默认 `https://codegoai.com`
- `CODEGO_RELEASE_POST_DEPLOY_RELOAD_COMMAND`：用于部署后在目标机器执行 reload / restart 命令，例如 `systemctl restart new-api`

自动部署行为：

1. workflow 先生成 `release-assets/deploy-bundle/`
2. workflow 本地执行 `scripts/verify-codego-release-bundle.mjs`，校验：
   - release manifest / `latest.json` 版本一致
   - updater 平台目标都能映射到真实构建产物与 `.sig`
   - manifest 里的 SHA256 和实际文件一致
   - deploy bundle 中 `static/`、`metadata/`、`runtime/` 三份内容一致
2. 将所有 `CodeGo_*` 安装包与 `.sig` 同步到 `CODEGO_RELEASE_STATIC_DIR`
3. 将 `codego-desktop-release-manifest.json` 写到 `CODEGO_RELEASE_MANIFEST_FILE`
4. 将 `latest.json` 与 `codego-desktop-release-manifest.json` 额外写入 `CODEGO_RELEASE_METADATA_DIR`
5. 如果配置了 `CODEGO_RELEASE_POST_DEPLOY_RELOAD_COMMAND`，在远端执行 reload / restart
6. 对外运行 smoke test，检查：
   - `/api/desktop/release/latest`
   - `/api/desktop/release/latest.json`
   - `.msi` / `.dmg` / `.app.tar.gz` 资产可达
   - 如果 CDN / 反向代理存在短暂传播延迟，workflow 会轮询等待版本切换完成，而不是只做单次命中
7. 保留一个名为 `codego-release-deploy-bundle-<tag>` 的 workflow artifact，便于人工回放部署
8. 额外生成一个名为 `codego-release-acceptance-template-<tag>` 的 workflow artifact，里面包含：
   - `codego-release-acceptance-record.json`
   - `codego-release-acceptance-checklist.md`

这个 acceptance artifact 不参与线上发布，但用于记录本次版本的跨平台安装 / 升级 / 回滚验收结果。

如果上述必需 secrets 缺失，workflow 不会失败，只会跳过远程部署，同时继续发布 GitHub Release 与 deploy bundle artifact。

## 5. Smoke Test

### 5.1 后端接口

确认网站下载页消费接口：

```powershell
Invoke-WebRequest "https://codegoai.com/api/desktop/release/latest" | Select-Object -ExpandProperty Content
```

确认 updater manifest：

```powershell
Invoke-WebRequest "https://codegoai.com/api/desktop/release/latest.json" | Select-Object -ExpandProperty Content
```

至少检查：

- `version` / `tag_name` 是否正确
- Windows x64 URL 是否指向 `.msi`
- macOS updater URL 是否指向 `.app.tar.gz`
- `darwin-aarch64` 和 `darwin-x86_64` 是否同时存在
- URL 是否都落在 `https://codegoai.com/downloads/codego/`

也可以直接使用仓库脚本做半自动验收：

```bash
node scripts/wait-for-codego-release-version.mjs \
  --release-url "https://codegoai.com/api/desktop/release/latest" \
  --latest-url "https://codegoai.com/api/desktop/release/latest.json" \
  --expected-version "3.16.4" \
  --timeout-ms "180000" \
  --interval-ms "5000"
```

这个脚本会持续轮询，直到：

- release manifest 与 `latest.json` 都切到目标版本
- 必需平台 target 存在
- 必需安装 / updater 资产可达

如果一直未切换到目标版本，会带着最后一次错误退出，适合部署后和回滚后的半自动验收。

### 5.2 静态资源

随机抽查几个实际下载地址：

```powershell
Invoke-WebRequest "https://codegoai.com/downloads/codego/CodeGo_3.16.4_x64_en-US.msi" -Method Head
Invoke-WebRequest "https://codegoai.com/downloads/codego/CodeGo_3.16.4_universal.dmg" -Method Head
Invoke-WebRequest "https://codegoai.com/downloads/codego/CodeGo_3.16.4_universal.app.tar.gz" -Method Head
```

### 5.3 网站下载页

打开：

```text
https://codegoai.com/download
```

确认：

- Windows 推荐卡片落到 `.msi`
- macOS 推荐卡片落到 `.dmg`
- Linux 推荐卡片落到 `.AppImage`
- 页面展示的文件名、大小、摘要来自 manifest

### 5.4 桌面端自动更新

在已安装旧版本桌面端的机器上验证：

- `Check for updates` 能拿到 `latest.json`
- macOS 能识别 `darwin-aarch64` / `darwin-x86_64`
- Windows 能识别 `windows-x86_64` / `windows-aarch64`
- 下载后安装流程不报签名错误

### 5.5 结构化验收记录

对每个正式版 tag，建议把 workflow 产出的 `codego-release-acceptance-template-<tag>` 下载下来，按真实机器或虚拟机结果填写。

模板里已经按当前 release metadata 生成了这些平台：

- Windows x64
- Windows ARM64
- macOS Universal
- Linux x64
- Linux ARM64

每个平台都要求记录四类场景：

- `fresh-install`
- `upgrade-from-previous`
- `rollback-to-previous`
- `updater-check`

填写完成后，用仓库脚本做一次关闭前校验：

```bash
node scripts/verify-codego-release-acceptance-record.mjs \
  --manifest "release-assets/codego-desktop-release-manifest.json" \
  --latest "release-assets/latest.json" \
  --record "release-assets/acceptance/codego-release-acceptance-record.json" \
  --require-executed \
  --require-passed
```

这个校验会确认：

- 验收记录仍然对应当前 release manifest / `latest.json`
- 记录里的平台、安装包 URL、SHA256、updater target 没有漂移
- 所有必填场景都不再是 `pending`
- 所有必填场景都已标成 `pass`

建议把填好的 JSON 和 Markdown checklist 连同截图、安装日志、签名验证输出一起归档到本次发布证据里。这样后续回看某个版本时，可以直接知道：

- 哪个平台做过 fresh install
- 用哪台机器或哪种镜像做过 upgrade / rollback
- updater 检查是在什么环境下通过的
- 哪个版本作为 previous stable baseline

## 6. 回滚方法

如果本次发布存在问题，优先回滚 manifest，而不是先删安装包：

1. 将 `CODEGO_DESKTOP_RELEASE_MANIFEST_FILE` 指向的文件内容替换为上一版 manifest，或把该路径切回上一版 manifest 文件
2. 如果路径未变，当前实现会在下一次请求时重新读取 manifest，通常不需要重启 `new-api`
3. 如果变更的是环境变量路径，或外层部署对文件做了额外缓存，再执行 reload / restart
4. 使用同一个轮询脚本确认 `/api/desktop/release/latest` 与 `/latest.json` 已恢复上一版
5. 必要时再移除错误的静态安装包

这样做的好处是：

- 网站下载页会立即回到上一版
- 桌面端 updater 会停止向坏版本升级
- 静态文件即使短时间还在，也不会继续被主入口引用

## 7. 当前限制

截至当前仓库状态，下面这些仍未完全闭环：

- 远程部署依赖 GitHub Actions secrets 和目标机器 SSH 写入权限，默认不开启
- reload 仍依赖你提供正确的远端命令，workflow 本身不理解你的 systemd / docker compose 编排
- 多平台安装验证矩阵仍需人工执行，但当前 workflow 已会为每个 tag 生成结构化验收模板与校验脚本

因此，这条发布链已经具备“构建产物 -> 本地 bundle 校验 -> deploy bundle -> 远端静态目录 / manifest 文件 -> 可选 reload -> 对外 smoke test -> 网站/桌面消费”的代码闭环，但仍未做到完整的端到端无人值守发布。
