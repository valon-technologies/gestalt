# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.38"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-macos-arm64.tar.gz"
      sha256 "9462a27889284de0c5ab08e95961eb48961284a20ab255506d901aeaf8932b1a"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-macos-x86_64.tar.gz"
      sha256 "89237980e1c064667bb43e68ad7d3af54e00e2970964bc36724d7b9f5fdfd73a"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-linux-arm64.tar.gz"
      sha256 "3baf9645455aa0a544084f811d909acd6bcad5120ebc5e8515937e8a86be28b7"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-linux-x86_64.tar.gz"
      sha256 "ffbd43a65853424f3d3efe792dcd3466a12c6a5eee705dd49aa089fc0c16651a"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
