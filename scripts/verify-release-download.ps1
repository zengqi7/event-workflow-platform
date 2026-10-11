param(
    [string]$Repo = 'zengqi7/event-workflow-platform',
    [string]$Tag = 'v0.1.0',
    [Parameter(Mandatory)][ValidatePattern('^[a-f0-9]{40}$')][string]$ExpectedSourceSha,
    # The signer digest is the workflow's commit SHA, not the workflow file blob.
    [Parameter(Mandatory)][Alias('ExpectedWorkflowSha')][ValidatePattern('^[a-f0-9]{40}$')][string]$ExpectedSignerSha,
    [Parameter(Mandatory)][string]$Destination,
    [string]$GhCommand = 'gh'
)
$ErrorActionPreference = 'Stop'
if ($Tag -ne 'v0.1.0') { throw 'only v0.1.0 is accepted by this release verifier' }
$destinationPath = [IO.Path]::GetFullPath($Destination)
if (Test-Path -LiteralPath $destinationPath) {
    if (Get-ChildItem -LiteralPath $destinationPath -Force) { throw 'destination must be new or empty' }
} else { New-Item -ItemType Directory -Path $destinationPath | Out-Null }
$gh = (Get-Command $GhCommand -ErrorAction Stop).Source
& $gh release download $Tag --repo $Repo --dir $destinationPath
if ($LASTEXITCODE -ne 0) { throw 'release download failed' }
$prefix = 'event-workflow-platform-v0.1.0'
$expected = @("$prefix-darwin-amd64.tar.gz","$prefix-darwin-arm64.tar.gz","$prefix-linux-amd64.tar.gz","$prefix-linux-arm64.tar.gz","$prefix-windows-amd64.zip","$prefix-windows-arm64.zip")
$archives = @(Get-ChildItem -LiteralPath $destinationPath -File | Where-Object { $_.Name -match '\.(zip|tar\.gz)$' } | Sort-Object Name)
if (($archives.Name -join "`n") -ne (($expected | Sort-Object) -join "`n")) { throw 'release archive set mismatch' }
$checksumPath = Join-Path $destinationPath 'checksums.txt'
$lines = @(Get-Content -LiteralPath $checksumPath -Encoding UTF8 | Where-Object { $_.Trim() })
if ($lines.Count -ne 6) { throw 'checksums.txt must contain exactly six entries' }
$checksumMap = @{}
foreach ($line in $lines) {
    if ($line -notmatch '^([a-fA-F0-9]{64})\s+\*?(?:\./)?(.+)$') { throw "invalid checksum line: $line" }
    $checksumMap[$Matches[2]] = $Matches[1].ToLowerInvariant()
}
$evidence = @()
foreach ($archive in $archives) {
    $actual = (Get-FileHash -LiteralPath $archive.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($checksumMap[$archive.Name] -ne $actual) { throw "checksum mismatch: $($archive.Name)" }
    $attestationPath = Join-Path $destinationPath "$($archive.Name).attestation.json"
    & $gh attestation verify $archive.FullName --repo $Repo --signer-workflow "$Repo/.github/workflows/release.yml@refs/tags/$Tag" --signer-digest $ExpectedSignerSha --source-digest $ExpectedSourceSha --source-ref "refs/tags/$Tag" --deny-self-hosted-runners --format json | Set-Content -LiteralPath $attestationPath -Encoding utf8
    if ($LASTEXITCODE -ne 0) { throw "provenance verification failed: $($archive.Name)" }
    $evidence += @{ file=$archive.Name; sha256=$actual; attestation=$attestationPath }
}
@{ repository=$Repo; tag=$Tag; source_sha=$ExpectedSourceSha; signer_sha=$ExpectedSignerSha; signer_workflow="$Repo/.github/workflows/release.yml@refs/tags/$Tag"; verified_at=(Get-Date).ToUniversalTime().ToString('o'); assets=$evidence } | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $destinationPath 'verification-summary.json') -Encoding utf8
Write-Output "verified six checksums and six GitHub build attestations in $destinationPath"
