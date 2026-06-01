# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.8"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.8/gestaltd-macos-arm64.tar.gz"
      sha256 "a969f24142a6266cdde28db266b6e19948f2f25be68adbbacecc28387215255f"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.8/gestaltd-macos-x86_64.tar.gz"
      sha256 "c68c4b81a6abb76335d740928819454b744879a96b1d5fd9be45ea9f9a2e4c56"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.8/gestaltd-linux-arm64.tar.gz"
      sha256 "061fd3af8135db46c305ca2889977b7a7a11d6db217ce9e4c73ad0b8994032d0"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.8/gestaltd-linux-x86_64.tar.gz"
      sha256 "d665d3257e46b7ab9d47a32f3f48014ef47d2fafb04a6821b10fac7cf7faac58"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
