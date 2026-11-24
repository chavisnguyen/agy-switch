class Agys < Formula
  desc "Switch and auto-failover between Antigravity (AGY) accounts"
  homepage "https://github.com/chavisnguyen/agys"
  url "https://github.com/chavisnguyen/agys/archive/refs/tags/v1.0.17.tar.gz"
  sha256 "dde9e10f6dce4c29a5c298ff8e3bb4672b2ad7fb35c4de8a42539ca5f9da8d1f"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.Version=v#{version}")
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/agys version")
  end
end
