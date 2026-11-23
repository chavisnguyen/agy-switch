class Agys < Formula
  desc "Switch and auto-failover between Antigravity (AGY) accounts"
  homepage "https://github.com/chavisnguyen/agy-switch"
  url "https://github.com/chavisnguyen/agy-switch/archive/refs/tags/v1.0.17.tar.gz"
  sha256 "86f42e0734e60b8936918428e1f4042b4b04332ce26f863b2b70cfd79db72f30"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.Version=v#{version}")
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/agys version")
  end
end
