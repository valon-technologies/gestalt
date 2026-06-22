# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.25"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.25/gestaltd-macos-arm64.tar.gz"
      sha256 "c074031fc25a744bbd14633cb775746ea230eac6d46bf0801abe28487d94abe3"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.25/gestaltd-macos-x86_64.tar.gz"
      sha256 "7ef374e8fab29bf5747eb2954d18bab982e4a78eb6897bd7f23ec18d1f141b1a"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.25/gestaltd-linux-arm64.tar.gz"
      sha256 "b05328eb7d4a852c2909bcaf10a404004a7c7ee9f92092bd2d02fcf6f6b58806"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.25/gestaltd-linux-x86_64.tar.gz"
      sha256 "a422e0badb139e1b8d8604d68d461a0c755f95314f8d77f7c47c91262583db65"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
