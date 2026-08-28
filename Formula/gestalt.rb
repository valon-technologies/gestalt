# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.15"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.15/gestalt-macos-arm64.tar.gz"
      sha256 "634c8193e5d1358d0a08333bd19cc1fca4df2da8eb40af138ae9744681ba8741"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.15/gestalt-macos-x86_64.tar.gz"
      sha256 "1257b2e020f0405591016b2b3bcaf568dec276ec38aa03f1ca0d2ecd03b922a0"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.15/gestalt-linux-arm64.tar.gz"
      sha256 "87eefaccf8d4ff25036d8a768f1cb0f4f5f4bf8a96ad6d08f80b44f79644cf0c"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.15/gestalt-linux-x86_64.tar.gz"
      sha256 "580b1c5c767d3458367fc53ade687a8113a79694ed593c9682675425d29217fa"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
