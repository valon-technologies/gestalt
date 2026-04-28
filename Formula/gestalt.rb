# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.9"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.9/gestalt-macos-arm64.tar.gz"
      sha256 "45d5caf2537cb7070a7bdf5d006a854fb109e710780a0d14cee1e0ee37c41657"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.9/gestalt-macos-x86_64.tar.gz"
      sha256 "12dd75a35ec46c0bef386a37e6f988664d629d6db033ff419b79e2be24c2cb52"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.9/gestalt-linux-arm64.tar.gz"
      sha256 "c639b1642e3ecb90c64c5e102702e36a51490df9fe2929a0f377e9ebca5d15c7"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.9/gestalt-linux-x86_64.tar.gz"
      sha256 "60fb028ecf2984f0473443bc646a676d4b6a4e6c1374e1eb5d82b2084809e87e"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
