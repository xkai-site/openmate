from __future__ import annotations

import difflib
import fnmatch
import json
import py_compile
import shutil
import subprocess
from contextlib import ExitStack
from pathlib import Path
from typing import Annotated, Any, Literal
from urllib import parse, request

from pydantic import BaseModel, Field, ValidationError

from openmate_agent.models import ToolResult

from .base import Tool, ToolContext

MAX_READ_LINES = 2000
MAX_LINE_CHARS = 2000
MAX_OUTPUT_BYTES = 50 * 1024
BINARY_EXTENSIONS = {
    ".png",
    ".jpg",
    ".jpeg",
    ".gif",
    ".webp",
    ".ico",
    ".pdf",
    ".zip",
    ".gz",
    ".tar",
    ".7z",
    ".exe",
    ".dll",
    ".bin",
}


def _resolve_in_workspace(workspace_root: Path, raw_path: str) -> Path:
    path = Path(raw_path)
    candidate = path.resolve() if path.is_absolute() else (workspace_root / path).resolve()
    candidate.relative_to(workspace_root)
    return candidate


def _looks_binary(path: Path) -> bool:
    if path.suffix.lower() in BINARY_EXTENSIONS:
        return True
    try:
        sample = path.read_bytes()[:2048]
    except Exception:
        return False
    return b"\x00" in sample


class ReadPayload(BaseModel):
    path: str = Field(min_length=1)
    offset: int = Field(default=0, ge=0)
    limit: int = Field(default=200, ge=1, le=MAX_READ_LINES)


class ReadTool(Tool):
    name = "read"
    description = "Read file or directory with paging, truncation, and line numbers."

    def run(self, context: ToolContext, payload: dict[str, Any]) -> ToolResult:
        try:
            args = ReadPayload.model_validate(payload)
            target = _resolve_in_workspace(context.workspace_root, args.path)
            if not target.exists():
                return ToolResult(tool_name=self.name, success=False, error="path does not exist")

            if target.is_dir():
                entries = []
                for child in sorted(target.iterdir(), key=lambda p: p.name.lower()):
                    entries.append(
                        {
                            "name": child.name,
                            "type": "dir" if child.is_dir() else "file",
                            "size": 0 if child.is_dir() else child.stat().st_size,
                        }
                    )
                return ToolResult(tool_name=self.name, output=json.dumps(entries, ensure_ascii=False, indent=2))

            if _looks_binary(target):
                return ToolResult(tool_name=self.name, success=False, error="binary file is not supported by read tool")

            text = target.read_text(encoding="utf-8")
            lines = text.splitlines()
            total_lines = len(lines)
            start = min(args.offset, total_lines)
            end = min(start + args.limit, total_lines)
            selected = lines[start:end]

            truncated_line_count = total_lines > MAX_READ_LINES
            truncated_by_offset = end < total_lines
            output_lines: list[str] = []
            byte_count = 0
            truncated_by_bytes = False
            truncated_line_chars = False
            for idx, line in enumerate(selected, start=start + 1):
                value = line
                if len(value) > MAX_LINE_CHARS:
                    value = f"{value[:MAX_LINE_CHARS]}...[line-truncated]"
                    truncated_line_chars = True
                rendered = f"|{idx}| {value}"
                line_bytes = len((rendered + "\n").encode("utf-8"))
                if byte_count + line_bytes > MAX_OUTPUT_BYTES:
                    truncated_by_bytes = True
                    break
                output_lines.append(rendered)
                byte_count += line_bytes

            notes: list[str] = []
            if truncated_line_chars:
                notes.append("Some lines exceeded 2000 characters and were truncated.")
            if truncated_line_count:
                notes.append("File exceeds 2000 lines; use offset/limit to continue reading.")
            if truncated_by_offset:
                notes.append(f"More content available. Next offset={end}.")
            if truncated_by_bytes:
                notes.append("Output exceeded 50KB and was truncated. Reduce limit or increase offset.")

            final_output = "\n".join(output_lines)
            if notes:
                final_output = f"{final_output}\n\n[read-notes]\n" + "\n".join(notes)

            context.file_time.set(target, target.stat().st_mtime)
            return ToolResult(tool_name=self.name, output=final_output)
        except ValidationError as exc:
            return ToolResult(tool_name=self.name, success=False, error=f"invalid payload: {exc.errors()}")
        except Exception as exc:  # pragma: no cover
            return ToolResult(tool_name=self.name, success=False, error=str(exc))


