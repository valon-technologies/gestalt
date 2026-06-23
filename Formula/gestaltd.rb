# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.29"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.29/gestaltd-macos-arm64.tar.gz"
      sha256 "a8118f42e23544cf265617686eccc61ce8c684729de7a4d1cd5ef493af26e112"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.29/gestaltd-macos-x86_64.tar.gz"
      sha256 "ad24dc6d0c4e2bbc6893beaa8c5cfe7093da121bc95e4c6f122583845948ff10"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.29/gestaltd-linux-arm64.tar.gz"
      sha256 "1e67a8471da4713f61fce82d9740655d7aea2514e1721f3364973f9501ba0264"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.29/gestaltd-linux-x86_64.tar.gz"
      sha256 "0c130bc54b32e9bf8156dccf43f7e4de0b3dccd2310091176ad25386061f008d"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
