# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.53"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.53/gestaltd-macos-arm64.tar.gz"
      sha256 "904c76450e6a03b92f31a22b2bf7e3354ca4e303ce37b4193dbb882e53b95c13"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.53/gestaltd-macos-x86_64.tar.gz"
      sha256 "af6e982104e64cc7c479e4f410810e9bce9766b469a0c25c2f9e023d1713f287"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.53/gestaltd-linux-arm64.tar.gz"
      sha256 "6a10a1f6d48ac4331aa653fcd5a0b96a8c48d45fdba4b6b4e4888840d08cf3dc"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.53/gestaltd-linux-x86_64.tar.gz"
      sha256 "1109088bdb8c35504c2aa75370d066ef0b38cc8f4f70bac37b736abef7bdec94"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
