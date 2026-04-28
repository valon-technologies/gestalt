# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.10"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.10/gestalt-macos-arm64.tar.gz"
      sha256 "2d8378ad4231a24db3463889b86fdf21cbcb8166234b1fde2461a8154dd17325"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.10/gestalt-macos-x86_64.tar.gz"
      sha256 "e30034e07af63a2e688817775de8f6495d2df76ede50aed21dd592cfca0cfd5f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.10/gestalt-linux-arm64.tar.gz"
      sha256 "836f07ab850152cc50415002e7f37dfe09cdb314636937d498abb9fdb42eab42"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.10/gestalt-linux-x86_64.tar.gz"
      sha256 "6fd4c7cc90b9dd239b82ea289eecdf2805a45c3e90e1913a971c561ac4729680"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
