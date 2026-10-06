# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.59"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.59/gestaltd-macos-arm64.tar.gz"
      sha256 "5f6764afd03a1870f4bf81eed9a210c1175d11ba2f2a4c27586ca346e85304f7"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.59/gestaltd-macos-x86_64.tar.gz"
      sha256 "9728df700b6964e1ab90136ae72d3f81ed2150c76ac9578d6f55cc70c2a1bf97"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.59/gestaltd-linux-arm64.tar.gz"
      sha256 "6795507df5bd44c35beb5172959dc4c997c4592e97b80d4cbc72acb0c49e52aa"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.59/gestaltd-linux-x86_64.tar.gz"
      sha256 "2297e9d5d2e9893ff6255b1eea4af08c9cddcf31cb8b3ae311fb7012ac1b8fad"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