class WritePayload(BaseModel):
    path: str = Field(min_length=1)
    content: str = ""


class WriteTool(Tool):
    name = "write"
    description = "Write full file content with FileTime conflict check and diff preview."

    def run(self, context: ToolContext, payload: dict[str, Any]) -> ToolResult:
        try:
            args = WritePayload.model_validate(payload)
            file_path = _resolve_in_workspace(context.workspace_root, args.path)
            old_content = file_path.read_text(encoding="utf-8") if file_path.exists() else ""

            with context.lock_manager.with_lock(file_path):
                ok, reason = context.file_time.assert_fresh(file_path)
                if not ok:
                    return ToolResult(tool_name=self.name, success=False, error=reason)

                diff_text = self._build_diff(file_path=file_path, old_content=old_content, new_content=args.content)

                file_path.parent.mkdir(parents=True, exist_ok=True)
                file_path.write_text(args.content, encoding="utf-8")
                context.file_time.set(file_path, file_path.stat().st_mtime)

            diagnostics = _collect_diagnostics(file_path)
            output_parts = [f"written:{file_path}", "[diff-preview]", diff_text]
            if diagnostics:
                output_parts.append("[diagnostics]")
                output_parts.append(diagnostics)
            return ToolResult(tool_name=self.name, output="\n".join(output_parts))
        except ValidationError as exc:
            return ToolResult(tool_name=self.name, success=False, error=f"invalid payload: {exc.errors()}")
        except TimeoutError as exc:
            return ToolResult(tool_name=self.name, success=False, error=str(exc))
        except Exception as exc:  # pragma: no cover
            return ToolResult(tool_name=self.name, success=False, error=str(exc))

    @staticmethod
    def _build_diff(file_path: Path, old_content: str, new_content: str) -> str:
        old_lines = old_content.splitlines(keepends=True)
        new_lines = new_content.splitlines(keepends=True)
        diff = difflib.unified_diff(
            old_lines,
            new_lines,
            fromfile=f"{file_path} (old)",
            tofile=f"{file_path} (new)",
            lineterm="",
        )
        text = "".join(diff).strip()
        return text or "(no changes)"


class EditPayload(BaseModel):
    path: str = Field(min_length=1)
    old_string: str = Field(min_length=1)
    new_string: str = ""


class EditTool(Tool):
    name = "edit"
    description = "Edit file by replacing old_string with new_string using resilient matching."

    def run(self, context: ToolContext, payload: dict[str, Any]) -> ToolResult:
        try:
            args = EditPayload.model_validate(payload)
            file_path = _resolve_in_workspace(context.workspace_root, args.path)
            if not file_path.exists():
                return ToolResult(tool_name=self.name, success=False, error="file does not exist")

            with context.lock_manager.with_lock(file_path):
                ok, reason = context.file_time.assert_fresh(file_path)
                if not ok:
                    return ToolResult(tool_name=self.name, success=False, error=reason)

                content = file_path.read_text(encoding="utf-8")
                match = _find_best_match(content=content, old_string=args.old_string)
                if not match.success:
                    return ToolResult(tool_name=self.name, success=False, error=match.message)

                start, end = match.span
                new_content = content[:start] + args.new_string + content[end:]
                diff_text = WriteTool._build_diff(file_path=file_path, old_content=content, new_content=new_content)
                file_path.write_text(new_content, encoding="utf-8")
                context.file_time.set(file_path, file_path.stat().st_mtime)

            diagnostics = _collect_diagnostics(file_path)
            output_parts = [f"edited:{file_path}", f"[match-strategy] {match.strategy}", "[diff-preview]", diff_text]
            if diagnostics:
                output_parts.append("[diagnostics]")
                output_parts.append(diagnostics)
            return ToolResult(tool_name=self.name, output="\n".join(output_parts))
        except ValidationError as exc:
            return ToolResult(tool_name=self.name, success=False, error=f"invalid payload: {exc.errors()}")
        except TimeoutError as exc:
            return ToolResult(tool_name=self.name, success=False, error=str(exc))
        except Exception as exc:  # pragma: no cover
            return ToolResult(tool_name=self.name, success=False, error=str(exc))


