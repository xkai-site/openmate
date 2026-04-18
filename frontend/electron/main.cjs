const { app, BrowserWindow, dialog, ipcMain, shell } = require("electron");
const crypto = require("node:crypto");
const fs = require("node:fs");
const fsPromises = require("node:fs/promises");
const path = require("node:path");
const { spawn } = require("node:child_process");

const NODE_ID = "electron-local";
const TOOL_TIMEOUT_MS = 20000;

const CHANNELS = {
  permission: {
    getWorkspace: "openmate:permission:get-workspace",
    selectWorkspace: "openmate:permission:select-workspace",
  },
  file: {
    read: "openmate:file:read",
    list: "openmate:file:list",
    write: "openmate:file:write",
    edit: "openmate:file:edit",
    patch: "openmate:file:patch",
    glob: "openmate:file:glob",
    grep: "openmate:file:grep",
  },
};

let mainWindow = null;
let workspaceRoot = null;

class AppError extends Error {
  constructor(code, message, details) {
    super(message);
    this.code = code;
    this.details = details;
  }
}

function getRepoRoot() {
  if (process.env.OPENMATE_REPO_ROOT) {
    return path.resolve(process.env.OPENMATE_REPO_ROOT);
  }
  return path.resolve(__dirname, "..", "..");
}

function getConfigPath() {
  return path.join(app.getPath("userData"), "openmate-local.json");
}

function getAuditLogPath() {
  if (workspaceRoot) {
    return path.join(workspaceRoot, ".openmate", "runtime", "electron_audit.log");
  }
  return path.join(app.getPath("userData"), "electron_audit.log");
}

function createAuditId() {
  return crypto.randomUUID();
}

async function appendAudit(record) {
  const logPath = getAuditLogPath();
  await fsPromises.mkdir(path.dirname(logPath), { recursive: true });
  await fsPromises.appendFile(logPath, `${JSON.stringify(record)}\n`, "utf-8");
}

function normalizeError(error) {
  if (error instanceof AppError) {
    return {
      code: error.code,
      message: error.message,
      details: error.details,
    };
  }
  return {
    code: "INTERNAL_ERROR",
    message: error instanceof Error ? error.message : "unexpected error",
  };
}

function requireWorkspaceRoot() {
  if (!workspaceRoot) {
    throw new AppError("WORKSPACE_REQUIRED", "workspace is not selected");
  }
  return workspaceRoot;
}

function isPathInside(rootPath, candidatePath) {
  const relative = path.relative(rootPath, candidatePath);
  return relative === "" || (!relative.startsWith("..") && !path.isAbsolute(relative));
}

function resolveWorkspacePath(rawPath) {
  if (typeof rawPath !== "string" || rawPath.trim() === "") {
    throw new AppError("INVALID_INPUT", "path is required");
  }
  const root = requireWorkspaceRoot();
  const input = rawPath.trim();
  const absolute = path.isAbsolute(input) ? path.resolve(input) : path.resolve(root, input);
  if (!isPathInside(root, absolute)) {
    throw new AppError("OUTSIDE_WORKSPACE", "path is outside workspace", { path: input });
  }
  return absolute;
}

function resolveWorkspaceScope(rawScope) {
  if (typeof rawScope !== "string" || rawScope.trim() === "") {
    return requireWorkspaceRoot();
  }
  return resolveWorkspacePath(rawScope);
}

function detectPythonExecutable(repoRoot) {
  const venvPython = path.join(repoRoot, ".venv", "Scripts", "python.exe");
  if (fs.existsSync(venvPython)) {
    return venvPython;
  }
  return "python";
}

function buildPythonPath(repoRoot) {
  const current = process.env.PYTHONPATH;
  if (!current) {
    return repoRoot;
  }
  return `${repoRoot}${path.delimiter}${current}`;
}

async function runCommand({ command, args, cwd, env, timeoutMs }) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd,
      env,
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
    });

    let stdout = "";
    let stderr = "";
    let settled = false;
    let timeoutHandle = null;

    if (typeof timeoutMs === "number" && timeoutMs > 0) {
      timeoutHandle = setTimeout(() => {
        if (settled) {
          return;
        }
        settled = true;
        child.kill("SIGTERM");
        reject(new AppError("CLI_TIMEOUT", `command timed out after ${timeoutMs}ms`));
      }, timeoutMs);
    }

    child.stdout.on("data", (chunk) => {
      stdout += chunk.toString("utf-8");
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk.toString("utf-8");
    });
    child.on("error", (error) => {
      if (settled) {
        return;
      }
      settled = true;
      if (timeoutHandle) {
        clearTimeout(timeoutHandle);
      }
      reject(new AppError("CLI_FAILED", "failed to start command", { error: String(error) }));
    });
    child.on("close", (code) => {
      if (settled) {
        return;
      }
      settled = true;
      if (timeoutHandle) {
        clearTimeout(timeoutHandle);
      }
      resolve({
        code: code ?? -1,
        stdout: stdout.trim(),
        stderr: stderr.trim(),
      });
    });
  });
}

