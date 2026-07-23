# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.11"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.11/gestalt-macos-arm64.tar.gz"
      sha256 "197174c4cde4e67f2d51a52d7e8e1e0e46843e9337e7e1c95ffa3468a27e4b6d"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.11/gestalt-macos-x86_64.tar.gz"
      sha256 "527672fb31576466a0176c6c7a6dc3d1382a315789b4e447ad9634fb1087f6ec"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.11/gestalt-linux-arm64.tar.gz"
      sha256 "46cefe3e238464a68b799d9f1786dd8b94f929c30dcce0e782c724e82965548d"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.11/gestalt-linux-x86_64.tar.gz"
      sha256 "0856b823bddac49105e8f587fa7355b1f33aac60e55ab7993c70135cd3e14a3d"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
