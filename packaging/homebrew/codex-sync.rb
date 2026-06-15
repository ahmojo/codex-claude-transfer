# Homebrew formula for codex-sync (prebuilt binaries).
#
# Install directly from this file:
#   brew install --formula ./packaging/homebrew/codex-sync.rb
#
# Or host it in a tap (e.g. github.com/<you>/homebrew-tap) and:
#   brew install <you>/tap/codex-sync
#
# Update `version` and the four sha256 values for each release (the sha256 of the
# corresponding release tarball, e.g. `shasum -a 256 codex-sync_vX_darwin_arm64.tar.gz`).
class CodexSync < Formula
  desc "Local Codex session portability across machines (unofficial)"
  homepage "https://github.com/ahmojo/Codex_Sync"
  version "0.1.10"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ahmojo/Codex_Sync/releases/download/v0.1.10/codex-sync_v0.1.10_darwin_arm64.tar.gz"
      sha256 "aa388a9ce102f60058d6f51a66f869657b3b7abefb47a105b694dd00bf99dbee"
    end
    on_intel do
      url "https://github.com/ahmojo/Codex_Sync/releases/download/v0.1.10/codex-sync_v0.1.10_darwin_amd64.tar.gz"
      sha256 "930de9fdfa9035e2e41a57604c17346f8148d1602e2563bc6e32e3c0a91dd028"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ahmojo/Codex_Sync/releases/download/v0.1.10/codex-sync_v0.1.10_linux_arm64.tar.gz"
      sha256 "56891830ceaaee2c3d60f8d2fe87e85d49a2e91e87fccc0fced83bec63ad6d31"
    end
    on_intel do
      url "https://github.com/ahmojo/Codex_Sync/releases/download/v0.1.10/codex-sync_v0.1.10_linux_amd64.tar.gz"
      sha256 "6f18c21a57f2d0b800b45f9f2552325f2c347dc44bd29d6b350af6f32f13b68f"
    end
  end

  def install
    bin.install "codex-sync"
  end

  test do
    assert_match "codex-sync", shell_output("#{bin}/codex-sync version")
  end
end
