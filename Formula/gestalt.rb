# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.4"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.4/gestalt-macos-arm64.tar.gz"
      sha256 "97d7aeaea3311cd34656d06bb5c61b21804bdaf02eacbaae2fa30447586a714c"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.4/gestalt-macos-x86_64.tar.gz"
      sha256 "a0f3b70605bac0c5e3d24dc509e5428b80bee3e87ecfead0152a68f7f97195b8"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.4/gestalt-linux-arm64.tar.gz"
      sha256 "1a0ea3cf3f38142223e8ca1c365c2168de3ffc402b1da0cfa9071c733a8a6eb4"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.4/gestalt-linux-x86_64.tar.gz"
      sha256 "59cc83dcbf8c82f99987d98e5e0f27fc84dd7873b7442d434cae1e63bd548a32"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