class QueryPayload(BaseModel):
    url: str = Field(min_length=1)
    method: Literal["GET", "POST"] = "GET"
    params: dict[str, Any] = Field(default_factory=dict)
    headers: dict[str, str] = Field(default_factory=dict)
    body: dict[str, Any] = Field(default_factory=dict)
    timeout_seconds: int = Field(default=10, ge=1, le=120)


class QueryTool(Tool):
    name = "query"
    description = "Perform HTTP query request."

    def run(self, context: ToolContext, payload: dict[str, Any]) -> ToolResult:
        _ = context
        try:
            args = QueryPayload.model_validate(payload)
            final_url = self._build_url(args.url, args.params)
            body_bytes: bytes | None = None
            headers = dict(args.headers)
            if args.method == "POST":
                body_bytes = json.dumps(args.body).encode("utf-8")
                headers.setdefault("Content-Type", "application/json")
            req = request.Request(url=final_url, data=body_bytes, headers=headers, method=args.method)
            with request.urlopen(req, timeout=args.timeout_seconds) as response:
                return ToolResult(tool_name=self.name, output=response.read().decode("utf-8", errors="replace"))
        except ValidationError as exc:
            return ToolResult(tool_name=self.name, success=False, error=f"invalid payload: {exc.errors()}")
        except Exception as exc:  # pragma: no cover
            return ToolResult(tool_name=self.name, success=False, error=str(exc))

    @staticmethod
    def _build_url(base_url: str, params: dict[str, Any]) -> str:
        if not params:
            return base_url
        encoded = parse.urlencode({str(k): str(v) for k, v in params.items()})
        separator = "&" if "?" in base_url else "?"
        return f"{base_url}{separator}{encoded}"


class GrepPayload(BaseModel):
    pattern: str = Field(min_length=1)
    scope: str = "."
    max_results: int = Field(default=100, ge=1, le=5000)
    file_glob: str | None = None


class GrepTool(Tool):
    name = "grep"
    description = "Search file content with regex via ripgrep."

    def run(self, context: ToolContext, payload: dict[str, Any]) -> ToolResult:
        try:
            args = GrepPayload.model_validate(payload)
            if shutil.which("rg") is None:
                return ToolResult(tool_name=self.name, success=False, error="ripgrep (rg) is not installed")

            scope_path = _resolve_in_workspace(context.workspace_root, args.scope)
            command = [
                "rg",
                "--line-number",
                "--no-heading",
                "--color",
                "never",
                "--max-count",
                str(args.max_results),
                args.pattern,
                str(scope_path),
            ]
            if args.file_glob:
                command.extend(["-g", args.file_glob])
            command = _with_ignore_file(command=command, workspace_root=context.workspace_root)

            proc = subprocess.run(command, capture_output=True, text=True, check=False, cwd=str(context.workspace_root))
            if proc.returncode in {0, 1}:
                return ToolResult(tool_name=self.name, output=proc.stdout.strip())
            return ToolResult(tool_name=self.name, success=False, error=proc.stderr.strip() or "rg failed")
        except ValidationError as exc:
            return ToolResult(tool_name=self.name, success=False, error=f"invalid payload: {exc.errors()}")
        except Exception as exc:  # pragma: no cover
            return ToolResult(tool_name=self.name, success=False, error=str(exc))


