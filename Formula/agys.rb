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
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.20/agys-darwin-arm64.tar.gz"
      sha256 "7a9d5c056116699002afedb952654815697e52dc630c42c4610811592e6b4f16"
    end
    on_intel do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.20/agys-darwin-amd64.tar.gz"
      sha256 "de43e1338d10df2c5e6a9c1964e96c4555b19bb5bceee6f27f620fc520595a1c"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.20/agys-linux-arm64.tar.gz"
      sha256 "92648138e7753a8b4ebafeda0698e9e8e24838e98e51a638d2650a4de62e1917"
    end
    on_intel do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.20/agys-linux-amd64.tar.gz"
      sha256 "e2747b67c07ff90498d82795a39d28d5ef6a215474d06a2ce24b767a76a4708f"
    end
  end

  def install
    bin.install "agys"
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/agys version")
  end
end
