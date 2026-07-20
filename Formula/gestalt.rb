# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.9"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.9/gestalt-macos-arm64.tar.gz"
      sha256 "a961e3b3b3422e5a60faa41916ed61b0b31829cdcbfa418b039cc314fb6dc9c7"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.9/gestalt-macos-x86_64.tar.gz"
      sha256 "0632156684b58be74eb11e9d3282c3142e85f26505ec5d133a777f99df24684e"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.9/gestalt-linux-arm64.tar.gz"
      sha256 "b00d1f14063aede1e1a70c3ba2da88f86279561e8c2fe914ec3ccd2eecfc483f"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.9/gestalt-linux-x86_64.tar.gz"
      sha256 "bf7e4dc6283c3313a7926100c8bf5dd447d0cc638557bca372177972c90ef7c8"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
