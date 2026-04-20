# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.3"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.3/gestalt-macos-arm64.tar.gz"
      sha256 "1790e54143ee709f8fff9b9e8aa7451dedbe6671af0667f9af9ca03288b4f682"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.3/gestalt-macos-x86_64.tar.gz"
      sha256 "32c6210043f5a6ba2d8a19fe696e26eb8ed4fe7d7c3c3d63211d39f4fb8179f0"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.3/gestalt-linux-arm64.tar.gz"
      sha256 "9a7c31a88e1b920c9d67615a53a765f2568ef65bb0225888a8e7f1f624cbeee1"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.3/gestalt-linux-x86_64.tar.gz"
      sha256 "360b268f5989d06d93306bb077fc14f1c5d560f42c2c56090aa51e8400ea5f01"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
