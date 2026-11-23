class Agys < Formula
  desc "Switch and auto-failover between Antigravity (AGY) accounts"
  homepage "https://github.com/chavisnguyen/agy-switch"
  url "https://github.com/chavisnguyen/agy-switch/archive/refs/tags/v1.0.16.tar.gz"
  sha256 "6e1772bef06aaa69c551c75cad68751be52413d6368c5a0259fd1bf9487048d1"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.Version=v#{version}")
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/agys version")
  end
end
