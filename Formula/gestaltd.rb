# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.43"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-macos-arm64.tar.gz"
      sha256 "3f5d83ebc907511ae07b8573351407f2a269c835ff14c60cf323f1dc5421dcbf"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-macos-x86_64.tar.gz"
      sha256 "9311c17d9cf264afc9d7f88eb72862f5dc1c90fafe3d6f85b208f6eada789ff6"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-linux-arm64.tar.gz"
      sha256 "8526235167ebb61d6b4608bd5b30272f1972199c9fbcc84eb66d346a768778c6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-linux-x86_64.tar.gz"
      sha256 "57405b47402ea374cc3643c70268fdf90795040bec8dc5a18aec592bd9c6349f"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
