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
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.19/agys-darwin-arm64.tar.gz"
      sha256 "73eda3efa203299e69cd69f1ab8c33ebc5165e26c3ca102180a1cafb564b1c83"
    end
    on_intel do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.19/agys-darwin-amd64.tar.gz"
      sha256 "e1aa6cc12becc8ca9f03c2ae49f8a0960e43f1958cc39757624306a718ebe554"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.19/agys-linux-arm64.tar.gz"
      sha256 "82a8e1f370f5b636435795f81c40c118a8b96d8575791f8c791da4d5f4326f2c"
    end
    on_intel do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.19/agys-linux-amd64.tar.gz"
      sha256 "5989c87c514e3940aac9aa19c24f2ab5790b075dfe65d2789f05199b4744ca8b"
    end
  end

  def install
    bin.install "agys"
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/agys version")
  end
end
