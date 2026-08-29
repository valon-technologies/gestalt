# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.17"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.17/gestalt-macos-arm64.tar.gz"
      sha256 "486bdf0963b4864e7f745635139973f4432a355c88491dc72e6352983e2b38b7"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.17/gestalt-macos-x86_64.tar.gz"
      sha256 "1b168519d41ab9ec59f73e7cfbf5a3421a0c61c36fc6f3f1762f48e3133240b5"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.17/gestalt-linux-arm64.tar.gz"
      sha256 "5edd5352516e681c044bb283cb6464896465230e9c665ebd275c2ba90147951e"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.17/gestalt-linux-x86_64.tar.gz"
      sha256 "e8811c1d8984f08e70642aa3f26eb56fa66948f4bc2f73724eb2d9408e6be852"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
