# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.21"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.21/gestaltd-macos-arm64.tar.gz"
      sha256 "e9817510ee09c81088efbdcc712aed93d342ac07def6ab34352218e2c68ed848"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.21/gestaltd-macos-x86_64.tar.gz"
      sha256 "44e21fe03311e5dc22d142eb9544f59238abf3a3bcc980860d151e88f9e89bd1"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.21/gestaltd-linux-arm64.tar.gz"
      sha256 "bbf39e0b38b9a9773fc82aced8a7188aea3d4a96e92445f59bb969e9a496d1d0"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.21/gestaltd-linux-x86_64.tar.gz"
      sha256 "97731c6ad9bb15b85cd1f69013c454b79a84110ab6e54be263a5166c22e6d5d9"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
