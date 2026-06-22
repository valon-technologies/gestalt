# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.26"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.26/gestaltd-macos-arm64.tar.gz"
      sha256 "3e3066a00093670bcb12149efc38be3768ccffd298100e3e8487ca1b4058ae36"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.26/gestaltd-macos-x86_64.tar.gz"
      sha256 "ae9abb24c1dad910b8f716a259db857a7ce6e44475cf0f53a9c7c96ca2003b47"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.26/gestaltd-linux-arm64.tar.gz"
      sha256 "177169c32951029dbf56af171e1fdc42f52a7bdb2126a4cda6fb3d780a734b81"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.26/gestaltd-linux-x86_64.tar.gz"
      sha256 "7aa38aa65c23705bc733a9d3fde84e3bbb2b3e0019f8e9d731728b43ca2a52f6"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
