class Agys < Formula
  desc "Switch and auto-failover between Antigravity (AGY) accounts"
  homepage "https://github.com/chavisnguyen/agys"
  url "https://github.com/chavisnguyen/agys/archive/refs/tags/v1.0.18.tar.gz"
  sha256 "73a76b355f6878bb77e5b1e6c4098d9ceb49119fdbafe625cdc92ae37ec0cbf6"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.Version=v#{version}")
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/agys version")
  end
end
