# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.13"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.13/gestalt-macos-arm64.tar.gz"
      sha256 "2bdef8bfc5c014a923a4939f639e6559167e1c2620ee2677c2985779e6d59669"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.13/gestalt-macos-x86_64.tar.gz"
      sha256 "b790fb177272f2c7ccb04594274bf1864fd748e74dfc27247b7e1aabf53a40e0"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.13/gestalt-linux-arm64.tar.gz"
      sha256 "b27b69d1e833553db32310d06ed8c413588770a5ad3fe4b39321e51f321a371e"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.13/gestalt-linux-x86_64.tar.gz"
      sha256 "2aae5a673bc23520c15860cd87557720382aff6766bc8f060f2ac60d6eddb729"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
