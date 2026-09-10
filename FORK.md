# DinTalDock — fork of Tutti

上游：`https://github.com/tutti-os/tutti`，fork 点 `87d456238fc65e0597e2e383cb2cffa5f306b0dd`（2026-08-11）。
协议：Apache-2.0（见 `LICENSE`、`NOTICE`）。

## 这是自研仓，不再跟上游

2026-09-10 起本仓与上游**彻底断开**：不维护 upstream 镜像分支，不做定期对齐。
`87d4562` 只作为历史地基保留，让 `git blame` 能分清哪行是上游的、哪行是我方的。
真要临时捞某个上游修复，`git remote add upstream <上游地址>` 后自己挑。

## 历史怎么来的

本仓在 rndmaster 里曾以 117 个 patch 文件的形式存在
（`rndmaster/third_party/tutti/patches/`，45,441 行 diff，覆盖 389 个上游文件），
每次构建都要 `git checkout 87d4562` 再重套一遍。2026-09-10 迁成真实提交：

- `87d4562` 之后的 117 个提交 = 原来的 117 个 patch，一个 patch 一个提交，
  commit subject 是原补丁文件名，body 是原 `third_party/tutti/README.md` 里那条说明
- 随后 3 个 `brand:` 提交 = 原先由 `build-tutti-web.sh` 在**构建期**改源码的三件事：
  品牌图替换、工作区壁纸、Agent 注入内容里的品牌词

## 还留在 rndmaster 构建脚本里的两件事

这两件作用在**已构建的产物**上，不是源码，所以不在本仓：

| 函数 | 做什么 |
|---|---|
| `rewrite_product_name()` | 对 bundle 里的 html/js/css 做 `\bTutti\b` → `DinTalDock`，同时把 `X-Tutti-Agent-Command-Origin` 这个 wire header 改回去 |
| `rewrite_agent_asset_urls()` | 坑83：`tutti-asset://agent/<name>.png` 改成 `./assets/<hash>.png`（iframe/WKWebView 里没有 Electron 协议解析器） |
