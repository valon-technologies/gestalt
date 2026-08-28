# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.16"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.16/gestalt-macos-arm64.tar.gz"
      sha256 "407ca39e4ba269186d3596c849455979cf204646fc567563d3542cec743c645d"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.16/gestalt-macos-x86_64.tar.gz"
      sha256 "7877cf46e095a6789f71b47663f95d85a8f28d882eb2edd0b1075bf99826a220"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.16/gestalt-linux-arm64.tar.gz"
      sha256 "a91776ddd480cb034ab6d1dac3a6d109e3eb9574d58aab6660722528eef47fd1"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.16/gestalt-linux-x86_64.tar.gz"
      sha256 "c2931511d88e7a952c5a24b6641f67d8be2864293bd7156d2bb55f4498a89bce"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
