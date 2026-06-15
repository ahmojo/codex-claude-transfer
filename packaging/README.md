# Packaging manifests

Community-installable manifests for codex-sync. They point at the prebuilt
binaries attached to each [GitHub Release](https://github.com/ahmojo/Codex_Sync/releases),
so they are version-pinned: bump the `version` and the `sha256`/`hash` values when
a new release is published.

## Homebrew (macOS / Linux)

[`homebrew/codex-sync.rb`](homebrew/codex-sync.rb) is a formula that installs the
matching prebuilt binary.

```bash
# Install directly from the formula file:
brew install --formula ./packaging/homebrew/codex-sync.rb

# Or, after copying it into a tap repo (github.com/<you>/homebrew-tap):
brew install <you>/tap/codex-sync
```

Refresh the four `sha256` values per release, e.g.
`shasum -a 256 codex-sync_vX.Y.Z_darwin_arm64.tar.gz`.

## Scoop (Windows)

[`scoop/codex-sync.json`](scoop/codex-sync.json) is a Scoop manifest.

```powershell
# Install directly from the manifest:
scoop install https://raw.githubusercontent.com/ahmojo/Codex_Sync/Main/packaging/scoop/codex-sync.json

# Or add it to a bucket and `scoop install codex-sync`.
```

`checkver`/`autoupdate` are set so Scoop can compute new URLs and hashes
automatically when you run `scoop update`.
