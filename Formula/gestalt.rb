# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.7"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.7/gestalt-macos-arm64.tar.gz"
      sha256 "9b58e3ed2686c250959d9ab40a0c405e551f8a1c510218c09bf629392923b0aa"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.7/gestalt-macos-x86_64.tar.gz"
      sha256 "9ee81e9694ea6e9faee16461df74420590b1bd2238b80bdd7f634649f5d77522"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.7/gestalt-linux-arm64.tar.gz"
      sha256 "dae74f6f0fe3ecf633d25767cd467feee5c4dd67d28b74016b1af5e99ca608c0"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.7/gestalt-linux-x86_64.tar.gz"
      sha256 "1a906b0afc39e37a5614702d88facca0c4fc35325a659fb299dd1db8991aa6e2"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
