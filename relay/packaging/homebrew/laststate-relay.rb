# Homebrew formula template for Last State Relay.
# Install: brew install --formula ./packaging/homebrew/laststate-relay.rb
class LaststateRelay < Formula
  desc "Offline-first Latch → Trace diagnostics gateway"
  homepage "https://github.com/laststate/relay"
  url "https://github.com/laststate/relay/archive/refs/tags/v0.3.0-alpha.tar.gz"
  # sha256 "REPLACE_ME"
  license "Apache-2.0"
  head "https://github.com/laststate/relay.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s -w
      -X main.cliVersion=#{version}
      -X main.gitCommit=homebrew
      -X main.buildDate=#{time.iso8601}
    ]
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/laststate-relay"
    (etc/"laststate").mkpath
  end

  service do
    run [opt_bin/"laststate-relay", "run", "--config", etc/"laststate/relay.yaml"]
    keep_alive true
    working_dir var/"lib/laststate"
    log_path var/"log/laststate-relay.log"
    error_log_path var/"log/laststate-relay.err"
  end

  test do
    system "#{bin}/laststate-relay", "version"
  end
end
