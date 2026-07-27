# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.47"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.47/gestaltd-macos-arm64.tar.gz"
      sha256 "a022d29a8102ffb0e4fb93effb0b6e980a9d7bb751c505813f2f3d229791a7b0"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.47/gestaltd-macos-x86_64.tar.gz"
      sha256 "81f6d865b8d7af8d03011c5d3b37fda44e0b5781a620cf4cf691b8ae3992ed28"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.47/gestaltd-linux-arm64.tar.gz"
      sha256 "715a0a656d1241023ac9ded1360e50a3e3bc37f6ef124c70a2bab25aec57fdf4"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.47/gestaltd-linux-x86_64.tar.gz"
      sha256 "25cd290fe05c31e647f680c01b47605d11ff9551557041a91b524982baf1cc8e"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
