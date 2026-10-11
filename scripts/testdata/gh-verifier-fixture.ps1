# Network-free fixture for verifier contract tests; never used for real releases.
$ErrorActionPreference = 'Stop'
if ($args[0] -eq 'release' -and $args[1] -eq 'download') {
    $tag = $args[2]
    $destination = $args[[Array]::IndexOf($args, '--dir') + 1]
    $project = ($env:VERIFIER_TEST_REPO -split '/')[-1]
    $prefix = "$project-$tag"
    $names = @("$prefix-darwin-amd64.tar.gz", "$prefix-darwin-arm64.tar.gz",
               "$prefix-linux-amd64.tar.gz", "$prefix-linux-arm64.tar.gz",
               "$prefix-windows-amd64.zip", "$prefix-windows-arm64.zip")
    if ($tag -ne 'v0.1.0') { $names += 'benchmark-evidence.tar.gz' }
    $lines = @()
    foreach ($name in $names) {
        [IO.File]::WriteAllBytes((Join-Path $destination $name), [Text.Encoding]::UTF8.GetBytes($name))
        $hash = (Get-FileHash (Join-Path $destination $name)).Hash.ToLowerInvariant()
        $lines += "$hash  $name"
    }
    switch ($env:VERIFIER_TEST_CASE) {
        'missing' { Remove-Item -LiteralPath (Join-Path $destination $names[0]) }
        'extra' { [IO.File]::WriteAllText((Join-Path $destination 'unexpected.json'), '{}') }
        'duplicate' { $lines[1] = $lines[0] }
        'checksum' { $lines[0] = ('0' * 64) + '  ' + $names[0] }
    }
    [IO.File]::WriteAllLines((Join-Path $destination 'checksums.txt'), $lines)
    $global:LASTEXITCODE = 0
} elseif ($args[0] -eq 'attestation' -and $args[1] -eq 'verify') {
    foreach ($flag in @('--repo','--signer-workflow','--signer-digest','--source-digest','--source-ref','--deny-self-hosted-runners','--format')) {
        if ($flag -notin $args) { throw "missing verification flag: $flag" }
    }
    $tag = $args[[Array]::IndexOf($args, '--source-ref') + 1] -replace '^refs/tags/', ''
    $identity = $args[[Array]::IndexOf($args, '--signer-workflow') + 1]
    if ($identity -ne "$env:VERIFIER_TEST_REPO/.github/workflows/release.yml@refs/tags/$tag") { throw 'wrong tagged workflow identity' }
    foreach ($flag in @('--signer-digest','--source-digest')) {
        if ($args[[Array]::IndexOf($args, $flag) + 1] -ne ('a' * 40)) { throw 'wrong pinned commit' }
    }
    '{"verified":true}'
    $global:LASTEXITCODE = if ($env:VERIFIER_TEST_CASE -eq 'provenance') { 1 } else { 0 }
} else { throw 'unexpected fixture command' }
