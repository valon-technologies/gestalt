# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.8"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.8/gestalt-macos-arm64.tar.gz"
      sha256 "8babebbda8a83ef942b0d8e206a7843cea948595e955ee64f606d5a2de1ccfe6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.8/gestalt-macos-x86_64.tar.gz"
      sha256 "2556ae74415e48e754448f4636a606fd19433352d889254e82664501f39f3fde"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.8/gestalt-linux-arm64.tar.gz"
      sha256 "3bfcfc9d5a571ad05f843d107d90ffaeba8763140e5ca8fc36aace807affe594"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.8/gestalt-linux-x86_64.tar.gz"
      sha256 "e2af7bdb059acd4c228ac697d62b6a7114687076217e3267cd922e980b2a85e2"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
