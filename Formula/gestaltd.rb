# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.54"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.54/gestaltd-macos-arm64.tar.gz"
      sha256 "603d1ddfbfe64dd6fb5f4a84136b40bee0879385b93cbc8b8ed5426b553e1cf2"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.54/gestaltd-macos-x86_64.tar.gz"
      sha256 "b697713c5c46cb724ca235b45a02ee5b175c862aa433207a8b85d5a5686e87c9"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.54/gestaltd-linux-arm64.tar.gz"
      sha256 "2d6e9d55c31a3ad7c25bcac2cd6a61ba09364514bc7514b6e10d4c8e071af2e4"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.54/gestaltd-linux-x86_64.tar.gz"
      sha256 "1461d365acd906f18c133cdda63802424cd0b1a1173c20d66bedbf61d96a8ca6"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
