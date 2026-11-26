class Agys < Formula
  desc "Switch and auto-failover between Antigravity (AGY) accounts"
  homepage "https://github.com/chavisnguyen/agys"
  license "MIT"

  # NOTE: `url`/`sha256` below are rewritten on every published Release by
  # the "Release assets" workflow (.github/workflows/release.yml). Do not edit
  # by hand. (`version` is inferred from the tag in the URL.)
  livecheck do
    url :homepage
    strategy :github_latest
  end

  on_macos do
    on_arm do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.22/agys-darwin-arm64.tar.gz"
      sha256 "ef5001afcceee4843b5b558f973efa5f701f38a9e6b068a826239f1e6fcb9b15"
    end
    on_intel do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.22/agys-darwin-amd64.tar.gz"
      sha256 "deff22c590c2346509ab9347c8c92d4ed83740ec090e72a9a84dcd27cb27ec12"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.22/agys-linux-arm64.tar.gz"
      sha256 "24ad20a22523511cd60c7d3660d998ba12b3c405855a2b63c7aef90ba0ec76e4"
    end
    on_intel do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.22/agys-linux-amd64.tar.gz"
      sha256 "ba02c2c5dc11b79cd6d93f48fcbcce5cd9f474f7ae2b415471a87ab96a3da00d"
    end
  end

  def install
    bin.install "agys"
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/agys version")
  end
end
