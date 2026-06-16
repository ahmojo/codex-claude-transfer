# Packaging manifests

Community-installable manifests for cct. They point at the prebuilt
binaries attached to each [GitHub Release](https://github.com/ahmojo/codex-claude-transfer/releases),
so they are version-pinned: bump the `version` and the `sha256`/`hash` values when
a new release is published.

## Homebrew (macOS / Linux)

[`homebrew/cct.rb`](homebrew/cct.rb) is a formula that installs the
matching prebuilt binary.

```bash
# Install directly from the formula file:
brew install --formula ./packaging/homebrew/cct.rb

# Or, after copying it into a tap repo (github.com/<you>/homebrew-tap):
brew install <you>/tap/cct
```

Refresh the four `sha256` values per release, e.g.
`shasum -a 256 cct_vX.Y.Z_darwin_arm64.tar.gz`.

## Scoop (Windows)

[`scoop/cct.json`](scoop/cct.json) is a Scoop manifest.

```powershell
# Install directly from the manifest:
scoop install https://raw.githubusercontent.com/ahmojo/codex-claude-transfer/Main/packaging/scoop/cct.json

# Or add it to a bucket and `scoop install cct`.
```

`checkver`/`autoupdate` are set so Scoop can compute new URLs and hashes
automatically when you run `scoop update`.