async function runAgentTool({
  toolName,
  args,
  workspace,
  timeoutMs = TOOL_TIMEOUT_MS,
  retrySeedPaths = [],
}) {
  const repoRoot = getRepoRoot();
  const pythonExecutable = detectPythonExecutable(repoRoot);
  const finalArgs = [
    "-m",
    "openmate_agent.cli",
    "tool",
    toolName,
    NODE_ID,
    ...args,
    "--is-safe",
    "--is-read-only",
  ];

  const env = {
    ...process.env,
    PYTHONPATH: buildPythonPath(repoRoot),
  };

  const response = await runCommand({
    command: pythonExecutable,
    args: finalArgs,
    cwd: workspace,
    env,
    timeoutMs,
  });

  let parsed;
  try {
    parsed = JSON.parse(response.stdout);
  } catch (error) {
    throw new AppError("CLI_FAILED", "tool output is not valid JSON", {
      toolName,
      stdout: response.stdout,
      stderr: response.stderr,
      exitCode: response.code,
      parseError: String(error),
    });
  }

  if (parsed && parsed.success) {
    return parsed;
  }

  const baselineMissing = String(parsed?.error ?? "").includes("Missing FileTime baseline");
  if (baselineMissing && retrySeedPaths.length > 0) {
    await seedBaseline(workspace, retrySeedPaths);
    return runAgentTool({
      toolName,
      args,
      workspace,
      timeoutMs,
      retrySeedPaths: [],
    });
  }

  throw new AppError("CLI_FAILED", parsed?.error || "tool execution failed", {
    toolName,
    exitCode: response.code,
    stderr: response.stderr,
    output: parsed?.output,
    error: parsed?.error,
  });
}

async function seedBaseline(workspace, absolutePaths) {
  for (const absolutePath of absolutePaths) {
    if (!fs.existsSync(absolutePath)) {
      continue;
    }
    await runAgentTool({
      toolName: "read",
      workspace,
      args: [
        "--path",
        absolutePath,
        "--offset",
        "0",
        "--limit",
        "1",
      ],
      retrySeedPaths: [],
    });
  }
}

async function confirmDangerousAction(actionName, payload) {
  const detailText =
    typeof payload === "string"
      ? payload
      : JSON.stringify(payload, null, 2).slice(0, 800);

  const result = await dialog.showMessageBox(mainWindow, {
    type: "warning",
    buttons: ["Cancel", "Continue"],
    defaultId: 0,
    cancelId: 0,
    title: "Confirm Local File Action",
    message: `This action can modify local files: ${actionName}`,
    detail: detailText,
    noLink: true,
  });
  return result.response === 1;
}

async function readWorkspaceConfig() {
  const configPath = getConfigPath();
  try {
    const raw = await fsPromises.readFile(configPath, "utf-8");
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed.workspaceRoot === "string" && parsed.workspaceRoot.trim()) {
      const normalized = path.resolve(parsed.workspaceRoot);
      if (fs.existsSync(normalized) && fs.statSync(normalized).isDirectory()) {
        workspaceRoot = normalized;
      }
    }
  } catch {
    workspaceRoot = null;
  }
}

async function writeWorkspaceConfig(rootPath) {
  const configPath = getConfigPath();
  await fsPromises.mkdir(path.dirname(configPath), { recursive: true });
  await fsPromises.writeFile(
    configPath,
    JSON.stringify({ workspaceRoot: rootPath }, null, 2),
    "utf-8",
  );
}

async function handleWithAudit(actionName, payload, handler) {
  const auditId = createAuditId();
  const startedAt = Date.now();
  try {
    const data = await handler();
    const duration = Date.now() - startedAt;
    await appendAudit({
      audit_id: auditId,
      ts: new Date().toISOString(),
      action: actionName,
      payload,
      ok: true,
      duration_ms: duration,
    });
    return {
      ok: true,
      data,
      audit_id: auditId,
      duration_ms: duration,
    };
  } catch (error) {
    const normalized = normalizeError(error);
    const duration = Date.now() - startedAt;
    await appendAudit({
      audit_id: auditId,
      ts: new Date().toISOString(),
      action: actionName,
      payload,
      ok: false,
      duration_ms: duration,
      error: normalized,
    });
    return {
      ok: false,
      error: normalized,
      audit_id: auditId,
      duration_ms: duration,
    };
  }
}

