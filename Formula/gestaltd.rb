# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.33"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.33/gestaltd-macos-arm64.tar.gz"
      sha256 "73fdc98e14902371ee05d5e77f39f12f84976695ed1264944aa8a81018a07786"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.33/gestaltd-macos-x86_64.tar.gz"
      sha256 "2418f187956befd150a8804272b65d7af0756223c458193a5cc5697020d9b1a5"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.33/gestaltd-linux-arm64.tar.gz"
      sha256 "17ac1f741ac23de913f6e146c0d10179a3866eff996bd2f9b5d38b0eb2ba76dd"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.33/gestaltd-linux-x86_64.tar.gz"
      sha256 "0885cd1868e1bf9200fcaefa27c8eb6464bc70ec58c679d646a7b3ce82229a00"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