class GlobPayload(BaseModel):
    pattern: str = Field(min_length=1)
    scope: str = "."
    max_results: int = Field(default=1000, ge=1, le=10000)


class GlobTool(Tool):
    name = "glob"
    description = "Search files by glob pattern via ripgrep, respecting .gitignore."

    def run(self, context: ToolContext, payload: dict[str, Any]) -> ToolResult:
        try:
            args = GlobPayload.model_validate(payload)
            if shutil.which("rg") is None:
                return ToolResult(tool_name=self.name, success=False, error="ripgrep (rg) is not installed")

            scope_path = _resolve_in_workspace(context.workspace_root, args.scope)
            command = [
                "rg",
                "--files",
                str(scope_path),
            ]
            command = _with_ignore_file(command=command, workspace_root=context.workspace_root)
            proc = subprocess.run(command, capture_output=True, text=True, check=False, cwd=str(context.workspace_root))
            if proc.returncode not in {0, 1}:
                return ToolResult(tool_name=self.name, success=False, error=proc.stderr.strip() or "rg failed")

            raw_lines = [line for line in proc.stdout.splitlines() if line.strip()]
            matched: list[str] = []
            for line in raw_lines:
                resolved = _resolve_rg_output_path(line=line, cwd=context.workspace_root)
                try:
                    rel = resolved.relative_to(context.workspace_root).as_posix()
                except ValueError:
                    rel = resolved.as_posix()
                if fnmatch.fnmatch(rel, args.pattern):
                    matched.append(str(resolved))

            limited = matched[: args.max_results]
            return ToolResult(tool_name=self.name, output="\n".join(limited))
        except ValidationError as exc:
            return ToolResult(tool_name=self.name, success=False, error=f"invalid payload: {exc.errors()}")
        except Exception as exc:  # pragma: no cover
            return ToolResult(tool_name=self.name, success=False, error=str(exc))


class ExecPayload(BaseModel):
    command: list[str] = Field(min_length=1)
    cwd: str | None = None
    timeout_seconds: int = Field(default=30, ge=1, le=300)
    expect_json: bool = False


class ExecTool(Tool):
    name = "exec"
    description = "Run structured command in workspace without shell string interpolation."

    def run(self, context: ToolContext, payload: dict[str, Any]) -> ToolResult:
        try:
            args = ExecPayload.model_validate(payload)
            cwd_path = context.workspace_root if args.cwd is None else _resolve_in_workspace(context.workspace_root, args.cwd)
            proc = subprocess.run(
                args.command,
                capture_output=True,
                text=True,
                cwd=str(cwd_path),
                check=False,
                timeout=args.timeout_seconds,
            )
            stdout = (proc.stdout or "").strip()
            stderr = (proc.stderr or "").strip()
            if proc.returncode != 0:
                return ToolResult(
                    tool_name=self.name,
                    success=False,
                    output=_format_exec_output(args.command, cwd_path, proc.returncode, stdout, stderr),
                    error=f"exit code {proc.returncode}",
                )
            if args.expect_json:
                try:
                    parsed = json.loads(stdout)
                except json.JSONDecodeError as exc:
                    return ToolResult(
                        tool_name=self.name,
                        success=False,
                        output=_format_exec_output(args.command, cwd_path, proc.returncode, stdout, stderr),
                        error=f"stdout is not valid json: {exc}",
                    )
                return ToolResult(
                    tool_name=self.name,
                    output=json.dumps(
                        {
                            "command": args.command,
                            "cwd": str(cwd_path),
                            "exit_code": proc.returncode,
                            "stdout_json": parsed,
                            "stderr": stderr,
                        },
                        ensure_ascii=False,
                        indent=2,
                    ),
                )
            return ToolResult(
                tool_name=self.name,
                output=_format_exec_output(args.command, cwd_path, proc.returncode, stdout, stderr),
            )
        except ValidationError as exc:
            return ToolResult(tool_name=self.name, success=False, error=f"invalid payload: {exc.errors()}")
        except Exception as exc:  # pragma: no cover
            return ToolResult(tool_name=self.name, success=False, error=str(exc))


