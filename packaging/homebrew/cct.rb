# Homebrew formula for cct (prebuilt binaries).
#
# Install directly from this file:
#   brew install --formula ./packaging/homebrew/cct.rb
#
# Or host it in a tap (e.g. github.com/<you>/homebrew-tap) and:
#   brew install <you>/tap/cct
#
# Update `version` and the four sha256 values for each release (the sha256 of the
# corresponding release tarball, e.g. `shasum -a 256 cct_vX_darwin_arm64.tar.gz`).
class CodexSync < Formula
  desc "Local Codex session portability across machines (unofficial)"
  homepage "https://github.com/ahmojo/codex-claude-transfer"
  version "0.1.13"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ahmojo/codex-claude-transfer/releases/download/v0.1.13/cct_v0.1.13_darwin_arm64.tar.gz"
      sha256 "2dcb752606646d352b999c6f97afb839c0187477a384288fa49803f8df8c595a"
    end
    on_intel do
      url "https://github.com/ahmojo/codex-claude-transfer/releases/download/v0.1.13/cct_v0.1.13_darwin_amd64.tar.gz"
      sha256 "655fd907545346c24f174fbf2b06e5b192c21ac8e4705ec918846722d9c9229a"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ahmojo/codex-claude-transfer/releases/download/v0.1.13/cct_v0.1.13_linux_arm64.tar.gz"
      sha256 "6500569db5cb09f35fed50df8f19fbc20bd7f4d3fb0234055b9c8b3d29d23865"
    end
    on_intel do
      url "https://github.com/ahmojo/codex-claude-transfer/releases/download/v0.1.13/cct_v0.1.13_linux_amd64.tar.gz"
      sha256 "f4dcb4272b557744bff549f5e53ccfa4e22794fca272ba7bd55d6fd192d69d75"
    end
  end

  def install
    bin.install "cct"
  end

  test do
    assert_match "cct", shell_output("#{bin}/cct version")
  end
end
