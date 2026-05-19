# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.1/gestaltd-macos-arm64.tar.gz"
      sha256 "5b81eb615df041a5fca91872fcc7801d52062a01f1c11fe9f5810b995ff9a08f"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.1/gestaltd-macos-x86_64.tar.gz"
      sha256 "8b80dd89a4ab3ee3f65a3b268138234d846773673285942aeef137a15bbe9e67"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.1/gestaltd-linux-arm64.tar.gz"
      sha256 "0a6e95f41552e71beae8c87c68f2040c7965e468e7d0487f247059223861ed10"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.1/gestaltd-linux-x86_64.tar.gz"
      sha256 "b79a8781a0dcea459b4b594448f17be135fa8ada9d3b247fd4d347486ad3861b"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
