# Verifies fail-closed asset and CLI-identity contracts without network access.
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$project = 'event-workflow-platform'
$env:VERIFIER_TEST_REPO = "zengqi7/$project"
$fixture = Join-Path $PSScriptRoot 'testdata/gh-verifier-fixture.ps1'
$temp = Join-Path ([IO.Path]::GetTempPath()) ("release-verifier-test-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory $temp | Out-Null
try {
    foreach ($tag in @('v0.1.0', 'v0.1.1')) {
        $env:VERIFIER_TEST_CASE = 'valid'
        $destination = Join-Path $temp $tag
        & "$PSScriptRoot/verify-release-download.ps1" -Tag $tag -ExpectedSourceSha ('a' * 40) -ExpectedSignerSha ('a' * 40) -Destination $destination -GhCommand $fixture
        $summary = Get-Content (Join-Path $destination 'verification-summary.json') -Raw | ConvertFrom-Json
        $expectedCount = if ($tag -eq 'v0.1.0') { 6 } else { 7 }
        if ($summary.assets.Count -ne $expectedCount) { throw 'wrong verified asset count' }
    }
    foreach ($case in @('missing','extra','duplicate','checksum','provenance','signer','source','occupied','tag')) {
        $env:VERIFIER_TEST_CASE = $case
        $destination = Join-Path $temp $case
        $signer = if ($case -eq 'signer') { 'b' * 40 } else { 'a' * 40 }
        $source = if ($case -eq 'source') { 'b' * 40 } else { 'a' * 40 }
        $tag = if ($case -eq 'tag') { 'snapshot' } else { 'v0.1.1' }
        if ($case -eq 'occupied') { $destination = Join-Path $temp 'v0.1.1' }
        $failed = $false
        try {
            & "$PSScriptRoot/verify-release-download.ps1" -Tag $tag -ExpectedSourceSha $source -ExpectedSignerSha $signer -Destination $destination -GhCommand $fixture
        } catch { $failed = $true }
        if (-not $failed) { throw "verifier accepted failure case: $case" }
    }
    Write-Output 'release verifier contracts passed (2 success and 9 rejection cases)'
} finally {
    # Only this test-created GUID directory is removed.
    Remove-Item -LiteralPath $temp -Recurse -Force
    Remove-Item Env:VERIFIER_TEST_REPO,Env:VERIFIER_TEST_CASE -ErrorAction SilentlyContinue
}
