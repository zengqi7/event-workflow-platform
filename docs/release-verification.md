# Release verification protocol

The verifier shipped in v0.1.1 tagged source includes the strict identity fixes.
[v0.1.0 completed report](release-verification-v0.1.0.md) remains historical:
its signed tag predates those verifier fixes and its assets were not replaced.

## Required gates

1. Pin the exact main commit; inspect green CI, signed annotated tag and VERSION.
2. The tag workflow validates that tag, VERSION and main resolve to that commit,
   runs real integration/race/security checks, and creates a Draft only.
3. Run `scripts/verify-release-download.ps1` in a fresh directory with the tag
   commit as both ExpectedSourceSha and ExpectedSignerSha. It requires exactly
   six binary archives plus benchmark-evidence.tar.gz (v0.1.1 onward), exactly
   matching checksum entries, and GitHub provenance for every archive.
4. Extract the benchmark archive and use `python capacity/evidence.py audit`
   on its benchmark-evidence directory. Check historical measurement commit and
   workload lock; this is not a fresh measurement of the patch release.
5. Exercise downloaded Linux amd64 and Windows amd64 packages against isolated
   fresh dependencies. Record successes and limitations, not just compilation.
6. Append measured verification results and workflow/source identity to the
   Draft notes. Publish only after every gate passes, then confirm isImmutable=true.
   Verify `gh release verify TAG` and `gh release verify-asset TAG FILE` after
   publication when supported by the client. Retain client checksum/provenance checks.

GitHub immutable releases are enabled for both repositories. Drafts can still
be corrected before publication. Already published v0.1.0 remains
isImmutable=false: enabling future enforcement does not retroactively lock it.
Never move that tag or replace those assets. See the official
[immutable releases API](https://docs.github.com/rest/repos/repos#enable-immutable-releases).

## Interpretation

Build provenance proves repository/workflow/source origin, not runtime safety,
benchmark truth, a SLSA level or deterministic binary reproducibility.
Checksums identify bytes, not publisher identity. The benchmark archive has
deterministic packaging, but binary archives are not claimed reproducible.
Capacity evidence retains original short single-host test limits and includes
historical-source attribution. Native verification does not cover macOS/arm64.

Completed patch-release results are recorded in the published Release notes;
the signed tag contains this protocol and its final verifier implementation.
