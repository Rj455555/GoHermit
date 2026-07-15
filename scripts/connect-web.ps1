param(
    [string]$HostAlias = "macmini",
    [int]$LocalPort = 8787,
    [int]$RemotePort = 8787,
    [int]$RetrySeconds = 3
)

$ErrorActionPreference = "Stop"

Write-Host "GoHermit tunnel: http://127.0.0.1:$LocalPort -> $HostAlias`:$RemotePort"
Write-Host "The tunnel reconnects while this window remains open. Press Ctrl+C to stop."

while ($true) {
    & ssh -N -T `
        -o ExitOnForwardFailure=yes `
        -o ServerAliveInterval=15 `
        -o ServerAliveCountMax=3 `
        -o ConnectTimeout=8 `
        -L "${LocalPort}:127.0.0.1:${RemotePort}" `
        $HostAlias

    if ($LASTEXITCODE -eq 0) {
        break
    }
    Write-Warning "Mac mini is unavailable. Retrying in $RetrySeconds seconds..."
    Start-Sleep -Seconds $RetrySeconds
}
