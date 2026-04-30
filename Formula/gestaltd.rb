# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.6"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.6/gestaltd-macos-arm64.tar.gz"
      sha256 "96ba80600b9e6acf8ab3e5c87af8ee5dbd6612e7a972bbdda34e064ed78c0a26"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.6/gestaltd-macos-x86_64.tar.gz"
      sha256 "327844811ee38c6374085a3a272284555c236b15427d6f8a3a0dbe155a58bfb1"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.6/gestaltd-linux-arm64.tar.gz"
      sha256 "0972e81c60d9357a4a0b06a29389f14d5b0fe87f636413875199f359f4d02e29"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.6/gestaltd-linux-x86_64.tar.gz"
      sha256 "b2cb15ca25c5a35baf46381bcfe08524797487acb894a595e95de5b8da953631"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