class ShellPayload(BaseModel):
    command: str = Field(min_length=1)
    cwd: str | None = None
    timeout_seconds: int = Field(default=30, ge=1, le=300)


class ShellTool(Tool):
    name = "shell"
    description = "Run shell command in workspace."

    def run(self, context: ToolContext, payload: dict[str, Any]) -> ToolResult:
        try:
            args = ShellPayload.model_validate(payload)
            cwd_path = context.workspace_root if args.cwd is None else _resolve_in_workspace(context.workspace_root, args.cwd)
            command = ["powershell", "-NoProfile", "-Command", args.command]
            proc = subprocess.run(
                command,
                capture_output=True,
                text=True,
                cwd=str(cwd_path),
                check=False,
                timeout=args.timeout_seconds,
            )
            output = (proc.stdout or "").strip()
            if proc.stderr:
                output = f"{output}\n{proc.stderr.strip()}".strip()
            if proc.returncode != 0:
                return ToolResult(tool_name=self.name, success=False, output=output, error=f"exit code {proc.returncode}")
            return ToolResult(tool_name=self.name, output=output)
        except ValidationError as exc:
            return ToolResult(tool_name=self.name, success=False, error=f"invalid payload: {exc.errors()}")
        except Exception as exc:  # pragma: no cover
            return ToolResult(tool_name=self.name, success=False, error=str(exc))


class PatchReplaceOperation(BaseModel):
    type: Literal["replace"]
    path: str = Field(min_length=1)
    old_string: str = Field(min_length=1)
    new_string: str = ""


class PatchWriteOperation(BaseModel):
    type: Literal["write"]
    path: str = Field(min_length=1)
    content: str = ""


PatchOperation = Annotated[PatchReplaceOperation | PatchWriteOperation, Field(discriminator="type")]


class PatchPayload(BaseModel):
    operations: list[PatchOperation] = Field(min_length=1)


class _PatchFileState(BaseModel):
    path: Path
    exists: bool
    original_content: str
    new_content: str

    model_config = {"arbitrary_types_allowed": True}


