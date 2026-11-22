class Agys < Formula
  desc "Switch and auto-failover between Antigravity (AGY) accounts"
  homepage "https://github.com/chavisnguyen/agy-switch"
  url "https://github.com/chavisnguyen/agy-switch/archive/refs/tags/v1.0.12.tar.gz"
  sha256 "f2546be49fc1d9e0b9cd49138c82f921dcd5ac9e7aab3394bc6c6b6878af7435"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.Version=v#{version}")
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/agys version")
  end
end
