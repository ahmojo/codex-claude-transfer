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
  version "0.1.11"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ahmojo/Codex_Sync/releases/download/v0.1.11/codex-sync_v0.1.11_darwin_arm64.tar.gz"
      sha256 "a3b4dff358aa6b9f9a8e56ff9b0e4c563e5a5f3fd9443f4a7a71b8e3929d7bf6"
    end
    on_intel do
      url "https://github.com/ahmojo/Codex_Sync/releases/download/v0.1.11/codex-sync_v0.1.11_darwin_amd64.tar.gz"
      sha256 "ff28f0e6d354f28c087ecddc3bc5ee179c992277de28450e233486b38dc9d667"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ahmojo/Codex_Sync/releases/download/v0.1.11/codex-sync_v0.1.11_linux_arm64.tar.gz"
      sha256 "1ee436d0fdbb8a1fdd0b80beb1c571fa0cce6ad620c3c2a39441164c15e0debf"
    end
    on_intel do
      url "https://github.com/ahmojo/Codex_Sync/releases/download/v0.1.11/codex-sync_v0.1.11_linux_amd64.tar.gz"
      sha256 "d3198e6742cedf9741bd90259f94e8f36f1d06f22475988e41c96236496a534c"
    end
  end

  def install
    bin.install "codex-sync"
  end

  test do
    assert_match "codex-sync", shell_output("#{bin}/codex-sync version")
  end
end
