# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.13"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.13/gestalt-macos-arm64.tar.gz"
      sha256 "a5e3336adfb21d97f3d06e7d73607dc34271b952171441962ca040542621ff37"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.13/gestalt-macos-x86_64.tar.gz"
      sha256 "02d3f8d9db4a927947200c71f5ff889cfe25963eafc404fbd754423615dce9d6"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.13/gestalt-linux-arm64.tar.gz"
      sha256 "1cae03e586638202dc99edde1709c95442f8194bf601cd0bc56ab0b804b8ae42"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.13/gestalt-linux-x86_64.tar.gz"
      sha256 "3a8fd3f2dae7582d8d1162d578e1a679480f8f4798291e96b8013238f87ab679"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
