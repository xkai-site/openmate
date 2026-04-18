---
name: electron-quickstart
description: OpenMate 项目 Electron 快速入门，包含语言特性、架构理解与编译/测试/运行命令。
---

# Electron快速入门

## Electron 是什么，怎么玩

1. Electron 本质是 `Chromium + Node.js` 的桌面应用运行时。
2. 你可以用前端技术（HTML/CSS/JS/TS + React/Vue）做 UI，同时用 Node 能力做本地文件、进程、系统交互。
3. 在这个仓库里，Electron 的角色是“桌面壳 + 本地能力层”：
   - `renderer`（前端页面）负责界面与交互。
   - `main`（主进程）负责本地文件能力、权限与调用 CLI。
   - `preload` 负责把安全白名单 API 暴露给前端。

## 语法更像什么语言

1. Electron 语法更像 `JavaScript / TypeScript`（不是 Go / Python）。
2. 主进程常见写法是 Node 风格（`require`/`module.exports` 或 ESM）。
3. 渲染进程就是标准前端语法（React + TSX）。
4. 这个仓库当前实现里：
   - Electron 主进程与 preload 使用 `CommonJS`（`.cjs`）。
   - 前端仍是 `TypeScript + React + Vite`。

## 关键特性（本项目重点）

1. 多进程模型：
   - `main` 有系统权限。
   - `renderer` 默认受限（浏览器沙箱思路）。
2. 安全桥接：
   - 通过 `contextBridge + IPC` 暴露能力。
   - 不把 Node 全量能力直接暴露给页面。
3. 本地文件能力：
   - 由主进程调用现有 CLI（Python/Go）完成。
   - 配合工作区白名单与高风险操作确认。
4. 打包分发：
   - 使用 `electron-builder` 产出 Windows 包。

## 本仓库目录速览

1. Electron 主进程：`frontend/electron/main.cjs`
2. 预加载桥接：`frontend/electron/preload.cjs`
3. 开发启动脚本：`frontend/scripts/electron-dev.mjs`
4. 前端桥接封装：`frontend/src/services/localFile.ts`
5. 桌面端入口按钮：`frontend/src/pages/Home/index.tsx`

## 编译 / 测试 / 运行命令（直接可用）

在仓库根目录执行：

```powershell
cd frontend
```

1. 安装依赖（首次或更新依赖后）：

```powershell
npm install
```

2. Web 开发模式（仅前端）：

```powershell
npm run dev
```

3. Electron 开发模式（Vite + Electron）：

```powershell
npm run electron:dev
```

4. Electron 直接启动（依赖已有前端构建产物）：

```powershell
npm run electron:start
```

5. 前端构建（TS + Vite）：

```powershell
npm run build
```

6. Electron 打包（Windows 目录产物）：

```powershell
npm run electron:build
```

7. Electron 脚本语法检查（快速烟测）：

```powershell
node --check electron/main.cjs
node --check electron/preload.cjs
```

## 常见问题

1. `electron-builder` 报 Windows 符号链接权限错误：
   - 本仓库已设置 `build.win.signAndEditExecutable=false`，避免常见权限链路。
2. 页面里拿不到本地能力：
   - 确认是 Electron 启动，不是纯浏览器打开。
   - 确认 `window.openmate` 可见（preload 已加载）。
3. 本地文件操作被拒绝：
   - 先在 Electron 内选择工作区。
   - 确认操作路径在工作区白名单内。
