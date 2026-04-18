import { spawn } from "node:child_process";

const isWindows = process.platform === "win32";
const npmCommand = isWindows ? "npm.cmd" : "npm";
const devUrl = process.env.OPENMATE_ELECTRON_DEV_URL || "http://127.0.0.1:5173";

function run(command, args, options = {}) {
  return spawn(command, args, {
    stdio: "inherit",
    shell: false,
    ...options,
  });
}

async function waitForUrl(url, timeoutMs = 60000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const response = await fetch(url);
      if (response.ok) {
        return;
      }
    } catch {
      // server not ready
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`Vite dev server not ready after ${timeoutMs}ms: ${url}`);
}

const vite = run(npmCommand, ["run", "dev", "--", "--host", "127.0.0.1", "--port", "5173"]);
let electron = null;

function cleanup(exitCode = 0) {
  if (electron && !electron.killed) {
    electron.kill();
  }
  if (!vite.killed) {
    vite.kill();
  }
  process.exit(exitCode);
}

process.on("SIGINT", () => cleanup(130));
process.on("SIGTERM", () => cleanup(143));

vite.on("exit", (code) => {
  if (!electron) {
    process.exit(code ?? 1);
    return;
  }
  if (!electron.killed) {
    electron.kill();
  }
});

try {
  await waitForUrl(devUrl);
  electron = run(npmCommand, ["run", "electron:start"], {
    env: {
      ...process.env,
      OPENMATE_ELECTRON_DEV_URL: devUrl,
    },
  });
  electron.on("exit", (code) => cleanup(code ?? 0));
} catch (error) {
  console.error(String(error));
  cleanup(1);
}
