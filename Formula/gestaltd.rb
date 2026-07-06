# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.38"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-macos-arm64.tar.gz"
      sha256 "e832917ecd8222427e8e9e87b5eff134036ad0b38be7680b3cc3b40685be0113"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-macos-x86_64.tar.gz"
      sha256 "07b229e1298fe8c84f858ad43d26d6250711e5e63bc0e18239038787efe09df0"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-linux-arm64.tar.gz"
      sha256 "540290b8eef97c163fb5d6b26b6dbd426d15f9b68e29c944a28401ede1ba318b"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.38/gestaltd-linux-x86_64.tar.gz"
      sha256 "b2075810281ffe9f83c18d0c753aea19f2c0ad68e7ea106034981bbab67b543f"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
