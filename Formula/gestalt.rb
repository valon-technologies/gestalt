# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.4"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.4/gestalt-macos-arm64.tar.gz"
      sha256 "4f2086b6831ea2b77705f43c24a2fa5d605c6f68fd6ec5f167da17adb9131c08"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.4/gestalt-macos-x86_64.tar.gz"
      sha256 "17f26322836bec544906081a7aaa96dc343e55181525bb610285834ce9b8acff"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.4/gestalt-linux-arm64.tar.gz"
      sha256 "a43386f48fe47096b5bae00ed9513e08b3a0f0fcfa3a3d46fd153f69f3c2c7e4"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.4/gestalt-linux-x86_64.tar.gz"
      sha256 "12d205579df8b1614fba03d3d5959439163931c0b236942daf3f7381ea5e523f"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