function registerIpcHandlers() {
  ipcMain.handle(CHANNELS.permission.getWorkspace, async () => {
    return handleWithAudit(CHANNELS.permission.getWorkspace, {}, async () => ({
      root: workspaceRoot,
    }));
  });

  ipcMain.handle(CHANNELS.permission.selectWorkspace, async () => {
    return handleWithAudit(CHANNELS.permission.selectWorkspace, {}, async () => {
      const result = await dialog.showOpenDialog(mainWindow, {
        title: "Select Workspace",
        properties: ["openDirectory", "createDirectory", "promptToCreate"],
      });
      if (result.canceled || result.filePaths.length === 0) {
        throw new AppError("PERMISSION_DENIED", "workspace selection canceled");
      }
      const selected = path.resolve(result.filePaths[0]);
      workspaceRoot = selected;
      await writeWorkspaceConfig(selected);
      return { root: selected };
    });
  });

  ipcMain.handle(CHANNELS.file.read, async (_event, payload = {}) => {
    return handleWithAudit(CHANNELS.file.read, payload, async () => {
      const absolutePath = resolveWorkspacePath(payload.path);
      const offset = Number.isInteger(payload.offset) ? payload.offset : 0;
      const limit = Number.isInteger(payload.limit) ? payload.limit : 200;
      const toolResult = await runAgentTool({
        toolName: "read",
        workspace: requireWorkspaceRoot(),
        args: [
          "--path",
          absolutePath,
          "--offset",
          String(Math.max(0, offset)),
          "--limit",
          String(Math.max(1, limit)),
        ],
      });
      return {
        path: absolutePath,
        output: toolResult.output,
      };
    });
  });

  ipcMain.handle(CHANNELS.file.list, async (_event, payload = {}) => {
    return handleWithAudit(CHANNELS.file.list, payload, async () => {
      const absolutePath = resolveWorkspacePath(payload.path || ".");
      const toolResult = await runAgentTool({
        toolName: "read",
        workspace: requireWorkspaceRoot(),
        args: ["--path", absolutePath, "--offset", "0", "--limit", "1"],
      });
      let entries = [];
      try {
        entries = JSON.parse(toolResult.output);
      } catch {
        throw new AppError("CLI_FAILED", "list result is not a directory JSON payload", {
          path: absolutePath,
          output: toolResult.output,
        });
      }
      return { path: absolutePath, entries };
    });
  });

  ipcMain.handle(CHANNELS.file.write, async (_event, payload = {}) => {
    return handleWithAudit(CHANNELS.file.write, payload, async () => {
      const confirmed = await confirmDangerousAction(CHANNELS.file.write, payload);
      if (!confirmed) {
        throw new AppError("PERMISSION_DENIED", "write action canceled by user");
      }

      const absolutePath = resolveWorkspacePath(payload.path);
      const content = String(payload.content ?? "");
      const mode = payload.mode === "append" ? "append" : "overwrite";
      const workspace = requireWorkspaceRoot();

      let finalContent = content;
      if (mode === "append" && fs.existsSync(absolutePath)) {
        const existing = await fsPromises.readFile(absolutePath, "utf-8");
        finalContent = `${existing}${content}`;
      }

      const retrySeedPaths = fs.existsSync(absolutePath) ? [absolutePath] : [];
      const toolResult = await runAgentTool({
        toolName: "write",
        workspace,
        args: ["--path", absolutePath, "--content", finalContent],
        retrySeedPaths,
      });

      return {
        path: absolutePath,
        mode,
        output: toolResult.output,
      };
    });
  });

  ipcMain.handle(CHANNELS.file.edit, async (_event, payload = {}) => {
    return handleWithAudit(CHANNELS.file.edit, payload, async () => {
      const confirmed = await confirmDangerousAction(CHANNELS.file.edit, payload);
      if (!confirmed) {
        throw new AppError("PERMISSION_DENIED", "edit action canceled by user");
      }

      const absolutePath = resolveWorkspacePath(payload.path);
      const oldString = String(payload.old_string ?? "");
      const newString = String(payload.new_string ?? "");
      if (!oldString) {
        throw new AppError("INVALID_INPUT", "old_string is required");
      }

      const toolResult = await runAgentTool({
        toolName: "edit",
        workspace: requireWorkspaceRoot(),
        args: [
          "--path",
          absolutePath,
          "--old-string",
          oldString,
          "--new-string",
          newString,
        ],
        retrySeedPaths: [absolutePath],
      });
      return {
        path: absolutePath,
        output: toolResult.output,
      };
    });
  });

  ipcMain.handle(CHANNELS.file.patch, async (_event, payload = {}) => {
    return handleWithAudit(CHANNELS.file.patch, payload, async () => {
      const confirmed = await confirmDangerousAction(CHANNELS.file.patch, payload);
      if (!confirmed) {
        throw new AppError("PERMISSION_DENIED", "patch action canceled by user");
      }
      if (!Array.isArray(payload.operations) || payload.operations.length === 0) {
        throw new AppError("INVALID_INPUT", "operations must be a non-empty array");
      }

      const normalizedOperations = payload.operations.map((operation) => {
        if (!operation || typeof operation !== "object") {
          throw new AppError("INVALID_INPUT", "operation must be an object");
        }
        const rawPath = String(operation.path ?? "");
        const normalizedPath = resolveWorkspacePath(rawPath);
        return {
          ...operation,
          path: normalizedPath,
        };
      });

      const existingPaths = normalizedOperations
        .map((operation) => operation.path)
        .filter((operationPath) => fs.existsSync(operationPath));
      await seedBaseline(requireWorkspaceRoot(), [...new Set(existingPaths)]);

      const toolResult = await runAgentTool({
        toolName: "patch",
        workspace: requireWorkspaceRoot(),
        args: [
          "--operations",
          JSON.stringify(normalizedOperations),
        ],
      });
      return {
        output: toolResult.output,
      };
    });
  });

  ipcMain.handle(CHANNELS.file.glob, async (_event, payload = {}) => {
    return handleWithAudit(CHANNELS.file.glob, payload, async () => {
      const pattern = String(payload.pattern ?? "");
      if (!pattern) {
        throw new AppError("INVALID_INPUT", "pattern is required");
      }
      const scope = resolveWorkspaceScope(payload.scope || ".");
      const maxResults = Number.isInteger(payload.max_results) ? payload.max_results : 1000;
      const toolResult = await runAgentTool({
        toolName: "glob",
        workspace: requireWorkspaceRoot(),
        args: [
          "--pattern",
          pattern,
          "--scope",
          scope,
          "--max-results",
          String(Math.max(1, maxResults)),
        ],
      });
      const lines = toolResult.output ? toolResult.output.split(/\r?\n/).filter(Boolean) : [];
      return {
        scope,
        pattern,
        files: lines,
      };
    });
  });

  ipcMain.handle(CHANNELS.file.grep, async (_event, payload = {}) => {
    return handleWithAudit(CHANNELS.file.grep, payload, async () => {
      const pattern = String(payload.pattern ?? "");
      if (!pattern) {
        throw new AppError("INVALID_INPUT", "pattern is required");
      }
      const scope = resolveWorkspaceScope(payload.scope || ".");
      const maxResults = Number.isInteger(payload.max_results) ? payload.max_results : 100;
      const fileGlob = typeof payload.file_glob === "string" ? payload.file_glob : "";
      const args = [
        "--pattern",
        pattern,
        "--scope",
        scope,
        "--max-results",
        String(Math.max(1, maxResults)),
      ];
      if (fileGlob) {
        args.push("--file-glob", fileGlob);
      }
      const toolResult = await runAgentTool({
        toolName: "grep",
        workspace: requireWorkspaceRoot(),
        args,
      });
      return {
        scope,
        pattern,
        output: toolResult.output,
      };
    });
  });
}

