type LocalFileErrorCode =
  | "PERMISSION_DENIED"
  | "WORKSPACE_REQUIRED"
  | "OUTSIDE_WORKSPACE"
  | "CLI_TIMEOUT"
  | "CLI_FAILED"
  | "INVALID_INPUT"
  | "INTERNAL_ERROR";

interface OpenMateError {
  code: LocalFileErrorCode;
  message: string;
  details?: unknown;
}

interface OpenMateInvokeResult<T = unknown> {
  ok: boolean;
  data?: T;
  error?: OpenMateError;
  audit_id: string;
  duration_ms: number;
}

interface OpenMateBridge {
  permission: {
    getWorkspace(): Promise<OpenMateInvokeResult<{ root: string | null }>>;
    selectWorkspace(): Promise<OpenMateInvokeResult<{ root: string }>>;
  };
  file: {
    read(payload: { path: string; offset?: number; limit?: number }): Promise<OpenMateInvokeResult<{ path: string; output: string }>>;
    list(payload: { path?: string }): Promise<OpenMateInvokeResult<{ path: string; entries: Array<{ name: string; type: "file" | "dir"; size: number }> }>>;
    write(payload: { path: string; content: string; mode?: "overwrite" | "append" }): Promise<OpenMateInvokeResult<{ path: string; mode: "overwrite" | "append"; output: string }>>;
    edit(payload: { path: string; old_string: string; new_string: string }): Promise<OpenMateInvokeResult<{ path: string; output: string }>>;
    patch(payload: { operations: unknown[] }): Promise<OpenMateInvokeResult<{ output: string }>>;
    glob(payload: { pattern: string; scope?: string; max_results?: number }): Promise<OpenMateInvokeResult<{ scope: string; pattern: string; files: string[] }>>;
    grep(payload: { pattern: string; scope?: string; max_results?: number; file_glob?: string }): Promise<OpenMateInvokeResult<{ scope: string; pattern: string; output: string }>>;
  };
}

interface Window {
  openmate?: OpenMateBridge;
}