class PatchTool(Tool):
    name = "patch"
    description = "Apply structured multi-file patch operations atomically after validation."

    def run(self, context: ToolContext, payload: dict[str, Any]) -> ToolResult:
        try:
            args = PatchPayload.model_validate(payload)
            file_states = self._prepare_states(context=context, operations=args.operations)
            ordered_paths = sorted(file_states.keys(), key=lambda path: str(path))
            with ExitStack() as stack:
                for file_path in ordered_paths:
                    stack.enter_context(context.lock_manager.with_lock(file_path))
                checked_states = self._validate_freshness(context=context, file_states=file_states)
                updated_states = self._apply_operations(context=context, file_states=checked_states, operations=args.operations)
                diagnostics_by_path = self._write_all(context=context, file_states=updated_states)
            return ToolResult(
                tool_name=self.name,
                output=self._format_output(file_states=updated_states, operations=args.operations, diagnostics_by_path=diagnostics_by_path),
            )
        except ValidationError as exc:
            return ToolResult(tool_name=self.name, success=False, error=f"invalid payload: {exc.errors()}")
        except TimeoutError as exc:
            return ToolResult(tool_name=self.name, success=False, error=str(exc))
        except Exception as exc:  # pragma: no cover
            return ToolResult(tool_name=self.name, success=False, error=str(exc))

    @staticmethod
    def _prepare_states(context: ToolContext, operations: list[PatchOperation]) -> dict[Path, _PatchFileState]:
        file_states: dict[Path, _PatchFileState] = {}
        for operation in operations:
            file_path = _resolve_in_workspace(context.workspace_root, operation.path)
            if file_path not in file_states:
                exists = file_path.exists()
                original_content = file_path.read_text(encoding="utf-8") if exists else ""
                file_states[file_path] = _PatchFileState(
                    path=file_path,
                    exists=exists,
                    original_content=original_content,
                    new_content=original_content,
                )
        return file_states

    @staticmethod
    def _validate_freshness(
        context: ToolContext,
        file_states: dict[Path, _PatchFileState],
    ) -> dict[Path, _PatchFileState]:
        for file_path, state in file_states.items():
            if not state.exists:
                continue
            ok, reason = context.file_time.assert_fresh(file_path)
            if not ok:
                raise ValueError(reason)
        return file_states

    @staticmethod
    def _apply_operations(
        context: ToolContext,
        file_states: dict[Path, _PatchFileState],
        operations: list[PatchOperation],
    ) -> dict[Path, _PatchFileState]:
        for operation in operations:
            target_path = _resolve_in_workspace(context.workspace_root, operation.path)
            state = file_states[target_path]
            if isinstance(operation, PatchReplaceOperation):
                if not state.exists:
                    raise ValueError(f"file does not exist: {state.path}")
                match = _find_best_match(content=state.new_content, old_string=operation.old_string)
                if not match.success:
                    raise ValueError(f"{state.path}: {match.message}")
                start, end = match.span
                state.new_content = state.new_content[:start] + operation.new_string + state.new_content[end:]
            else:
                state.new_content = operation.content
        return file_states

    @staticmethod
    def _write_all(
        context: ToolContext,
        file_states: dict[Path, _PatchFileState],
    ) -> dict[Path, str]:
        diagnostics_by_path: dict[Path, str] = {}
        for file_path, state in file_states.items():
            file_path.parent.mkdir(parents=True, exist_ok=True)
            file_path.write_text(state.new_content, encoding="utf-8")
            context.file_time.set(file_path, file_path.stat().st_mtime)
            diagnostics_by_path[file_path] = _collect_diagnostics(file_path)
        return diagnostics_by_path

    @staticmethod
    def _format_output(
        file_states: dict[Path, _PatchFileState],
        operations: list[PatchOperation],
        diagnostics_by_path: dict[Path, str],
    ) -> str:
        lines = [f"patched:{len(file_states)} files, {len(operations)} operations"]
        for file_path in sorted(file_states.keys(), key=lambda path: str(path)):
            state = file_states[file_path]
            lines.extend(
                [
                    f"[file] {file_path}",
                    "[diff-preview]",
                    WriteTool._build_diff(file_path=file_path, old_content=state.original_content, new_content=state.new_content),
                ]
            )
            diagnostics = diagnostics_by_path.get(file_path, "")
            if diagnostics:
                lines.append("[diagnostics]")
                lines.append(diagnostics)
        return "\n".join(lines)


def _format_exec_output(
    command: list[str],
    cwd_path: Path,
    return_code: int,
    stdout: str,
    stderr: str,
) -> str:
    lines = [
        f"command:{json.dumps(command, ensure_ascii=False)}",
        f"cwd:{cwd_path}",
        f"exit_code:{return_code}",
    ]
    if stdout:
        lines.append("[stdout]")
        lines.append(stdout)
    if stderr:
        lines.append("[stderr]")
        lines.append(stderr)
    return "\n".join(lines)


class _MatchResult(BaseModel):
    success: bool
    strategy: str = ""
    span: tuple[int, int] = (0, 0)
    message: str = ""


def _find_best_match(content: str, old_string: str) -> _MatchResult:
    exact_idx = content.find(old_string)
    if exact_idx >= 0:
        return _MatchResult(success=True, strategy="ExactMatch", span=(exact_idx, exact_idx + len(old_string)))

    ws_result = _whitespace_normalizer_match(content, old_string)
    if ws_result.success:
        return ws_result

    anchor_result = _block_anchor_match(content, old_string)
    if anchor_result.success:
        return anchor_result

    similarity_result = _similarity_match(content, old_string)
    if similarity_result.success:
        return similarity_result

    trim_result = _line_trim_match(content, old_string)
    if trim_result.success:
        return trim_result

    return _MatchResult(success=False, message="old_string not found. Provide more specific context.")


