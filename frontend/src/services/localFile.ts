function requireBridge(): OpenMateBridge {
  if (typeof window === "undefined" || !window.openmate) {
    throw new Error("Electron local file bridge is unavailable in browser mode.");
  }
  return window.openmate;
}

function unwrapResult<T>(result: OpenMateInvokeResult<T>): T {
  if (result.ok) {
    return result.data as T;
  }
  const code = result.error?.code ?? "INTERNAL_ERROR";
  const message = result.error?.message ?? "local file action failed";
  throw new Error(`[${code}] ${message}`);
}

export function isLocalFileBridgeAvailable(): boolean {
  return typeof window !== "undefined" && Boolean(window.openmate);
}

export async function getLocalWorkspace(): Promise<{ root: string | null }> {
  const bridge = requireBridge();
  const result = await bridge.permission.getWorkspace();
  return unwrapResult(result);
}

export async function selectLocalWorkspace(): Promise<{ root: string }> {
  const bridge = requireBridge();
  const result = await bridge.permission.selectWorkspace();
  return unwrapResult(result);
}

export async function localReadFile(path: string, options?: { offset?: number; limit?: number }): Promise<{ path: string; output: string }> {
  const bridge = requireBridge();
  const result = await bridge.file.read({
    path,
    offset: options?.offset,
    limit: options?.limit,
  });
  return unwrapResult(result);
}

export async function localListDirectory(path = "."): Promise<{ path: string; entries: Array<{ name: string; type: "file" | "dir"; size: number }> }> {
  const bridge = requireBridge();
  const result = await bridge.file.list({ path });
  return unwrapResult(result);
}

export async function localWriteFile(path: string, content: string, mode: "overwrite" | "append" = "overwrite"): Promise<{ path: string; mode: "overwrite" | "append"; output: string }> {
  const bridge = requireBridge();
  const result = await bridge.file.write({ path, content, mode });
  return unwrapResult(result);
}

export async function localEditFile(path: string, oldString: string, newString: string): Promise<{ path: string; output: string }> {
  const bridge = requireBridge();
  const result = await bridge.file.edit({
    path,
    old_string: oldString,
    new_string: newString,
  });
  return unwrapResult(result);
}

export async function localPatchFiles(operations: unknown[]): Promise<{ output: string }> {
  const bridge = requireBridge();
  const result = await bridge.file.patch({ operations });
  return unwrapResult(result);
}

export async function localGlobFiles(pattern: string, scope = ".", maxResults = 1000): Promise<{ scope: string; pattern: string; files: string[] }> {
  const bridge = requireBridge();
  const result = await bridge.file.glob({
    pattern,
    scope,
    max_results: maxResults,
  });
  return unwrapResult(result);
}

export async function localGrep(pattern: string, scope = ".", maxResults = 100, fileGlob?: string): Promise<{ scope: string; pattern: string; output: string }> {
  const bridge = requireBridge();
  const result = await bridge.file.grep({
    pattern,
    scope,
    max_results: maxResults,
    file_glob: fileGlob,
  });
  return unwrapResult(result);
}
