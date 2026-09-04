# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.18"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.18/gestalt-macos-arm64.tar.gz"
      sha256 "f34d2ec376a0da0398aeac13248ceccdcf015f3952dddbb31ac75ece864ab075"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.18/gestalt-macos-x86_64.tar.gz"
      sha256 "5804cc2bbcf8c15f96c88b94b88e923e5bbe12ef03ef45bd4bf4f708e5c90a0f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.18/gestalt-linux-arm64.tar.gz"
      sha256 "58c6eab5bcaa4bd5c640faa3968697ea97fd11351b57fbf49962821905764cca"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.18/gestalt-linux-x86_64.tar.gz"
      sha256 "bcb873c8496e0ad7ec723696e8ad1c59ccb1610acc8710470761b5920b974e4d"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
