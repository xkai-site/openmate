[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [Parameter(Mandatory = $false)]
    [string]$WorkspaceRoot = "."
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Resolve-AbsolutePath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$PathValue
    )

    $providerPath = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($PathValue)
    return [System.IO.Path]::GetFullPath($providerPath)
}

function Is-PathInsideRoot {
    param(
        [Parameter(Mandatory = $true)]
        [string]$PathValue,
        [Parameter(Mandatory = $true)]
        [string]$RootValue
    )

    $target = (Resolve-AbsolutePath -PathValue $PathValue).TrimEnd("\", "/")
    $root = (Resolve-AbsolutePath -PathValue $RootValue).TrimEnd("\", "/")
    if ($target.Equals($root, [System.StringComparison]::OrdinalIgnoreCase)) {
        return $true
    }
    $prefix = $root + [System.IO.Path]::DirectorySeparatorChar
    return $target.StartsWith($prefix, [System.StringComparison]::OrdinalIgnoreCase)
}

function Remove-TargetPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$TargetPath,
        [Parameter(Mandatory = $true)]
        [string]$RootPath,
        [Parameter(Mandatory = $true)]
        [string]$Reason
    )

    if (-not (Test-Path -LiteralPath $TargetPath)) {
        return $false
    }

    if (-not (Is-PathInsideRoot -PathValue $TargetPath -RootValue $RootPath)) {
        throw "Refusing to delete path outside workspace root: $TargetPath"
    }

    if ($PSCmdlet.ShouldProcess($TargetPath, "Remove [$Reason]")) {
        Remove-Item -LiteralPath $TargetPath -Recurse -Force
    }
    return $true
}

$resolvedWorkspaceRoot = Resolve-AbsolutePath -PathValue $WorkspaceRoot

if (-not (Test-Path -LiteralPath $resolvedWorkspaceRoot -PathType Container)) {
    throw "Workspace root does not exist: $resolvedWorkspaceRoot"
}

$removedItems = New-Object System.Collections.Generic.List[string]

function Try-Remove {
    param(
        [Parameter(Mandatory = $true)]
        [string]$PathValue,
        [Parameter(Mandatory = $true)]
        [string]$Reason
    )

    if (Remove-TargetPath -TargetPath $PathValue -RootPath $resolvedWorkspaceRoot -Reason $Reason) {
        $removedItems.Add((Resolve-AbsolutePath -PathValue $PathValue)) | Out-Null
    }
}

$repoTargets = @(
    # Unified runtime history
    @{ Path = ".openmate/runtime/openmate.db"; Reason = "runtime sqlite history" },
    @{ Path = ".openmate/runtime/openmate.db-shm"; Reason = "runtime sqlite shared memory" },
    @{ Path = ".openmate/runtime/openmate.db-wal"; Reason = "runtime sqlite write-ahead log" },
    @{ Path = ".openmate/runtime/vos_state.json"; Reason = "VOS state history json" },
    @{ Path = ".openmate/runtime/electron_audit.log"; Reason = "electron audit history log" },

    # Legacy history files
    @{ Path = ".vos_state.json"; Reason = "legacy VOS state history json" },
    @{ Path = ".pool_state.json"; Reason = "legacy pool state history json" },
    @{ Path = ".pool_state.db"; Reason = "legacy pool sqlite history" },
    @{ Path = ".pool_state.db-shm"; Reason = "legacy pool sqlite shared memory" },
    @{ Path = ".pool_state.db-wal"; Reason = "legacy pool sqlite write-ahead log" },

    # Frontend generated cache/artifacts
    @{ Path = "frontend/dist"; Reason = "frontend build cache" },
    @{ Path = "frontend/.vite"; Reason = "frontend vite cache" },
    @{ Path = "frontend/release"; Reason = "frontend electron release artifacts" },
    @{ Path = "frontend/electron-dist"; Reason = "frontend electron dist artifacts" },
    @{ Path = "frontend/vite.config.d.ts"; Reason = "frontend generated d.ts cache" }
)

foreach ($target in $repoTargets) {
    $absolute = Join-Path $resolvedWorkspaceRoot $target.Path
    Try-Remove -PathValue $absolute -Reason $target.Reason
}

$tsBuildInfoFiles = Get-ChildItem -LiteralPath (Join-Path $resolvedWorkspaceRoot "frontend") -Filter "*.tsbuildinfo" -File -ErrorAction SilentlyContinue
foreach ($file in $tsBuildInfoFiles) {
    Try-Remove -PathValue $file.FullName -Reason "frontend ts incremental cache"
}

Write-Host ""
Write-Host "Cleanup completed."
Write-Host "Workspace root: $resolvedWorkspaceRoot"
Write-Host ("Removed items: {0}" -f $removedItems.Count)
foreach ($item in $removedItems) {
    Write-Host ("  - {0}" -f $item)
}

Write-Host ""
Write-Host "Preserved by design:"
Write-Host ("  - {0}" -f (Join-Path $resolvedWorkspaceRoot "model.json"))
Write-Host ("  - {0}" -f (Join-Path $resolvedWorkspaceRoot ".openmate/bin"))
Write-Host ("  - {0}" -f (Join-Path $resolvedWorkspaceRoot ".venv"))
Write-Host ("  - {0}" -f (Join-Path $resolvedWorkspaceRoot "frontend/node_modules"))
