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
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.21/agys-darwin-arm64.tar.gz"
      sha256 "d43aa97309aeb8569721cc28b568463c5f25cbf4fc2614746a211dc7b01f13a7"
    end
    on_intel do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.21/agys-darwin-amd64.tar.gz"
      sha256 "bee498afb0861624c06639fafda917e42509f6bebe33de562d306926778bbfe9"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.21/agys-linux-arm64.tar.gz"
      sha256 "58110a8ab35a7e94321d0e095ea07691b0b543c9848d2ff64360b866acc7a349"
    end
    on_intel do
      url "https://github.com/chavisnguyen/agys/releases/download/v1.0.21/agys-linux-amd64.tar.gz"
      sha256 "cbecf42e418ea791b0fb96991bb5e615589a85c2674bea33ae556a6f7e2c1bc6"
    end
  end

  def install
    bin.install "agys"
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/agys version")
  end
end