def _whitespace_normalizer_match(content: str, old_string: str) -> _MatchResult:
    norm_content = content.replace("\t", "    ")
    norm_old = old_string.replace("\t", "    ")
    idx = norm_content.find(norm_old)
    if idx < 0:
        return _MatchResult(success=False)
    original_idx = _approximate_original_index(content, idx, len(norm_old))
    return _MatchResult(
        success=True,
        strategy="WhitespaceNormalizer",
        span=(original_idx, original_idx + len(old_string)),
    )


def _block_anchor_match(content: str, old_string: str) -> _MatchResult:
    target = "".join(line.strip() for line in old_string.splitlines())
    if not target:
        return _MatchResult(success=False)
    lines = content.splitlines(keepends=True)
    for start in range(len(lines)):
        buffer = ""
        char_start = sum(len(l) for l in lines[:start])
        for end in range(start, len(lines)):
            buffer += lines[end].strip()
            if buffer == target:
                char_end = sum(len(l) for l in lines[: end + 1])
                return _MatchResult(success=True, strategy="BlockAnchorReplacer", span=(char_start, char_end))
            if len(buffer) > len(target):
                break
    return _MatchResult(success=False)


def _similarity_match(content: str, old_string: str) -> _MatchResult:
    target_lines = old_string.splitlines()
    if not target_lines:
        return _MatchResult(success=False)
    content_lines = content.splitlines()
    window = len(target_lines)
    candidates: list[tuple[float, int, int]] = []
    for i in range(0, max(1, len(content_lines) - window + 1)):
        candidate = "\n".join(content_lines[i : i + window])
        ratio = difflib.SequenceMatcher(a=old_string, b=candidate).ratio()
        if ratio >= 0.3:
            start_char = len("\n".join(content_lines[:i]))
            if i > 0:
                start_char += 1
            end_char = start_char + len(candidate)
            candidates.append((ratio, start_char, end_char))

    high = [c for c in candidates if c[0] >= 0.7]
    if len(high) == 1:
        _, start, end = high[0]
        return _MatchResult(success=True, strategy="SimilarityMatcher", span=(start, end))
    if len(high) > 1:
        return _MatchResult(success=False, message="ambiguous match. Provide more surrounding context in old_string.")
    return _MatchResult(success=False)


def _line_trim_match(content: str, old_string: str) -> _MatchResult:
    target = "\n".join(line.strip() for line in old_string.splitlines())
    content_lines = content.splitlines()
    for i in range(len(content_lines)):
        for j in range(i + 1, len(content_lines) + 1):
            candidate = "\n".join(line.strip() for line in content_lines[i:j])
            if candidate == target:
                start_char = len("\n".join(content_lines[:i]))
                if i > 0:
                    start_char += 1
                segment = "\n".join(content_lines[i:j])
                end_char = start_char + len(segment)
                return _MatchResult(success=True, strategy="LineTrimMatcher", span=(start_char, end_char))
    return _MatchResult(success=False)


def _approximate_original_index(content: str, norm_index: int, norm_len: int) -> int:
    _ = norm_len
    seen = 0
    for idx, ch in enumerate(content):
        seen += 4 if ch == "\t" else 1
        if seen >= norm_index:
            return idx
    return 0


def _collect_diagnostics(path: Path) -> str:
    if path.suffix.lower() != ".py":
        return ""
    try:
        py_compile.compile(str(path), doraise=True)
        return ""
    except py_compile.PyCompileError as exc:
        return str(exc)


def _with_ignore_file(command: list[str], workspace_root: Path) -> list[str]:
    ignore_file = workspace_root / ".gitignore"
    if ignore_file.exists():
        return [*command, "--ignore-file", str(ignore_file)]
    return command


def _resolve_rg_output_path(line: str, cwd: Path) -> Path:
    candidate = Path(line)
    if candidate.is_absolute():
        return candidate.resolve()
    return (cwd / candidate).resolve()