function createMainWindow() {
  const devUrl = process.env.OPENMATE_ELECTRON_DEV_URL;
  const allowedOrigins = new Set(["http://localhost:5173", "http://127.0.0.1:5173"]);
  if (devUrl) {
    try {
      allowedOrigins.add(new URL(devUrl).origin);
    } catch {
      // ignore invalid dev URL
    }
  }

  mainWindow = new BrowserWindow({
    width: 1400,
    height: 900,
    minWidth: 1100,
    minHeight: 760,
    webPreferences: {
      preload: path.join(__dirname, "preload.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webSecurity: true,
    },
  });

  mainWindow.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  mainWindow.webContents.on("will-navigate", (event, url) => {
    try {
      const parsed = new URL(url);
      if (parsed.protocol === "file:" || allowedOrigins.has(parsed.origin)) {
        return;
      }
      event.preventDefault();
      void shell.openExternal(url);
    } catch {
      event.preventDefault();
    }
  });

  if (devUrl) {
    void mainWindow.loadURL(devUrl);
    return;
  }
  const indexPath = path.join(__dirname, "..", "dist", "index.html");
  void mainWindow.loadFile(indexPath);
}

app.whenReady().then(async () => {
  await readWorkspaceConfig();
  registerIpcHandlers();
  createMainWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createMainWindow();
    }
  });
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});
