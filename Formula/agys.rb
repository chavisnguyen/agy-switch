class Agys < Formula
  desc "Switch and auto-failover between Antigravity (AGY) accounts"
  homepage "https://github.com/chavisnguyen/agy-switch"
  url "https://github.com/chavisnguyen/agy-switch/archive/refs/tags/v1.0.13.tar.gz"
  sha256 "3785b53c5ebd2c1d93cba432eaa36e49fb0f9c182555970ab20aecc930e4016b"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.Version=v#{version}")
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/agys version")
  end
end
