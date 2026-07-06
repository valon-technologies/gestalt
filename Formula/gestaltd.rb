# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.38"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-macos-arm64.tar.gz"
      sha256 "5b0209352255747b7d1814dd5219221cb8ea841a80c395c0b7a98e6eddc608fc"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-macos-x86_64.tar.gz"
      sha256 "0d1bbad957dfdcbbdff017f577064feaaa1dceb624502a5b328a7bc9f750a846"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-linux-arm64.tar.gz"
      sha256 "5bbe946be50bf29be7ae6125357c5d24d0c88bf1da3f297dfefb9928372ded4c"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-linux-x86_64.tar.gz"
      sha256 "e6cbd1f9a22c896e050aa547aa129e25f491525ee366397fc1c102f918fcf777"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
