class Agys < Formula
  desc "Switch and auto-failover between Antigravity (AGY) accounts"
  homepage "https://github.com/chavisnguyen/agy-switch"
  url "https://github.com/chavisnguyen/agy-switch/archive/refs/tags/v1.0.14.tar.gz"
  sha256 "8a5ff73bbbfd07a7c749f6d5a3280f1330dbc3672fa53c410f77089a157bbc52"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.Version=v#{version}")
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/agys version")
  end
end
