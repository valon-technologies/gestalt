# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.3"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.3/gestalt-macos-arm64.tar.gz"
      sha256 "b60b0f4000ad08b1c7b84f4ffa86d9e0ed465734079d373355d774972a72a136"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.3/gestalt-macos-x86_64.tar.gz"
      sha256 "ffa83ee28427e08a3d6c7b72270742b2db87dc4e678a340f0b0b3cd6d402f09d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.3/gestalt-linux-arm64.tar.gz"
      sha256 "691730c12ea9f347ab3f6e72328a2dd0804d7f68687f6a3f3e7abb7d0f2a2726"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.3/gestalt-linux-x86_64.tar.gz"
      sha256 "0356d639ad6deaf74648b7d4b69005960ad5c3d5e474357d41d4872c88afc69f"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
